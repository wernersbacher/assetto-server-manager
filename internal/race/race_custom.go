package race

import (
	"fmt"
	"net/http"
	"time"

	"4d63.com/tz"
	"github.com/go-chi/chi"
	"github.com/sirupsen/logrus"
	smconfig "github.com/wernersbacher/assetto-server-manager/internal/config"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	smstore "github.com/wernersbacher/assetto-server-manager/internal/store"
	smweb "github.com/wernersbacher/assetto-server-manager/internal/web"
)

type CustomRaceHandler struct {
	*smweb.BaseHandler

	config      *smconfig.Configuration
	raceManager *RaceManager
	store       smstore.Store
}

func NewCustomRaceHandler(config *smconfig.Configuration, base *smweb.BaseHandler, raceManager *RaceManager, store smstore.Store) *CustomRaceHandler {
	return &CustomRaceHandler{
		config:      config,
		BaseHandler: base,
		raceManager: raceManager,
		store:       store,
	}
}

type customRaceListTemplateVars struct {
	smweb.BaseTemplateVars

	Recent, Starred, Loop, Scheduled []*smmodel.CustomRace
}

func (crh *CustomRaceHandler) List(w http.ResponseWriter, r *http.Request) {
	recent, starred, looped, scheduled, err := crh.raceManager.ListCustomRaces()

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
	customRaceData, err := crh.raceManager.BuildRaceOpts(r)

	if err != nil {
		logrus.WithError(err).Errorf("couldn't build custom race")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	crh.ViewRenderer.MustLoadTemplate(w, r, "custom-race/new.html", customRaceData)
}

func (crh *CustomRaceHandler) Submit(w http.ResponseWriter, r *http.Request) {
	err := crh.raceManager.SetupCustomRace(r)

	if err != nil {
		logrus.WithError(err).Errorf("couldn't apply custom race")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	action := r.FormValue("action")

	if action == "justSave" {
		smweb.AddFlash(w, r, "Custom race saved!")
		http.Redirect(w, r, "/custom", http.StatusFound)
	} else if action == "schedule" {
		smweb.AddFlash(w, r, "Custom race scheduled!")
		http.Redirect(w, r, "/custom", http.StatusFound)
	} else {
		smweb.AddFlash(w, r, "Custom race started!")
		if crh.config.Server.PerformanceMode {
			http.Redirect(w, r, "/", http.StatusFound)
		} else {
			http.Redirect(w, r, "/live-timing", http.StatusFound)
		}
	}
}

func (crh *CustomRaceHandler) Schedule(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		logrus.WithError(err).Errorf("couldn't parse schedule race form")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
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

	err = crh.raceManager.ScheduleRace(raceID, date, r.FormValue("action"), r.FormValue("event-schedule-recurrence"))

	if err != nil {
		logrus.WithError(err).Errorf("couldn't schedule race")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	smweb.AddFlash(w, r, fmt.Sprintf("We have scheduled the race to begin at %s", date.Format(time.RFC1123)))
	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (crh *CustomRaceHandler) RemoveSchedule(w http.ResponseWriter, r *http.Request) {
	err := crh.raceManager.ScheduleRace(chi.URLParam(r, "uuid"), time.Time{}, "remove", "")

	if err != nil {
		logrus.WithError(err).Errorf("couldn't remove scheduled race")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (crh *CustomRaceHandler) Start(w http.ResponseWriter, r *http.Request) {
	_, err := crh.raceManager.StartCustomRace(chi.URLParam(r, "uuid"), false)

	if err != nil {
		logrus.WithError(err).Errorf("couldn't apply custom race")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	smweb.AddFlash(w, r, "Custom race started!")

	if crh.config.Server.PerformanceMode {
		http.Redirect(w, r, "/", http.StatusFound)
	} else {
		http.Redirect(w, r, "/live-timing", http.StatusFound)
	}
}

func (crh *CustomRaceHandler) Delete(w http.ResponseWriter, r *http.Request) {
	err := crh.raceManager.DeleteCustomRace(chi.URLParam(r, "uuid"))

	if err != nil {
		logrus.WithError(err).Errorf("couldn't delete custom race")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	smweb.AddFlash(w, r, "Custom race deleted!")
	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (crh *CustomRaceHandler) Star(w http.ResponseWriter, r *http.Request) {
	err := crh.raceManager.ToggleStarCustomRace(chi.URLParam(r, "uuid"))

	if err != nil {
		logrus.WithError(err).Errorf("couldn't star custom race")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	smweb.AddFlash(w, r, "Custom race starred")
	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (crh *CustomRaceHandler) Loop(w http.ResponseWriter, r *http.Request) {
	loopStatus, err := crh.raceManager.ToggleLoopCustomRace(chi.URLParam(r, "uuid"))

	if err != nil {
		logrus.WithError(err).Errorf("couldn't add custom race to loop")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if loopStatus {
		smweb.AddFlash(w, r, "Custom race added to loop")
	} else {
		smweb.AddFlash(w, r, "Custom race removed from loop")
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}
