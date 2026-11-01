package championship

import (
	smcontent "github.com/wernersbacher/ACPitwall/internal/content"
	"github.com/wernersbacher/ACPitwall/internal/i18n"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smrace "github.com/wernersbacher/ACPitwall/internal/race"
	smresults "github.com/wernersbacher/ACPitwall/internal/results"
	"github.com/wernersbacher/ACPitwall/internal/steam"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"

	smconfig "github.com/wernersbacher/ACPitwall/internal/config"

	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"4d63.com/tz"
	"github.com/go-chi/chi"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

type ChampionshipsHandler struct {
	*smweb.BaseHandler
	steam.LoginHandler

	config     *smconfig.Configuration
	managerFor ManagerFor
	active     ActiveEvents
}

func NewChampionshipsHandler(config *smconfig.Configuration, baseHandler *smweb.BaseHandler, managerFor ManagerFor, active ActiveEvents) *ChampionshipsHandler {
	return &ChampionshipsHandler{
		config:      config,
		BaseHandler: baseHandler,
		managerFor:  managerFor,
		active:      active,
	}
}

type championshipListTemplateVars struct {
	smweb.BaseTemplateVars

	ActiveChampionships, CompletedChampionships []*smmodel.Championship
}

// lists all available Championships known to ACPitwall
func (ch *ChampionshipsHandler) List(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	championships, err := cm.ListChampionships()

	if err != nil {
		logrus.WithError(err).Errorf("couldn't list championships")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	var activeChampionships, completedChampionships []*smmodel.Championship

	for _, championship := range championships {
		if championship.Hidden && !smweb.CanSeeHidden(r) {
			continue
		}

		if championship.Progress() == 100 {
			completedChampionships = append(completedChampionships, championship)
		} else {
			activeChampionships = append(activeChampionships, championship)
		}
	}

	ch.ViewRenderer.MustLoadTemplate(w, r, "championships/index.html", &championshipListTemplateVars{
		ActiveChampionships:    activeChampionships,
		CompletedChampionships: completedChampionships,
	})
}

// createOrEdit builds a Championship form for the user to create a Championship.
func (ch *ChampionshipsHandler) CreateOrEdit(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	_, opts, err := cm.BuildChampionshipOpts(r)

	if err != nil {
		logrus.WithError(err).Errorf("couldn't build championship form")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	ch.ViewRenderer.MustLoadTemplate(w, r, "championships/new.html", opts)
}

// submit creates a given Championship and redirects the user to begin
// the flow of adding events to the new Championship
func (ch *ChampionshipsHandler) Submit(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	championship, edited, err := cm.HandleCreateChampionship(r)

	if _, ok := err.(ValidationError); ok {
		logrus.WithError(err).Warn("invalid championship form")
		http.Error(w, smweb.ErrorText(r, err), http.StatusBadRequest)
		return
	} else if err != nil {
		logrus.WithError(err).Errorf("couldn't create championship")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if edited {
		smweb.AddFlash(w, r, smweb.T(r, "flash.championship.edited"))
		http.Redirect(w, r, "/championship/"+championship.ID.String(), http.StatusFound)
	} else {

		smweb.AddFlash(w, r, smweb.T(r, "flash.championship.created"))

		if r.FormValue("action") == "addRaceWeekend" {
			http.Redirect(w, r, "/race-weekends/new?championshipID="+championship.ID.String(), http.StatusFound)
		} else {
			http.Redirect(w, r, "/championship/"+championship.ID.String()+"/event", http.StatusFound)
		}
	}
}

// duplicate duplicates a championship, clearing results
func (ch *ChampionshipsHandler) Duplicate(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	championship, err := cm.DuplicateChampionship(chi.URLParam(r, "championshipID"))

	if err != nil {
		logrus.WithError(err).Errorf("couldn't duplicate championship (id: %s)", chi.URLParam(r, "championshipID"))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	smweb.AddFlash(w, r, smweb.T(r, "flash.championship.duplicated"))
	http.Redirect(w, r, "/championship/"+championship.ID.String(), http.StatusFound)
}

type championshipViewTemplateVars struct {
	smweb.BaseTemplateVars

	Championship    *smmodel.Championship
	EventInProgress bool
	Account         *smmodel.Account
	RaceWeekends    map[uuid.UUID]*smmodel.RaceWeekend

	// ActiveServer is the other server which runs the championship, if there is one.
	ActiveServer *smweb.ServerRef
}

// view shows details of a given Championship
func (ch *ChampionshipsHandler) View(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	championship, err := cm.LoadChampionship(chi.URLParam(r, "championshipID"))

	if err != nil {
		logrus.WithError(err).Errorf("couldn't load championship")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if ch.denyHidden(w, r, championship) {
		return
	}

	eventInProgress := false

	for _, event := range championship.Events {
		if event.InProgress() && cm.activeChampionship != nil {
			eventInProgress = true
			break
		}
	}

	var activeServer *smweb.ServerRef

	var elsewhere ActiveElsewhereError

	if errors.As(cm.CheckNotActiveElsewhere(chi.URLParam(r, "championshipID"), ch.active), &elsewhere) {
		activeServer = &elsewhere.Server
		eventInProgress = true
	}

	raceWeekends := make(map[uuid.UUID]*smmodel.RaceWeekend)

	for _, event := range championship.Events {
		if event.IsRaceWeekend() {
			raceWeekends[event.RaceWeekendID], err = cm.store.LoadRaceWeekend(event.RaceWeekendID.String())

			if err != nil {
				logrus.WithError(err).Errorf("couldn't load championship")
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
		}
	}

	account := smweb.AccountFromRequest(r)
	ch.ViewRenderer.MustLoadTemplate(w, r, "championships/view.html", &championshipViewTemplateVars{
		Championship:    championship,
		EventInProgress: eventInProgress,
		Account:         account,
		RaceWeekends:    raceWeekends,
		ActiveServer:    activeServer,
	})
}

// export returns all known data about a Championship in JSON format.
func (ch *ChampionshipsHandler) Export(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	championship, err := cm.LoadChampionship(chi.URLParam(r, "championshipID"))

	if err != nil {
		logrus.WithError(err).Errorf("couldn't export championship")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if ch.denyHidden(w, r, championship) {
		return
	}

	account := smweb.AccountFromRequest(r)

	if !account.HasGroupPrivilege(cm.env.ServerID(), smmodel.GroupAdmin) {
		// sign up responses are hidden for data protection reasons
		championship.SignUpForm.Responses = nil
	}

	if !account.HasGroupPrivilege(cm.env.ServerID(), smmodel.GroupWrite) {
		// if you don't have write access or above you can't see the replacement password
		championship.ReplacementPassword = ""
		championship.OverridePassword = false
	}

	w.Header().Add("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.json"`, championship.Name))

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(championship)
}

// importChampionship reads Championship data from JSON.
func (ch *ChampionshipsHandler) ImportChampionship(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	if r.Method == http.MethodPost {
		championshipID, err := cm.ImportChampionship(r.FormValue("import"))

		if err != nil {
			logrus.WithError(err).Errorf("couldn't import championship")
			smweb.AddErrorFlash(w, r, smweb.T(r, "flash.championship.import_failed"))
		} else {
			smweb.AddFlash(w, r, smweb.T(r, "flash.championship.imported"))
			http.Redirect(w, r, "/championship/"+championshipID, http.StatusFound)
		}
	}

	ch.ViewRenderer.MustLoadTemplate(w, r, "championships/import-championship.html", nil)
}

type championshipResultsCollection struct {
	Name    string                `json:"name"`
	Results []championshipResults `json:"results"`
}

type championshipResults struct {
	Name string   `json:"name"`
	Log  []string `json:"log"`
}

// exportResults returns championship result files in JSON format.
func (ch *ChampionshipsHandler) ExportResults(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	championship, err := cm.LoadChampionship(chi.URLParam(r, "championshipID"))

	if err != nil {
		logrus.WithError(err).Errorf("couldn't export championship")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if ch.denyHidden(w, r, championship) {
		return
	}

	var results []championshipResults

	for _, event := range championship.Events {

		if !event.Completed() {
			continue
		}

		var sessionFiles []string

		for _, session := range event.Sessions {
			if session.Results == nil {
				continue
			}

			sessionFiles = append(sessionFiles, session.Results.GetURL(ch.config.HTTP.BaseURL))
		}

		results = append(results, championshipResults{
			Name: "Event at " + smcontent.PrettifyName(event.RaceSetup.Track, false) + ", completed on " + event.CompletedTime.Format("Monday, January 2, 2006 3:04 PM (MST)"),
			Log:  sessionFiles,
		})
	}

	champResultsCollection := championshipResultsCollection{
		Name:    championship.Name,
		Results: results,
	}

	w.Header().Add("Content-Type", "application/json")

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(champResultsCollection)
}

// delete soft deletes a Championship.
func (ch *ChampionshipsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	err := cm.DeleteChampionship(chi.URLParam(r, "championshipID"))

	if err != nil {
		logrus.WithError(err).Errorf("couldn't delete championship")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	smweb.AddFlash(w, r, smweb.T(r, "flash.championship.deleted"))
	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

type eventImportTemplateVars struct {
	smweb.BaseTemplateVars

	Results        []smresults.SessionResults
	ChampionshipID string
	Event          *smmodel.ChampionshipEvent
}

func (ch *ChampionshipsHandler) EventImport(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	championshipID := chi.URLParam(r, "championshipID")
	eventID := chi.URLParam(r, "eventID")

	if r.Method == http.MethodPost {
		err := cm.ImportEvent(championshipID, eventID, r)

		if err != nil {
			logrus.Errorf("Could not import championship event, error: %s", err)
			smweb.AddErrorFlash(w, r, smweb.T(r, "flash.championship.session_import_failed"))
		} else {
			smweb.AddFlash(w, r, smweb.T(r, "flash.championship.sessions_imported"))
			http.Redirect(w, r, "/championship/"+championshipID, http.StatusFound)
			return
		}
	}

	event, results, err := cm.ListAvailableResultsFilesForEvent(championshipID, eventID)

	if err != nil {
		logrus.WithError(err).Errorf("Couldn't load session files")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	ch.ViewRenderer.MustLoadTemplate(w, r, "championships/import-event.html", &eventImportTemplateVars{
		Results:        results,
		ChampionshipID: championshipID,
		Event:          event,
	})
}

// eventConfiguration builds a Custom Race form with slight modifications
// to allow a user to configure a ChampionshipEvent.
func (ch *ChampionshipsHandler) EventConfiguration(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	championshipRaceOpts, err := cm.BuildChampionshipEventOpts(r)

	if err != nil {
		logrus.WithError(err).Errorf("couldn't build championship race")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	ch.ViewRenderer.MustLoadTemplate(w, r, "custom-race/new.html", championshipRaceOpts)
}

// submitEventConfiguration takes an Event Configuration from a form and
// builds an event optionally, this is used for editing ChampionshipEvents.
func (ch *ChampionshipsHandler) SubmitEventConfiguration(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	championship, event, edited, err := cm.SaveChampionshipEvent(r)

	var validationErrors smrace.ValidationErrors

	if errors.As(err, &validationErrors) {
		opts, err := cm.BuildChampionshipEventOptsFromSubmission(r, validationErrors)

		if err != nil {
			logrus.WithError(err).Errorf("couldn't build championship event form with errors")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusUnprocessableEntity)
		ch.ViewRenderer.MustLoadTemplate(w, r, "custom-race/new.html", opts)
		return
	}

	if err != nil {
		logrus.WithError(err).Errorf("couldn't build championship race")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if edited {
		smweb.AddFlash(w, r, smweb.T(r, "flash.championship.event_edited", "track", smcontent.PrettifyName(event.RaceSetup.Track, false)))
	} else {
		smweb.AddFlash(w, r, smweb.T(r, "flash.championship.event_added", "track", smcontent.PrettifyName(event.RaceSetup.Track, false)))
	}

	if r.FormValue("action") == "saveChampionship" {
		// end the race creation flow
		http.Redirect(w, r, "/championship/"+championship.ID.String(), http.StatusFound)
		return
	}

	// add another event
	http.Redirect(w, r, "/championship/"+championship.ID.String()+"/event", http.StatusFound)
}

// startEvent begins a championship event given by its ID
func (ch *ChampionshipsHandler) StartEvent(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	err := cm.CheckCanStartEvent(chi.URLParam(r, "championshipID"), ch.active)

	if err == nil {
		err = cm.StartEvent(chi.URLParam(r, "championshipID"), chi.URLParam(r, "eventID"), false)
	}

	if err != nil {
		logrus.WithError(err).Errorf("Could not start championship event")

		flashEventError(w, r, err, smweb.T(r, "flash.championship.event_not_started"))
	} else {
		smweb.AddFlash(w, r, smweb.T(r, "flash.championship.event_started"))
		time.Sleep(time.Second * 1)
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (ch *ChampionshipsHandler) ScheduleEvent(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		logrus.WithError(err).Errorf("couldn't parse schedule race form")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	// the event is scheduled on the server which the form selected, whose manager runs the timer
	cm, ok := ch.manager(w, smweb.ScheduleTarget(r))

	if !ok {
		return
	}

	championshipID := chi.URLParam(r, "championshipID")
	championshipEventID := chi.URLParam(r, "eventID")
	dateString := r.FormValue("event-schedule-date")
	timeString := r.FormValue("event-schedule-time")
	timezone := r.FormValue("event-schedule-timezone")

	location, err := tz.LoadLocation(timezone)

	if err != nil {
		logrus.WithError(err).Errorf("could not find location: %s", location)
		location = time.Local
	}

	// Parse time in correct time zone
	date, err := time.ParseInLocation("2006-01-02-15:04", dateString+"-"+timeString, location)

	if err != nil {
		logrus.WithError(err).Errorf("couldn't parse schedule championship event date")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	err = cm.ScheduleEvent(championshipID, championshipEventID, date, r.FormValue("action"), r.FormValue("event-schedule-recurrence"))

	if err != nil {
		logrus.WithError(err).Errorf("couldn't schedule championship event")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	smweb.AddFlash(w, r, smweb.T(r, "flash.championship.event_scheduled", "date", date.Format(time.RFC1123)))
	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (ch *ChampionshipsHandler) ScheduleEventRemove(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	err := cm.ScheduleEvent(chi.URLParam(r, "championshipID"), chi.URLParam(r, "eventID"),
		time.Time{}, "remove", "")

	if err != nil {
		logrus.WithError(err).Errorf("couldn't schedule championship event")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

// deleteEvent soft deletes a championship event
func (ch *ChampionshipsHandler) DeleteEvent(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	err := cm.DeleteEvent(chi.URLParam(r, "championshipID"), chi.URLParam(r, "eventID"))

	if err != nil {
		logrus.WithError(err).Errorf("Could not delete championship event")

		smweb.AddErrorFlash(w, r, smweb.T(r, "flash.championship.event_not_deleted"))
	} else {
		smweb.AddFlash(w, r, smweb.T(r, "flash.championship.event_deleted"))
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

// startPracticeEvent starts a Practice session for a given event
func (ch *ChampionshipsHandler) StartPracticeEvent(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	err := cm.CheckCanStartEvent(chi.URLParam(r, "championshipID"), ch.active)

	if err == nil {
		err = cm.StartPracticeEvent(chi.URLParam(r, "championshipID"), chi.URLParam(r, "eventID"))
	}

	if err != nil {
		logrus.WithError(err).Errorf("Could not start practice championship event")

		flashEventError(w, r, err, smweb.T(r, "flash.championship.practice_not_started"))
	} else {
		smweb.AddFlash(w, r, smweb.T(r, "flash.championship.practice_started"))
		time.Sleep(time.Second * 1)
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

// cancelEvent stops a running championship event and clears any saved results
func (ch *ChampionshipsHandler) CancelEvent(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	err := cm.CheckNotActiveElsewhere(chi.URLParam(r, "championshipID"), ch.active)

	if err == nil {
		err = cm.CancelEvent(chi.URLParam(r, "championshipID"), chi.URLParam(r, "eventID"))
	}

	if err != nil {
		logrus.WithError(err).Errorf("Could not cancel championship event")

		flashEventError(w, r, err, smweb.T(r, "flash.championship.event_not_cancelled"))
	} else {
		smweb.AddFlash(w, r, smweb.T(r, "flash.championship.event_cancelled"))
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

// FinishEvent completes a partially driven event: the results of the sessions driven so far are kept, the remaining
// sessions are skipped and the server is stopped if the event runs on it.
func (ch *ChampionshipsHandler) FinishEvent(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	err := cm.CheckNotActiveElsewhere(chi.URLParam(r, "championshipID"), ch.active)

	if err == nil {
		err = cm.FinishEvent(chi.URLParam(r, "championshipID"), chi.URLParam(r, "eventID"))
	}

	if err != nil {
		logrus.WithError(err).Errorf("Could not finish championship event")

		flashEventError(w, r, err, smweb.T(r, "flash.championship.event_not_finished"))
	} else {
		smweb.AddFlash(w, r, smweb.T(r, "flash.championship.event_finished"))
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

// restartEvent stops a running championship event and clears any saved results
// then starts the event again.
func (ch *ChampionshipsHandler) RestartEvent(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	err := cm.CheckNotActiveElsewhere(chi.URLParam(r, "championshipID"), ch.active)

	if err == nil {
		err = cm.RestartEvent(chi.URLParam(r, "championshipID"), chi.URLParam(r, "eventID"))
	}

	if err != nil {
		logrus.WithError(err).Errorf("Could not restart championship event")

		flashEventError(w, r, err, smweb.T(r, "flash.championship.event_not_restarted"))
	} else {
		smweb.AddFlash(w, r, smweb.T(r, "flash.championship.event_restarted"))
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

type championshipCustomRaceImportTemplateVars struct {
	smweb.BaseTemplateVars

	Recent, Starred, Loop, Scheduled []*smmodel.CustomRace
	Championship                     *smmodel.Championship
}

type championshipRaceWeekendImportTemplateVars struct {
	smweb.BaseTemplateVars

	RaceWeekends []*smmodel.RaceWeekend
	Championship *smmodel.Championship
}

func (ch *ChampionshipsHandler) ListCustomRacesForImport(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	recent, starred, looped, scheduled, err := cm.RaceManager.ListCustomRaces()

	if err != nil {
		logrus.WithError(err).Errorf("couldn't list custom races")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	championship, err := cm.LoadChampionship(chi.URLParam(r, "championshipID"))

	if err != nil {
		logrus.WithError(err).Errorf("couldn't load championship for custom race import page list")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	ch.ViewRenderer.MustLoadTemplate(w, r, "championships/import-custom.html", &championshipCustomRaceImportTemplateVars{
		Recent:       recent,
		Starred:      starred,
		Loop:         looped,
		Scheduled:    scheduled,
		Championship: championship,
	})
}

func (ch *ChampionshipsHandler) CustomRaceImport(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	championshipID := chi.URLParam(r, "championshipID")

	err := cm.ImportEventSetup(championshipID, chi.URLParam(r, "eventID"))

	if err != nil {
		logrus.WithError(err).Error("could not import event setup to championship")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/championship/"+championshipID, http.StatusFound)
}

func (ch *ChampionshipsHandler) ListRaceWeekendsForImport(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	raceWeekends, err := cm.store.ListRaceWeekends()

	if err != nil {
		logrus.WithError(err).Errorf("couldn't list race weekends for championship import")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	championship, err := cm.LoadChampionship(chi.URLParam(r, "championshipID"))

	if err != nil {
		logrus.WithError(err).Errorf("couldn't load championship for custom race import page list")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	ch.ViewRenderer.MustLoadTemplate(w, r, "championships/import-weekend.html", &championshipRaceWeekendImportTemplateVars{
		RaceWeekends: raceWeekends,
		Championship: championship,
	})
}

func (ch *ChampionshipsHandler) RaceWeekendImport(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	championshipID := chi.URLParam(r, "championshipID")

	err := cm.ImportRaceWeekendSetup(championshipID, chi.URLParam(r, "weekendID"))

	if err != nil {
		logrus.WithError(err).Error("could not import race weekend setup to championship")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/championship/"+championshipID, http.StatusFound)
}

func (ch *ChampionshipsHandler) IcalFeed(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	if championship, err := cm.LoadChampionship(chi.URLParam(r, "championshipID")); err == nil && ch.denyHidden(w, r, championship) {
		return
	}

	w.Header().Add("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Add("Content-Disposition", "inline; filename=championship.ics")

	err := cm.BuildICalFeed(chi.URLParam(r, "championshipID"), w)

	if err != nil {
		logrus.WithError(err).Error("could not build scheduled races feed")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
}

func (ch *ChampionshipsHandler) DriverPenalty(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	err := cm.ModifyDriverPenalty(
		chi.URLParam(r, "championshipID"),
		chi.URLParam(r, "classID"),
		chi.URLParam(r, "driverGUID"),
		PenaltyAction(r.FormValue("action")),
		smweb.FormValueAsInt(r.FormValue("PointsPenalty")),
	)

	if err != nil {
		logrus.WithError(err).Errorf("Could not modify championship driver penalty")

		smweb.AddErrorFlash(w, r, smweb.T(r, "flash.championship.driver_penalty_failed"))
	} else {
		smweb.AddFlash(w, r, smweb.T(r, "flash.championship.driver_penalty_modified"))
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (ch *ChampionshipsHandler) TeamPenalty(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	err := cm.ModifyTeamPenalty(
		chi.URLParam(r, "championshipID"),
		chi.URLParam(r, "classID"),
		chi.URLParam(r, "team"),
		PenaltyAction(r.FormValue("action")),
		smweb.FormValueAsInt(r.FormValue("PointsPenalty")),
	)

	if err != nil {
		logrus.WithError(err).Errorf("Could not modify championship penalty")

		smweb.AddErrorFlash(w, r, smweb.T(r, "flash.championship.team_penalty_failed"))
	} else {
		smweb.AddFlash(w, r, smweb.T(r, "flash.championship.team_penalty_modified"))
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

type championshipSignUpFormTemplateVars struct {
	*ChampionshipTemplateVars

	FormData *smmodel.ChampionshipSignUpResponse

	// AvailableCars are the skins per car the driver can choose: the skins of the car and their own and team skins.
	AvailableCars   map[string][]string
	ValidationError string
	LockSteamGUID   bool

	// LoggedIn tells whether the visitor has a login, which they need to choose a team.
	LoggedIn bool

	// TeamOptions are the teams the driver can choose. LockedResponse is set if the driver is registered already:
	// their team is fixed then.
	TeamOptions    []*SignUpTeamOption
	LockedResponse *smmodel.ChampionshipSignUpResponse
	TeamLimit      int
	TeamLimited    bool
}

func (ch *ChampionshipsHandler) SignUpForm(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	if hidden, err := cm.LoadChampionship(chi.URLParam(r, "championshipID")); err == nil && ch.denyHidden(w, r, hidden) {
		return
	}

	championship, championshipOpts, err := cm.BuildChampionshipOpts(r)

	if err != nil {
		logrus.WithError(err).Error("couldn't load championship")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	opts := &championshipSignUpFormTemplateVars{
		ChampionshipTemplateVars: championshipOpts,
	}

	if !championship.SignUpAvailable() {
		http.NotFound(w, r)
		return
	}

	account := smweb.AccountFromRequest(r)

	if account != smweb.ScopeFromRequest(r).OpenAccount {
		opts.FormData = &smmodel.ChampionshipSignUpResponse{
			Name: account.DriverName,
			GUID: account.GUID,
			Team: account.Team,
		}
	} else {
		opts.FormData = &smmodel.ChampionshipSignUpResponse{}
	}

	if r.Method == http.MethodPost {
		signUpResponse, foundSlot, err := cm.HandleChampionshipSignUp(r)

		if err != nil {
			switch err.(type) {
			case ValidationError:
				opts.FormData = signUpResponse
				opts.ValidationError = smweb.ErrorText(r, err)
			default:
				logrus.WithError(err).Error("couldn't handle championship")
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
		} else {
			if championship.SignUpForm.RequiresApproval {
				smweb.AddFlash(w, r, smweb.T(r, "flash.championship.signed_up_pending"))
				http.Redirect(w, r, "/championship/"+championship.ID.String(), http.StatusFound)
				return
			}

			if foundSlot {
				smweb.AddFlash(w, r, smweb.T(r, "flash.championship.signed_up"))
				http.Redirect(w, r, "/championship/"+championship.ID.String(), http.StatusFound)
				return
			}

			opts.FormData = signUpResponse
			opts.ValidationError = smweb.TM(r, i18n.Msg("validation.championship.no_car_slots", "There are no more available slots for the car: {car}. Please pick a different car.", "car", smcontent.PrettifyName(signUpResponse.GetCar(), true)))
		}
	}

	if steamGUID := r.URL.Query().Get("steamGUID"); steamGUID != "" {
		opts.FormData.GUID = steamGUID
		opts.LockSteamGUID = true
	}

	if championship.SignUpForm.AskForTeam {
		opts.LoggedIn = smweb.LoggedIn(r)() && account.ID != uuid.Nil
		opts.TeamLimit, opts.TeamLimited = championship.SignUpForm.TeamLimit()
		opts.LockedResponse = SignUpTeamLock(championship, opts.FormData.GUID)

		if opts.TeamOptions, err = cm.SignUpTeamOptions(championship, account); err != nil {
			logrus.WithError(err).Error("couldn't load the teams of the account")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
	}

	if opts.AvailableCars, err = cm.SignUpCars(opts.CarOpts.AsMap(), skinAccountID(r)); err != nil {
		logrus.WithError(err).Error("couldn't load the skins of the account")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	ch.ViewRenderer.MustLoadTemplate(w, r, "championships/sign-up.html", opts)
}

type signedUpEntrantsTemplateVars struct {
	smweb.BaseTemplateVars

	Championship *smmodel.Championship

	// Teams are all teams, for moving a driver to another team.
	Teams []*smmodel.Team
}

func (ch *ChampionshipsHandler) SignedUpEntrants(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	championship, err := cm.LoadChampionship(chi.URLParam(r, "championshipID"))

	if err != nil {
		logrus.WithError(err).Error("couldn't load championship")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if !championship.SignUpForm.Enabled {
		http.NotFound(w, r)
		return
	}

	sort.Slice(championship.SignUpForm.Responses, func(i, j int) bool {
		return championship.SignUpForm.Responses[i].Created.After(championship.SignUpForm.Responses[j].Created)
	})

	teams, err := cm.AllTeams()

	if err != nil {
		logrus.WithError(err).Error("couldn't list teams")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	ch.ViewRenderer.MustLoadTemplate(w, r, "championships/signed-up-entrants.html", &signedUpEntrantsTemplateVars{
		BaseTemplateVars: smweb.BaseTemplateVars{WideContainer: true},
		Championship:     championship,
		Teams:            teams,
	})
}

func (ch *ChampionshipsHandler) SignedUpEntrantsCSV(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	championship, err := cm.LoadChampionship(chi.URLParam(r, "championshipID"))

	if err != nil {
		logrus.WithError(err).Error("couldn't load championship")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	headers := []string{
		"Created",
		"Name",
		"Team",
		"GUID",
		"Email",
		"Car",
		"Skin",
		"Status",
	}

	headers = append(headers, championship.SignUpForm.ExtraFields...)

	var out [][]string

	out = append(out, headers)

	for _, entrant := range championship.SignUpForm.Responses {
		data := []string{
			entrant.Created.String(),
			entrant.Name,
			entrant.Team,
			entrant.GUID,
			entrant.Email,
			entrant.Car,
			entrant.Skin,
			string(entrant.Status),
		}

		for _, question := range championship.SignUpForm.ExtraFields {
			if response, ok := entrant.Questions[question]; ok {
				data = append(data, response)
			} else {
				data = append(data, "")
			}
		}

		out = append(out, data)
	}

	w.Header().Add("Content-Type", "text/csv")
	w.Header().Add("Content-Disposition", fmt.Sprintf(`attachment;filename="Entrants_%s.csv"`, championship.Name))
	wr := csv.NewWriter(w)
	wr.UseCRLF = true
	_ = wr.WriteAll(out)
}

func (ch *ChampionshipsHandler) ModifyEntrantStatus(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	if r.URL.Query().Get("action") == "team" {
		// moderators move a driver to another team (drivers cannot change their team themselves)
		ch.changeEntrantTeam(w, r, cm)
		return
	}

	// the status changes the number of drivers of a team, nobody may register in between
	defer LockSignUps()()

	championship, err := cm.LoadChampionship(chi.URLParam(r, "championshipID"))

	if err != nil {
		logrus.WithError(err).Error("couldn't load championship")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if !championship.SignUpForm.Enabled {
		http.NotFound(w, r)
		return
	}

	entrantGUID := chi.URLParam(r, "entrantGUID")

	for index, entrant := range championship.SignUpForm.Responses {
		if entrant.GUID != entrantGUID {
			continue
		}

		switch r.URL.Query().Get("action") {
		case "accept":
			if entrant.Status == smmodel.ChampionshipEntrantAccepted {
				smweb.AddFlash(w, r, smweb.T(r, "flash.championship.entrant_already_accepted"))
				break
			}

			// accepting a rejected registration puts the driver back into the team
			if err := CheckAcceptTeamLimit(championship, entrant); err != nil {
				smweb.AddErrorFlash(w, r, smweb.ErrorText(r, err))
				break
			}

			// add the entrant to the entrylist
			foundSlot, _, err := cm.AddEntrantFromSessionData(championship, entrant, true, championship.SignUpForm.HideCarChoice)

			if err != nil {
				logrus.WithError(err).Error("couldn't add entrant to championship")
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}

			if foundSlot {
				entrant.Status = smmodel.ChampionshipEntrantAccepted

				smweb.AddFlash(w, r, smweb.T(r, "flash.championship.entrant_accepted"))
			} else {
				smweb.AddErrorFlash(w, r, smweb.T(r, "flash.championship.no_slots"))
			}
		case "reject":
			entrant.Status = smmodel.ChampionshipEntrantRejected
			championship.ClearEntrant(entrantGUID)
		case "delete":
			championship.SignUpForm.Responses = append(championship.SignUpForm.Responses[:index], championship.SignUpForm.Responses[index+1:]...)
			championship.ClearEntrant(entrantGUID)

		default:
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
	}

	if err := cm.UpsertChampionship(championship); err != nil {
		logrus.WithError(err).Error("couldn't save championship")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

// changeEntrantTeam moves a registered driver to another team. The route is only reachable with write access, the
// rules for the team of drivers do not apply to moderators.
func (ch *ChampionshipsHandler) changeEntrantTeam(w http.ResponseWriter, r *http.Request, cm *ChampionshipManager) {
	warning, err := cm.ChangeEntrantTeam(
		chi.URLParam(r, "championshipID"),
		chi.URLParam(r, "entrantGUID"),
		r.FormValue("teamID"),
		r.FormValue("team"),
	)

	switch err.(type) {
	case nil:
		smweb.AddFlash(w, r, smweb.T(r, "flash.championship.entrant_team_changed"))

		if warning != nil {
			smweb.AddErrorFlash(w, r, smweb.TM(r, *warning))
		}
	case ValidationError:
		smweb.AddErrorFlash(w, r, smweb.ErrorText(r, err))
	default:
		logrus.WithError(err).Error("couldn't change the team of the entrant")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (ch *ChampionshipsHandler) ReorderEvents(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	var eventIDsInOrder []string

	if err := json.NewDecoder(r.Body).Decode(&eventIDsInOrder); err != nil {
		logrus.WithError(err).Error("couldn't parse event reorder request")
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	if err := cm.ReorderChampionshipEvents(chi.URLParam(r, "championshipID"), eventIDsInOrder); err != nil {
		logrus.WithError(err).Error("couldn't reorder championship events")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
}

func (ch *ChampionshipsHandler) DuplicateEvent(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	championshipID := chi.URLParam(r, "championshipID")
	eventID := chi.URLParam(r, "eventID")

	newEvent, err := cm.DuplicateEventInChampionship(championshipID, eventID)

	if err != nil {
		logrus.WithError(err).Error("couldn't duplicate championship race weekend")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if newEvent.IsRaceWeekend() {
		smweb.AddFlash(w, r, smweb.T(r, "flash.championship.race_weekend_duplicated"))
		http.Redirect(w, r, "/race-weekend/"+newEvent.RaceWeekendID.String(), http.StatusFound)
	} else {
		smweb.AddFlash(w, r, smweb.T(r, "flash.championship.event_duplicated"))
		http.Redirect(w, r, "/championship/"+championshipID+"/event/"+newEvent.ID.String()+"/edit", http.StatusFound)
	}
}

// denyHidden answers with 404 and returns true if the championship is hidden and the requester may not see hidden
// championships. A hidden championship must look like one that does not exist.
func (ch *ChampionshipsHandler) denyHidden(w http.ResponseWriter, r *http.Request, championship *smmodel.Championship) bool {
	if championship.Hidden && !smweb.CanSeeHidden(r) {
		http.NotFound(w, r)
		return true
	}

	return false
}
