package race

import (
	"net/http"

	"github.com/sirupsen/logrus"
	smconfig "github.com/wernersbacher/assetto-server-manager/internal/config"
	smweb "github.com/wernersbacher/assetto-server-manager/internal/web"
)

type QuickRaceHandler struct {
	*smweb.BaseHandler

	config     *smconfig.Configuration
	managerFor ManagerFor
}

func NewQuickRaceHandler(config *smconfig.Configuration, baseHandler *smweb.BaseHandler, managerFor ManagerFor) *QuickRaceHandler {
	return &QuickRaceHandler{
		config:      config,
		BaseHandler: baseHandler,
		managerFor:  managerFor,
	}
}

func (qrh *QuickRaceHandler) Create(w http.ResponseWriter, r *http.Request) {
	manager, ok := qrh.manager(w, r)

	if !ok {
		return
	}

	quickRaceData, err := manager.BuildRaceOpts(r)

	if err != nil {
		logrus.WithError(err).Errorf("couldn't build quick race")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	qrh.ViewRenderer.MustLoadTemplate(w, r, "quick-race.html", quickRaceData)
}

func (qrh *QuickRaceHandler) Submit(w http.ResponseWriter, r *http.Request) {
	manager, ok := qrh.manager(w, r)

	if !ok {
		return
	}

	err := manager.SetupQuickRace(r)

	if err == ErrMustSubmitCar {
		smweb.AddErrorFlash(w, r, "You must choose at least one car!")
		http.Redirect(w, r, r.Referer(), http.StatusFound)
		return
	} else if err != nil {
		logrus.WithError(err).Errorf("couldn't apply quick race")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	smweb.AddFlash(w, r, "Quick race successfully started!")

	if qrh.config.Server.PerformanceMode {
		http.Redirect(w, r, "/", http.StatusFound)
	} else {
		http.Redirect(w, r, smweb.ServerURL(r, "/live-timing"), http.StatusFound)
	}
}
