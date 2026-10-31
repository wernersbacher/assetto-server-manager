package resultshttp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi"

	smweb "github.com/wernersbacher/ACPitwall/internal/web"
	"github.com/wernersbacher/ACPitwall/internal/web/webtest"
)

// The collisions overlay needs the map of the track. Without it (e.g. a track without data/map.ini) there is nothing
// to draw on, which is "not found" and not a server error.
func TestResultsHandlerRenderCollisions_NoTrackMap(t *testing.T) {
	store, _, cleanup := setupResultsDeleteTest(t)
	defer cleanup()

	rh := &ResultsHandler{store: store, env: testEnv()}

	r := chi.NewRouter()
	r.Get("/results/{fileName}/collisions", rh.RenderCollisions)

	req := smweb.WithScope(httptest.NewRequest(http.MethodGet, "/results/2019_2_15_21_16_RACE/collisions?collisions=all", nil), webtest.RequestScope())
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 without track map, got %d", rec.Code)
	}
}
