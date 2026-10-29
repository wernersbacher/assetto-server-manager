package servermanager

import (
	"net/http"

	"github.com/sirupsen/logrus"
	smconfig "github.com/wernersbacher/assetto-server-manager/internal/config"
)

type QuickRaceHandler struct {
	*BaseHandler

	config      *smconfig.Configuration
	raceManager *RaceManager
}

func NewQuickRaceHandler(config *smconfig.Configuration, baseHandler *BaseHandler, raceManager *RaceManager) *QuickRaceHandler {
	return &QuickRaceHandler{
		config:      config,
		BaseHandler: baseHandler,
		raceManager: raceManager,
	}
}

func (qrh *QuickRaceHandler) create(w http.ResponseWriter, r *http.Request) {
	quickRaceData, err := qrh.raceManager.BuildRaceOpts(r)

	if err != nil {
		logrus.WithError(err).Errorf("couldn't build quick race")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	qrh.viewRenderer.MustLoadTemplate(w, r, "quick-race.html", quickRaceData)
}

func (qrh *QuickRaceHandler) submit(w http.ResponseWriter, r *http.Request) {
	err := qrh.raceManager.SetupQuickRace(r)

	if err == ErrMustSubmitCar {
		AddErrorFlash(w, r, "You must choose at least one car!")
		http.Redirect(w, r, r.Referer(), http.StatusFound)
		return
	} else if err != nil {
		logrus.WithError(err).Errorf("couldn't apply quick race")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	AddFlash(w, r, "Quick race successfully started!")

	if qrh.config.Server.PerformanceMode {
		http.Redirect(w, r, "/", http.StatusFound)
	} else {
		http.Redirect(w, r, "/live-timing", http.StatusFound)
	}
}
