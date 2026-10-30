package store

import (
	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
)

// slotStore is a Store whose server options, live timings and last race event are those of one server slot. Everything
// else is shared with the wrapped store.
type slotStore struct {
	Store

	id smmodel.ServerID
}

// ForServer returns a view of the store for one server slot: the methods without an id of the server options, the
// live timings and the last race event work on the data of the slot, so the managers of a slot need not know about
// slots. The id is the one the slot is stored under, which is empty for the default slot.
func ForServer(store Store, id smmodel.ServerID) Store {
	if id == "" {
		return store
	}

	return &slotStore{Store: store, id: id}
}

func (s *slotStore) UpsertServerOptions(so *serverconfig.GlobalServerConfig) error {
	return s.Store.UpsertServerOptionsFor(s.id, so)
}

func (s *slotStore) LoadServerOptions() (*serverconfig.GlobalServerConfig, error) {
	return s.Store.LoadServerOptionsFor(s.id)
}

func (s *slotStore) UpsertLiveTimingsData(lt *smmodel.LiveTimingsPersistedData) error {
	return s.Store.UpsertLiveTimingsDataFor(s.id, lt)
}

func (s *slotStore) LoadLiveTimingsData() (*smmodel.LiveTimingsPersistedData, error) {
	return s.Store.LoadLiveTimingsDataFor(s.id)
}

func (s *slotStore) UpsertLastRaceEvent(r smmodel.RaceEvent) error {
	return s.Store.UpsertLastRaceEventFor(s.id, r)
}

func (s *slotStore) LoadLastRaceEvent() (smmodel.RaceEvent, error) {
	return s.Store.LoadLastRaceEventFor(s.id)
}

func (s *slotStore) ClearLastRaceEvent() error {
	return s.Store.ClearLastRaceEventFor(s.id)
}
