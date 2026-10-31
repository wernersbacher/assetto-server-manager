package pluginhttp

import (
	"net/http"

	"github.com/sirupsen/logrus"

	"github.com/wernersbacher/ACPitwall/internal/serverenv"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
)

// Slot is the part of a server slot the plugin handlers work with.
type Slot struct {
	// Store is the view of the store for the slot (see smstore.ForServer): the plugin options are those of the slot.
	Store smstore.Store

	// Env is the environment of the slot, the plugins are installed in its install path.
	Env *serverenv.Env
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
