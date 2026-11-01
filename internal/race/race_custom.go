package race

import (
	"errors"
	"net/http"
	"time"

	"4d63.com/tz"
	"github.com/go-chi/chi"
	"github.com/sirupsen/logrus"
	smconfig "github.com/wernersbacher/ACPitwall/internal/config"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

type CustomRaceHandler struct {
	*smweb.BaseHandler

	config     *smconfig.Configuration
	managerFor ManagerFor
	store      smstore.Store
}

func NewCustomRaceHandler(config *smconfig.Configuration, base *smweb.BaseHandler, managerFor ManagerFor, store smstore.Store) *CustomRaceHandler {
	return &CustomRaceHandler{
		config:      config,
		BaseHandler: base,
		managerFor:  managerFor,
		store:       store,
	}
}

type customRaceListTemplateVars struct {
	smweb.BaseTemplateVars

	Recent, Starred, Loop, Scheduled []*smmodel.CustomRace
}

func (crh *CustomRaceHandler) List(w http.ResponseWriter, r *http.Request) {
	manager, ok := crh.manager(w, r)

	if !ok {
		return
	}

	recent, starred, looped, scheduled, err := manager.ListCustomRaces()

	if err != nil {
		logrus.WithError(err).Errorf("couldn't list custom races")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	crh.ViewRenderer.MustLoadTemplate(w, r, "custom-race/index.html", &customRaceListTemplateVars{
		Recent:    recent,
		Starred:   starred,
		Loop:      looped,
		Scheduled: scheduled,
	})
}

func (crh *CustomRaceHandler) CreateOrEdit(w http.ResponseWriter, r *http.Request) {
	manager, ok := crh.manager(w, r)

	if !ok {
		return
	}

	customRaceData, err := manager.BuildRaceOpts(r)

	if err != nil {
		logrus.WithError(err).Errorf("couldn't build custom race")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	crh.ViewRenderer.MustLoadTemplate(w, r, "custom-race/new.html", customRaceData)
}

func (crh *CustomRaceHandler) Submit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		logrus.WithError(err).Errorf("couldn't parse custom race form")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	action := r.FormValue("action")

	// a schedule is run by the manager of the server it is scheduled for
	request := r

	if action == "schedule" {
		request = smweb.ScheduleTarget(r)
	}

	manager, ok := crh.manager(w, request)

	if !ok {
		return
	}

	// saving or editing a race does not start it
	if action != "justSave" && action != "schedule" && r.FormValue("Editing") == "" && flashServerBusy(w, r, manager.CheckCanStart()) {
		return
	}

	err := manager.SetupCustomRace(r)

	var validationErrors ValidationErrors

	if errors.As(err, &validationErrors) {
		crh.renderInvalid(w, r, manager, validationErrors)
		return
	}

	if err != nil {
		logrus.WithError(err).Errorf("couldn't apply custom race")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if action == "justSave" {
		smweb.AddFlash(w, r, smweb.T(r, "flash.race.custom_saved"))
		http.Redirect(w, r, smweb.ServerURL(r, "/custom"), http.StatusFound)
	} else if action == "schedule" {
		smweb.AddFlash(w, r, smweb.T(r, "flash.race.custom_scheduled"))
		http.Redirect(w, r, smweb.ServerURL(r, "/custom"), http.StatusFound)
	} else {
		smweb.AddFlash(w, r, smweb.T(r, "flash.race.custom_started"))
		if crh.config.Server.PerformanceMode {
			http.Redirect(w, r, "/", http.StatusFound)
		} else {
			http.Redirect(w, r, smweb.ServerURL(r, "/live-timing"), http.StatusFound)
		}
	}
}

func (crh *CustomRaceHandler) Schedule(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		logrus.WithError(err).Errorf("couldn't parse schedule race form")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	// the race is scheduled on the server which the form selected, whose manager runs the timer
	manager, ok := crh.manager(w, smweb.ScheduleTarget(r))

	if !ok {
		return
	}

	raceID := chi.URLParam(r, "uuid")
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
		logrus.WithError(err).Errorf("couldn't parse schedule race date")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	err = manager.ScheduleRace(raceID, date, r.FormValue("action"), r.FormValue("event-schedule-recurrence"))

	if err != nil {
		logrus.WithError(err).Errorf("couldn't schedule race")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	smweb.AddFlash(w, r, smweb.T(r, "flash.race.scheduled", "date", date.Format(time.RFC1123)))
	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (crh *CustomRaceHandler) RemoveSchedule(w http.ResponseWriter, r *http.Request) {
	manager, ok := crh.manager(w, r)

	if !ok {
		return
	}

	err := manager.ScheduleRace(chi.URLParam(r, "uuid"), time.Time{}, "remove", "")

	if err != nil {
		logrus.WithError(err).Errorf("couldn't remove scheduled race")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (crh *CustomRaceHandler) Start(w http.ResponseWriter, r *http.Request) {
	manager, ok := crh.manager(w, r)

	if !ok {
		return
	}

	if flashServerBusy(w, r, manager.CheckCanStart()) {
		return
	}

	_, err := manager.StartCustomRace(chi.URLParam(r, "uuid"), false)

	if err != nil {
		logrus.WithError(err).Errorf("couldn't apply custom race")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	smweb.AddFlash(w, r, smweb.T(r, "flash.race.custom_started"))

	if crh.config.Server.PerformanceMode {
		http.Redirect(w, r, "/", http.StatusFound)
	} else {
		http.Redirect(w, r, smweb.ServerURL(r, "/live-timing"), http.StatusFound)
	}
}

func (crh *CustomRaceHandler) Delete(w http.ResponseWriter, r *http.Request) {
	manager, ok := crh.manager(w, r)

	if !ok {
		return
	}

	err := manager.DeleteCustomRace(chi.URLParam(r, "uuid"))

	if err != nil {
		logrus.WithError(err).Errorf("couldn't delete custom race")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	smweb.AddFlash(w, r, smweb.T(r, "flash.race.custom_deleted"))
	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (crh *CustomRaceHandler) Star(w http.ResponseWriter, r *http.Request) {
	manager, ok := crh.manager(w, r)

	if !ok {
		return
	}

	err := manager.ToggleStarCustomRace(chi.URLParam(r, "uuid"))

	if err != nil {
		logrus.WithError(err).Errorf("couldn't star custom race")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	smweb.AddFlash(w, r, smweb.T(r, "flash.race.custom_starred"))
	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (crh *CustomRaceHandler) Loop(w http.ResponseWriter, r *http.Request) {
	manager, ok := crh.manager(w, r)

	if !ok {
		return
	}

	loopStatus, err := manager.ToggleLoopCustomRace(chi.URLParam(r, "uuid"))

	if err != nil {
		logrus.WithError(err).Errorf("couldn't add custom race to loop")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if loopStatus {
		smweb.AddFlash(w, r, smweb.T(r, "flash.race.custom_looped"))
	} else {
		smweb.AddFlash(w, r, smweb.T(r, "flash.race.custom_unlooped"))
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

// renderInvalid shows the custom race form again with the input of the user and the validation errors.
func (crh *CustomRaceHandler) renderInvalid(w http.ResponseWriter, r *http.Request, manager *RaceManager, errs ValidationErrors) {
	customRaceData, err := manager.BuildRaceOptsFromSubmission(r, errs)

	if err != nil {
		logrus.WithError(err).Errorf("couldn't build custom race form with errors")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusUnprocessableEntity)
	crh.ViewRenderer.MustLoadTemplate(w, r, "custom-race/new.html", customRaceData)
}
