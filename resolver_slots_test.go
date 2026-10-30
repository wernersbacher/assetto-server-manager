package servermanager

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/wernersbacher/assetto-server-manager/internal/admin"
	smconfig "github.com/wernersbacher/assetto-server-manager/internal/config"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	"github.com/wernersbacher/assetto-server-manager/internal/serverenv"
	"github.com/wernersbacher/assetto-server-manager/internal/servers"
	smstore "github.com/wernersbacher/assetto-server-manager/internal/store"
	smweb "github.com/wernersbacher/assetto-server-manager/internal/web"
	"github.com/wernersbacher/assetto-server-manager/pkg/udp"
)

func newTestResolver(t *testing.T, config *smconfig.Configuration) (*Resolver, smstore.Store, func()) {
	t.Helper()

	dir, err := ioutil.TempDir("", "resolver-slots-test")

	if err != nil {
		t.Fatal(err)
	}

	store := smstore.NewJSONStore(filepath.Join(dir, "store"), filepath.Join(dir, "shared"))

	resolver, err := NewResolver(config, smweb.NewFilesystemTemplateLoader(filepath.Join("cmd", "server-manager", "views")), false, store, serverenv.NewState())

	if err != nil {
		t.Fatal(err)
	}

	return resolver, store, func() { _ = os.RemoveAll(dir) }
}

func connectMessage(carID udp.CarID, guid udp.DriverGUID) udp.SessionCarInfo {
	return udp.SessionCarInfo{
		CarID:      carID,
		DriverName: "Driver " + string(guid),
		DriverGUID: guid,
		CarModel:   "car_model",
		CarSkin:    "red",
		EventType:  udp.EventNewConnection,
	}
}

func connectedGUIDs(slot *servers.Slot) []udp.DriverGUID {
	var guids []udp.DriverGUID

	_ = slot.RaceControl.ConnectedDrivers.Each(func(driverGUID udp.DriverGUID, driver *smmodel.RaceControlDriver) error {
		guids = append(guids, driverGUID)
		return nil
	})

	return guids
}

func TestResolverUDPCallbackIsPerSlot(t *testing.T) {
	dir, err := ioutil.TempDir("", "resolver-slots-install")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(dir)

	resolver, _, cleanup := newTestResolver(t, &smconfig.Configuration{Servers: []smconfig.ServerSlotConfig{
		{ID: "a", InstallPath: filepath.Join(dir, "a")},
		{ID: "b", InstallPath: filepath.Join(dir, "b")},
	}})
	defer cleanup()

	registry := resolver.Servers()

	if registry.Len() != 2 {
		t.Fatalf("expected 2 slots, got %d", registry.Len())
	}

	slotA, _ := registry.ByID("a")
	slotB, _ := registry.ByID("b")

	if slotA.RaceControl == slotB.RaceControl || slotA.RaceManager == slotB.RaceManager || slotA.Process == slotB.Process ||
		slotA.ChampionshipManager == slotB.ChampionshipManager || slotA.RaceWeekendManager == slotB.RaceWeekendManager ||
		slotA.ContentManagerWrapper == slotB.ContentManagerWrapper || slotA.RaceControlHub == slotB.RaceControlHub {
		t.Fatal("the slots must not share runtime components")
	}

	// the callbacks the processes are created with
	callbackA := resolver.slotUDPCallback(slotA)
	callbackB := resolver.slotUDPCallback(slotB)

	callbackA(connectMessage(1, "111"))

	if guids := connectedGUIDs(slotA); len(guids) != 1 || guids[0] != "111" {
		t.Errorf("slot a: expected driver 111, got %v", guids)
	}

	if guids := connectedGUIDs(slotB); len(guids) != 0 {
		t.Errorf("slot b must not see the message of slot a, got %v", guids)
	}

	callbackB(connectMessage(2, "222"))

	if guids := connectedGUIDs(slotB); len(guids) != 1 || guids[0] != "222" {
		t.Errorf("slot b: expected driver 222, got %v", guids)
	}

	if guids := connectedGUIDs(slotA); len(guids) != 1 || guids[0] != "111" {
		t.Errorf("slot a must not see the message of slot b, got %v", guids)
	}
}

func TestResolverSlotsHaveTheirOwnEnvAndData(t *testing.T) {
	dir, err := ioutil.TempDir("", "resolver-slots-install")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(dir)

	resolver, store, cleanup := newTestResolver(t, &smconfig.Configuration{Servers: []smconfig.ServerSlotConfig{
		{ID: "a", InstallPath: filepath.Join(dir, "a")},
		{ID: "b", InstallPath: filepath.Join(dir, "b")},
	}})
	defer cleanup()

	registry := resolver.Servers()
	slotA, _ := registry.ByID("a")
	slotB, _ := registry.ByID("b")

	if slotA.Env.ServerID() != "a" || slotB.Env.ServerID() != "b" {
		t.Errorf("unexpected server ids %q, %q", slotA.Env.ServerID(), slotB.Env.ServerID())
	}

	if slotA.Env.InstallPath() == slotB.Env.InstallPath() {
		t.Error("the slots must have their own install path")
	}

	// the managers of a slot see the options of that slot
	opts, err := resolver.slotStore(slotB).LoadServerOptions()

	if err != nil {
		t.Fatal(err)
	}

	opts.Name = "Server B"

	if err := resolver.slotStore(slotB).UpsertServerOptions(opts); err != nil {
		t.Fatal(err)
	}

	if loaded, err := store.LoadServerOptionsFor("a"); err != nil || loaded.Name == "Server B" {
		t.Errorf("the options of slot b must not be those of slot a: %v %v", loaded, err)
	}

	if loaded, err := store.LoadServerOptionsFor("b"); err != nil || loaded.Name != "Server B" {
		t.Errorf("expected the options of slot b, got %v %v", loaded, err)
	}
}

func TestResolverSingleSlotIsTheDefaultStack(t *testing.T) {
	resolver, _, cleanup := newTestResolver(t, &smconfig.Configuration{})
	defer cleanup()

	registry := resolver.Servers()

	if registry.Len() != 1 {
		t.Fatalf("expected one slot, got %d", registry.Len())
	}

	slot := registry.Default()

	if !resolver.slotConfig(slot).IsDefault() {
		t.Error("the only slot is the default slot")
	}

	if resolver.resolveServerProcess() != slot.Process || resolver.resolveRaceManager() != slot.RaceManager ||
		resolver.resolveChampionshipManager() != slot.ChampionshipManager || resolver.resolveRaceWeekendManager() != slot.RaceWeekendManager ||
		resolver.ResolveRaceControl() != slot.RaceControl || resolver.resolveContentManagerWrapper() != slot.ContentManagerWrapper ||
		resolver.resolveEnv() != slot.Env {
		t.Error("the single-slot accessors must return the components of the default slot")
	}

	// the default slot works on the store itself
	if resolver.slotStore(slot) != resolver.ResolveStore() {
		t.Error("the default slot must use the store itself")
	}

	resolver.resolveSlot(slot).DispatchUDP(connectMessage(1, "111"), false)

	if guids := connectedGUIDs(slot); len(guids) != 1 {
		t.Errorf("expected the driver to reach race control, got %v", guids)
	}

	// performance mode skips race control
	slot.DispatchUDP(connectMessage(2, "222"), true)

	if guids := connectedGUIDs(slot); len(guids) != 1 {
		t.Errorf("race control must not get messages in performance mode, got %v", guids)
	}
}

func TestResolverServerRefsAndAdminSlots(t *testing.T) {
	dir, err := ioutil.TempDir("", "resolver-slots-install")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(dir)

	resolver, _, cleanup := newTestResolver(t, &smconfig.Configuration{Servers: []smconfig.ServerSlotConfig{
		{ID: "a", Name: "Server A", InstallPath: filepath.Join(dir, "a")},
		{ID: "b", Name: "Server B", InstallPath: filepath.Join(dir, "b")},
	}})
	defer cleanup()

	refs := resolver.serverRefs()
	slots := resolver.adminSlots()

	if len(refs) != 2 || len(slots) != 2 {
		t.Fatalf("expected 2 servers, got %d refs and %d slots", len(refs), len(slots))
	}

	for i, id := range []smmodel.ServerID{"a", "b"} {
		if refs[i].ID != id || slots[i].ID != id || slots[i].Slot.Env.ServerID() != id {
			t.Errorf("unexpected server at %d: %+v %+v", i, refs[i], slots[i].ServerRef)
		}

		// no event runs, and the summary of an idle server needs no race control data
		if refs[i].Running || admin.NewServerSummary(slots[i]).Running {
			t.Errorf("server %q must be idle", id)
		}
	}
}
