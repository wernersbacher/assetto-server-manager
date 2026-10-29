package admin

import (
	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	"github.com/wernersbacher/assetto-server-manager/internal/api"
	smchampionship "github.com/wernersbacher/assetto-server-manager/internal/championship"
	smconfig "github.com/wernersbacher/assetto-server-manager/internal/config"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	smrace "github.com/wernersbacher/assetto-server-manager/internal/race"
	smraceweekend "github.com/wernersbacher/assetto-server-manager/internal/raceweekend"
	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"
	"github.com/wernersbacher/assetto-server-manager/internal/schedule"
	smserver "github.com/wernersbacher/assetto-server-manager/internal/server"
	"github.com/wernersbacher/assetto-server-manager/internal/serverenv"
	smstore "github.com/wernersbacher/assetto-server-manager/internal/store"
	smweb "github.com/wernersbacher/assetto-server-manager/internal/web"

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

	config              *smconfig.Configuration
	store               smstore.Store
	raceManager         *smrace.RaceManager
	championshipManager *smchampionship.ChampionshipManager
	raceWeekendManager  *smraceweekend.RaceWeekendManager
	process             smserver.ServerProcess
	scheduledRaces      *schedule.Manager
	env                 *serverenv.Env

	managerLog, pluginsLog *smserver.LogBuffer
	changelogHTML          func() template.HTML
	setFallBackSorting     func(bool)
}

// Logs is what the administration needs from the process wide state which is not part of the serverenv.Env.
type Logs struct {
	// Manager and Plugins buffer the output of the server manager and of the plugins.
	Manager, Plugins *smserver.LogBuffer
}

func NewServerAdministrationHandler(
	config *smconfig.Configuration,
	baseHandler *smweb.BaseHandler,
	store smstore.Store,
	raceManager *smrace.RaceManager,
	championshipManager *smchampionship.ChampionshipManager,
	raceWeekendManager *smraceweekend.RaceWeekendManager,
	process smserver.ServerProcess,
	scheduledRaces *schedule.Manager,
	env *serverenv.Env,
	logs Logs,
	changelogHTML func() template.HTML,
	setFallBackSorting func(bool),
) *ServerAdministrationHandler {
	return &ServerAdministrationHandler{
		config:              config,
		BaseHandler:         baseHandler,
		store:               store,
		raceManager:         raceManager,
		championshipManager: championshipManager,
		raceWeekendManager:  raceWeekendManager,
		process:             process,
		scheduledRaces:      scheduledRaces,
		env:                 env,
		managerLog:          logs.Manager,
		pluginsLog:          logs.Plugins,
		changelogHTML:       changelogHTML,
		setFallBackSorting:  setFallBackSorting,
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
}

// homeHandler serves content to /
func (sah *ServerAdministrationHandler) Home(w http.ResponseWriter, r *http.Request) {
	currentRace, entryList := sah.raceManager.CurrentRace()

	var customRace *smmodel.CustomRace

	if currentRace != nil {
		customRace = &smmodel.CustomRace{EntryList: entryList, RaceConfig: currentRace.CurrentRaceConfig}
	}

	// the overview still renders if the calendar or the results can't be loaded, they are just left out
	upcoming, err := sah.scheduledRaces.UpcomingRaces(time.Now(), homeUpcomingRaces)

	if err != nil {
		logrus.WithError(err).Error("couldn't load upcoming races for the overview")
	}

	recentResults, err := sah.env.Results().ListRecent(homeRecentResults)

	if err != nil {
		logrus.WithError(err).Error("couldn't load recent results for the overview")
	}

	sah.ViewRenderer.MustLoadTemplate(w, r, "home.html", &homeTemplateVars{
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
	opts, err := sah.store.LoadServerOptions()

	if err != nil {
		logrus.WithError(err).Error("couldn't load server options")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if r.Method == http.MethodPost {
		wrapped := wordwrap.WrapString(r.FormValue("motd"), 140)

		success := true

		err := ioutil.WriteFile(filepath.Join(sah.env.InstallPath(), MOTDFilename), []byte(wrapped), 0644)

		if err != nil {
			logrus.WithError(err).Error("couldn't save message of the day")
			smweb.AddErrorFlash(w, r, "Failed to save message changes")
			success = false
		}

		opts.ServerJoinMessage = r.FormValue("serverJoinMessage")
		opts.ContentManagerWelcomeMessage = r.FormValue("contentManagerWelcomeMessage")

		if err := sah.store.UpsertServerOptions(opts); err != nil {
			logrus.WithError(err).Error("couldn't save messages")
			smweb.AddErrorFlash(w, r, "Failed to save message changes")
			success = false
		}

		if success {
			smweb.AddFlash(w, r, "Messages successfully saved!")
		}
	}

	b, err := ioutil.ReadFile(filepath.Join(sah.env.InstallPath(), MOTDFilename))

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
}

func (sah *ServerAdministrationHandler) CurrentConfig(w http.ResponseWriter, r *http.Request) {
	config := &serverconfig.ServerConfig{}
	entryList := &serverconfig.EntryList{}

	configText, err := config.ReadString(filepath.Join(sah.env.InstallPath(), serverenv.ConfigPath))

	if err != nil {
		logrus.WithError(err).Error("Couldn't load server config")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	entryListText, err := entryList.ReadString(filepath.Join(sah.env.InstallPath(), serverenv.ConfigPath))

	if err != nil {
		logrus.WithError(err).Error("Couldn't load entry list")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	sah.ViewRenderer.MustLoadTemplate(w, r, "server/current-config.html", &currentCFGTemplateVars{
		ConfigText:    configText,
		EntryListText: entryListText,
	})
}

type serverOptionsTemplateVars struct {
	smweb.BaseTemplateVars

	Form template.HTML
}

func (sah *ServerAdministrationHandler) Options(w http.ResponseWriter, r *http.Request) {
	serverOpts, err := sah.raceManager.LoadServerOptions()

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
		err = sah.raceManager.SaveServerOptions(serverOpts)

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
		Form: form,
	})
}

type serverBlacklistTemplateVars struct {
	smweb.BaseTemplateVars

	Text string
}

func (sah *ServerAdministrationHandler) Blacklist(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		// save to blacklist.txt
		var text string

		if r.FormValue("type") == "single" {
			// we're adding a single GUID, load the existing blacklist list then append
			b, err := ioutil.ReadFile(filepath.Join(sah.env.InstallPath(), "blacklist.txt"))
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

		err := ioutil.WriteFile(filepath.Join(sah.env.InstallPath(), "blacklist.txt"), []byte(text), 0644)

		if err != nil {
			logrus.WithError(err).Error("couldn't save blacklist")
			smweb.AddErrorFlash(w, r, "Failed to save Server blacklist changes")
		} else {
			smweb.AddFlash(w, r, "Server blacklist successfully changed!")
		}
	}

	// load blacklist.txt
	b, err := ioutil.ReadFile(filepath.Join(sah.env.InstallPath(), "blacklist.txt")) // just pass the file name
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
	entrants, err := sah.raceManager.ListAutoFillEntrants()

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
	err := sah.store.DeleteEntrant(chi.URLParam(r, "entrantID"))

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
	w.Header().Set("Content-Type", "application/json")

	_ = json.NewEncoder(w).Encode(logData{
		ServerLog:  sah.process.Logs(),
		ManagerLog: sah.managerLog.String(),
		PluginsLog: sah.pluginsLog.String(),
	})
}

// downloading logfiles
func (sah *ServerAdministrationHandler) LogsDownload(w http.ResponseWriter, r *http.Request) {
	logFile := chi.URLParam(r, "logFile")
	var outputString string

	if logFile == "server" {
		outputString = sah.process.Logs()
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
	var err error
	var txt string

	event := sah.process.Event()

	switch chi.URLParam(r, "action") {
	case "stop":
		if event.IsChampionship() && !event.IsPractice() {
			err = sah.championshipManager.StopActiveEvent()
		} else if event.IsRaceWeekend() && !event.IsPractice() {
			err = sah.raceWeekendManager.StopActiveSession()
		} else {
			err = sah.process.Stop()
		}
		txt = "stopped"
	case "restart":
		if event.IsChampionship() && !event.IsPractice() {
			err = sah.championshipManager.RestartActiveEvent()
		} else if event.IsRaceWeekend() && !event.IsPractice() {
			err = sah.raceWeekendManager.RestartActiveSession()
		} else {
			err = sah.process.Restart()
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
	// do we want to let robots on the internet know things about us?!?
	serverOpts, err := sah.store.LoadServerOptions()

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
