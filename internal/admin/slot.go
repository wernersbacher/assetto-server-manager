package admin

import (
	"net/http"

	"github.com/sirupsen/logrus"

	smchampionship "github.com/wernersbacher/ACPitwall/internal/championship"
	smrace "github.com/wernersbacher/ACPitwall/internal/race"
	"github.com/wernersbacher/ACPitwall/internal/racecontrol"
	smraceweekend "github.com/wernersbacher/ACPitwall/internal/raceweekend"
	smserver "github.com/wernersbacher/ACPitwall/internal/server"
	"github.com/wernersbacher/ACPitwall/internal/serverenv"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
)

// Slot is the part of a server slot the administration handlers work with.
type Slot struct {
	// Store is the view of the store for the slot (see smstore.ForServer).
	Store smstore.Store
	// ConfigLocks are the server option fields which config.yml fixes for this slot (read-only in the UI).
	ConfigLocks         smstore.LockedFields
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
