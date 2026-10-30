package race

import (
	"net/http"

	"github.com/sirupsen/logrus"
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
