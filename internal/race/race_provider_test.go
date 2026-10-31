package race

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi"
	"github.com/google/uuid"

	smconfig "github.com/wernersbacher/ACPitwall/internal/config"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/server/servertest"
	"github.com/wernersbacher/ACPitwall/internal/serverenv/envtest"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
	"github.com/wernersbacher/ACPitwall/internal/web/webtest"
)

func TestCustomRaceHandlerUsesManagerOfRequest(t *testing.T) {
	store, cleanup := envtest.TempJSONStore()
	defer cleanup()

	managers := make(map[smmodel.ServerID]*RaceManager)

	for _, id := range []smmodel.ServerID{envtest.ServerID, "b"} {
		env := envtest.New(t.TempDir())
		serverID := id
		env.ServerID = func() smmodel.ServerID { return serverID }

		managers[id] = NewRaceManager(&smconfig.Configuration{}, store, servertest.Dummy{}, nil, nil, nil, nil, env)
	}

	handler := NewCustomRaceHandler(&smconfig.Configuration{}, smweb.NewBaseHandler(nil), func(r *http.Request) (*RaceManager, error) {
		id := smweb.RequestedServerID(r)

		if id == "" {
			id = envtest.ServerID
		}

		manager, ok := managers[id]

		if !ok {
			return nil, http.ErrNoLocation
		}

		return manager, nil
	}, store)

	raceID := uuid.New()

	loop := func(requested smmodel.ServerID) int {
		req := smweb.WithScope(httptest.NewRequest(http.MethodPost, "/custom/"+raceID.String()+"/loop", nil), webtest.RequestScope())
		req = smweb.WithRequestedServerID(req, requested)

		routeContext := chi.NewRouteContext()
		routeContext.URLParams.Add("uuid", raceID.String())
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeContext))

		rec := httptest.NewRecorder()
		handler.Loop(rec, req)

		return rec.Code
	}

	if err := store.UpsertCustomRace(&smmodel.CustomRace{UUID: raceID}); err != nil {
		t.Fatal(err)
	}

	if code := loop("b"); code != http.StatusFound {
		t.Fatalf("expected a redirect, got %d", code)
	}

	race, err := store.FindCustomRaceByID(raceID.String())

	if err != nil {
		t.Fatal(err)
	}

	if !race.IsLooping("b") || race.IsLooping(envtest.ServerID) {
		t.Errorf("server=b must loop the race on slot b only: %+v", race.LoopServer)
	}

	if code := loop(""); code != http.StatusFound {
		t.Fatalf("expected a redirect, got %d", code)
	}

	race, _ = store.FindCustomRaceByID(raceID.String())

	if !race.IsLooping(envtest.ServerID) {
		t.Errorf("a request without server must use the default slot: %+v", race.LoopServer)
	}

	if code := loop("missing"); code != http.StatusNotFound {
		t.Errorf("expected 404 for an unknown server, got %d", code)
	}
}
