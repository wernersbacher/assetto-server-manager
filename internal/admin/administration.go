package admin

import (
	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	"github.com/wernersbacher/ACPitwall/internal/api"
	smconfig "github.com/wernersbacher/ACPitwall/internal/config"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smresults "github.com/wernersbacher/ACPitwall/internal/results"
	"github.com/wernersbacher/ACPitwall/internal/schedule"
	smserver "github.com/wernersbacher/ACPitwall/internal/server"
	"github.com/wernersbacher/ACPitwall/internal/serverenv"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"

	"encoding/json"
	"html/template"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi"
	"github.com/mitchellh/go-wordwrap"
	"github.com/sirupsen/logrus"
)

type ServerAdministrationHandler struct {
	*smweb.BaseHandler

	config         *smconfig.Configuration
	slotFor        SlotFor
	slotsFunc      SlotsFunc
	portWarnings   smweb.PortWarnings
	scheduledRaces *schedule.Manager

	managerLog, pluginsLog *smserver.LogBuffer
	changelogHTML          func() template.HTML
	setFallBackSorting     func(bool)
}

// Logs is what the administration needs from the process wide state which is not part of the serverenv.Env.
type Logs struct {
	// Manager and Plugins buffer the output of ACPitwall and of the plugins.
	Manager, Plugins *smserver.LogBuffer
}

func NewServerAdministrationHandler(
	config *smconfig.Configuration,
	baseHandler *smweb.BaseHandler,
	slotFor SlotFor,
	slotsFunc SlotsFunc,
	portWarnings smweb.PortWarnings,
	scheduledRaces *schedule.Manager,
	logs Logs,
	changelogHTML func() template.HTML,
	setFallBackSorting func(bool),
) *ServerAdministrationHandler {
	return &ServerAdministrationHandler{
		config:             config,
		BaseHandler:        baseHandler,
		slotFor:            slotFor,
		slotsFunc:          slotsFunc,
		portWarnings:       portWarnings,
		scheduledRaces:     scheduledRaces,
		managerLog:         logs.Manager,
		pluginsLog:         logs.Plugins,
		changelogHTML:      changelogHTML,
		setFallBackSorting: setFallBackSorting,
	}
}

const (
	homeUpcomingRaces = 5
	homeRecentResults = 5
)

type homeTemplateVars struct {
	smweb.BaseTemplateVars

	RaceDetails     *smmodel.CustomRace
	PerformanceMode bool
	UpcomingRaces   []*api.UpcomingRace
	RecentResults   []*smresults.SessionResults

	// ServerOverview summarises all servers, it is only set with more than one server.
	ServerOverview []*ServerSummary
}

// homeHandler serves content to /
func (sah *ServerAdministrationHandler) Home(w http.ResponseWriter, r *http.Request) {
	slot, ok := sah.slot(w, r)

	if !ok {
		return
	}

	currentRace, entryList := slot.RaceManager.CurrentRace()

	var customRace *smmodel.CustomRace

	if currentRace != nil {
		customRace = &smmodel.CustomRace{EntryList: entryList, RaceConfig: currentRace.CurrentRaceConfig}
	}

	// the overview still renders if the calendar or the results can't be loaded, they are just left out
	upcoming, err := sah.scheduledRaces.UpcomingRaces(time.Now(), homeUpcomingRaces, "", smweb.CanSeeHidden(r))

	if err != nil {
		logrus.WithError(err).Error("couldn't load upcoming races for the overview")
	}

	recentResults, err := slot.Env.Results().ListRecent(homeRecentResults)

	if err != nil {
		logrus.WithError(err).Error("couldn't load recent results for the overview")
	}

	var overview []*ServerSummary

	if len(sah.slots()) > 1 {
		overview = sah.serverSummaries()
	}

	sah.ViewRenderer.MustLoadTemplate(w, r, "home.html", &homeTemplateVars{
		ServerOverview:  overview,
		RaceDetails:     customRace,
		PerformanceMode: sah.config.Server.PerformanceMode,
		UpcomingRaces:   upcoming,
		RecentResults:   recentResults,
	})
}

const MOTDFilename = "motd.txt"

type motdTemplateVars struct {
	smweb.BaseTemplateVars

	MOTDText string
	Opts     *serverconfig.GlobalServerConfig
}

func (sah *ServerAdministrationHandler) MOTD(w http.ResponseWriter, r *http.Request) {
	slot, ok := sah.slot(w, r)

	if !ok {
		return
	}

	opts, err := slot.Store.LoadServerOptions()

	if err != nil {
		logrus.WithError(err).Error("couldn't load server options")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if r.Method == http.MethodPost {
		wrapped := wordwrap.WrapString(r.FormValue("motd"), 140)

		success := true

		err := ioutil.WriteFile(filepath.Join(slot.Env.InstallPath(), MOTDFilename), []byte(wrapped), 0644)

		if err != nil {
			logrus.WithError(err).Error("couldn't save message of the day")
			smweb.AddErrorFlash(w, r, "Failed to save message changes")
			success = false
		}

		opts.ServerJoinMessage = r.FormValue("serverJoinMessage")
		opts.ContentManagerWelcomeMessage = r.FormValue("contentManagerWelcomeMessage")

		if err := slot.Store.UpsertServerOptions(opts); err != nil {
			logrus.WithError(err).Error("couldn't save messages")
			smweb.AddErrorFlash(w, r, "Failed to save message changes")
			success = false
		}

		if success {
			smweb.AddFlash(w, r, "Messages successfully saved!")
		}
	}

	b, err := ioutil.ReadFile(filepath.Join(slot.Env.InstallPath(), MOTDFilename))

	if err != nil && !os.IsNotExist(err) {
		logrus.WithError(err).Error("couldn't find motd.txt")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	sah.ViewRenderer.MustLoadTemplate(w, r, "server/motd.html", &motdTemplateVars{
		MOTDText: string(b),
		Opts:     opts,
	})
}

type currentCFGTemplateVars struct {
	smweb.BaseTemplateVars

	ConfigText    string
	EntryListText string

	// NotWritten is true if the server has not written its configuration yet (no event was started).
	NotWritten bool
	// ReadError is set if the configuration exists but could not be read.
	ReadError string
}

// readCurrentConfig reads the server_cfg.ini and entry_list.ini of the install path. Missing files are not an
// error: the server writes them when the first event is started.
func readCurrentConfig(installPath string) *currentCFGTemplateVars {
	vars := &currentCFGTemplateVars{}
	dir := filepath.Join(installPath, serverenv.ConfigPath)

	configText, err := (&serverconfig.ServerConfig{}).ReadString(dir)
	configMissing := os.IsNotExist(err)

	if err != nil && !configMissing {
		logrus.WithError(err).Error("Couldn't load server config")
		vars.ReadError = "The server config could not be read: " + err.Error()
	}

	entryListText, err := (&serverconfig.EntryList{}).ReadString(dir)

	entryMissing := os.IsNotExist(err)

	if err != nil && !entryMissing {
		logrus.WithError(err).Error("Couldn't load entry list")
		vars.ReadError = "The entry list could not be read: " + err.Error()
	}

	vars.NotWritten = configMissing && entryMissing

	vars.ConfigText = configText
	vars.EntryListText = entryListText

	return vars
}

func (sah *ServerAdministrationHandler) CurrentConfig(w http.ResponseWriter, r *http.Request) {
	slot, ok := sah.slot(w, r)

	if !ok {
		return
	}

	sah.ViewRenderer.MustLoadTemplate(w, r, "server/current-config.html", readCurrentConfig(slot.Env.InstallPath()))
}

type serverOptionsTemplateVars struct {
	smweb.BaseTemplateVars

	Form template.HTML

	// PortWarnings are the ports this server shares with other servers.
	PortWarnings []string
}

func (sah *ServerAdministrationHandler) Options(w http.ResponseWriter, r *http.Request) {
	slot, ok := sah.slot(w, r)

	if !ok {
		return
	}

	serverOpts, err := slot.RaceManager.LoadServerOptions()

	if err != nil {
		logrus.WithError(err).Errorf("couldn't load server options")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if r.Method == http.MethodPost {
		err := smweb.DecodeFormData(serverOpts, r)

		if err != nil {
			logrus.WithError(err).Errorf("couldn't submit form")
		}

		smresults.UseShortenedDriverNames = serverOpts.UseShortenedDriverNames == 1
		sah.setFallBackSorting(serverOpts.FallBackResultsSorting == 1)

		// save the config
		err = slot.RaceManager.SaveServerOptions(serverOpts)

		if err != nil {
			logrus.WithError(err).Errorf("couldn't save config")
			smweb.AddErrorFlash(w, r, "Failed to save server options")
		} else {
			smweb.AddFlash(w, r, "Server options successfully saved!")
		}

	}

	form, err := smweb.EncodeFormData(serverOpts, r)

	if err != nil {
		logrus.WithError(err).Errorf("Couldn't encode form data")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	sah.ViewRenderer.MustLoadTemplate(w, r, "server/options.html", &serverOptionsTemplateVars{
		Form:         form,
		PortWarnings: sah.portWarnings.For(slot.Env.ServerID()),
	})
}

type serverBlacklistTemplateVars struct {
	smweb.BaseTemplateVars

	Text string
}

func (sah *ServerAdministrationHandler) Blacklist(w http.ResponseWriter, r *http.Request) {
	slot, ok := sah.slot(w, r)

	if !ok {
		return
	}

	if r.Method == http.MethodPost {
		// save to blacklist.txt
		var text string

		if r.FormValue("type") == "single" {
			// we're adding a single GUID, load the existing blacklist list then append
			b, err := ioutil.ReadFile(filepath.Join(slot.Env.InstallPath(), "blacklist.txt"))
			if err != nil {
				logrus.WithError(err).Error("couldn't find blacklist.txt")
			}

			text = string(b) + r.FormValue("blacklist")
		} else {
			text = r.FormValue("blacklist")
		}

		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}

		err := ioutil.WriteFile(filepath.Join(slot.Env.InstallPath(), "blacklist.txt"), []byte(text), 0644)

		if err != nil {
			logrus.WithError(err).Error("couldn't save blacklist")
			smweb.AddErrorFlash(w, r, "Failed to save Server blacklist changes")
		} else {
			smweb.AddFlash(w, r, "Server blacklist successfully changed!")
		}
	}

	// load blacklist.txt
	b, err := ioutil.ReadFile(filepath.Join(slot.Env.InstallPath(), "blacklist.txt")) // just pass the file name
	if err != nil {
		logrus.WithError(err).Error("couldn't find blacklist.txt")
	}

	// render blacklist edit page
	sah.ViewRenderer.MustLoadTemplate(w, r, "server/blacklist.html", &serverBlacklistTemplateVars{
		Text: string(b),
	})
}

type autoFillEntrantListTemplateVars struct {
	smweb.BaseTemplateVars

	Entrants []*serverconfig.Entrant
}

func (sah *ServerAdministrationHandler) AutoFillEntrantList(w http.ResponseWriter, r *http.Request) {
	slot, ok := sah.slot(w, r)

	if !ok {
		return
	}

	entrants, err := slot.RaceManager.ListAutoFillEntrants()

	if err != nil {
		logrus.WithError(err).Error("could not list entrants")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	sah.ViewRenderer.MustLoadTemplate(w, r, "server/autofill-entrants.html", &autoFillEntrantListTemplateVars{
		Entrants: entrants,
	})
}

func (sah *ServerAdministrationHandler) AutoFillEntrantDelete(w http.ResponseWriter, r *http.Request) {
	slot, ok := sah.slot(w, r)

	if !ok {
		return
	}

	err := slot.Store.DeleteEntrant(chi.URLParam(r, "entrantID"))

	if err != nil {
		logrus.WithError(err).Error("could not delete entrant")
		smweb.AddErrorFlash(w, r, "Could not delete entrant")
	} else {
		smweb.AddFlash(w, r, "Successfully deleted entrant")
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (sah *ServerAdministrationHandler) Logs(w http.ResponseWriter, r *http.Request) {
	sah.ViewRenderer.MustLoadTemplate(w, r, "server/logs.html", &smweb.BaseTemplateVars{
		WideContainer: true,
	})
}

type logData struct {
	ServerLog, ManagerLog, PluginsLog string
}

func (sah *ServerAdministrationHandler) LogsAPI(w http.ResponseWriter, r *http.Request) {
	slot, ok := sah.slot(w, r)

	if !ok {
		return
	}

	w.Header().Set("Content-Type", "application/json")

	_ = json.NewEncoder(w).Encode(logData{
		ServerLog:  slot.Process.Logs(),
		ManagerLog: sah.managerLog.String(),
		PluginsLog: sah.pluginsLog.String(),
	})
}

// downloading logfiles
func (sah *ServerAdministrationHandler) LogsDownload(w http.ResponseWriter, r *http.Request) {
	slot, ok := sah.slot(w, r)

	if !ok {
		return
	}

	logFile := chi.URLParam(r, "logFile")
	var outputString string

	if logFile == "server" {
		outputString = slot.Process.Logs()
	} else if logFile == "manager" {
		outputString = sah.managerLog.String()
	} else if logFile == "plugins" {
		outputString = sah.pluginsLog.String()
	} else {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	// tell the browser this is a file download
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename= \""+logFile+"_"+time.Now().Format(time.RFC3339)+".log\"")

	_, err := w.Write([]byte(outputString))

	if err != nil {
		logrus.WithError(err).Error("failed to return log " + logFile + " as file via http")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

// serverProcessHandler modifies the server process.
func (sah *ServerAdministrationHandler) ServerProcess(w http.ResponseWriter, r *http.Request) {
	slot, ok := sah.slot(w, r)

	if !ok {
		return
	}

	var err error
	var txt string

	event := slot.Process.Event()

	switch chi.URLParam(r, "action") {
	case "stop":
		if event.IsChampionship() && !event.IsPractice() {
			err = slot.ChampionshipManager.StopActiveEvent()
		} else if event.IsRaceWeekend() && !event.IsPractice() {
			err = slot.RaceWeekendManager.StopActiveSession()
		} else {
			err = slot.Process.Stop()
		}
		txt = "stopped"
	case "restart":
		if event.IsChampionship() && !event.IsPractice() {
			err = slot.ChampionshipManager.RestartActiveEvent()
		} else if event.IsRaceWeekend() && !event.IsPractice() {
			err = slot.RaceWeekendManager.RestartActiveSession()
		} else {
			err = slot.Process.Restart()
		}
		txt = "restarted"
	}

	noun := "Server"

	if event.IsChampionship() {
		noun = "Championship"
	} else if event.IsRaceWeekend() {
		noun = "Race Weekend"
	}

	if event.IsPractice() {
		noun += " Practice"
	}

	if err != nil {
		logrus.WithError(err).Errorf("could not change " + noun + " status")
		smweb.AddErrorFlash(w, r, "Unable to change "+noun+" status")
	} else {
		smweb.AddFlash(w, r, noun+" successfully "+txt)
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

type changelogTemplateVars struct {
	smweb.BaseTemplateVars

	Changelog template.HTML
}

func (sah *ServerAdministrationHandler) Changelog(w http.ResponseWriter, r *http.Request) {
	sah.ViewRenderer.MustLoadTemplate(w, r, "changelog.html", &changelogTemplateVars{
		Changelog: sah.changelogHTML(),
	})
}

func (sah *ServerAdministrationHandler) Robots(w http.ResponseWriter, r *http.Request) {
	slot, ok := sah.slot(w, r)

	if !ok {
		return
	}

	// do we want to let robots on the internet know things about us?!?
	serverOpts, err := slot.Store.LoadServerOptions()

	if err != nil {
		logrus.WithError(err).Errorf("couldn't load server options")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	var response string

	w.Header().Set("Content-Type", "text/plain")

	if serverOpts.PreventWebCrawlers == 1 {
		response = "User-agent: *\nDisallow: /"
	} else {
		response = "User-agent: *\nDisallow:"
	}

	_, err = w.Write([]byte(response))

	if err != nil {
		logrus.WithError(err).Errorf("couldn't write response text")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
}

// NotFound renders the styled 404 page (status 404) for page navigations; see smweb.NotFoundPageMiddleware.
func (sah *ServerAdministrationHandler) NotFound(w http.ResponseWriter, r *http.Request) {
	// the status is sent with the first body write, so that the renderer can still set cookies (flash messages)
	sah.ViewRenderer.MustLoadTemplate(&notFoundStatusWriter{ResponseWriter: w}, r, "404.html", &smweb.BaseTemplateVars{})
}

type notFoundStatusWriter struct {
	http.ResponseWriter

	wroteHeader bool
}

func (w *notFoundStatusWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.wroteHeader = true
		w.ResponseWriter.WriteHeader(http.StatusNotFound)
	}

	return w.ResponseWriter.Write(b)
}
