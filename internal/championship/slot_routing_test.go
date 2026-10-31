package championship

import (
	"context"
	"errors"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi"
	"github.com/google/uuid"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smconfig "github.com/wernersbacher/ACPitwall/internal/config"
	smcontent "github.com/wernersbacher/ACPitwall/internal/content"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/notification/notificationtest"
	smrace "github.com/wernersbacher/ACPitwall/internal/race"
	"github.com/wernersbacher/ACPitwall/internal/racecontrol"
	"github.com/wernersbacher/ACPitwall/internal/server/servertest"
	"github.com/wernersbacher/ACPitwall/internal/serverenv"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
	"github.com/wernersbacher/ACPitwall/internal/web/webtest"
)

// testSlot is the championship manager of one server slot with its process.
type testSlot struct {
	manager *ChampionshipManager
	process *servertest.Interruptible
}

// newTestSlots builds one slot per id on a shared store, each with its own fake install.
func newTestSlots(t *testing.T, ids ...smmodel.ServerID) (map[smmodel.ServerID]*testSlot, smstore.Store) {
	dir, err := ioutil.TempDir("", "slot-routing")

	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	store := smstore.NewJSONStore(filepath.Join(dir, "store"), filepath.Join(dir, "store-shared"))
	slots := make(map[smmodel.ServerID]*testSlot)

	for _, id := range ids {
		installPath := filepath.Join(dir, string(id))

		if err := os.MkdirAll(filepath.Join(installPath, serverenv.ConfigPath), 0755); err != nil {
			t.Fatal(err)
		}

		env := testEnv(installPath)
		serverID := id
		env.ServerID = func() smmodel.ServerID { return serverID }

		process := &servertest.Interruptible{}
		config := &smconfig.Configuration{}
		notifications := &notificationtest.Dummy{}

		slots[id] = &testSlot{
			process: process,
			manager: NewChampionshipManager(
				smrace.NewRaceManager(
					config,
					store,
					process,
					smcontent.NewCarManager(installPath, false, false),
					smcontent.NewTrackManager(installPath),
					notifications,
					racecontrol.NewRaceControl(config, racecontrol.NilBroadcaster{}, racecontrol.NilTrackData{}, process, store, racecontrol.NewPenaltiesManager(store, env), env),
					env,
				),
				config,
				store,
				process,
				env,
				notifications,
			),
		}
	}

	return slots, store
}

func newSlotTestHandler(slots map[smmodel.ServerID]*testSlot, defaultID smmodel.ServerID, active ActiveEvents) *ChampionshipsHandler {
	return NewChampionshipsHandler(&smconfig.Configuration{}, smweb.NewBaseHandler(nil), func(r *http.Request) (*ChampionshipManager, error) {
		id := smweb.RequestedServerID(r)

		if id == "" {
			id = defaultID
		}

		slot, ok := slots[id]

		if !ok {
			return nil, errors.New("unknown server")
		}

		return slot.manager, nil
	}, active)
}

func newSlotTestChampionship(t *testing.T, store smstore.Store) (*smmodel.Championship, *smmodel.ChampionshipEvent) {
	championship := smmodel.NewChampionship("Slot Championship")
	event := smmodel.NewChampionshipEvent()
	event.RaceSetup = serverconfig.ConfigIniDefault().CurrentRaceConfig
	championship.Events = append(championship.Events, event)

	if err := store.UpsertChampionship(championship); err != nil {
		t.Fatal(err)
	}

	return championship, event
}

func eventRequest(championship *smmodel.Championship, event *smmodel.ChampionshipEvent, server smmodel.ServerID) *http.Request {
	req := smweb.WithScope(httptest.NewRequest(http.MethodGet, "/championship/x/event/y/start", nil), webtest.RequestScope())
	req = smweb.WithRequestedServerID(req, server)

	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("championshipID", championship.ID.String())
	routeContext.URLParams.Add("eventID", event.ID.String())

	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeContext))
}

// activeOn is an ActiveEvents with a fixed answer.
type activeOn map[uuid.UUID]smweb.ServerRef

func (a activeOn) ChampionshipActiveOn(id uuid.UUID) (smweb.ServerRef, bool) {
	ref, ok := a[id]

	return ref, ok
}

func (a activeOn) RaceWeekendActiveOn(id uuid.UUID) (smweb.ServerRef, bool) {
	return smweb.ServerRef{}, false
}

func TestChampionshipsHandlerStartsEventOnServerOfRequest(t *testing.T) {
	slots, store := newTestSlots(t, "a", "b")
	handler := newSlotTestHandler(slots, "a", nil)
	championship, event := newSlotTestChampionship(t, store)

	rec := httptest.NewRecorder()
	handler.StartEvent(rec, eventRequest(championship, event, "b"))

	if rec.Code != http.StatusFound {
		t.Fatalf("expected a redirect, got %d", rec.Code)
	}

	if len(slots["b"].process.Started) != 1 || len(slots["a"].process.Started) != 0 {
		t.Fatalf("server=b must start the event on slot b only: a %d, b %d", len(slots["a"].process.Started), len(slots["b"].process.Started))
	}

	if slots["b"].manager.activeChampionship == nil || slots["a"].manager.activeChampionship != nil {
		t.Error("the active championship must belong to slot b only")
	}
}

func TestChampionshipsHandlerStartsEventOnDefaultServer(t *testing.T) {
	slots, store := newTestSlots(t, "a", "b")
	handler := newSlotTestHandler(slots, "a", nil)
	championship, event := newSlotTestChampionship(t, store)

	handler.StartEvent(httptest.NewRecorder(), eventRequest(championship, event, ""))

	if len(slots["a"].process.Started) != 1 || len(slots["b"].process.Started) != 0 {
		t.Fatalf("a request without server must start the event on the default slot: a %d, b %d", len(slots["a"].process.Started), len(slots["b"].process.Started))
	}
}

func TestChampionshipsHandlerUnknownServer(t *testing.T) {
	slots, store := newTestSlots(t, "a")
	handler := newSlotTestHandler(slots, "a", nil)
	championship, event := newSlotTestChampionship(t, store)

	rec := httptest.NewRecorder()
	handler.StartEvent(rec, eventRequest(championship, event, "missing"))

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for an unknown server, got %d", rec.Code)
	}

	if len(slots["a"].process.Started) != 0 {
		t.Error("nothing must be started for an unknown server")
	}
}

func TestChampionshipsHandlerBusyServerRejectsEvent(t *testing.T) {
	slots, store := newTestSlots(t, "a", "b")
	handler := newSlotTestHandler(slots, "a", nil)
	championship, event := newSlotTestChampionship(t, store)

	if err := slots["b"].process.Start(&smmodel.QuickRace{}, "", 0, "", 0); err != nil {
		t.Fatal(err)
	}

	if err := slots["b"].manager.CheckCanStartEvent(championship.ID.String(), nil); !errors.Is(err, ErrServerBusy) {
		t.Fatalf("expected ErrServerBusy, got %v", err)
	}

	rec := httptest.NewRecorder()
	handler.StartEvent(rec, eventRequest(championship, event, "b"))

	if rec.Code != http.StatusFound {
		t.Fatalf("expected a redirect, got %d", rec.Code)
	}

	if len(slots["b"].process.Started) != 1 || slots["b"].process.Stopped != 0 {
		t.Errorf("the running event must not be replaced: started %d, stopped %d", len(slots["b"].process.Started), slots["b"].process.Stopped)
	}

	// the other server is free
	if err := slots["a"].manager.CheckCanStartEvent(championship.ID.String(), nil); err != nil {
		t.Errorf("server a is not busy: %v", err)
	}
}

func TestChampionshipsHandlerChampionshipActiveOnOtherServer(t *testing.T) {
	slots, store := newTestSlots(t, "a", "b")
	championship, event := newSlotTestChampionship(t, store)

	active := activeOn{championship.ID: {ID: "a", Name: "Server A"}}
	handler := newSlotTestHandler(slots, "a", active)

	err := slots["b"].manager.CheckCanStartEvent(championship.ID.String(), active)

	var elsewhere ActiveElsewhereError

	if !errors.As(err, &elsewhere) || elsewhere.Server.ID != "a" {
		t.Fatalf("expected the championship to be active on a, got %v", err)
	}

	if !strings.Contains(err.Error(), "championship") || !strings.Contains(err.Error(), "Server A") {
		t.Errorf("the error must name the server: %s", err)
	}

	handler.StartEvent(httptest.NewRecorder(), eventRequest(championship, event, "b"))

	if len(slots["b"].process.Started) != 0 {
		t.Error("the championship must not be started on b while it is active on a")
	}

	// cancelling on the wrong server must not stop the process there
	handler.CancelEvent(httptest.NewRecorder(), eventRequest(championship, event, "b"))

	if slots["b"].process.Stopped != 0 {
		t.Error("cancelling on a server which does not run the championship must not stop it")
	}

	// on its own server the championship can be used
	if err := slots["a"].manager.CheckCanStartEvent(championship.ID.String(), active); err != nil {
		t.Errorf("the championship runs on a, starting there must work: %v", err)
	}
}

// A timer of one server must not start an event which is scheduled for another server.
func TestChampionshipManagerStartScheduledEventIgnoresOtherServer(t *testing.T) {
	slots, store := newTestSlots(t, "a", "b")

	championship, event := newSlotTestChampionship(t, store)
	event.Scheduled = time.Now().Add(time.Hour)
	event.ScheduledServerID = "b"

	if err := store.UpsertChampionship(championship); err != nil {
		t.Fatal(err)
	}

	if err := slots["a"].manager.StartScheduledEvent(championship, event); err != nil {
		t.Fatal(err)
	}

	if len(slots["a"].process.Started) != 0 {
		t.Error("slot a must not start an event which is scheduled for slot b")
	}
}
