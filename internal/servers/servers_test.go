package servers_test

import (
	"github.com/google/uuid"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/server/servertest"
	"path/filepath"
	"testing"

	"github.com/wernersbacher/ACPitwall/internal/config"
	"github.com/wernersbacher/ACPitwall/internal/serverenv"
	"github.com/wernersbacher/ACPitwall/internal/servers"
)

func newRegistry(t *testing.T, state *serverenv.State, slots ...config.Slot) *servers.Registry {
	t.Helper()

	registry, err := servers.NewRegistry(slots, state, func(fn func()) { fn() }, false)

	if err != nil {
		t.Fatal(err)
	}

	return registry
}

func TestRegistry(t *testing.T) {
	state := serverenv.NewState()
	state.SetServerID("main-id")

	root := filepath.FromSlash("/srv")
	registry := newRegistry(t, state,
		config.Slot{ID: config.DefaultSlotID, Name: "Main", InstallPath: filepath.Join(root, "a")},
		config.Slot{ID: "b", Name: "Second", InstallPath: filepath.Join(root, "b")},
		config.Slot{ID: "c", Name: "Third", InstallPath: filepath.Join(root, "c")},
	)

	if registry.Len() != 3 {
		t.Fatalf("expected 3 slots, got %d", registry.Len())
	}

	var ids []string

	for _, slot := range registry.All() {
		ids = append(ids, string(slot.ID()))
	}

	if ids[0] != "main-id" || ids[1] != "b" || ids[2] != "c" {
		t.Errorf("unexpected order %v", ids)
	}

	if registry.Default() != registry.All()[0] {
		t.Error("the default slot must be the first one")
	}

	if slot, ok := registry.ByID("b"); !ok || slot.Name != "Second" {
		t.Errorf("unexpected slot for b: %v %v", slot, ok)
	}

	if slot, ok := registry.ByID(""); !ok || slot != registry.Default() {
		t.Error("the empty id must resolve to the default slot")
	}

	if slot, ok := registry.ByID("main-id"); !ok || slot != registry.Default() {
		t.Error("the id of the default slot must resolve to it")
	}

	if _, ok := registry.ByID("unknown"); ok {
		t.Error("unknown id must not be found")
	}
}

func TestRegistryLazyDefaultServerID(t *testing.T) {
	state := serverenv.NewState()
	registry := newRegistry(t, state, config.Slot{ID: config.DefaultSlotID, InstallPath: "x"})

	if id := registry.Default().ID(); id != "" {
		t.Errorf("expected no id yet, got %q", id)
	}

	state.SetServerID("later")

	if id := registry.Default().ID(); id != "later" {
		t.Errorf("expected the id set after the registry was built, got %q", id)
	}

	if slot, ok := registry.ByID("later"); !ok || slot != registry.Default() {
		t.Error("the default slot must be found by its late id")
	}
}

func TestRegistrySlotEnv(t *testing.T) {
	state := serverenv.NewState()
	root := filepath.FromSlash("/srv")
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	registry := newRegistry(t, state,
		config.Slot{ID: config.DefaultSlotID, InstallPath: a},
		config.Slot{ID: "b", InstallPath: b},
	)

	for _, slot := range registry.All() {
		env := slot.Env

		if env.InstallPath() != slot.InstallPath {
			t.Errorf("unexpected install path %q", env.InstallPath())
		}

		dirs := env.ResultsDirs()

		if len(dirs) != 2 || dirs[0] != filepath.Join(a, "results") || dirs[1] != filepath.Join(b, "results") {
			t.Errorf("unexpected results dirs %v", dirs)
		}

		loader := env.Results()

		if loader.Dir != filepath.Join(slot.InstallPath, "results") || len(loader.ExtraDirs) != 1 {
			t.Errorf("unexpected loader %q %v", loader.Dir, loader.ExtraDirs)
		}
	}

	state.SetFallBackSorting(true)

	if !registry.All()[1].Env.FallBackSorting() {
		t.Error("fallback sorting must be shared")
	}
}

func TestNewRegistryErrors(t *testing.T) {
	state := serverenv.NewState()

	if _, err := servers.NewRegistry(nil, state, nil, false); err == nil {
		t.Error("expected an error without slots")
	}

	if _, err := servers.NewRegistry([]config.Slot{{ID: "a"}, {ID: "a"}}, state, nil, false); err == nil {
		t.Error("expected an error for duplicate ids")
	}
}

func TestRegistryActiveEvents(t *testing.T) {
	state := serverenv.NewState()
	state.SetServerID("main-id")

	root := filepath.FromSlash("/srv")
	registry := newRegistry(t, state,
		config.Slot{ID: config.DefaultSlotID, Name: "Main", InstallPath: filepath.Join(root, "a")},
		config.Slot{ID: "b", Name: "Second", InstallPath: filepath.Join(root, "b")},
	)

	championshipID, raceWeekendID, linkedChampionshipID := uuid.New(), uuid.New(), uuid.New()

	main, second := &servertest.Interruptible{}, &servertest.Interruptible{}
	registry.All()[0].Process = main
	registry.All()[1].Process = second

	if _, ok := registry.ChampionshipActiveOn(championshipID); ok {
		t.Fatal("nothing is running")
	}

	_ = main.Start(&smmodel.ActiveChampionship{ChampionshipID: championshipID}, "", 0, "", 0)
	_ = second.Start(&smmodel.ActiveRaceWeekend{RaceWeekendID: raceWeekendID, ChampionshipID: linkedChampionshipID}, "", 0, "", 0)

	if ref, ok := registry.ChampionshipActiveOn(championshipID); !ok || ref.ID != "main-id" || ref.Name != "Main" {
		t.Errorf("the championship runs on the main slot: %v %v", ref, ok)
	}

	if ref, ok := registry.RaceWeekendActiveOn(raceWeekendID); !ok || ref.ID != "b" {
		t.Errorf("the race weekend runs on b: %v %v", ref, ok)
	}

	if ref, ok := registry.ChampionshipActiveOn(linkedChampionshipID); !ok || ref.ID != "b" {
		t.Errorf("the championship of a running race weekend is active on b: %v %v", ref, ok)
	}

	if _, ok := registry.RaceWeekendActiveOn(championshipID); ok {
		t.Error("a championship id is not a race weekend")
	}

	main.ClearEvent()

	if _, ok := registry.ChampionshipActiveOn(championshipID); ok {
		t.Error("the championship is not running any more")
	}
}
