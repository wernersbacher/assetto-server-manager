package race

import (
	"errors"
	"net/http"

	"github.com/sirupsen/logrus"

	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

// ManagerFor returns the race manager of the server slot a request is for. The resolver provides it, as the handlers
// cannot know the slots.
type ManagerFor func(r *http.Request) (*RaceManager, error)

func managerOrError(managerFor ManagerFor, w http.ResponseWriter, r *http.Request) (*RaceManager, bool) {
	manager, err := managerFor(r)

	if err != nil {
		logrus.WithError(err).Error("couldn't find the race manager of the server")
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return nil, false
	}

	return manager, true
}

func (crh *CustomRaceHandler) manager(w http.ResponseWriter, r *http.Request) (*RaceManager, bool) {
	return managerOrError(crh.managerFor, w, r)
}

func (qrh *QuickRaceHandler) manager(w http.ResponseWriter, r *http.Request) (*RaceManager, bool) {
	return managerOrError(qrh.managerFor, w, r)
}

// ErrServerBusy is returned when an event is started on a server which already runs an event.
var ErrServerBusy = errors.New("servermanager: the server is already running an event")

// CheckCanStart returns ErrServerBusy if the server of this manager already runs an event. Events which a user starts
// by hand are rejected then instead of replacing the running event; scheduled starts and the race loop do not check.
func (rm *RaceManager) CheckCanStart() error {
	if rm.process.IsRunning() {
		return ErrServerBusy
	}

	return nil
}

// flashServerBusy tells the user that an event can't be started because the server is busy, and sends them back. It
// returns false if err is not ErrServerBusy.
func flashServerBusy(w http.ResponseWriter, r *http.Request, err error) bool {
	if !errors.Is(err, ErrServerBusy) {
		return false
	}

	smweb.AddErrorFlash(w, r, smweb.T(r, "flash.race.server_busy"))

	if referer := r.Referer(); referer != "" {
		http.Redirect(w, r, referer, http.StatusFound)
	} else {
		http.Redirect(w, r, smweb.ServerURL(r, "/custom"), http.StatusFound)
	}

	return true
}
