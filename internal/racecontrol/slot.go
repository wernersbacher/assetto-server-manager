package racecontrol

import (
	"net/http"

	"github.com/sirupsen/logrus"

	smserver "github.com/wernersbacher/ACPitwall/internal/server"
	"github.com/wernersbacher/ACPitwall/internal/serverenv"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
)

// Slot is the part of a server slot the live timing handlers work with.
type Slot struct {
	// Store is the view of the store for the slot (see smstore.ForServer).
	Store       smstore.Store
	Env         *serverenv.Env
	Process     smserver.ServerProcess
	RaceManager CurrentRaceProvider
	RaceControl *RaceControl
	Hub         *RaceControlHub
}

// SlotFor returns the server slot a request is for. The resolver provides it, as the handlers cannot know the slots.
type SlotFor func(r *http.Request) (*Slot, error)

func (rch *RaceControlHandler) slot(w http.ResponseWriter, r *http.Request) (*Slot, bool) {
	slot, err := rch.slotFor(r)

	if err != nil {
		logrus.WithError(err).Error("couldn't find the server")
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return nil, false
	}

	return slot, true
}
