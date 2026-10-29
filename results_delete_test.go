package servermanager

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi"
	"github.com/google/uuid"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"
)

func setupResultsDeleteTest(t *testing.T) (store Store, resultsDir string, cleanup func()) {
	t.Helper()

	dir, err := ioutil.TempDir("", "results-delete")

	if err != nil {
		t.Fatal(err)
	}

	resultsDir = filepath.Join(dir, "results")

	if err := os.MkdirAll(resultsDir, 0755); err != nil {
		t.Fatal(err)
	}

	fixture, err := ioutil.ReadFile(filepath.Join("fixtures", "results", "2019_2_15_21_16_RACE.json"))

	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"2019_2_15_21_16_RACE.json", "2019_2_15_22_16_RACE.json"} {
		if err := ioutil.WriteFile(filepath.Join(resultsDir, name), fixture, 0644); err != nil {
			t.Fatal(err)
		}
	}

	// a file outside of the results directory, which must never be deleted
	if err := ioutil.WriteFile(filepath.Join(dir, "outside.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	oldInstallPath := ServerInstallPath
	ServerInstallPath = dir

	store = NewJSONStore(filepath.Join(dir, "store"), filepath.Join(dir, "store-shared"))

	return store, resultsDir, func() {
		ServerInstallPath = oldInstallPath
		_ = os.RemoveAll(dir)
	}
}

func postResultsDelete(t *testing.T, rh *ResultsHandler, fileName string) *httptest.ResponseRecorder {
	t.Helper()

	r := chi.NewRouter()
	r.Post("/results/{fileName}/delete", rh.delete)

	req := httptest.NewRequest(http.MethodPost, "/results/"+fileName+"/delete", nil)
	req = withRequestScope(req, testRequestScope())
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	return rec
}

func fileExists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

func TestResultsHandlerDelete(t *testing.T) {
	store, resultsDir, cleanup := setupResultsDeleteTest(t)
	defer cleanup()

	rh := &ResultsHandler{store: store}

	t.Run("successful delete", func(t *testing.T) {
		rec := postResultsDelete(t, rh, "2019_2_15_22_16_RACE.json")

		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/results" {
			t.Fatalf("expected redirect to /results, got %d %q", rec.Code, rec.Header().Get("Location"))
		}

		if fileExists(filepath.Join(resultsDir, "2019_2_15_22_16_RACE.json")) {
			t.Fatal("results file was not deleted")
		}

		if !fileExists(filepath.Join(resultsDir, "2019_2_15_21_16_RACE.json")) {
			t.Fatal("unrelated results file was deleted")
		}
	})

	t.Run("non-existent file", func(t *testing.T) {
		rec := postResultsDelete(t, rh, "2019_2_15_23_16_RACE.json")

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rec.Code)
		}
	})

	t.Run("path traversal and invalid names", func(t *testing.T) {
		for _, name := range []string{"..%2Foutside.json", "..%5Coutside.json", "..", "outside", "2019_2_15_21_16_RACE.txt", ".json"} {
			rec := postResultsDelete(t, rh, name)

			if rec.Code == http.StatusFound {
				t.Errorf("%q: expected rejection, got redirect", name)
			}
		}

		if !fileExists(filepath.Join(filepath.Dir(resultsDir), "outside.json")) {
			t.Fatal("file outside of the results directory was deleted")
		}

		for _, name := range []string{"../outside.json", `..\outside.json`, "a/b.json", ""} {
			if err := deleteResultsFile(store, name); err != ErrInvalidResultsFileName {
				t.Errorf("%q: expected ErrInvalidResultsFileName, got %v", name, err)
			}
		}
	})
}

func TestDeleteResultsFileReferenced(t *testing.T) {
	store, resultsDir, cleanup := setupResultsDeleteTest(t)
	defer cleanup()

	championship := &smmodel.Championship{
		ID:   uuid.New(),
		Name: "Test Championship",
		Events: []*smmodel.ChampionshipEvent{
			{
				Sessions: map[serverconfig.SessionType]*smmodel.ChampionshipSession{
					serverconfig.SessionTypeRace: {Results: &smresults.SessionResults{SessionFile: "2019_2_15_21_16_RACE"}},
				},
			},
		},
	}

	if err := store.UpsertChampionship(championship); err != nil {
		t.Fatal(err)
	}

	raceWeekend := &smmodel.RaceWeekend{
		ID:   uuid.New(),
		Name: "Test Weekend",
		Sessions: []*smmodel.RaceWeekendSession{
			{Results: &smresults.SessionResults{SessionFile: "2019_2_15_22_16_RACE"}},
		},
	}

	if err := store.UpsertRaceWeekend(raceWeekend); err != nil {
		t.Fatal(err)
	}

	for name, wantReference := range map[string]string{
		"2019_2_15_21_16_RACE.json": "Test Championship",
		"2019_2_15_22_16_RACE.json": "Test Weekend",
	} {
		err := deleteResultsFile(store, name)

		if err == nil || !strings.Contains(err.Error(), wantReference) {
			t.Fatalf("%s: expected refusal mentioning %q, got %v", name, wantReference, err)
		}

		if !fileExists(filepath.Join(resultsDir, name)) {
			t.Fatalf("%s: referenced results file was deleted", name)
		}
	}

	rec := postResultsDelete(t, &ResultsHandler{store: store}, "2019_2_15_21_16_RACE.json")

	if rec.Code != http.StatusFound || !fileExists(filepath.Join(resultsDir, "2019_2_15_21_16_RACE.json")) {
		t.Fatal("handler should redirect and keep the referenced file")
	}
}
