package servermanager

import (
	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	smplugins "github.com/wernersbacher/assetto-server-manager/internal/plugins"
)

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
