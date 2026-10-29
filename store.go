package servermanager

import (
	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smplugins "github.com/wernersbacher/assetto-server-manager/internal/plugins"
)

type Store interface {
	// Custom Races
	UpsertCustomRace(race *CustomRace) error
	FindCustomRaceByID(uuid string) (*CustomRace, error)
	ListCustomRaces() ([]*CustomRace, error)
	DeleteCustomRace(race *CustomRace) error

	// Entrants
	UpsertEntrant(entrant serverconfig.Entrant) error
	ListEntrants() ([]*serverconfig.Entrant, error)
	DeleteEntrant(id string) error

	// Server Options
	UpsertServerOptions(so *serverconfig.GlobalServerConfig) error
	LoadServerOptions() (*serverconfig.GlobalServerConfig, error)

	// Championships
	UpsertChampionship(c *Championship) error
	ListChampionships() ([]*Championship, error)
	LoadChampionship(id string) (*Championship, error)
	DeleteChampionship(id string) error

	// Live Timings
	UpsertLiveTimingsData(*LiveTimingsPersistedData) error
	LoadLiveTimingsData() (*LiveTimingsPersistedData, error)
	UpsertLastRaceEvent(r RaceEvent) error
	LoadLastRaceEvent() (RaceEvent, error)
	ClearLastRaceEvent() error

	UpsertLiveFrames([]string) error
	ListPrevFrames() ([]string, error)

	// Meta
	SetMeta(key string, value interface{}) error
	GetMeta(key string, out interface{}) error

	// Accounts
	ListAccounts() ([]*Account, error)
	UpsertAccount(a *Account) error
	FindAccountByName(name string) (*Account, error)
	FindAccountByID(id string) (*Account, error)
	DeleteAccount(id string) error

	// Audit Log
	GetAuditEntries() ([]*AuditEntry, error)
	AddAuditEntry(entry *AuditEntry) error

	// Race Weekend
	ListRaceWeekends() ([]*RaceWeekend, error)
	UpsertRaceWeekend(rw *RaceWeekend) error
	LoadRaceWeekend(id string) (*RaceWeekend, error)
	DeleteRaceWeekend(id string) error

	// Stracker Options
	UpsertStrackerOptions(sto *smplugins.StrackerConfiguration) error
	LoadStrackerOptions() (*smplugins.StrackerConfiguration, error)

	// KissMyRank Options
	UpsertKissMyRankOptions(kmr *smplugins.KissMyRankConfig) error
	LoadKissMyRankOptions() (*smplugins.KissMyRankConfig, error)

	// RealPenalty options
	UpsertRealPenaltyOptions(rpc *smplugins.RealPenaltyConfig) error
	LoadRealPenaltyOptions() (*smplugins.RealPenaltyConfig, error)
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

func loadChampionshipRaceWeekends(championship *Championship, store Store) error {
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
