package contenthttp

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi"

	smresults "github.com/wernersbacher/ACPitwall/internal/results"
	"github.com/wernersbacher/ACPitwall/internal/serverenv/envtest"
	"github.com/wernersbacher/ACPitwall/internal/stats"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

// recordsSetup places a race on server a and a qualifying on server b (both barbagallo).
func recordsSetup(t *testing.T) (th *TracksHandler, rs *Records, cleanup func()) {
	t.Helper()

	dir, err := ioutil.TempDir("", "records")

	if err != nil {
		t.Fatal(err)
	}

	dirA, dirB := filepath.Join(dir, "a", "results"), filepath.Join(dir, "b", "results")

	for d, name := range map[string]string{dirA: "2019_2_15_21_16_RACE.json", dirB: "2019_2_15_21_8_QUALIFY.json"} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}

		fixture, err := ioutil.ReadFile(filepath.Join("..", "..", "fixtures", "results", name))

		if err != nil {
			t.Fatal(err)
		}

		if err := ioutil.WriteFile(filepath.Join(d, name), fixture, 0644); err != nil {
			t.Fatal(err)
		}
	}

	env := envtest.New(filepath.Join(dir, "a"))
	env.ResultsDirs = func() []string { return []string{dirA, dirB} }

	rs = NewRecords(stats.New(env.Results(), 0), func() []ServerDir {
		return []ServerDir{
			{ServerRef: smweb.ServerRef{ID: "a", Name: "Server A"}, ResultsDir: dirA},
			{ServerRef: smweb.ServerRef{ID: "b", Name: "Server B"}, ResultsDir: dirB},
		}
	})

	return &TracksHandler{env: env, records: rs}, rs, func() { _ = os.RemoveAll(dir) }
}

func getRecords(t *testing.T, th *TracksHandler, target string) (*httptest.ResponseRecorder, *APITrackRecords) {
	t.Helper()

	r := chi.NewRouter()
	r.Get("/api/track-records", th.RecordsAPI)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))

	var out APITrackRecords

	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
	}

	return rec, &out
}

func TestTrackRecordsAPI(t *testing.T) {
	th, _, cleanup := recordsSetup(t)
	defer cleanup()

	rec, all := getRecords(t, th, "/api/track-records?track=barbagallo")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	if len(all.Records) == 0 || all.Overall == nil {
		t.Fatalf("expected records and an overall record, got %+v", all)
	}

	if all.Overall.LapTime != all.Records[0].LapTime || all.Overall.LapTime <= 0 {
		t.Fatalf("overall record is not the fastest: %+v", all.Overall)
	}

	for i := 1; i < len(all.Records); i++ {
		if all.Records[i].LapTime < all.Records[i-1].LapTime {
			t.Fatal("records are not sorted fastest first")
		}
	}

	if all.Overall.Server == nil || all.Overall.CarName == "" || all.Overall.FileName == "" {
		t.Fatalf("expected server, car name and file name: %+v", all.Overall)
	}

	t.Run("filterServer", func(t *testing.T) {
		_, onA := getRecords(t, th, "/api/track-records?track=barbagallo&filterServer=a")
		_, onB := getRecords(t, th, "/api/track-records?track=barbagallo&filterServer=b")
		_, unknown := getRecords(t, th, "/api/track-records?track=barbagallo&filterServer=nope")

		for _, r := range onA.Records {
			if r.Server.ID != "a" {
				t.Fatalf("record of server %s in the list of a", r.Server.ID)
			}
		}

		for _, r := range onB.Records {
			if r.Server.ID != "b" {
				t.Fatalf("record of server %s in the list of b", r.Server.ID)
			}
		}

		if len(onA.Records) == 0 || len(onB.Records) == 0 {
			t.Fatal("expected records on both servers")
		}

		if len(unknown.Records) != 0 || unknown.Overall != nil {
			t.Fatal("an unknown server matches nothing")
		}
	})

	t.Run("car filter", func(t *testing.T) {
		_, byCar := getRecords(t, th, "/api/track-records?track=barbagallo&car="+all.Overall.CarModel)

		if len(byCar.Records) != 1 || byCar.Overall.CarModel != all.Overall.CarModel {
			t.Fatalf("expected one record of the car, got %+v", byCar.Records)
		}

		_, none := getRecords(t, th, "/api/track-records?track=barbagallo&car=no_such_car")

		if len(none.Records) != 0 || none.Overall != nil {
			t.Fatal("expected no records for an unknown car")
		}
	})

	t.Run("other layout and track are empty", func(t *testing.T) {
		_, layout := getRecords(t, th, "/api/track-records?track=barbagallo&layout=gp")
		_, track := getRecords(t, th, "/api/track-records?track=monza")

		if len(layout.Records) != 0 || len(track.Records) != 0 {
			t.Fatal("expected no records")
		}
	})

	t.Run("invalid names", func(t *testing.T) {
		for _, target := range []string{
			"/api/track-records",
			"/api/track-records?track=..%2Fetc",
			"/api/track-records?track=barbagallo&layout=..%2F..",
			"/api/track-records?track=barbagallo&car=a%2Fb",
		} {
			if rec, _ := getRecords(t, th, target); rec.Code != http.StatusBadRequest {
				t.Errorf("%s: expected 400, got %d", target, rec.Code)
			}
		}
	})
}

func TestRecordRowsServerBadge(t *testing.T) {
	_, rs, cleanup := recordsSetup(t)
	defer cleanup()

	rows := rs.rows(rs.forTrack("barbagallo", "", stats.Filter{}))

	if len(rows) == 0 {
		t.Fatal("expected rows")
	}

	for _, row := range rows {
		if row.Server != "Server A" && row.Server != "Server B" {
			t.Fatalf("unexpected server %q", row.Server)
		}
	}

	single := NewRecords(rs.index, func() []ServerDir { return []ServerDir{{ServerRef: smweb.ServerRef{ID: "a", Name: "A"}}} })

	for _, row := range single.rows(single.forTrack("barbagallo", "", stats.Filter{})) {
		if row.Server != "" {
			t.Fatal("a single server is not attributed")
		}
	}

	var none *Records

	if len(none.rows(none.forTrack("barbagallo", "", stats.Filter{}))) != 0 || len(none.forCar("x", stats.Filter{})) != 0 {
		t.Fatal("a nil Records has no records")
	}
}

func TestHiddenResultsAreFilteredFromRecordsAndResults(t *testing.T) {
	th, rs, cleanup := recordsSetup(t)
	defer cleanup()

	rs.Hide = func(*http.Request) smresults.HideFunc {
		return func(string, string, string) bool { return true }
	}

	_, all := getRecords(t, th, "/api/track-records?track=barbagallo")

	if len(all.Records) != 0 {
		t.Fatalf("records of hidden results are exposed: %+v", all.Records)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)

	results, err := th.resultsForLayout("barbagallo", "", rs.hideFunc(req))

	if err != nil || len(results) != 0 {
		t.Fatalf("hidden results listed for the layout: %v %v", len(results), err)
	}

	rs.Hide = nil

	if results, err := th.resultsForLayout("barbagallo", "", rs.hideFunc(req)); err != nil || len(results) == 0 {
		t.Fatalf("results must be listed without a filter: %v %v", len(results), err)
	}
}
