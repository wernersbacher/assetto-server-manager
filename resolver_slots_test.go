package servermanager

import (
	"github.com/wernersbacher/assetto-server-manager/internal/web/webtest"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wernersbacher/assetto-server-manager/internal/admin"
	smconfig "github.com/wernersbacher/assetto-server-manager/internal/config"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	"github.com/wernersbacher/assetto-server-manager/internal/server/servertest"
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

func TestResolverChecksPortsOfSlots(t *testing.T) {
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

	// both slots have the default ports
	if warnings := resolver.portWarnings("b"); len(warnings) == 0 {
		t.Fatal("expected warnings about the shared default ports")
	}

	if err := resolver.checkPorts(slotB); err != nil {
		t.Errorf("no server runs, expected the start to be allowed, got %v", err)
	}

	process := &servertest.Interruptible{}
	slotA.Process = process

	if err := process.Start(smmodel.QuickRace{}, "", 0, "", 0); err != nil {
		t.Fatal(err)
	}

	err = resolver.checkPorts(slotB)

	if err == nil || !strings.Contains(err.Error(), "collide with a running server") {
		t.Fatalf("expected the start to be refused while a runs on the same ports, got %v", err)
	}

	// b gets its own acServer ports, plugin options of both slots are per slot
	opts, err := store.LoadServerOptionsFor("b")

	if err != nil {
		t.Fatal(err)
	}

	opts.UDPPort, opts.TCPPort, opts.HTTPPort = 9701, 9701, 8082

	if err := store.UpsertServerOptionsFor("b", opts); err != nil {
		t.Fatal(err)
	}

	if err := resolver.checkPorts(slotB); err != nil {
		t.Errorf("expected the start to be allowed on its own ports, got %v", err)
	}

	if warnings := resolver.portWarnings("b"); len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}

	// plugins on the same ports are warned about, but don't stop the start
	for _, id := range []smmodel.ServerID{"a", "b"} {
		kmr, err := store.LoadKissMyRankOptionsFor(id)

		if err != nil {
			t.Fatal(err)
		}

		kmr.EnableKissMyRank = true

		if err := store.UpsertKissMyRankOptionsFor(id, kmr); err != nil {
			t.Fatal(err)
		}
	}

	if warnings := resolver.portWarnings("b"); len(warnings) == 0 || !strings.Contains(warnings[0], "KissMyRank") {
		t.Errorf("expected a warning about the KissMyRank ports, got %v", warnings)
	}

	if err := resolver.checkPorts(slotB); err != nil {
		t.Errorf("plugin port conflicts must not stop the start, got %v", err)
	}

	// with one server there is nothing to compare
	single, _, cleanupSingle := newTestResolver(t, &smconfig.Configuration{})
	defer cleanupSingle()

	single.Servers()

	if warnings := single.portWarnings(single.registry.Default().ID()); len(warnings) != 0 {
		t.Errorf("expected no warnings with one server, got %v", warnings)
	}
}

// serveWithServerSelection runs the request through the server selection middleware, as the router does.
func serveWithServerSelection(resolver *Resolver, scope *smweb.Scope, handler http.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	smweb.ServerSelectionMiddleware(resolver.isServerID)(handler).ServeHTTP(rec, smweb.WithScope(req, scope))

	return rec
}

// Saving the options with ?server=b must change server b even if the session remembers server a (e.g. because the user
// switched the server in another browser tab), and must leave a alone.
func TestResolverOptionsSaveGoesToServerOfRequest(t *testing.T) {
	dir, err := ioutil.TempDir("", "resolver-slots-install")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(dir)

	resolver, store, cleanup := newTestResolver(t, &smconfig.Configuration{Servers: []smconfig.ServerSlotConfig{
		{ID: "a", Name: "Server A", InstallPath: filepath.Join(dir, "a")},
		{ID: "b", Name: "Server B", InstallPath: filepath.Join(dir, "b")},
	}})
	defer cleanup()

	scope := webtest.RequestScope()
	handler := resolver.resolveServerAdministrationHandler().Options

	var seen smmodel.ServerID

	probe := func(w http.ResponseWriter, r *http.Request) { seen = smweb.RequestedServerID(r) }

	// the session remembers server a
	rec := serveWithServerSelection(resolver, scope, probe, httptest.NewRequest(http.MethodGet, "/server-options?server=a", nil))
	cookies := rec.Result().Cookies()

	if len(cookies) == 0 {
		t.Fatal("expected the server to be remembered in a cookie")
	}

	req := httptest.NewRequest(http.MethodGet, "/server-options", nil)

	for _, c := range cookies {
		req.AddCookie(c)
	}

	serveWithServerSelection(resolver, scope, probe, req)

	if seen != "a" {
		t.Fatalf("expected the session to remember server a, got %q", seen)
	}

	before, err := store.LoadServerOptionsFor("a")

	if err != nil {
		t.Fatal(err)
	}

	nameOfA := before.Name

	// a form as the page renders it: the action carries the server of the page
	form := url.Values{"Name": {"Renamed B"}}
	post := httptest.NewRequest(http.MethodPost, "/server-options?server=b", strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	for _, c := range cookies {
		post.AddCookie(c)
	}

	serveWithServerSelection(resolver, scope, handler, post)

	if loaded, err := store.LoadServerOptionsFor("b"); err != nil || loaded.Name != "Renamed B" {
		t.Errorf("expected the options of b to be saved, got %v %v", loaded, err)
	}

	if loaded, err := store.LoadServerOptionsFor("a"); err != nil || loaded.Name != nameOfA {
		t.Errorf("the options of a must not change, got %v %v", loaded, err)
	}
}

func TestResolverServersPageShowsPortConflicts(t *testing.T) {
	dir, err := ioutil.TempDir("", "resolver-slots-install")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(dir)

	resolver, store, cleanup := newTestResolver(t, &smconfig.Configuration{Servers: []smconfig.ServerSlotConfig{
		{ID: "a", Name: "Server A", InstallPath: filepath.Join(dir, "a")},
		{ID: "b", Name: "Server B", InstallPath: filepath.Join(dir, "b")},
	}})
	defer cleanup()

	// b keeps the default ports and so conflicts with a; stracker of a is enabled
	strackerOpts, err := store.LoadStrackerOptionsFor("a")

	if err != nil {
		t.Fatal(err)
	}

	strackerOpts.EnableStracker = true

	if err := store.UpsertStrackerOptionsFor("a", strackerOpts); err != nil {
		t.Fatal(err)
	}

	req := smweb.WithScope(httptest.NewRequest(http.MethodGet, "/servers", nil), webtest.RequestScope())
	rec := httptest.NewRecorder()

	resolver.resolveServerAdministrationHandler().Servers(rec, smweb.WithRequestedServerID(req, "a"))

	body := rec.Body.String()

	if rec.Code != http.StatusOK {
		t.Fatalf("expected the page to render, got %d %q", rec.Code, body)
	}

	for _, expected := range []string{"Server A", "Server B", "Port conflict with another server", "acServer UDP port", "9600", "/stracker/options?server=a", "/server-options?server=b"} {
		if !strings.Contains(body, expected) {
			t.Errorf("expected the servers page to contain %q", expected)
		}
	}
}
