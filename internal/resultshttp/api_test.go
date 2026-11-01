package resultshttp

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi"
	"github.com/google/uuid"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smresults "github.com/wernersbacher/ACPitwall/internal/results"
	"github.com/wernersbacher/ACPitwall/internal/serverenv"
	"github.com/wernersbacher/ACPitwall/internal/serverenv/envtest"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
	"github.com/wernersbacher/ACPitwall/internal/web/webtest"
)

func setupResultsAPITest(t *testing.T) func() {
	t.Helper()

	dir, err := ioutil.TempDir("", "results-api")

	if err != nil {
		t.Fatal(err)
	}

	resultsDir := filepath.Join(dir, "results")

	if err := os.MkdirAll(resultsDir, 0755); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"2019_2_15_21_16_RACE.json", "2019_2_15_21_8_QUALIFY.json", "2019_3_2_14_41_PRACTICE.json"} {
		fixture, err := ioutil.ReadFile(filepath.Join("..", "..", "fixtures", "results", name))

		if err != nil {
			t.Fatal(err)
		}

		if err := ioutil.WriteFile(filepath.Join(resultsDir, name), fixture, 0644); err != nil {
			t.Fatal(err)
		}
	}

	oldShortened := smresults.UseShortenedDriverNames
	testInstallPath = dir
	smresults.UseShortenedDriverNames = false

	resetResultSummaryCache()

	return func() {
		smresults.UseShortenedDriverNames = oldShortened

		resetResultSummaryCache()

		_ = os.RemoveAll(dir)
	}
}

func resetResultSummaryCache() {
	smresults.ClearSummaryCache()
}

func getResultsAPI(t *testing.T, target string) *httptest.ResponseRecorder {
	t.Helper()

	return getResultsAPIWithClasses(t, nil, target)
}

func getResultsAPIWithClasses(t *testing.T, classes smweb.CarClasses, target string) *httptest.ResponseRecorder {
	t.Helper()

	rh := &ResultsHandler{CarClasses: classes, env: testEnv(), store: smstore.NewJSONStore(filepath.Join(testInstallPath, "store"), filepath.Join(testInstallPath, "store-shared"))}

	r := chi.NewRouter()
	r.Get("/api/results", rh.ListAPI)
	r.Get("/api/results/{fileName}", rh.ViewAPI)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, smweb.WithScope(httptest.NewRequest(http.MethodGet, target, nil), webtest.RequestScope()))

	return rec
}

func TestResultsHandlerListAPI(t *testing.T) {
	defer setupResultsAPITest(t)()

	list := func(t *testing.T, target string) *APIResultsList {
		t.Helper()

		rec := getResultsAPI(t, target)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		if contentType := rec.Header().Get("Content-Type"); contentType != "application/json" {
			t.Errorf("expected json content type, got %s", contentType)
		}

		var out APIResultsList

		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}

		return &out
	}

	t.Run("newest first", func(t *testing.T) {
		out := list(t, "/api/results")

		if out.Page != 0 || out.Pages != 1 || len(out.Results) != 3 {
			t.Fatalf("unexpected list: %+v", out)
		}

		expected := []string{"2019_3_2_14_41_PRACTICE.json", "2019_2_15_21_16_RACE.json", "2019_2_15_21_8_QUALIFY.json"}

		for i, fileName := range expected {
			if out.Results[i].FileName != fileName {
				t.Errorf("result %d: expected %s, got %s", i, fileName, out.Results[i].FileName)
			}
		}

		race := out.Results[1]

		if race.SessionType != serverconfig.SessionTypeRace || race.Track != "barbagallo" || race.TrackName != "Barbagallo" || race.Date.IsZero() {
			t.Errorf("unexpected race summary: %+v", race)
		}

		if len(race.Drivers) == 0 || race.Drivers[0] != "Joseph Elton" {
			t.Errorf("unexpected race drivers: %v", race.Drivers)
		}
	})

	t.Run("type filter", func(t *testing.T) {
		out := list(t, "/api/results?type=race")

		if len(out.Results) != 1 || out.Results[0].FileName != "2019_2_15_21_16_RACE.json" {
			t.Fatalf("unexpected filtered list: %+v", out)
		}
	})

	t.Run("driver filter", func(t *testing.T) {
		if out := list(t, "/api/results?driver=joseph"); len(out.Results) == 0 {
			t.Error("expected results for driver joseph")
		}

		if out := list(t, "/api/results?driver=nobody-with-this-name"); len(out.Results) != 0 || out.Pages != 0 {
			t.Errorf("expected no results, got %+v", out)
		}
	})

	t.Run("page not found", func(t *testing.T) {
		if rec := getResultsAPI(t, "/api/results?page=1"); rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rec.Code)
		}
	})

	t.Run("empty results are a list", func(t *testing.T) {
		rec := getResultsAPI(t, "/api/results?track=does-not-exist")

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var raw map[string]json.RawMessage

		if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
			t.Fatal(err)
		}

		if string(raw["Results"]) != "[]" {
			t.Errorf("expected empty list, got %s", raw["Results"])
		}
	})
}

func TestResultsHandlerViewAPI(t *testing.T) {
	defer setupResultsAPITest(t)()

	view := func(t *testing.T, target string) *APIResult {
		t.Helper()

		rec := getResultsAPI(t, target)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", target, rec.Code)
		}

		var out APIResult

		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}

		return &out
	}

	for _, target := range []string{"/api/results/2019_2_15_21_16_RACE.json", "/api/results/2019_2_15_21_16_RACE"} {
		t.Run("found "+target, func(t *testing.T) {
			out := view(t, target)

			if out.FileName != "2019_2_15_21_16_RACE.json" || out.SessionType != serverconfig.SessionTypeRace || out.TrackName != "Barbagallo" {
				t.Errorf("unexpected result: %+v", out.APIResultSummary)
			}

			if len(out.Classification) == 0 || len(out.Laps) == 0 {
				t.Fatalf("expected classification and laps, got %d and %d", len(out.Classification), len(out.Laps))
			}

			winner := out.Classification[0]

			if winner.Position != 1 || winner.DriverName != "Joseph Elton" || winner.DriverGUID != "76561198029578060" ||
				winner.CarModel != "rss_formula_rss_4" || winner.CarName == "" || winner.BestLap != 57521 || winner.TotalTime != 363829 || winner.Laps == 0 {
				t.Errorf("unexpected winner: %+v", winner)
			}

			if lap := out.Laps[0]; lap.Number != 1 || lap.LapTime == 0 || len(lap.Sectors) == 0 || lap.DriverName == "" {
				t.Errorf("unexpected first lap: %+v", lap)
			}
		})
	}

	t.Run("team", func(t *testing.T) {
		out := view(t, "/api/results/2019_2_15_21_16_RACE.json")

		if out.Classification[0].Team == "" {
			t.Errorf("expected a team for %+v", out.Classification[0])
		}
	})

	t.Run("shortened driver names", func(t *testing.T) {
		smresults.UseShortenedDriverNames = true
		defer func() { smresults.UseShortenedDriverNames = false }()

		out := view(t, "/api/results/2019_2_15_21_16_RACE.json")

		if out.Classification[0].DriverName != "Joseph E." || out.Drivers[0] != "Joseph E." {
			t.Errorf("driver names were not shortened: %+v %v", out.Classification[0], out.Drivers)
		}
	})

	t.Run("not found", func(t *testing.T) {
		for _, target := range []string{"/api/results/2019_1_1_1_1_RACE.json", "/api/results/2019_1_1_1_1_RACE"} {
			if rec := getResultsAPI(t, target); rec.Code != http.StatusNotFound {
				t.Errorf("%s: expected 404, got %d", target, rec.Code)
			}
		}
	})

	t.Run("invalid file names", func(t *testing.T) {
		for _, target := range []string{"/api/results/.json", "/api/results/..%2Fsecret.json", "/api/results/%5Csecret.json", "/api/results/sub%2Ffile"} {
			if rec := getResultsAPI(t, target); rec.Code != http.StatusNotFound {
				t.Errorf("%s: expected 404, got %d", target, rec.Code)
			}
		}
	})
}

// testInstallPath is the install path of the Env which testEnv returns. The tests which need files set it to a temporary directory.
var testInstallPath string

func testEnv() *serverenv.Env {
	return envtest.New(testInstallPath)
}

func TestResultsHandlerListAPICarClass(t *testing.T) {
	defer setupResultsAPITest(t)()

	formula := &smmodel.CarClass{ID: uuid.New(), Name: "Formula", Cars: []string{"rss_formula_rss_4"}}
	gt3 := &smmodel.CarClass{ID: uuid.New(), Name: "GT3", Cars: []string{"ks_mclaren_650_gt3", "ks_nissan_gtr_gt3"}}
	empty := &smmodel.CarClass{ID: uuid.New(), Name: "Nobody drove it", Cars: []string{"no_such_car"}}
	classes := webtest.CarClasses{formula, gt3, empty}

	list := func(t *testing.T, query string) []string {
		t.Helper()

		rec := getResultsAPIWithClasses(t, classes, "/api/results?"+query)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d: %s", query, rec.Code, rec.Body.String())
		}

		var out APIResultsList

		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}

		names := []string{}

		for _, result := range out.Results {
			names = append(names, result.FileName)
		}

		return names
	}

	equal := func(t *testing.T, got []string, want ...string) {
		t.Helper()

		if len(got) != len(want) {
			t.Fatalf("expected %v, got %v", want, got)
		}

		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("expected %v, got %v", want, got)
			}
		}
	}

	t.Run("one class", func(t *testing.T) {
		equal(t, list(t, "carClass="+formula.ID.String()), "2019_2_15_21_16_RACE.json", "2019_2_15_21_8_QUALIFY.json")
		equal(t, list(t, "carClass="+gt3.ID.String()), "2019_3_2_14_41_PRACTICE.json")
	})

	t.Run("several classes are a union", func(t *testing.T) {
		equal(t, list(t, "carClass="+formula.ID.String()+"&carClass="+gt3.ID.String()),
			"2019_3_2_14_41_PRACTICE.json", "2019_2_15_21_16_RACE.json", "2019_2_15_21_8_QUALIFY.json")
	})

	t.Run("class and car are a union", func(t *testing.T) {
		equal(t, list(t, "carClass="+gt3.ID.String()+"&car=rss_formula_rss_4"),
			"2019_3_2_14_41_PRACTICE.json", "2019_2_15_21_16_RACE.json", "2019_2_15_21_8_QUALIFY.json")
		equal(t, list(t, "carClass="+formula.ID.String()+"&car=rss_formula_rss_4"), "2019_2_15_21_16_RACE.json", "2019_2_15_21_8_QUALIFY.json")
	})

	t.Run("combines with the other filters", func(t *testing.T) {
		equal(t, list(t, "type=race&carClass="+formula.ID.String()), "2019_2_15_21_16_RACE.json")
		equal(t, list(t, "type=practice&carClass="+formula.ID.String()))
	})

	t.Run("class nobody drove is an empty list", func(t *testing.T) {
		equal(t, list(t, "carClass="+empty.ID.String()))
	})

	t.Run("unknown or invalid class is a 400", func(t *testing.T) {
		for _, query := range []string{
			"carClass=" + uuid.New().String(),
			"carClass=../x",
			"carClass=" + formula.ID.String() + "&carClass=" + uuid.New().String(),
			"car=../x",
		} {
			if rec := getResultsAPIWithClasses(t, classes, "/api/results?"+query); rec.Code != http.StatusBadRequest {
				t.Errorf("%s: expected 400, got %d", query, rec.Code)
			}
		}

		if rec := getResultsAPIWithClasses(t, nil, "/api/results?carClass="+formula.ID.String()); rec.Code != http.StatusBadRequest {
			t.Errorf("no classes: expected 400, got %d", rec.Code)
		}
	})
}
