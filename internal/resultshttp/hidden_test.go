package resultshttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi"
	"github.com/google/uuid"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smresults "github.com/wernersbacher/ACPitwall/internal/results"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
	"github.com/wernersbacher/ACPitwall/internal/web/webtest"
)

const (
	hiddenChampionshipFile = "2019_2_15_21_16_RACE"    // session of a hidden championship
	hiddenWeekendFile      = "2019_2_15_21_8_QUALIFY"  // session of a hidden standalone race weekend
	linkedWeekendFile      = "2019_3_2_14_41_PRACTICE" // session of a weekend in a hidden championship
)

func TestHiddenResults(t *testing.T) {
	defer setupResultsAPITest(t)()

	store := smstore.NewJSONStore(filepath.Join(testInstallPath, "store"), filepath.Join(testInstallPath, "store-shared"))

	hiddenChampionship := smmodel.NewChampionship("Hidden")
	hiddenChampionship.Hidden = true
	hiddenChampionship.Events = []*smmodel.ChampionshipEvent{
		{Sessions: map[serverconfig.SessionType]*smmodel.ChampionshipSession{
			serverconfig.SessionTypeRace: {Results: &smresults.SessionResults{SessionFile: hiddenChampionshipFile}},
		}},
	}

	visibleChampionship := smmodel.NewChampionship("Visible")

	hiddenWeekend := &smmodel.RaceWeekend{
		ID:     uuid.New(),
		Hidden: true,
		Sessions: []*smmodel.RaceWeekendSession{
			{Results: &smresults.SessionResults{SessionFile: hiddenWeekendFile}},
		},
	}

	linkedWeekend := &smmodel.RaceWeekend{
		ID:             uuid.New(),
		ChampionshipID: hiddenChampionship.ID,
		Sessions: []*smmodel.RaceWeekendSession{
			{Results: &smresults.SessionResults{SessionFile: linkedWeekendFile}},
		},
	}

	for _, c := range []*smmodel.Championship{hiddenChampionship, visibleChampionship} {
		if err := store.UpsertChampionship(c); err != nil {
			t.Fatal(err)
		}
	}

	for _, rw := range []*smmodel.RaceWeekend{hiddenWeekend, linkedWeekend} {
		if err := store.UpsertRaceWeekend(rw); err != nil {
			t.Fatal(err)
		}
	}

	rh := &ResultsHandler{store: store, env: testEnv()}

	router := chi.NewRouter()
	router.Get("/results", rh.List)
	router.Get("/results/{fileName}", rh.View)
	router.Get("/results/{fileName}/collisions", rh.RenderCollisions)
	router.Get("/results/download/{fileName}", rh.File)
	router.Get("/api/results", rh.ListAPI)
	router.Get("/api/results/{fileName}", rh.ViewAPI)

	reader := &smmodel.Account{Name: "reader", Groups: map[smmodel.ServerID]smmodel.Group{"": smmodel.GroupRead}}

	get := func(account *smmodel.Account, target string) *httptest.ResponseRecorder {
		scope := webtest.RequestScope()

		req := smweb.WithScope(httptest.NewRequest(http.MethodGet, target, nil), scope)

		if account != nil {
			req = smweb.WithAccount(req, account)
		}

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		return rec
	}

	for _, name := range []string{hiddenChampionshipFile, hiddenWeekendFile, linkedWeekendFile} {
		for _, target := range []string{
			"/results/" + name,
			"/results/" + name + "/collisions?collisions=all",
			"/results/download/" + name + ".json",
			"/api/results/" + name,
		} {
			if rec := get(nil, target); rec.Code != http.StatusNotFound {
				t.Errorf("%s: anonymous visitors must get 404, got %d", target, rec.Code)
			}
		}

		for _, target := range []string{"/results/download/" + name + ".json", "/api/results/" + name} {
			if rec := get(reader, target); rec.Code != http.StatusOK {
				t.Errorf("%s: logged in accounts must see the results, got %d", target, rec.Code)
			}
		}
	}

	t.Run("api list", func(t *testing.T) {
		count := func(account *smmodel.Account) int {
			rec := get(account, "/api/results")

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", rec.Code)
			}

			return len(decodeResultsList(t, rec.Body.Bytes()).Results)
		}

		if n := count(nil); n != 0 {
			t.Errorf("anonymous visitors must not see any of the 3 hidden results, got %d", n)
		}

		if n := count(reader); n != 3 {
			t.Errorf("logged in accounts see all results, got %d", n)
		}
	})

	t.Run("visible championship results stay visible", func(t *testing.T) {
		visibleChampionship.Events = []*smmodel.ChampionshipEvent{
			{Sessions: map[serverconfig.SessionType]*smmodel.ChampionshipSession{
				serverconfig.SessionTypeRace: {Results: &smresults.SessionResults{SessionFile: hiddenChampionshipFile}},
			}},
		}

		hiddenChampionship.Hidden = false
		hiddenChampionship.Events = nil
		hiddenWeekend.Hidden = false

		for _, c := range []*smmodel.Championship{hiddenChampionship, visibleChampionship} {
			if err := store.UpsertChampionship(c); err != nil {
				t.Fatal(err)
			}
		}

		if err := store.UpsertRaceWeekend(hiddenWeekend); err != nil {
			t.Fatal(err)
		}

		for _, name := range []string{hiddenChampionshipFile, hiddenWeekendFile, linkedWeekendFile} {
			if rec := get(nil, "/api/results/"+name); rec.Code != http.StatusOK {
				t.Errorf("%s: expected 200, got %d", name, rec.Code)
			}
		}
	})
}

func TestHiddenResultsByStoredIDs(t *testing.T) {
	hidden := &HiddenResults{
		files:        map[string]bool{"a": true},
		championship: map[string]bool{"champ": true},
		raceWeekend:  map[string]bool{"rw": true},
	}

	for _, tc := range []struct {
		file, championship, raceWeekend string
		want                            bool
	}{
		{"a.json", "", "", true},
		{"a", "", "", true},
		{"b.json", "champ", "", true},
		{"b.json", "", "rw", true},
		{"b.json", "other", "other", false},
		{"b.json", "", "", false},
	} {
		if got := hidden.Hides(tc.file, tc.championship, tc.raceWeekend); got != tc.want {
			t.Errorf("%+v: got %v", tc, got)
		}
	}

	var none *HiddenResults

	if none.Hides("a.json", "champ", "rw") || none.HideFunc() != nil {
		t.Error("nil index must hide nothing")
	}
}

func decodeResultsList(t *testing.T, body []byte) *APIResultsList {
	t.Helper()

	var out APIResultsList

	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}

	return &out
}
