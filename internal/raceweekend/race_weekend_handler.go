package raceweekend

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"4d63.com/tz"
	"github.com/go-chi/chi"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	smchampionship "github.com/wernersbacher/assetto-server-manager/internal/championship"
	smcontent "github.com/wernersbacher/assetto-server-manager/internal/content"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"
	smweb "github.com/wernersbacher/assetto-server-manager/internal/web"
)

type RaceWeekendHandler struct {
	*smweb.BaseHandler

	managerFor ManagerFor
	active     smchampionship.ActiveEvents
}

func NewRaceWeekendHandler(baseHandler *smweb.BaseHandler, managerFor ManagerFor, active smchampionship.ActiveEvents) *RaceWeekendHandler {
	return &RaceWeekendHandler{
		BaseHandler: baseHandler,
		managerFor:  managerFor,
		active:      active,
	}
}

type raceWeekendListTemplateVars struct {
	smweb.BaseTemplateVars

	RaceWeekends []*smmodel.RaceWeekend
}

func (rwh *RaceWeekendHandler) List(w http.ResponseWriter, r *http.Request) {
	rwm, ok := rwh.manager(w, r)

	if !ok {
		return
	}

	raceWeekends, err := rwm.ListRaceWeekends()

	if err != nil {
		logrus.WithError(err).Errorf("couldn't list weekends")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	rwh.ViewRenderer.MustLoadTemplate(w, r, "race-weekend/index.html", &raceWeekendListTemplateVars{
		RaceWeekends: raceWeekends,
	})
}

type raceWeekendViewTemplateVars struct {
	smweb.BaseTemplateVars

	RaceWeekend *smmodel.RaceWeekend
	Account     *smmodel.Account

	// ActiveServer is the other server which runs the race weekend, if there is one.
	ActiveServer *smweb.ServerRef
}

func (rwh *RaceWeekendHandler) View(w http.ResponseWriter, r *http.Request) {
	rwm, ok := rwh.manager(w, r)

	if !ok {
		return
	}

	raceWeekend, err := rwm.LoadRaceWeekend(chi.URLParam(r, "raceWeekendID"))

	if err != nil {
		logrus.WithError(err).Errorf("couldn't load race weekend")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	var activeServer *smweb.ServerRef

	var elsewhere smchampionship.ActiveElsewhereError

	if errors.As(rwm.CheckNotActiveElsewhere(raceWeekend.ID.String(), rwh.active), &elsewhere) {
		activeServer = &elsewhere.Server
	}

	rwh.ViewRenderer.MustLoadTemplate(w, r, "race-weekend/view.html", &raceWeekendViewTemplateVars{
		BaseTemplateVars: smweb.BaseTemplateVars{
			WideContainer: true,
		},
		RaceWeekend:  raceWeekend,
		Account:      smweb.AccountFromRequest(r),
		ActiveServer: activeServer,
	})
}

func (rwh *RaceWeekendHandler) CreateOrEdit(w http.ResponseWriter, r *http.Request) {
	rwm, ok := rwh.manager(w, r)

	if !ok {
		return
	}

	raceWeekendOpts, err := rwm.BuildRaceWeekendTemplateOpts(r)

	if err != nil {
		logrus.WithError(err).Errorf("couldn't load race weekend")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	rwh.ViewRenderer.MustLoadTemplate(w, r, "race-weekend/new.html", raceWeekendOpts)
}

func (rwh *RaceWeekendHandler) Delete(w http.ResponseWriter, r *http.Request) {
	rwm, ok := rwh.manager(w, r)

	if !ok {
		return
	}

	err := rwm.DeleteRaceWeekend(chi.URLParam(r, "raceWeekendID"))

	if err != nil {
		logrus.WithError(err).Errorf("couldn't delete race weekend")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	smweb.AddFlash(w, r, "Race Weekend successfully deleted!")
	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (rwh *RaceWeekendHandler) Submit(w http.ResponseWriter, r *http.Request) {
	rwm, ok := rwh.manager(w, r)

	if !ok {
		return
	}

	raceWeekend, edited, err := rwm.SaveRaceWeekend(r)

	if err != nil {
		logrus.WithError(err).Errorf("couldn't create race weekend")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if edited {
		smweb.AddFlash(w, r, "Race Weekend successfully edited!")
		http.Redirect(w, r, "/race-weekend/"+raceWeekend.ID.String(), http.StatusFound)
	} else {
		smweb.AddFlash(w, r, "We've created the Race Weekend. Now you need to add some sessions!")
		http.Redirect(w, r, "/race-weekend/"+raceWeekend.ID.String()+"/session", http.StatusFound)
	}
}

func (rwh *RaceWeekendHandler) SessionConfiguration(w http.ResponseWriter, r *http.Request) {
	rwm, ok := rwh.manager(w, r)

	if !ok {
		return
	}

	raceWeekendSessionOpts, err := rwm.BuildRaceWeekendSessionOpts(r)

	if err != nil {
		logrus.WithError(err).Errorf("couldn't build race weekend session")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	rwh.ViewRenderer.MustLoadTemplate(w, r, "custom-race/new.html", raceWeekendSessionOpts)
}

func (rwh *RaceWeekendHandler) SubmitSessionConfiguration(w http.ResponseWriter, r *http.Request) {
	rwm, ok := rwh.manager(w, r)

	if !ok {
		return
	}

	raceWeekend, session, edited, err := rwm.SaveRaceWeekendSession(r)

	if err != nil {
		logrus.WithError(err).Errorf("couldn't build race weekend session")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if edited {
		smweb.AddFlash(w, r,
			fmt.Sprintf(
				"Race Weekend session at %s was successfully edited!",
				smcontent.PrettifyName(session.RaceConfig.Track, false),
			),
		)
	} else {
		smweb.AddFlash(w, r,
			fmt.Sprintf(
				"Race Weekend session at %s was successfully added!",
				smcontent.PrettifyName(session.RaceConfig.Track, false),
			),
		)
	}

	if r.FormValue("action") == "saveRaceWeekend" {
		// end the race creation flow
		http.Redirect(w, r, "/race-weekend/"+raceWeekend.ID.String(), http.StatusFound)
		return
	}

	// add another session
	http.Redirect(w, r, "/race-weekend/"+raceWeekend.ID.String()+"/session", http.StatusFound)
}

func (rwh *RaceWeekendHandler) StartSession(w http.ResponseWriter, r *http.Request) {
	rwm, ok := rwh.manager(w, r)

	if !ok {
		return
	}

	err := rwm.CheckCanStartSession(chi.URLParam(r, "raceWeekendID"), rwh.active)

	if err == nil {
		err = rwm.StartSession(chi.URLParam(r, "raceWeekendID"), chi.URLParam(r, "sessionID"), false)
	}

	if err != nil {
		logrus.WithError(err).Errorf("Could not start Race Weekend session")

		flashSessionError(w, r, err, "Couldn't start the Session")
	} else {
		smweb.AddFlash(w, r, "Session started successfully!")
		time.Sleep(time.Second * 1)
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (rwh *RaceWeekendHandler) StartPracticeSession(w http.ResponseWriter, r *http.Request) {
	rwm, ok := rwh.manager(w, r)

	if !ok {
		return
	}

	err := rwm.CheckCanStartSession(chi.URLParam(r, "raceWeekendID"), rwh.active)

	if err == nil {
		err = rwm.StartPracticeSession(chi.URLParam(r, "raceWeekendID"), chi.URLParam(r, "sessionID"))
	}

	if err != nil {
		logrus.WithError(err).Errorf("Could not start Race Weekend practice session")

		flashSessionError(w, r, err, "Couldn't start the Session")
	} else {
		smweb.AddFlash(w, r, "Session started successfully!")
		time.Sleep(time.Second * 1)
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (rwh *RaceWeekendHandler) RestartSession(w http.ResponseWriter, r *http.Request) {
	rwm, ok := rwh.manager(w, r)

	if !ok {
		return
	}

	err := rwm.CheckNotActiveElsewhere(chi.URLParam(r, "raceWeekendID"), rwh.active)

	if err == nil {
		err = rwm.RestartSession(chi.URLParam(r, "raceWeekendID"), chi.URLParam(r, "sessionID"))
	}

	if err != nil {
		logrus.WithError(err).Errorf("Could not restart Race Weekend session")

		flashSessionError(w, r, err, "Couldn't restart the Session")
	} else {
		smweb.AddFlash(w, r, "Session restarted successfully!")
		time.Sleep(time.Second * 1)
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (rwh *RaceWeekendHandler) CancelSession(w http.ResponseWriter, r *http.Request) {
	rwm, ok := rwh.manager(w, r)

	if !ok {
		return
	}

	err := rwm.CheckNotActiveElsewhere(chi.URLParam(r, "raceWeekendID"), rwh.active)

	if err == nil {
		err = rwm.CancelSession(chi.URLParam(r, "raceWeekendID"), chi.URLParam(r, "sessionID"))
	}

	if err != nil {
		logrus.WithError(err).Errorf("Could not cancel Race Weekend session")

		flashSessionError(w, r, err, "Couldn't cancel the Session")
	} else {
		smweb.AddFlash(w, r, "Session canceled successfully!")
		time.Sleep(time.Second * 1)
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (rwh *RaceWeekendHandler) DeleteSession(w http.ResponseWriter, r *http.Request) {
	rwm, ok := rwh.manager(w, r)

	if !ok {
		return
	}

	err := rwm.DeleteSession(chi.URLParam(r, "raceWeekendID"), chi.URLParam(r, "sessionID"))

	if err != nil {
		logrus.WithError(err).Errorf("Could not delete Race Weekend session")

		smweb.AddErrorFlash(w, r, "Couldn't delete the Session")
	} else {
		smweb.AddFlash(w, r, "Session deleted successfully!")
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

type raceWeekendSessionImportTemplateVars struct {
	smweb.BaseTemplateVars

	Results       []smresults.SessionResults
	RaceWeekendID string
	Session       *smmodel.RaceWeekendSession
}

func (rwh *RaceWeekendHandler) ImportSessionResults(w http.ResponseWriter, r *http.Request) {
	rwm, ok := rwh.manager(w, r)

	if !ok {
		return
	}

	raceWeekendID := chi.URLParam(r, "raceWeekendID")
	sessionID := chi.URLParam(r, "sessionID")

	if r.Method == http.MethodPost {
		err := rwm.ImportSession(raceWeekendID, sessionID, r)

		if err != nil {
			logrus.WithError(err).Errorf("Could not import race weekend session")
			smweb.AddErrorFlash(w, r, "Could not import race weekend session files")
		} else {
			smweb.AddFlash(w, r, "Successfully imported session files!")
			http.Redirect(w, r, "/race-weekend/"+raceWeekendID, http.StatusFound)
			return
		}
	}

	session, results, err := rwm.ListAvailableResultsFilesForSession(raceWeekendID, sessionID)

	if err != nil {
		logrus.WithError(err).Errorf("Couldn't load session files")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	rwh.ViewRenderer.MustLoadTemplate(w, r, "race-weekend/import-session.html", &raceWeekendSessionImportTemplateVars{
		Results:       results,
		RaceWeekendID: raceWeekendID,
		Session:       session,
	})
}

type raceWeekendFilterTemplateVars struct {
	smweb.BaseTemplateVars

	RaceWeekend                 *smmodel.RaceWeekend
	ParentSession, ChildSession *smmodel.RaceWeekendSession
	ResultsAvailableForSorting  []smresults.SessionResults
	Filter                      *smmodel.RaceWeekendSessionToSessionFilter
	AvailableSorters            []RaceWeekendEntryListSorterDescription
	ParentSessionResults        []*smmodel.RaceWeekendSessionEntrant
	ChampionshipClasses         map[uuid.UUID]*smmodel.ChampionshipClass
}

func (rwh *RaceWeekendHandler) ManageFilters(w http.ResponseWriter, r *http.Request) {
	rwm, ok := rwh.manager(w, r)

	if !ok {
		return
	}

	raceWeekendID := chi.URLParam(r, "raceWeekendID")
	parentSessionID := r.URL.Query().Get("parentSessionID")
	childSessionID := r.URL.Query().Get("childSessionID")

	raceWeekend, parentSession, childSession, err := rwm.FindConnectedSessions(raceWeekendID, parentSessionID, childSessionID)

	if err != nil {
		logrus.WithError(err).Errorf("Couldn't load connected sessions")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	filter, err := raceWeekend.GetFilterOrUseDefault(parentSessionID, childSessionID)

	if err != nil {
		logrus.WithError(err).Errorf("Couldn't load session filters")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	sessionResults, err := rwm.ListAvailableResultsFilesForSorting(raceWeekend, childSession)

	if err != nil {
		logrus.WithError(err).Error("Couldn't list results files for sorting")
	}

	parentSessionResults, err := raceWeekendFinishingGrid(rwm.env, parentSession, raceWeekend)

	if err != nil {
		logrus.WithError(err).Errorf("Couldn't load previous session results")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	var championshipClasses map[uuid.UUID]*smmodel.ChampionshipClass

	if raceWeekend.HasLinkedChampionship() {
		championshipClasses = make(map[uuid.UUID]*smmodel.ChampionshipClass)

		for _, result := range parentSessionResults {
			class := result.ChampionshipClass(raceWeekend)

			championshipClasses[class.ID] = class
		}
	}

	rwh.ViewRenderer.MustLoadPartial(w, r, "race-weekend/popups/manage-filters.html", &raceWeekendFilterTemplateVars{
		RaceWeekend:                raceWeekend,
		ParentSession:              parentSession,
		ChildSession:               childSession,
		ResultsAvailableForSorting: sessionResults,
		Filter:                     filter,
		AvailableSorters:           RaceWeekendEntryListSorters(rwm.env),
		ParentSessionResults:       parentSessionResults,
		ChampionshipClasses:        championshipClasses,
	})
}

func (rwh *RaceWeekendHandler) GridPreview(w http.ResponseWriter, r *http.Request) {
	rwm, ok := rwh.manager(w, r)

	if !ok {
		return
	}

	raceWeekendID := chi.URLParam(r, "raceWeekendID")
	parentSessionID := r.URL.Query().Get("parentSessionID")
	childSessionID := r.URL.Query().Get("childSessionID")

	var filter *smmodel.RaceWeekendSessionToSessionFilter

	if err := json.NewDecoder(r.Body).Decode(&filter); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	filter.IsPreview = true

	previewResponse, err := rwm.PreviewGrid(raceWeekendID, parentSessionID, childSessionID, filter)

	if err != nil {
		logrus.WithError(err).Errorf("Couldn't preview session grid")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Add("Content-Type", "application/json")

	_ = json.NewEncoder(w).Encode(previewResponse)
}

func (rwh *RaceWeekendHandler) UpdateGrid(w http.ResponseWriter, r *http.Request) {
	rwm, ok := rwh.manager(w, r)

	if !ok {
		return
	}

	raceWeekendID := chi.URLParam(r, "raceWeekendID")
	parentSessionID := r.URL.Query().Get("parentSessionID")
	childSessionID := r.URL.Query().Get("childSessionID")

	var filter *smmodel.RaceWeekendSessionToSessionFilter

	if err := json.NewDecoder(r.Body).Decode(&filter); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	if err := rwm.UpdateGrid(raceWeekendID, parentSessionID, childSessionID, filter); err != nil {
		logrus.WithError(err).Errorf("Couldn't update session grid")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

type raceWeekendManageEntryListTemplateVars struct {
	smweb.BaseTemplateVars

	RaceWeekend      *smmodel.RaceWeekend
	Session          *smmodel.RaceWeekendSession
	AvailableSorters []RaceWeekendEntryListSorterDescription
}

func (rwh *RaceWeekendHandler) ManageEntryList(w http.ResponseWriter, r *http.Request) {
	rwm, ok := rwh.manager(w, r)

	if !ok {
		return
	}

	raceWeekendID := chi.URLParam(r, "raceWeekendID")
	sessionID := r.URL.Query().Get("sessionID")

	raceWeekend, session, err := rwm.FindSession(raceWeekendID, sessionID)

	if err != nil {
		logrus.WithError(err).Errorf("Couldn't load manage entry list popup")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	rwh.ViewRenderer.MustLoadPartial(w, r, "race-weekend/popups/manage-entrylist.html", &raceWeekendManageEntryListTemplateVars{
		RaceWeekend:      raceWeekend,
		Session:          session,
		AvailableSorters: RaceWeekendEntryListSorters(rwm.env),
	})
}

func (rwh *RaceWeekendHandler) EntryListPreview(w http.ResponseWriter, r *http.Request) {
	rwm, ok := rwh.manager(w, r)

	if !ok {
		return
	}

	raceWeekendID := chi.URLParam(r, "raceWeekendID")
	sessionID := r.URL.Query().Get("sessionID")
	sortType := r.URL.Query().Get("sortType")
	reverseNumber := smweb.FormValueAsInt(r.URL.Query().Get("reverseGrid"))

	previewResponse, err := rwm.PreviewSessionEntryList(raceWeekendID, sessionID, sortType, reverseNumber)

	if err != nil {
		logrus.WithError(err).Errorf("Couldn't preview session grid")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Add("Content-Type", "application/json")

	_ = json.NewEncoder(w).Encode(previewResponse)
}

func (rwh *RaceWeekendHandler) UpdateEntryList(w http.ResponseWriter, r *http.Request) {
	rwm, ok := rwh.manager(w, r)

	if !ok {
		return
	}

	raceWeekendID := chi.URLParam(r, "raceWeekendID")
	parentSessionID := r.URL.Query().Get("sessionID")
	sortType := r.URL.Query().Get("sortType")
	reverseNumber := smweb.FormValueAsInt(r.URL.Query().Get("reverseGrid"))

	if err := rwm.UpdateSessionSorting(raceWeekendID, parentSessionID, sortType, reverseNumber); err != nil {
		logrus.WithError(err).Errorf("Couldn't update session entrylist sorting")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (rwh *RaceWeekendHandler) Export(w http.ResponseWriter, r *http.Request) {
	rwm, ok := rwh.manager(w, r)

	if !ok {
		return
	}

	raceWeekend, err := rwm.LoadRaceWeekend(chi.URLParam(r, "raceWeekendID"))

	if err != nil {
		logrus.WithError(err).Errorf("couldn't export race weeeknd")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if !smweb.AccountFromRequest(r).HasGroupPrivilege(rwm.env.ServerID(), smmodel.GroupWrite) {
		// if you don't have write access or above you can't see the replacement password
		for _, session := range raceWeekend.Sessions {
			session.ReplacementPassword = ""
		}
	}

	w.Header().Add("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.json"`, raceWeekend.Name))

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(raceWeekend)
}

func (rwh *RaceWeekendHandler) ImportRaceWeekend(w http.ResponseWriter, r *http.Request) {
	rwm, ok := rwh.manager(w, r)

	if !ok {
		return
	}

	if r.Method == http.MethodPost {
		raceWeekendID, err := rwm.ImportRaceWeekend(r.FormValue("import"))

		if err != nil {
			logrus.WithError(err).Error("could not import race weekend")
			smweb.AddErrorFlash(w, r, "Sorry, we couldn't import that Race Weekend! Check your JSON formatting.")
		} else {
			smweb.AddFlash(w, r, "Race Weekend successfully imported!")
			http.Redirect(w, r, "/race-weekend/"+raceWeekendID, http.StatusFound)
		}
	}

	rwh.ViewRenderer.MustLoadTemplate(w, r, "race-weekend/import-raceweekend.html", nil)
}

func (rwh *RaceWeekendHandler) ScheduleSession(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		logrus.WithError(err).Errorf("couldn't parse schedule race form")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	// the session is scheduled on the server which the form selected, whose manager runs the timer
	rwm, ok := rwh.manager(w, smweb.ScheduleTarget(r))

	if !ok {
		return
	}

	championshipID := chi.URLParam(r, "raceWeekendID")
	championshipEventID := chi.URLParam(r, "sessionID")
	dateString := r.FormValue("session-schedule-date")
	timeString := r.FormValue("session-schedule-time")
	timezone := r.FormValue("session-schedule-timezone")
	startWhenParentFinished := smweb.FormValueAsInt(r.FormValue("session-start-after-parent")) == 1

	var message string
	var date time.Time

	if !startWhenParentFinished {
		var location *time.Location

		location, err := tz.LoadLocation(timezone)

		if err != nil {
			logrus.WithError(err).Errorf("could not find location: %s", location)
			location = time.Local
		}

		// Parse time in correct time zone
		date, err = time.ParseInLocation("2006-01-02-15:04", dateString+"-"+timeString, location)

		if err != nil {
			logrus.WithError(err).Errorf("couldn't parse schedule race weekend session date")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		message = fmt.Sprintf("We have scheduled the Race Weekend Session to begin at %s", date.Format(time.RFC1123))

	} else {
		message = "We have scheduled the Race Weekend Session to begin after the parent session(s) complete."
	}

	err := rwm.ScheduleSession(championshipID, championshipEventID, date, startWhenParentFinished)

	if err != nil {
		logrus.WithError(err).Errorf("couldn't schedule race weekend session")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	smweb.AddFlash(w, r, message)
	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (rwh *RaceWeekendHandler) RemoveSessionSchedule(w http.ResponseWriter, r *http.Request) {
	rwm, ok := rwh.manager(w, r)

	if !ok {
		return
	}

	err := rwm.DeScheduleSession(chi.URLParam(r, "raceWeekendID"), chi.URLParam(r, "sessionID"))

	if err != nil {
		logrus.WithError(err).Errorf("couldn't de-schedule race weekend session")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}
