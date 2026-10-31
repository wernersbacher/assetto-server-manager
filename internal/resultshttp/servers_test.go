package resultshttp

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smresults "github.com/wernersbacher/ACPitwall/internal/results"
	"github.com/wernersbacher/ACPitwall/internal/serverenv/envtest"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
	"github.com/wernersbacher/ACPitwall/internal/web/webtest"
)

// twoServers sets up two server slots: "a" holds a race and a qualifying, "b" a practice session.
func twoServers(t *testing.T) (rh *ResultsHandler, dirA, dirB string, cleanup func()) {
	t.Helper()

	dir, err := ioutil.TempDir("", "results-servers")

	if err != nil {
		t.Fatal(err)
	}

	dirA, dirB = filepath.Join(dir, "a", "results"), filepath.Join(dir, "b", "results")

	for _, d := range []string{dirA, dirB} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}

	place := func(d, name string) {
		fixture, err := ioutil.ReadFile(filepath.Join("..", "..", "fixtures", "results", name))

		if err != nil {
			t.Fatal(err)
		}

		if err := ioutil.WriteFile(filepath.Join(d, name), fixture, 0644); err != nil {
			t.Fatal(err)
		}
	}

	place(dirA, "2019_2_15_21_16_RACE.json")
	place(dirA, "2019_2_15_21_8_QUALIFY.json")
	place(dirB, "2019_3_2_14_41_PRACTICE.json")

	env := envtest.New(filepath.Join(dir, "a"))
	env.ResultsDirs = func() []string { return []string{dirA, dirB} }

	store, cleanStore := envtest.TempJSONStore()

	smresults.ClearSummaryCache()

	rh = &ResultsHandler{
		store: store,
		env:   env,
		servers: func() []ServerDir {
			return []ServerDir{
				{ServerRef: smweb.ServerRef{ID: "a", Name: "Server A"}, ResultsDir: dirA},
				{ServerRef: smweb.ServerRef{ID: "b", Name: "Server B"}, ResultsDir: dirB},
			}
		},
	}

	return rh, dirA, dirB, func() {
		smresults.ClearSummaryCache()
		cleanStore()
		_ = os.RemoveAll(dir)
	}
}

func getAPI(t *testing.T, rh *ResultsHandler, target string) *httptest.ResponseRecorder {
	t.Helper()

	r := chi.NewRouter()
	r.Get("/api/results", rh.ListAPI)
	r.Get("/api/results/{fileName}", rh.ViewAPI)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))

	return rec
}

func TestResultsAcrossServers(t *testing.T) {
	rh, dirA, dirB, cleanup := twoServers(t)
	defer cleanup()

	list := func(target string) *APIResultsList {
		t.Helper()

		rec := getAPI(t, rh, target)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", target, rec.Code)
		}

		var out APIResultsList

		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}

		return &out
	}

	t.Run("list shows the results of all servers with their origin", func(t *testing.T) {
		out := list("/api/results")

		if len(out.Results) != 3 {
			t.Fatalf("expected 3 results, got %d", len(out.Results))
		}

		want := map[string]string{
			"2019_3_2_14_41_PRACTICE.json": "b",
			"2019_2_15_21_16_RACE.json":    "a",
			"2019_2_15_21_8_QUALIFY.json":  "a",
		}

		for _, result := range out.Results {
			if result.Server == nil || result.Server.ID != want[result.FileName] {
				t.Errorf("%s: unexpected server %+v", result.FileName, result.Server)
			}
		}

		if out.Results[0].Server.Name != "Server B" {
			t.Errorf("unexpected server name %q", out.Results[0].Server.Name)
		}
	})

	t.Run("filterServer", func(t *testing.T) {
		out := list("/api/results?filterServer=b")

		if len(out.Results) != 1 || out.Results[0].FileName != "2019_3_2_14_41_PRACTICE.json" {
			t.Fatalf("unexpected list for server b: %+v", out.Results)
		}

		out = list("/api/results?filterServer=a&type=race")

		if len(out.Results) != 1 || out.Results[0].FileName != "2019_2_15_21_16_RACE.json" {
			t.Fatalf("unexpected list for server a and race: %+v", out.Results)
		}

		if out = list("/api/results?filterServer=nope"); len(out.Results) != 0 {
			t.Fatalf("an unknown server matched %d results", len(out.Results))
		}
	})

	t.Run("single result", func(t *testing.T) {
		rec := getAPI(t, rh, "/api/results/2019_3_2_14_41_PRACTICE")

		var out APIResult

		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
			t.Fatalf("unexpected response %d", rec.Code)
		}

		if out.Server == nil || out.Server.ID != "b" {
			t.Errorf("unexpected server %+v", out.Server)
		}
	})

	t.Run("server of a result", func(t *testing.T) {
		index := rh.serverIndex()

		if server := index.of(rh.env.Results(), "2019_3_2_14_41_PRACTICE.json"); server == nil || server.ID != smmodel.ServerID("b") {
			t.Errorf("unexpected server %+v", server)
		}
	})

	t.Run("delete from the second server", func(t *testing.T) {
		rec := postResultsDelete(t, rh, "2019_3_2_14_41_PRACTICE.json")

		if rec.Code != http.StatusFound {
			t.Fatalf("expected redirect, got %d", rec.Code)
		}

		if fileExists(filepath.Join(dirB, "2019_3_2_14_41_PRACTICE.json")) {
			t.Fatal("results file of the second server was not deleted")
		}

		if !fileExists(filepath.Join(dirA, "2019_2_15_21_16_RACE.json")) {
			t.Fatal("results of the first server were deleted")
		}
	})
}

func TestResultsEditAndCollisionsOfSecondServer(t *testing.T) {
	rh, dirA, dirB, cleanup := twoServers(t)
	defer cleanup()

	const fileName = "2019_3_2_14_41_PRACTICE"

	r := chi.NewRouter()
	r.Post("/results/{fileName}/edit", rh.Edit)

	req := httptest.NewRequest(http.MethodPost, "/results/"+fileName+"/edit", strings.NewReader("guid:76561198029578060=Renamed"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = smweb.WithScope(req, webtest.RequestScope())

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", rec.Code)
	}

	if fileExists(filepath.Join(dirA, fileName+".json")) {
		t.Fatal("editing the result created a copy in the first server's results")
	}

	if !fileExists(filepath.Join(dirB, fileName+".json")) {
		t.Fatal("result of the second server is gone")
	}

	// the collisions of a result are loaded from whichever directory holds it
	if _, err := rh.env.Results().Load(fileName + ".json"); err != nil {
		t.Fatal(err)
	}
}

func TestResultsSingleServerUnchanged(t *testing.T) {
	defer setupResultsAPITest(t)()

	for _, servers := range []ServersFunc{
		nil,
		func() []ServerDir {
			return []ServerDir{{ServerRef: smweb.ServerRef{ID: "a", Name: "Only"}, ResultsDir: filepath.Join(testInstallPath, "results")}}
		},
	} {
		rh := &ResultsHandler{env: testEnv(), servers: servers}

		rec := getAPI(t, rh, "/api/results")

		if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"Server"`) {
			t.Fatalf("a single server must not change the output: %d %s", rec.Code, rec.Body.String())
		}

		// filterServer is ignored with a single server
		if rec = getAPI(t, rh, "/api/results?filterServer=zzz"); strings.Contains(rec.Body.String(), `"Results":[]`) {
			t.Fatalf("filterServer filtered with a single server: %s", rec.Body.String())
		}
	}
}

func TestResultsUploadRefusedForNameOfOtherServer(t *testing.T) {
	rh, _, _, cleanup := twoServers(t)
	defer cleanup()

	// "2019_3_2_14_41_PRACTICE.json" lives in server b's directory, the handler's env writes to a's
	if !rh.env.Results().HeldByOtherDir("2019_3_2_14_41_PRACTICE.json") {
		t.Fatal("the file of the other server was not detected")
	}

	if rh.env.Results().HeldByOtherDir("2019_2_15_21_16_RACE.json") || rh.env.Results().HeldByOtherDir("missing.json") {
		t.Fatal("unexpected detection")
	}
}
