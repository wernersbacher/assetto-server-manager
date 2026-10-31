// Package servers holds the server slots of ACPitwall: every slot is one Assetto Corsa server with its own
// working directory and its own runtime stack (server process, race control, race, championship and race weekend
// managers). The Registry looks the slots up. The package sits above raceweekend, so the handler packages, which
// cannot import it, get small provider functions from the resolver instead.
package servers

import (
	"fmt"
	"path/filepath"

	smchampionship "github.com/wernersbacher/ACPitwall/internal/championship"
	"github.com/wernersbacher/ACPitwall/internal/config"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smrace "github.com/wernersbacher/ACPitwall/internal/race"
	"github.com/wernersbacher/ACPitwall/internal/racecontrol"
	smraceweekend "github.com/wernersbacher/ACPitwall/internal/raceweekend"
	smserver "github.com/wernersbacher/ACPitwall/internal/server"
	"github.com/wernersbacher/ACPitwall/internal/serverenv"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
	"github.com/wernersbacher/ACPitwall/pkg/udp"
)

// Slot is one Assetto Corsa server and its runtime stack. NewRegistry fills the configuration and the Env; the
// resolver sets the runtime components afterwards. They are plain fields: they must all be set before the slot is
// used concurrently and must not be changed afterwards.
type Slot struct {
	Name           string
	InstallPath    string
	ExecutablePath string

	// Env is the environment of the managers of this slot (install path and server id of the slot, results
	// directories of all slots).
	Env *serverenv.Env

	// Store is the view of the store for the slot (see smstore.ForServer): its server options, live timings and last
	// race event are those of the slot.
	Store smstore.Store

	Process               smserver.ServerProcess
	ContentManagerWrapper *smserver.ContentManagerWrapper
	RaceControl           *racecontrol.RaceControl
	RaceControlHub        *racecontrol.RaceControlHub
	RaceManager           *smrace.RaceManager
	ChampionshipManager   *smchampionship.ChampionshipManager
	RaceWeekendManager    *smraceweekend.RaceWeekendManager
}

// ID is the id of the slot. It is read from the Env, as the id of the default slot is only known once the store has
// been initialised.
func (s *Slot) ID() smmodel.ServerID {
	return s.Env.ServerID()
}

// Registry is the list of the server slots, in the order of the configuration. The first slot is the default slot.
// The list is not changed after NewRegistry.
type Registry struct {
	slots []*Slot
}

// NewRegistry builds a slot, without runtime components, for each of the configured slots. The server id of the
// default slot (the one derived from the steam section) is that of state, the others have the id of their
// configuration. Every Env returns the results directories of all slots.
func NewRegistry(slots []config.Slot, state *serverenv.State, panicCapture func(fn func()), debug bool) (*Registry, error) {
	if len(slots) == 0 {
		return nil, fmt.Errorf("servers: at least one server slot is required")
	}

	resultsDirs := func() []string {
		dirs := make([]string, 0, len(slots))

		for _, slot := range slots {
			dirs = append(dirs, filepath.Join(slot.InstallPath, "results"))
		}

		return dirs
	}

	registry := &Registry{}
	seen := make(map[string]bool)

	for _, slot := range slots {
		if seen[slot.ID] {
			return nil, fmt.Errorf("servers: duplicate server id %q", slot.ID)
		}

		seen[slot.ID] = true

		serverID := state.ServerID

		if !slot.IsDefault() {
			id := smmodel.ServerID(slot.ID)
			serverID = func() smmodel.ServerID { return id }
		}

		registry.slots = append(registry.slots, &Slot{
			Name:           slot.Name,
			InstallPath:    slot.InstallPath,
			ExecutablePath: slot.ExecutablePath,
			Env:            state.EnvFor(slot.InstallPath, serverID, resultsDirs, panicCapture, debug),
		})
	}

	return registry, nil
}

// Default returns the first slot.
func (r *Registry) Default() *Slot {
	return r.slots[0]
}

// ByID returns the slot with the given id. The empty id is the default slot. It returns false if there is no such slot.
func (r *Registry) ByID(id smmodel.ServerID) (*Slot, bool) {
	if id == "" {
		return r.Default(), true
	}

	for _, slot := range r.slots {
		if slot.ID() == id {
			return slot, true
		}
	}

	return nil, false
}

// All returns the slots in the order of the configuration. The slice must not be modified.
func (r *Registry) All() []*Slot {
	return r.slots
}

// Len returns the number of slots.
func (r *Registry) Len() int {
	return len(r.slots)
}

// DispatchUDP hands a message of this slot's server to the managers of this slot, and only to them. Messages of other
// slots never get here, as every process is given the callback of its own slot. Race control is skipped in
// performance mode, car updates only go to race control.
func (s *Slot) DispatchUDP(message udp.Message, performanceMode bool) {
	if !performanceMode {
		s.RaceControl.UDPCallback(message)
	}

	if message.Event() != udp.EventCarUpdate {
		s.ChampionshipManager.ChampionshipEventCallback(message)
		s.RaceWeekendManager.UDPCallback(message)
		s.RaceManager.LoopCallback(message)
		s.ContentManagerWrapper.UDPCallback(message)
	}
}
