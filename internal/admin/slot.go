package admin

import (
	"net/http"

	"github.com/sirupsen/logrus"

	smchampionship "github.com/wernersbacher/assetto-server-manager/internal/championship"
	smrace "github.com/wernersbacher/assetto-server-manager/internal/race"
	"github.com/wernersbacher/assetto-server-manager/internal/racecontrol"
	smraceweekend "github.com/wernersbacher/assetto-server-manager/internal/raceweekend"
	smserver "github.com/wernersbacher/assetto-server-manager/internal/server"
	"github.com/wernersbacher/assetto-server-manager/internal/serverenv"
	smstore "github.com/wernersbacher/assetto-server-manager/internal/store"
)

// Slot is the part of a server slot the administration handlers work with.
type Slot struct {
	// Store is the view of the store for the slot (see smstore.ForServer).
	Store               smstore.Store
	Env                 *serverenv.Env
	Process             smserver.ServerProcess
	RaceControl         *racecontrol.RaceControl
	RaceManager         *smrace.RaceManager
	ChampionshipManager *smchampionship.ChampionshipManager
	RaceWeekendManager  *smraceweekend.RaceWeekendManager
}

// SlotFor returns the server slot a request is for. The resolver provides it, as the handlers cannot know the slots.
type SlotFor func(r *http.Request) (*Slot, error)

func slotOrError(slotFor SlotFor, w http.ResponseWriter, r *http.Request) (*Slot, bool) {
	slot, err := slotFor(r)

	if err != nil {
		logrus.WithError(err).Error("couldn't find the server")
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return nil, false
	}

	return slot, true
}

func (sah *ServerAdministrationHandler) slot(w http.ResponseWriter, r *http.Request) (*Slot, bool) {
	return slotOrError(sah.slotFor, w, r)
}

func (edh *EventDetailsHandler) slot(w http.ResponseWriter, r *http.Request) (*Slot, bool) {
	return slotOrError(edh.slotFor, w, r)
}
