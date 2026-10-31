package store

import (
	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smplugins "github.com/wernersbacher/ACPitwall/internal/plugins"
)

// slotStore is a Store whose server options, plugin options, live timings and last race event are those of one server
// slot. Everything else is shared with the wrapped store.
type slotStore struct {
	Store

	id smmodel.ServerID
}

// ForServer returns a view of the store for one server slot: the methods without an id of the server options, the
// plugin options (stracker, KissMyRank, Real Penalty), the live timings and the last race event work on the data of
// the slot, so the managers of a slot need not know about slots. The id is the one the slot is stored under, which is
// empty for the default slot.
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

func (s *slotStore) UpsertStrackerOptions(sto *smplugins.StrackerConfiguration) error {
	return s.Store.UpsertStrackerOptionsFor(s.id, sto)
}

func (s *slotStore) LoadStrackerOptions() (*smplugins.StrackerConfiguration, error) {
	return s.Store.LoadStrackerOptionsFor(s.id)
}

func (s *slotStore) UpsertKissMyRankOptions(kmr *smplugins.KissMyRankConfig) error {
	return s.Store.UpsertKissMyRankOptionsFor(s.id, kmr)
}

func (s *slotStore) LoadKissMyRankOptions() (*smplugins.KissMyRankConfig, error) {
	return s.Store.LoadKissMyRankOptionsFor(s.id)
}

func (s *slotStore) UpsertRealPenaltyOptions(rpc *smplugins.RealPenaltyConfig) error {
	return s.Store.UpsertRealPenaltyOptionsFor(s.id, rpc)
}

func (s *slotStore) LoadRealPenaltyOptions() (*smplugins.RealPenaltyConfig, error) {
	return s.Store.LoadRealPenaltyOptionsFor(s.id)
}
