package raceweekend

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

	smchampionship "github.com/wernersbacher/ACPitwall/internal/championship"
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

// testSlot is the race weekend manager of one server slot with its process.
type testSlot struct {
	manager *RaceWeekendManager
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
		carManager := smcontent.NewCarManager(installPath, false, false)

		raceManager := smrace.NewRaceManager(
			config,
			store,
			process,
			carManager,
			smcontent.NewTrackManager(installPath),
			notifications,
			racecontrol.NewRaceControl(config, racecontrol.NilBroadcaster{}, racecontrol.NilTrackData{}, process, store, racecontrol.NewPenaltiesManager(store, env), env),
			env,
		)

		championshipManager := smchampionship.NewChampionshipManager(raceManager, config, store, process, env, notifications)

		slots[id] = &testSlot{
			process: process,
			manager: NewRaceWeekendManager(config, raceManager, championshipManager, store, process, notifications, carManager, env),
		}
	}

	return slots, store
}

func newSlotTestHandler(slots map[smmodel.ServerID]*testSlot, defaultID smmodel.ServerID, active smchampionship.ActiveEvents) *RaceWeekendHandler {
	return NewRaceWeekendHandler(smweb.NewBaseHandler(nil), func(r *http.Request) (*RaceWeekendManager, error) {
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

func newSlotTestRaceWeekend(t *testing.T, store smstore.Store) (*smmodel.RaceWeekend, *smmodel.RaceWeekendSession) {
	raceWeekend := smmodel.NewRaceWeekend()

	session := smmodel.NewRaceWeekendSession()
	session.RaceConfig = testRaceWeekendEventSetup()
	session.ParentIDs = []uuid.UUID{raceWeekend.ID}
	raceWeekend.AddSession(session, nil)

	if err := store.UpsertRaceWeekend(raceWeekend); err != nil {
		t.Fatal(err)
	}

	return raceWeekend, session
}

func sessionRequest(raceWeekend *smmodel.RaceWeekend, session *smmodel.RaceWeekendSession, server smmodel.ServerID) *http.Request {
	req := smweb.WithScope(httptest.NewRequest(http.MethodGet, "/race-weekend/x/session/y/start", nil), webtest.RequestScope())
	req = smweb.WithRequestedServerID(req, server)

	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("raceWeekendID", raceWeekend.ID.String())
	routeContext.URLParams.Add("sessionID", session.ID.String())

	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeContext))
}

// activeOn is an ActiveEvents with a fixed answer.
type activeOn struct {
	championships map[uuid.UUID]smweb.ServerRef
	raceWeekends  map[uuid.UUID]smweb.ServerRef
}

func (a activeOn) ChampionshipActiveOn(id uuid.UUID) (smweb.ServerRef, bool) {
	ref, ok := a.championships[id]

	return ref, ok
}

func (a activeOn) RaceWeekendActiveOn(id uuid.UUID) (smweb.ServerRef, bool) {
	ref, ok := a.raceWeekends[id]

	return ref, ok
}

func TestRaceWeekendHandlerStartsSessionOnServerOfRequest(t *testing.T) {
	slots, store := newTestSlots(t, "a", "b")
	handler := newSlotTestHandler(slots, "a", nil)
	raceWeekend, session := newSlotTestRaceWeekend(t, store)

	rec := httptest.NewRecorder()
	handler.StartSession(rec, sessionRequest(raceWeekend, session, "b"))

	if rec.Code != http.StatusFound {
		t.Fatalf("expected a redirect, got %d", rec.Code)
	}

	if len(slots["b"].process.Started) != 1 || len(slots["a"].process.Started) != 0 {
		t.Fatalf("server=b must start the session on slot b only: a %d, b %d", len(slots["a"].process.Started), len(slots["b"].process.Started))
	}

	if slots["b"].manager.activeRaceWeekend == nil || slots["a"].manager.activeRaceWeekend != nil {
		t.Error("the active race weekend must belong to slot b only")
	}
}

func TestRaceWeekendHandlerStartsSessionOnDefaultServer(t *testing.T) {
	slots, store := newTestSlots(t, "a", "b")
	handler := newSlotTestHandler(slots, "a", nil)
	raceWeekend, session := newSlotTestRaceWeekend(t, store)

	handler.StartSession(httptest.NewRecorder(), sessionRequest(raceWeekend, session, ""))

	if len(slots["a"].process.Started) != 1 || len(slots["b"].process.Started) != 0 {
		t.Fatalf("a request without server must start the session on the default slot: a %d, b %d", len(slots["a"].process.Started), len(slots["b"].process.Started))
	}
}

func TestRaceWeekendHandlerUnknownServer(t *testing.T) {
	slots, store := newTestSlots(t, "a")
	handler := newSlotTestHandler(slots, "a", nil)
	raceWeekend, session := newSlotTestRaceWeekend(t, store)

	rec := httptest.NewRecorder()
	handler.StartSession(rec, sessionRequest(raceWeekend, session, "missing"))

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for an unknown server, got %d", rec.Code)
	}

	if len(slots["a"].process.Started) != 0 {
		t.Error("nothing must be started for an unknown server")
	}
}

func TestRaceWeekendHandlerBusyServerRejectsSession(t *testing.T) {
	slots, store := newTestSlots(t, "a", "b")
	handler := newSlotTestHandler(slots, "a", nil)
	raceWeekend, session := newSlotTestRaceWeekend(t, store)

	if err := slots["b"].process.Start(&smmodel.QuickRace{}, "", 0, "", 0); err != nil {
		t.Fatal(err)
	}

	if err := slots["b"].manager.CheckCanStartSession(raceWeekend.ID.String(), nil); !errors.Is(err, smchampionship.ErrServerBusy) {
		t.Fatalf("expected ErrServerBusy, got %v", err)
	}

	handler.StartSession(httptest.NewRecorder(), sessionRequest(raceWeekend, session, "b"))

	if len(slots["b"].process.Started) != 1 || slots["b"].process.Stopped != 0 {
		t.Errorf("the running event must not be replaced: started %d, stopped %d", len(slots["b"].process.Started), slots["b"].process.Stopped)
	}

	if err := slots["a"].manager.CheckCanStartSession(raceWeekend.ID.String(), nil); err != nil {
		t.Errorf("server a is not busy: %v", err)
	}
}

func TestRaceWeekendHandlerRaceWeekendActiveOnOtherServer(t *testing.T) {
	slots, store := newTestSlots(t, "a", "b")
	raceWeekend, session := newSlotTestRaceWeekend(t, store)

	active := activeOn{raceWeekends: map[uuid.UUID]smweb.ServerRef{raceWeekend.ID: {ID: "a", Name: "Server A"}}}
	handler := newSlotTestHandler(slots, "a", active)

	err := slots["b"].manager.CheckCanStartSession(raceWeekend.ID.String(), active)

	var elsewhere smchampionship.ActiveElsewhereError

	if !errors.As(err, &elsewhere) || elsewhere.Server.ID != "a" {
		t.Fatalf("expected the race weekend to be active on a, got %v", err)
	}

	if !strings.Contains(err.Error(), "race weekend") || !strings.Contains(err.Error(), "Server A") {
		t.Errorf("the error must name the server: %s", err)
	}

	handler.StartSession(httptest.NewRecorder(), sessionRequest(raceWeekend, session, "b"))
	handler.CancelSession(httptest.NewRecorder(), sessionRequest(raceWeekend, session, "b"))

	if len(slots["b"].process.Started) != 0 || slots["b"].process.Stopped != 0 {
		t.Error("the race weekend must neither be started nor cancelled on b while it is active on a")
	}

	if err := slots["a"].manager.CheckCanStartSession(raceWeekend.ID.String(), active); err != nil {
		t.Errorf("the race weekend runs on a, starting there must work: %v", err)
	}
}

func TestRaceWeekendLinkedChampionshipActiveOnOtherServer(t *testing.T) {
	slots, store := newTestSlots(t, "a", "b")

	championship := smmodel.NewChampionship("Linked")
	raceWeekend, _ := newSlotTestRaceWeekend(t, store)
	raceWeekend.ChampionshipID = championship.ID

	if err := store.UpsertChampionship(championship); err != nil {
		t.Fatal(err)
	}

	if err := store.UpsertRaceWeekend(raceWeekend); err != nil {
		t.Fatal(err)
	}

	active := activeOn{championships: map[uuid.UUID]smweb.ServerRef{championship.ID: {ID: "a", Name: "Server A"}}}

	var elsewhere smchampionship.ActiveElsewhereError

	if err := slots["b"].manager.CheckCanStartSession(raceWeekend.ID.String(), active); !errors.As(err, &elsewhere) {
		t.Errorf("a session of a race weekend whose championship runs on a must be rejected on b, got %v", err)
	}
}

// A timer must not start a session whose schedule was changed or moved to another server.
func TestRaceWeekendManagerScheduleStillValid(t *testing.T) {
	slots, store := newTestSlots(t, "a", "b")
	raceWeekend, session := newSlotTestRaceWeekend(t, store)

	scheduled := time.Now().Add(time.Hour)
	session.ScheduledTime = scheduled
	session.ScheduledServerID = "b"

	if err := store.UpsertRaceWeekend(raceWeekend); err != nil {
		t.Fatal(err)
	}

	if !slots["b"].manager.scheduleStillValid(raceWeekend.ID.String(), session.ID.String(), scheduled) {
		t.Error("the schedule is valid on b")
	}

	if slots["a"].manager.scheduleStillValid(raceWeekend.ID.String(), session.ID.String(), scheduled) {
		t.Error("the session is scheduled for b, the timer of a must not start it")
	}

	if slots["b"].manager.scheduleStillValid(raceWeekend.ID.String(), session.ID.String(), scheduled.Add(time.Minute)) {
		t.Error("a timer of an older schedule must not start the session")
	}
}
