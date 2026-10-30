package servermanager

import (
	"fmt"
	"net/http"

	"github.com/wernersbacher/assetto-server-manager/internal/admin"
	smchampionship "github.com/wernersbacher/assetto-server-manager/internal/championship"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	smrace "github.com/wernersbacher/assetto-server-manager/internal/race"
	"github.com/wernersbacher/assetto-server-manager/internal/racecontrol"
	smraceweekend "github.com/wernersbacher/assetto-server-manager/internal/raceweekend"
	"github.com/wernersbacher/assetto-server-manager/internal/servers"
	smweb "github.com/wernersbacher/assetto-server-manager/internal/web"
)

// The handler packages cannot import internal/servers, so they get functions which look up the server slot a request
// is for (see smweb.RequestedServerID). Requests without a server parameter are for the default slot.

// slotForRequest returns the server slot a request is for.
func (r *Resolver) slotForRequest(req *http.Request) (*servers.Slot, error) {
	id := smweb.RequestedServerID(req)

	slot, ok := r.registry.ByID(id)

	if !ok {
		return nil, fmt.Errorf("unknown server %q", id)
	}

	return r.resolveSlot(slot), nil
}

// isServerID tells if the id is that of a server slot.
func (r *Resolver) isServerID(id smmodel.ServerID) bool {
	_, ok := r.registry.ByID(id)

	return ok
}

func (r *Resolver) raceManagerFor(req *http.Request) (*smrace.RaceManager, error) {
	slot, err := r.slotForRequest(req)

	if err != nil {
		return nil, err
	}

	return slot.RaceManager, nil
}

func (r *Resolver) adminSlotFor(req *http.Request) (*admin.Slot, error) {
	slot, err := r.slotForRequest(req)

	if err != nil {
		return nil, err
	}

	return adminSlot(slot), nil
}

func adminSlot(slot *servers.Slot) *admin.Slot {
	return &admin.Slot{
		Store:               slot.Store,
		Env:                 slot.Env,
		Process:             slot.Process,
		RaceControl:         slot.RaceControl,
		RaceManager:         slot.RaceManager,
		ChampionshipManager: slot.ChampionshipManager,
		RaceWeekendManager:  slot.RaceWeekendManager,
	}
}

func (r *Resolver) raceControlSlotFor(req *http.Request) (*racecontrol.Slot, error) {
	slot, err := r.slotForRequest(req)

	if err != nil {
		return nil, err
	}

	return &racecontrol.Slot{
		Store:       slot.Store,
		Env:         slot.Env,
		Process:     slot.Process,
		RaceManager: slot.RaceManager,
		RaceControl: slot.RaceControl,
		Hub:         slot.RaceControlHub,
	}, nil
}

// serverContextFor is what the template renderer shows about the slot of a request.
func (r *Resolver) serverContextFor(req *http.Request) (*smweb.ServerContext, error) {
	slot, err := r.slotForRequest(req)

	if err != nil {
		return nil, err
	}

	return &smweb.ServerContext{
		ServerRef: smweb.ServerRef{ID: slot.ID(), Name: slot.Name},
		Store:     slot.Store,
		State:     slot.Process,
	}, nil
}

// adminSlots lists all server slots with what the administration handlers work with.
func (r *Resolver) adminSlots() []admin.NamedSlot {
	slots := make([]admin.NamedSlot, 0, r.registry.Len())

	for _, slot := range r.registry.All() {
		slots = append(slots, admin.NamedSlot{
			ServerRef: smweb.ServerRef{ID: slot.ID(), Name: slot.Name},
			Slot:      adminSlot(r.resolveSlot(slot)),
		})
	}

	return slots
}

// serverRefs lists all server slots for the templates.
func (r *Resolver) serverRefs() []smweb.ServerRef {
	refs := make([]smweb.ServerRef, 0, r.registry.Len())

	for _, slot := range r.registry.All() {
		refs = append(refs, smweb.ServerRef{ID: slot.ID(), Name: slot.Name, Running: r.resolveSlot(slot).Process.IsRunning()})
	}

	return refs
}

func (r *Resolver) championshipManagerFor(req *http.Request) (*smchampionship.ChampionshipManager, error) {
	slot, err := r.slotForRequest(req)

	if err != nil {
		return nil, err
	}

	return slot.ChampionshipManager, nil
}

func (r *Resolver) raceWeekendManagerFor(req *http.Request) (*smraceweekend.RaceWeekendManager, error) {
	slot, err := r.slotForRequest(req)

	if err != nil {
		return nil, err
	}

	return slot.RaceWeekendManager, nil
}
