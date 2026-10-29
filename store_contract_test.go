package servermanager

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/etcd-io/bbolt"
	"github.com/google/uuid"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	smplugins "github.com/wernersbacher/assetto-server-manager/internal/plugins"
)

// The tests in this file form a contract test suite for the Store interface. Every test runs identically against
// both implementations (BoltStore and JSONStore), including the errors returned for missing data, so both stores
// must behave identically.

type storeContractImpl struct {
	name string
	// new creates a fresh, empty Store inside dir. The returned func releases any resources held by the store.
	new func(t *testing.T, dir string) (Store, func())
}

var storeContractImpls = []storeContractImpl{
	{
		name: "BoltStore",
		new: func(t *testing.T, dir string) (Store, func()) {
			db, err := bbolt.Open(filepath.Join(dir, "server_manager.db"), 0644, &bbolt.Options{Timeout: 2 * time.Second})

			if err != nil {
				t.Fatalf("could not open bolt db: %s", err)
			}

			return NewBoltStore(db), func() { _ = db.Close() }
		},
	},
	{
		name: "JSONStore",
		new: func(t *testing.T, dir string) (Store, func()) {
			return NewJSONStore(filepath.Join(dir, "store"), filepath.Join(dir, "store-shared")), func() {}
		},
	},
}

// runStoreContract runs fn once per Store implementation, each time with a fresh, empty store.
func runStoreContract(t *testing.T, fn func(t *testing.T, store Store)) {
	for _, impl := range storeContractImpls {
		impl := impl

		t.Run(impl.name, func(t *testing.T) {
			dir, err := ioutil.TempDir("", "asm-store-contract")

			if err != nil {
				t.Fatal(err)
			}

			defer os.RemoveAll(dir)

			store, closeFn := impl.new(t, dir)
			defer closeFn()

			fn(t, store)
		})
	}
}

func TestStoreContract_CustomRaces(t *testing.T) {
	runStoreContract(t, func(t *testing.T, store Store) {
		races, err := store.ListCustomRaces()

		if err != nil {
			t.Fatalf("list on empty store: %s", err)
		}

		if len(races) != 0 {
			t.Fatalf("expected no custom races in empty store, got %d", len(races))
		}

		if _, err := store.FindCustomRaceByID(uuid.New().String()); err != ErrCustomRaceNotFound {
			t.Errorf("expected ErrCustomRaceNotFound on fresh store, got: %v", err)
		}

		raceA := &CustomRace{Name: "Race A", UUID: uuid.New(), Starred: true}
		raceB := &CustomRace{Name: "Race B", UUID: uuid.New()}
		raceB.RaceConfig.Track = "ks_vallelunga"

		for _, race := range []*CustomRace{raceA, raceB} {
			if err := store.UpsertCustomRace(race); err != nil {
				t.Fatalf("upsert custom race: %s", err)
			}

			if race.Updated.IsZero() {
				t.Errorf("expected UpsertCustomRace to set Updated on %s", race.Name)
			}
		}

		found, err := store.FindCustomRaceByID(raceB.UUID.String())

		if err != nil {
			t.Fatalf("find custom race: %s", err)
		}

		if found.Name != "Race B" || found.UUID != raceB.UUID || found.RaceConfig.Track != "ks_vallelunga" {
			t.Errorf("custom race did not round-trip: %+v", found)
		}

		if !found.Deleted.IsZero() {
			t.Errorf("expected a fresh custom race not to be deleted")
		}

		if _, err := store.FindCustomRaceByID(uuid.New().String()); err != ErrCustomRaceNotFound {
			t.Errorf("expected ErrCustomRaceNotFound for a missing custom race, got: %v", err)
		}

		races, err = store.ListCustomRaces()

		if err != nil {
			t.Fatal(err)
		}

		assertStringSet(t, "custom races", customRaceNames(races), "Race A", "Race B")

		// updating an existing race overwrites it rather than adding a new one
		raceA.Name = "Race A (renamed)"

		if err := store.UpsertCustomRace(raceA); err != nil {
			t.Fatal(err)
		}

		races, err = store.ListCustomRaces()

		if err != nil {
			t.Fatal(err)
		}

		assertStringSet(t, "custom races after update", customRaceNames(races), "Race A (renamed)", "Race B")

		// delete is a soft delete in both stores: the race disappears from the list, but can still be found by ID
		if err := store.DeleteCustomRace(raceA); err != nil {
			t.Fatalf("delete custom race: %s", err)
		}

		races, err = store.ListCustomRaces()

		if err != nil {
			t.Fatal(err)
		}

		assertStringSet(t, "custom races after delete", customRaceNames(races), "Race B")

		deleted, err := store.FindCustomRaceByID(raceA.UUID.String())

		if err != nil {
			t.Fatalf("expected soft-deleted custom race to still be findable, got: %s", err)
		}

		if deleted.Deleted.IsZero() {
			t.Errorf("expected soft-deleted custom race to have Deleted set")
		}
	})
}

func TestStoreContract_Entrants(t *testing.T) {
	runStoreContract(t, func(t *testing.T, store Store) {
		entrants, err := store.ListEntrants()

		if err != nil {
			t.Fatalf("list on empty store: %s", err)
		}

		if len(entrants) != 0 {
			t.Fatalf("expected no entrants in empty store, got %d", len(entrants))
		}

		// race specific values (Model, Skin, SpectatorMode) are not stored in the AutoFill entrant list
		alice := serverconfig.Entrant{
			Name:          "Alice",
			Team:          "Team A",
			GUID:          "76561198000000001",
			Ballast:       10,
			Restrictor:    5,
			Model:         "ks_mazda_mx5_cup",
			Skin:          "red",
			SpectatorMode: 1,
		}
		bob := serverconfig.Entrant{Name: "Bob", Team: "Team B", GUID: "76561198000000002"}
		// entrants without a GUID are identified by their name (Entrant.ID())
		carol := serverconfig.Entrant{Name: "Carol", Team: "Team C"}
		dave := serverconfig.Entrant{Name: "Dave", Team: "Team D"}

		for _, e := range []serverconfig.Entrant{alice, bob, carol, dave} {
			if err := store.UpsertEntrant(e); err != nil {
				t.Fatalf("upsert entrant: %s", err)
			}
		}

		entrants, err = store.ListEntrants()

		if err != nil {
			t.Fatal(err)
		}

		assertStringSet(t, "entrants", entrantIDs(entrants), alice.GUID, bob.GUID, "Carol", "Dave")

		for _, e := range entrants {
			if e.GUID != alice.GUID {
				continue
			}

			if e.Name != "Alice" || e.Team != "Team A" || e.Ballast != 10 || e.Restrictor != 5 {
				t.Errorf("entrant did not round-trip: %+v", e)
			}

			if e.Model != "" || e.Skin != "" || e.SpectatorMode != 0 {
				t.Errorf("expected race specific values to be cleared, got model=%q skin=%q spectatorMode=%d", e.Model, e.Skin, e.SpectatorMode)
			}
		}

		// upserting an existing entrant overwrites it rather than adding a new one
		aliceUpdated := alice
		aliceUpdated.Name = "Alice Updated"
		aliceUpdated.Team = "Team A2"

		carolUpdated := carol
		carolUpdated.Team = "Team C2"

		for _, e := range []serverconfig.Entrant{aliceUpdated, carolUpdated} {
			if err := store.UpsertEntrant(e); err != nil {
				t.Fatalf("upsert existing entrant: %s", err)
			}
		}

		entrants, err = store.ListEntrants()

		if err != nil {
			t.Fatal(err)
		}

		assertStringSet(t, "entrants after update", entrantIDs(entrants), alice.GUID, bob.GUID, "Carol", "Dave")

		for _, e := range entrants {
			switch e.ID() {
			case alice.GUID:
				if e.Name != "Alice Updated" || e.Team != "Team A2" {
					t.Errorf("expected entrant with GUID to be updated, got: %+v", e)
				}
			case "Carol":
				if e.Team != "Team C2" {
					t.Errorf("expected entrant without GUID to be updated, got: %+v", e)
				}
			case "Dave":
				if e.Team != "Team D" {
					t.Errorf("expected other entrant without GUID to be untouched, got: %+v", e)
				}
			}
		}

		for _, id := range []string{alice.ID(), carol.ID()} {
			if err := store.DeleteEntrant(id); err != nil {
				t.Fatalf("delete entrant: %s", err)
			}
		}

		entrants, err = store.ListEntrants()

		if err != nil {
			t.Fatal(err)
		}

		assertStringSet(t, "entrants after delete", entrantIDs(entrants), bob.GUID, "Dave")

		// deleting an unknown entrant is not an error
		if err := store.DeleteEntrant("unknown"); err != nil {
			t.Errorf("expected deleting an unknown entrant to succeed, got: %s", err)
		}

		entrants, err = store.ListEntrants()

		if err != nil {
			t.Fatal(err)
		}

		assertStringSet(t, "entrants after deleting unknown", entrantIDs(entrants), bob.GUID, "Dave")
	})
}

func TestStoreContract_Accounts(t *testing.T) {
	runStoreContract(t, func(t *testing.T, store Store) {
		accounts, err := store.ListAccounts()

		if err != nil {
			t.Fatalf("list on empty store: %s", err)
		}

		if len(accounts) != 0 {
			t.Fatalf("expected no accounts in empty store, got %d", len(accounts))
		}

		if _, err := store.FindAccountByName("nobody"); err != ErrAccountNotFound {
			t.Errorf("expected ErrAccountNotFound on fresh store, got: %v", err)
		}

		admin := smmodel.NewAccount(serverID, BuildVersion)
		admin.Name = "admin"
		admin.DriverName = "Admin Driver"
		admin.Groups = map[smmodel.ServerID]smmodel.Group{serverID: smmodel.GroupAdmin}

		user := smmodel.NewAccount(serverID, BuildVersion)
		user.Name = "user"

		for _, a := range []*smmodel.Account{admin, user} {
			if err := store.UpsertAccount(a); err != nil {
				t.Fatalf("upsert account: %s", err)
			}

			if a.Updated.IsZero() {
				t.Errorf("expected UpsertAccount to set Updated on %s", a.Name)
			}
		}

		byName, err := store.FindAccountByName("admin")

		if err != nil {
			t.Fatalf("find account by name: %s", err)
		}

		if byName.ID != admin.ID || byName.DriverName != "Admin Driver" || byName.Groups[serverID] != smmodel.GroupAdmin {
			t.Errorf("account did not round-trip: %+v", byName)
		}

		byID, err := store.FindAccountByID(user.ID.String())

		if err != nil {
			t.Fatalf("find account by id: %s", err)
		}

		if byID.Name != "user" {
			t.Errorf("expected account 'user', got %q", byID.Name)
		}

		if _, err := store.FindAccountByName("nobody"); err != ErrAccountNotFound {
			t.Errorf("expected ErrAccountNotFound for missing name, got: %v", err)
		}

		if _, err := store.FindAccountByID(uuid.New().String()); err != ErrAccountNotFound {
			t.Errorf("expected ErrAccountNotFound for missing id, got: %v", err)
		}

		accounts, err = store.ListAccounts()

		if err != nil {
			t.Fatal(err)
		}

		assertStringSet(t, "accounts", accountNames(accounts), "admin", "user")

		// delete is a soft delete: hidden from list and ID lookup, but still findable by name with Deleted set
		if err := store.DeleteAccount(user.ID.String()); err != nil {
			t.Fatalf("delete account: %s", err)
		}

		accounts, err = store.ListAccounts()

		if err != nil {
			t.Fatal(err)
		}

		assertStringSet(t, "accounts after delete", accountNames(accounts), "admin")

		if _, err := store.FindAccountByID(user.ID.String()); err != ErrAccountNotFound {
			t.Errorf("expected ErrAccountNotFound for deleted account by id, got: %v", err)
		}

		deleted, err := store.FindAccountByName("user")

		if err != nil {
			t.Fatalf("expected soft-deleted account to be findable by name, got: %s", err)
		}

		if deleted.Deleted.IsZero() {
			t.Errorf("expected soft-deleted account to have Deleted set")
		}

		if err := store.DeleteAccount(uuid.New().String()); err != ErrAccountNotFound {
			t.Errorf("expected ErrAccountNotFound deleting a missing account, got: %v", err)
		}
	})
}

func TestStoreContract_Meta(t *testing.T) {
	type metaValue struct {
		Name  string
		Count int
	}

	runStoreContract(t, func(t *testing.T, store Store) {
		var s string

		// missing key on a fresh store
		if err := store.GetMeta("missing", &s); err != ErrValueNotSet {
			t.Errorf("expected ErrValueNotSet on fresh store, got: %v", err)
		}

		if err := store.SetMeta("string-key", "hello"); err != nil {
			t.Fatalf("set meta: %s", err)
		}

		// missing key once other keys exist
		if err := store.GetMeta("missing", &s); err != ErrValueNotSet {
			t.Errorf("expected ErrValueNotSet for missing key, got: %v", err)
		}

		if err := store.GetMeta("string-key", &s); err != nil {
			t.Fatalf("get meta: %s", err)
		}

		if s != "hello" {
			t.Errorf("expected %q, got %q", "hello", s)
		}

		in := metaValue{Name: "struct", Count: 42}

		if err := store.SetMeta("struct-key", in); err != nil {
			t.Fatal(err)
		}

		var out metaValue

		if err := store.GetMeta("struct-key", &out); err != nil {
			t.Fatal(err)
		}

		if out != in {
			t.Errorf("expected %+v, got %+v", in, out)
		}

		// overwrite
		if err := store.SetMeta("string-key", "world"); err != nil {
			t.Fatal(err)
		}

		if err := store.GetMeta("string-key", &s); err != nil {
			t.Fatal(err)
		}

		if s != "world" {
			t.Errorf("expected overwritten value %q, got %q", "world", s)
		}
	})
}

func TestStoreContract_Championships(t *testing.T) {
	runStoreContract(t, func(t *testing.T, store Store) {
		championships, err := store.ListChampionships()

		if err != nil {
			t.Fatalf("list on empty store: %s", err)
		}

		if len(championships) != 0 {
			t.Fatalf("expected no championships in empty store, got %d", len(championships))
		}

		if _, err := store.LoadChampionship(uuid.New().String()); err != ErrChampionshipNotFound {
			t.Errorf("expected ErrChampionshipNotFound on fresh store, got: %v", err)
		}

		champA := NewChampionship("Champ A")
		champA.OpenEntrants = true
		champA.Events = append(champA.Events, NewChampionshipEvent())

		champB := NewChampionship("Champ B")

		for _, c := range []*Championship{champA, champB} {
			if err := store.UpsertChampionship(c); err != nil {
				t.Fatalf("upsert championship: %s", err)
			}

			if c.Updated.IsZero() {
				t.Errorf("expected UpsertChampionship to set Updated on %s", c.Name)
			}
		}

		loaded, err := store.LoadChampionship(champA.ID.String())

		if err != nil {
			t.Fatalf("load championship: %s", err)
		}

		if loaded.Name != "Champ A" || !loaded.OpenEntrants || len(loaded.Events) != 1 || loaded.Events[0].ID != champA.Events[0].ID {
			t.Errorf("championship did not round-trip: %+v", loaded)
		}

		if _, err := store.LoadChampionship(uuid.New().String()); err != ErrChampionshipNotFound {
			t.Errorf("expected ErrChampionshipNotFound for a missing championship, got: %v", err)
		}

		championships, err = store.ListChampionships()

		if err != nil {
			t.Fatal(err)
		}

		assertStringSet(t, "championships", championshipNames(championships), "Champ A", "Champ B")

		// delete is a soft delete in both stores: hidden from the list, still loadable with Deleted set
		if err := store.DeleteChampionship(champB.ID.String()); err != nil {
			t.Fatalf("delete championship: %s", err)
		}

		championships, err = store.ListChampionships()

		if err != nil {
			t.Fatal(err)
		}

		assertStringSet(t, "championships after delete", championshipNames(championships), "Champ A")

		deleted, err := store.LoadChampionship(champB.ID.String())

		if err != nil {
			t.Fatalf("expected soft-deleted championship to be loadable, got: %s", err)
		}

		if deleted.Deleted.IsZero() {
			t.Errorf("expected soft-deleted championship to have Deleted set")
		}

		if err := store.DeleteChampionship(uuid.New().String()); err != ErrChampionshipNotFound {
			t.Errorf("expected ErrChampionshipNotFound deleting a missing championship, got: %v", err)
		}
	})
}

func TestStoreContract_ChampionshipLoadsRaceWeekends(t *testing.T) {
	runStoreContract(t, func(t *testing.T, store Store) {
		rw := NewRaceWeekend()
		rw.Name = "Linked Weekend"

		if err := store.UpsertRaceWeekend(rw); err != nil {
			t.Fatal(err)
		}

		champ := NewChampionship("Champ with weekend")
		event := NewChampionshipEvent()
		event.RaceWeekendID = rw.ID
		champ.Events = append(champ.Events, event)

		if err := store.UpsertChampionship(champ); err != nil {
			t.Fatal(err)
		}

		loaded, err := store.LoadChampionship(champ.ID.String())

		if err != nil {
			t.Fatalf("load championship: %s", err)
		}

		if len(loaded.Events) != 1 || loaded.Events[0].RaceWeekend == nil {
			t.Fatalf("expected the race weekend of the championship event to be loaded")
		}

		if loaded.Events[0].RaceWeekend.Name != "Linked Weekend" {
			t.Errorf("expected linked race weekend name %q, got %q", "Linked Weekend", loaded.Events[0].RaceWeekend.Name)
		}
	})
}

func TestStoreContract_RaceWeekends(t *testing.T) {
	runStoreContract(t, func(t *testing.T, store Store) {
		raceWeekends, err := store.ListRaceWeekends()

		if err != nil {
			t.Fatalf("list on empty store: %s", err)
		}

		if len(raceWeekends) != 0 {
			t.Fatalf("expected no race weekends in empty store, got %d", len(raceWeekends))
		}

		if _, err := store.LoadRaceWeekend(uuid.New().String()); err != ErrRaceWeekendNotFound {
			t.Errorf("expected ErrRaceWeekendNotFound on fresh store, got: %v", err)
		}

		rwA := NewRaceWeekend()
		rwA.Name = "Weekend A"
		rwA.SpectatorCarEnabled = true

		rwB := NewRaceWeekend()
		rwB.Name = "Weekend B"

		for _, rw := range []*RaceWeekend{rwA, rwB} {
			if err := store.UpsertRaceWeekend(rw); err != nil {
				t.Fatalf("upsert race weekend: %s", err)
			}

			if rw.Updated.IsZero() {
				t.Errorf("expected UpsertRaceWeekend to set Updated on %s", rw.Name)
			}
		}

		loaded, err := store.LoadRaceWeekend(rwA.ID.String())

		if err != nil {
			t.Fatalf("load race weekend: %s", err)
		}

		if loaded.Name != "Weekend A" || !loaded.SpectatorCarEnabled || loaded.ID != rwA.ID {
			t.Errorf("race weekend did not round-trip: %+v", loaded)
		}

		if _, err := store.LoadRaceWeekend(uuid.New().String()); err != ErrRaceWeekendNotFound {
			t.Errorf("expected ErrRaceWeekendNotFound, got: %v", err)
		}

		raceWeekends, err = store.ListRaceWeekends()

		if err != nil {
			t.Fatal(err)
		}

		assertStringSet(t, "race weekends", raceWeekendNames(raceWeekends), "Weekend A", "Weekend B")

		// soft delete
		if err := store.DeleteRaceWeekend(rwA.ID.String()); err != nil {
			t.Fatalf("delete race weekend: %s", err)
		}

		raceWeekends, err = store.ListRaceWeekends()

		if err != nil {
			t.Fatal(err)
		}

		assertStringSet(t, "race weekends after delete", raceWeekendNames(raceWeekends), "Weekend B")

		deleted, err := store.LoadRaceWeekend(rwA.ID.String())

		if err != nil {
			t.Fatalf("expected soft-deleted race weekend to be loadable, got: %s", err)
		}

		if deleted.Deleted.IsZero() {
			t.Errorf("expected soft-deleted race weekend to have Deleted set")
		}

		if err := store.DeleteRaceWeekend(uuid.New().String()); err != ErrRaceWeekendNotFound {
			t.Errorf("expected ErrRaceWeekendNotFound deleting a missing race weekend, got: %v", err)
		}
	})
}

func TestStoreContract_AuditEntries(t *testing.T) {
	runStoreContract(t, func(t *testing.T, store Store) {
		// no audit entries is not an error
		entries, err := store.GetAuditEntries()

		if err != nil {
			t.Fatalf("get audit entries on empty store: %s", err)
		}

		if len(entries) != 0 {
			t.Fatalf("expected no audit entries in empty store, got %d", len(entries))
		}

		now := time.Now().Truncate(time.Second)

		for i, url := range []string{"/a", "/b", "/c"} {
			err := store.AddAuditEntry(&smmodel.AuditEntry{
				UserGroup: smmodel.GroupAdmin,
				Method:    "POST",
				URL:       url,
				User:      "admin",
				Time:      now.Add(time.Duration(i) * time.Second),
			})

			if err != nil {
				t.Fatalf("add audit entry: %s", err)
			}
		}

		entries, err = store.GetAuditEntries()

		if err != nil {
			t.Fatalf("get audit entries: %s", err)
		}

		if len(entries) != 3 {
			t.Fatalf("expected 3 audit entries, got %d", len(entries))
		}

		for i, url := range []string{"/a", "/b", "/c"} {
			e := entries[i]

			if e.URL != url || e.Method != "POST" || e.User != "admin" || e.UserGroup != smmodel.GroupAdmin {
				t.Errorf("audit entry %d did not round-trip: %+v", i, e)
			}

			if !e.Time.Equal(now.Add(time.Duration(i) * time.Second)) {
				t.Errorf("audit entry %d: unexpected time %s", i, e.Time)
			}
		}
	})
}

func TestStoreContract_LastRaceEvent(t *testing.T) {
	runStoreContract(t, func(t *testing.T, store Store) {
		// nothing stored is not an error
		if loaded, err := store.LoadLastRaceEvent(); err != nil || loaded != nil {
			t.Errorf("expected (nil, nil) on fresh store, got (%T, %v)", loaded, err)
		}

		race := &CustomRace{Name: "Last Race", UUID: uuid.New()}
		race.RaceConfig.Track = "spa"

		if err := store.UpsertLastRaceEvent(race); err != nil {
			t.Fatalf("upsert last race event: %s", err)
		}

		loaded, err := store.LoadLastRaceEvent()

		if err != nil {
			t.Fatalf("load last race event: %s", err)
		}

		loadedRace, ok := loaded.(*CustomRace)

		if !ok {
			t.Fatalf("expected *CustomRace, got %T", loaded)
		}

		if loadedRace.Name != "Last Race" || loadedRace.UUID != race.UUID || loadedRace.GetRaceConfig().Track != "spa" {
			t.Errorf("last race event did not round-trip: %+v", loadedRace)
		}

		// overwrite with a different event type
		quick := &QuickRace{}
		quick.RaceConfig.Track = "monza"

		if err := store.UpsertLastRaceEvent(quick); err != nil {
			t.Fatal(err)
		}

		loaded, err = store.LoadLastRaceEvent()

		if err != nil {
			t.Fatal(err)
		}

		if q, ok := loaded.(*QuickRace); !ok || q.RaceConfig.Track != "monza" {
			t.Errorf("expected overwritten *QuickRace on monza, got %T %+v", loaded, loaded)
		}

		if err := store.ClearLastRaceEvent(); err != nil {
			t.Fatalf("clear last race event: %s", err)
		}

		loaded, err = store.LoadLastRaceEvent()

		if err != nil || loaded != nil {
			t.Errorf("expected (nil, nil) after clear, got (%T, %v)", loaded, err)
		}

		// clearing twice is not an error
		if err := store.ClearLastRaceEvent(); err != nil {
			t.Errorf("expected clearing an already cleared last race event to succeed, got: %s", err)
		}
	})
}

func TestStoreContract_LiveTimingsData(t *testing.T) {
	runStoreContract(t, func(t *testing.T, store Store) {
		// nothing stored is not an error
		if lt, err := store.LoadLiveTimingsData(); err != nil || lt != nil {
			t.Errorf("expected (nil, nil) on fresh store, got (%+v, %v)", lt, err)
		}

		in := &smmodel.LiveTimingsPersistedData{
			Track:       "ks_nurburgring",
			TrackLayout: "gp",
			SessionName: "Practice",
		}

		if err := store.UpsertLiveTimingsData(in); err != nil {
			t.Fatalf("upsert live timings: %s", err)
		}

		out, err := store.LoadLiveTimingsData()

		if err != nil {
			t.Fatalf("load live timings: %s", err)
		}

		if out == nil || out.Track != in.Track || out.TrackLayout != in.TrackLayout || out.SessionName != in.SessionName {
			t.Errorf("live timings did not round-trip: %+v", out)
		}
	})
}

func TestStoreContract_LiveFrames(t *testing.T) {
	runStoreContract(t, func(t *testing.T, store Store) {
		frames, err := store.ListPrevFrames()

		if err != nil {
			t.Fatalf("list frames on empty store: %s", err)
		}

		if len(frames) != 0 {
			t.Fatalf("expected no frames on empty store, got %v", frames)
		}

		if err := store.UpsertLiveFrames([]string{"https://a.example", "https://b.example"}); err != nil {
			t.Fatal(err)
		}

		if err := store.UpsertLiveFrames([]string{"https://c.example"}); err != nil {
			t.Fatal(err)
		}

		frames, err = store.ListPrevFrames()

		if err != nil {
			t.Fatal(err)
		}

		if len(frames) != 1 || frames[0] != "https://c.example" {
			t.Errorf("expected frames to be replaced, got %v", frames)
		}
	})
}

func TestStoreContract_ServerOptions(t *testing.T) {
	runStoreContract(t, func(t *testing.T, store Store) {
		defaults := serverconfig.ConfigIniDefault().GlobalServerConfig

		so, err := store.LoadServerOptions()

		if err != nil {
			t.Fatalf("load server options on empty store: %s", err)
		}

		if so.Name != defaults.Name || so.UDPPort != defaults.UDPPort || so.HTTPPort != defaults.HTTPPort {
			t.Errorf("expected default server options, got name=%q udp=%d http=%d", so.Name, so.UDPPort, so.HTTPPort)
		}

		so.Name = "Contract Test Server"
		so.UDPPort = 9601

		if err := store.UpsertServerOptions(so); err != nil {
			t.Fatal(err)
		}

		loaded, err := store.LoadServerOptions()

		if err != nil {
			t.Fatal(err)
		}

		if loaded.Name != "Contract Test Server" || loaded.UDPPort != 9601 || loaded.HTTPPort != defaults.HTTPPort {
			t.Errorf("server options did not round-trip: name=%q udp=%d http=%d", loaded.Name, loaded.UDPPort, loaded.HTTPPort)
		}
	})
}

func TestStoreContract_PluginOptions(t *testing.T) {
	runStoreContract(t, func(t *testing.T, store Store) {
		// defaults, loaded on a completely fresh store
		sto, err := store.LoadStrackerOptions()

		if err != nil {
			t.Fatalf("load stracker defaults: %s", err)
		}

		if sto.InstanceConfiguration.ACServerAddress != smplugins.DefaultStrackerIni(ServerInstallPath).InstanceConfiguration.ACServerAddress {
			t.Errorf("expected default stracker options, got %+v", sto.InstanceConfiguration)
		}

		kmr, err := store.LoadKissMyRankOptions()

		if err != nil {
			t.Fatalf("load kissmyrank defaults: %s", err)
		}

		if kmr.MaxPing != smplugins.DefaultKissMyRankConfig().MaxPing {
			t.Errorf("expected default kissmyrank max ping %d, got %d", smplugins.DefaultKissMyRankConfig().MaxPing, kmr.MaxPing)
		}

		rpc, err := store.LoadRealPenaltyOptions()

		if err != nil {
			t.Fatalf("load real penalty defaults: %s", err)
		}

		if rpc.RealPenaltyAppConfig.General.EnableRealPenalty {
			t.Errorf("expected real penalty to be disabled by default")
		}

		// round-trips
		sto.EnableStracker = true
		sto.InstanceConfiguration.ACServerAddress = "10.0.0.1"

		if err := store.UpsertStrackerOptions(sto); err != nil {
			t.Fatal(err)
		}

		kmr.EnableKissMyRank = true
		kmr.MaxPing = 123

		if err := store.UpsertKissMyRankOptions(kmr); err != nil {
			t.Fatal(err)
		}

		rpc.RealPenaltyAppConfig.General.EnableRealPenalty = true
		rpc.RealPenaltyAppConfig.General.UDPPort = 12345

		if err := store.UpsertRealPenaltyOptions(rpc); err != nil {
			t.Fatal(err)
		}

		loadedSto, err := store.LoadStrackerOptions()

		if err != nil {
			t.Fatal(err)
		}

		if !loadedSto.EnableStracker || loadedSto.InstanceConfiguration.ACServerAddress != "10.0.0.1" {
			t.Errorf("stracker options did not round-trip: %+v", loadedSto.InstanceConfiguration)
		}

		loadedKmr, err := store.LoadKissMyRankOptions()

		if err != nil {
			t.Fatal(err)
		}

		if !loadedKmr.EnableKissMyRank || loadedKmr.MaxPing != 123 {
			t.Errorf("kissmyrank options did not round-trip: enabled=%t maxPing=%d", loadedKmr.EnableKissMyRank, loadedKmr.MaxPing)
		}

		loadedRpc, err := store.LoadRealPenaltyOptions()

		if err != nil {
			t.Fatal(err)
		}

		if !loadedRpc.RealPenaltyAppConfig.General.EnableRealPenalty || loadedRpc.RealPenaltyAppConfig.General.UDPPort != 12345 {
			t.Errorf("real penalty options did not round-trip: %+v", loadedRpc.RealPenaltyAppConfig.General)
		}

		// plugin options must not clobber the server options (BoltStore keeps them in the same bucket)
		so, err := store.LoadServerOptions()

		if err != nil {
			t.Fatal(err)
		}

		if so.Name != serverconfig.ConfigIniDefault().GlobalServerConfig.Name {
			t.Errorf("expected server options to be unaffected by plugin options, got name %q", so.Name)
		}
	})
}

func assertStringSet(t *testing.T, what string, got []string, expected ...string) {
	t.Helper()

	sort.Strings(got)
	sort.Strings(expected)

	if len(got) != len(expected) {
		t.Errorf("%s: expected %v, got %v", what, expected, got)
		return
	}

	for i := range got {
		if got[i] != expected[i] {
			t.Errorf("%s: expected %v, got %v", what, expected, got)
			return
		}
	}
}

func customRaceNames(races []*CustomRace) []string {
	var out []string

	for _, r := range races {
		out = append(out, r.Name)
	}

	return out
}

func entrantIDs(entrants []*serverconfig.Entrant) []string {
	var out []string

	for _, e := range entrants {
		out = append(out, e.ID())
	}

	return out
}

func accountNames(accounts []*smmodel.Account) []string {
	var out []string

	for _, a := range accounts {
		out = append(out, a.Name)
	}

	return out
}

func championshipNames(championships []*Championship) []string {
	var out []string

	for _, c := range championships {
		out = append(out, c.Name)
	}

	return out
}

func raceWeekendNames(raceWeekends []*RaceWeekend) []string {
	var out []string

	for _, rw := range raceWeekends {
		out = append(out, rw.Name)
	}

	return out
}
