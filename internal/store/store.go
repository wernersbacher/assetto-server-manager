package store

import (
	"errors"

	"github.com/google/uuid"
	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smplugins "github.com/wernersbacher/ACPitwall/internal/plugins"
)

// Option configures a Store.
type Option func(*storeOptions)

type storeOptions struct {
	installPath func() string
}

// WithInstallPath sets the function which returns the install path of the assetto corsa server, which the default
// plugin configuration is based on. It is a function because the path is only set after the store has been opened.
func WithInstallPath(installPath func() string) Option {
	return func(o *storeOptions) {
		o.installPath = installPath
	}
}

func newStoreOptions(opts []Option) storeOptions {
	var o storeOptions

	for _, opt := range opts {
		opt(&o)
	}

	return o
}

func (o storeOptions) serverInstallPath() string {
	if o.installPath == nil {
		return ""
	}

	return o.installPath()
}

type Store interface {
	// Custom Races
	UpsertCustomRace(race *smmodel.CustomRace) error
	FindCustomRaceByID(uuid string) (*smmodel.CustomRace, error)
	ListCustomRaces() ([]*smmodel.CustomRace, error)
	DeleteCustomRace(race *smmodel.CustomRace) error

	// Entrants
	UpsertEntrant(entrant serverconfig.Entrant) error
	ListEntrants() ([]*serverconfig.Entrant, error)
	DeleteEntrant(id string) error

	// Server Options
	UpsertServerOptions(so *serverconfig.GlobalServerConfig) error
	LoadServerOptions() (*serverconfig.GlobalServerConfig, error)
	// UpsertServerOptionsFor and LoadServerOptionsFor store the server options of one server slot. The default slot
	// (empty id) uses the same storage as UpsertServerOptions and LoadServerOptions. A slot without stored options
	// gets the defaults, which are persisted, like LoadServerOptions.
	UpsertServerOptionsFor(id smmodel.ServerID, so *serverconfig.GlobalServerConfig) error
	LoadServerOptionsFor(id smmodel.ServerID) (*serverconfig.GlobalServerConfig, error)

	// Championships
	UpsertChampionship(c *smmodel.Championship) error
	ListChampionships() ([]*smmodel.Championship, error)
	LoadChampionship(id string) (*smmodel.Championship, error)
	DeleteChampionship(id string) error

	// Live Timings
	UpsertLiveTimingsData(*smmodel.LiveTimingsPersistedData) error
	LoadLiveTimingsData() (*smmodel.LiveTimingsPersistedData, error)
	UpsertLastRaceEvent(r smmodel.RaceEvent) error
	LoadLastRaceEvent() (smmodel.RaceEvent, error)
	ClearLastRaceEvent() error
	// The ...For variants store the live timings and the last race event of one server slot. The default slot (empty
	// id) uses the same storage as the methods without an id.
	UpsertLiveTimingsDataFor(id smmodel.ServerID, lt *smmodel.LiveTimingsPersistedData) error
	LoadLiveTimingsDataFor(id smmodel.ServerID) (*smmodel.LiveTimingsPersistedData, error)
	UpsertLastRaceEventFor(id smmodel.ServerID, r smmodel.RaceEvent) error
	LoadLastRaceEventFor(id smmodel.ServerID) (smmodel.RaceEvent, error)
	ClearLastRaceEventFor(id smmodel.ServerID) error

	UpsertLiveFrames([]string) error
	ListPrevFrames() ([]string, error)

	// Meta
	SetMeta(key string, value interface{}) error
	GetMeta(key string, out interface{}) error

	// Accounts
	ListAccounts() ([]*smmodel.Account, error)
	UpsertAccount(a *smmodel.Account) error
	FindAccountByName(name string) (*smmodel.Account, error)
	FindAccountByID(id string) (*smmodel.Account, error)
	DeleteAccount(id string) error

	// Audit Log
	GetAuditEntries() ([]*smmodel.AuditEntry, error)
	AddAuditEntry(entry *smmodel.AuditEntry) error

	// Race Weekend
	ListRaceWeekends() ([]*smmodel.RaceWeekend, error)
	UpsertRaceWeekend(rw *smmodel.RaceWeekend) error
	LoadRaceWeekend(id string) (*smmodel.RaceWeekend, error)
	DeleteRaceWeekend(id string) error

	// Stracker Options
	UpsertStrackerOptions(sto *smplugins.StrackerConfiguration) error
	LoadStrackerOptions() (*smplugins.StrackerConfiguration, error)
	// The ...For variants of the plugin options (stracker, KissMyRank, Real Penalty) store the options of one server
	// slot. The default slot (empty id) uses the same storage as the methods without an id.
	UpsertStrackerOptionsFor(id smmodel.ServerID, sto *smplugins.StrackerConfiguration) error
	LoadStrackerOptionsFor(id smmodel.ServerID) (*smplugins.StrackerConfiguration, error)

	// KissMyRank Options
	UpsertKissMyRankOptions(kmr *smplugins.KissMyRankConfig) error
	LoadKissMyRankOptions() (*smplugins.KissMyRankConfig, error)
	UpsertKissMyRankOptionsFor(id smmodel.ServerID, kmr *smplugins.KissMyRankConfig) error
	LoadKissMyRankOptionsFor(id smmodel.ServerID) (*smplugins.KissMyRankConfig, error)

	// RealPenalty options
	UpsertRealPenaltyOptions(rpc *smplugins.RealPenaltyConfig) error
	LoadRealPenaltyOptions() (*smplugins.RealPenaltyConfig, error)
	UpsertRealPenaltyOptionsFor(id smmodel.ServerID, rpc *smplugins.RealPenaltyConfig) error
	LoadRealPenaltyOptionsFor(id smmodel.ServerID) (*smplugins.RealPenaltyConfig, error)
}

// autoFillEntrant prepares an entrant for storage in the AutoFill entrant list. The list is used to fill in
// driver details (name, team, GUID) when building entry lists, so race specific values (car, skin, spectator mode)
// are cleared out and not stored.
func autoFillEntrant(entrant serverconfig.Entrant) serverconfig.Entrant {
	entrant.Model = ""
	entrant.Skin = ""
	entrant.SpectatorMode = 0

	return entrant
}

func loadChampionshipRaceWeekends(championship *smmodel.Championship, store Store) error {
	var err error

	for _, event := range championship.Events {
		if event.IsRaceWeekend() {
			event.RaceWeekend, err = store.LoadRaceWeekend(event.RaceWeekendID.String())

			if err != nil {
				return err
			}
		}
	}

	return nil
}

// ErrCustomRaceNotFound is returned when a custom race does not exist.
var ErrCustomRaceNotFound = errors.New("servermanager: custom race not found")

const serverIDMetaKey = "server_id"

// InitServerID loads the id of this ACPitwall instance from the store. If the instance does not have an id yet
// a new one is created and stored.
func InitServerID(store Store) (smmodel.ServerID, error) {
	var serverID smmodel.ServerID

	err := store.GetMeta(serverIDMetaKey, &serverID)

	if err == ErrValueNotSet {
		serverID = smmodel.ServerID(uuid.New().String())

		if err := store.SetMeta(serverIDMetaKey, serverID); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}

	return serverID, nil
}
