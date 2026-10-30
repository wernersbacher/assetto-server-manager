package race

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi"
	"github.com/google/uuid"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smconfig "github.com/wernersbacher/assetto-server-manager/internal/config"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	"github.com/wernersbacher/assetto-server-manager/internal/notification/notificationtest"
	"github.com/wernersbacher/assetto-server-manager/internal/server/servertest"
	"github.com/wernersbacher/assetto-server-manager/internal/serverenv/envtest"
	smstore "github.com/wernersbacher/assetto-server-manager/internal/store"
	smweb "github.com/wernersbacher/assetto-server-manager/internal/web"
	"github.com/wernersbacher/assetto-server-manager/internal/web/webtest"
)

type testSlot struct {
	manager *RaceManager
	process *servertest.Interruptible
}

// newTestSlots builds a race manager with its own process and install for each id, on a shared store.
func newTestSlots(t *testing.T, ids ...smmodel.ServerID) (map[smmodel.ServerID]*testSlot, smstore.Store) {
	store, cleanup := envtest.TempJSONStore()
	t.Cleanup(cleanup)

	slots := make(map[smmodel.ServerID]*testSlot)

	for _, id := range ids {
		env := envtest.New(t.TempDir())
		serverID := id
		env.ServerID = func() smmodel.ServerID { return serverID }

		process := &servertest.Interruptible{}

		slots[id] = &testSlot{
			process: process,
			manager: NewRaceManager(&smconfig.Configuration{}, store, process, nil, nil, &notificationtest.Dummy{}, nil, env),
		}
	}

	return slots, store
}

func slotManagerFor(slots map[smmodel.ServerID]*testSlot, defaultID smmodel.ServerID) ManagerFor {
	return func(r *http.Request) (*RaceManager, error) {
		id := smweb.RequestedServerID(r)

		if id == "" {
			id = defaultID
		}

		slot, ok := slots[id]

		if !ok {
			return nil, http.ErrNoLocation
		}

		return slot.manager, nil
	}
}

func raceRequest(method, target, raceID string, form url.Values, requested smmodel.ServerID) *http.Request {
	var req *http.Request

	if form != nil {
		req = httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}

	req.Header.Set("Referer", "/custom")
	req = smweb.WithScope(req, webtest.RequestScope())

	if requested != "" {
		req = smweb.WithRequestedServerID(req, requested)
	}

	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("uuid", raceID)

	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeContext))
}

func newTestCustomRace(t *testing.T, store smstore.Store) *smmodel.CustomRace {
	race := &smmodel.CustomRace{UUID: uuid.New(), Name: "Slot Race"}
	race.RaceConfig = serverconfig.ConfigIniDefault().CurrentRaceConfig

	if err := store.UpsertCustomRace(race); err != nil {
		t.Fatal(err)
	}

	return race
}

func TestCustomRaceHandlerStartRejectsBusyServer(t *testing.T) {
	slots, store := newTestSlots(t, "a", "b")
	handler := NewCustomRaceHandler(&smconfig.Configuration{}, smweb.NewBaseHandler(nil), slotManagerFor(slots, "a"), store)
	race := newTestCustomRace(t, store)

	if err := slots["b"].process.Start(&smmodel.QuickRace{}, "", 0, "", 0); err != nil {
		t.Fatal(err)
	}

	if err := slots["b"].manager.CheckCanStart(); err != ErrServerBusy {
		t.Fatalf("expected ErrServerBusy, got %v", err)
	}

	rec := httptest.NewRecorder()
	handler.Start(rec, raceRequest(http.MethodGet, "/custom/load/"+race.UUID.String(), race.UUID.String(), nil, "b"))

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/custom" {
		t.Fatalf("expected a redirect back with a flash message, got %d %q", rec.Code, rec.Header().Get("Location"))
	}

	if len(slots["b"].process.Started) != 1 || slots["b"].process.Stopped != 0 {
		t.Errorf("the running event must not be replaced: started %d, stopped %d", len(slots["b"].process.Started), slots["b"].process.Stopped)
	}

	// the other server is free
	if err := slots["a"].manager.CheckCanStart(); err != nil {
		t.Fatalf("server a is not busy: %v", err)
	}
}

func TestQuickRaceHandlerSubmitRejectsBusyServer(t *testing.T) {
	slots, _ := newTestSlots(t, "a", "b")
	handler := NewQuickRaceHandler(&smconfig.Configuration{}, smweb.NewBaseHandler(nil), slotManagerFor(slots, "a"))

	if err := slots["b"].process.Start(&smmodel.QuickRace{}, "", 0, "", 0); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	handler.Submit(rec, raceRequest(http.MethodPost, "/quick/submit", "", url.Values{"Cars": {"car"}}, "b"))

	if rec.Code != http.StatusFound || len(slots["b"].process.Started) != 1 || slots["b"].process.Stopped != 0 {
		t.Errorf("a quick race must not replace the running event: %d, started %d, stopped %d", rec.Code, len(slots["b"].process.Started), slots["b"].process.Stopped)
	}
}

func TestCustomRaceHandlerScheduleSelectsServer(t *testing.T) {
	slots, store := newTestSlots(t, "a", "b")
	handler := NewCustomRaceHandler(&smconfig.Configuration{}, smweb.NewBaseHandler(nil), slotManagerFor(slots, "a"), store)
	race := newTestCustomRace(t, store)

	when := time.Now().Add(48 * time.Hour)

	schedule := func(form url.Values) *httptest.ResponseRecorder {
		form.Set("event-schedule-date", when.Format("2006-01-02"))
		form.Set("event-schedule-time", when.Format("15:04"))
		form.Set("event-schedule-timezone", "UTC")
		form.Set("action", "add")

		rec := httptest.NewRecorder()
		handler.Schedule(rec, raceRequest(http.MethodPost, "/custom/schedule/"+race.UUID.String(), race.UUID.String(), form, "a"))

		return rec
	}

	defer func() {
		for _, slot := range slots {
			for _, timer := range slot.manager.customRaceStartTimers {
				timer.Stop()
			}
		}
	}()

	if rec := schedule(url.Values{smweb.ScheduleServerParam: {"b"}}); rec.Code != http.StatusFound {
		t.Fatalf("expected a redirect, got %d", rec.Code)
	}

	stored, err := store.FindCustomRaceByID(race.UUID.String())

	if err != nil {
		t.Fatal(err)
	}

	if stored.ScheduledServerID != "b" || stored.ScheduledEvents["b"] == nil || stored.ScheduledEvents["a"] != nil {
		t.Errorf("the race must be scheduled for server b only: %q, %+v", stored.ScheduledServerID, stored.ScheduledEvents)
	}

	if slots["b"].manager.customRaceStartTimers[race.UUID.String()] == nil || slots["a"].manager.customRaceStartTimers[race.UUID.String()] != nil {
		t.Error("the start timer must run on the manager of the selected server")
	}

	// without a selection the server of the request is used
	if rec := schedule(url.Values{}); rec.Code != http.StatusFound {
		t.Fatalf("expected a redirect, got %d", rec.Code)
	}

	stored, _ = store.FindCustomRaceByID(race.UUID.String())

	if stored.ScheduledServerID != "a" || stored.ScheduledEvents["a"] == nil {
		t.Errorf("the race must be scheduled for the server of the request: %q, %+v", stored.ScheduledServerID, stored.ScheduledEvents)
	}

	// unknown servers are rejected
	if rec := schedule(url.Values{smweb.ScheduleServerParam: {"missing"}}); rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for an unknown server, got %d", rec.Code)
	}
}
