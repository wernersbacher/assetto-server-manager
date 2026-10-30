package statshttp

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"
	"github.com/wernersbacher/assetto-server-manager/internal/serverenv/envtest"
	"github.com/wernersbacher/assetto-server-manager/internal/stats"
	smweb "github.com/wernersbacher/assetto-server-manager/internal/web"
)

// driverSetup places a race on server a and a qualifying on server b.
func driverSetup(t *testing.T, servers bool) (dh *DriverHandler, guid string, cleanup func()) {
	t.Helper()

	dir, err := ioutil.TempDir("", "driverstats")

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

	index := stats.New(env.Results(), 0)

	var serversFunc smweb.ServersFunc

	if servers {
		serversFunc = func() []smweb.ServerDir {
			return []smweb.ServerDir{
				{ServerRef: smweb.ServerRef{ID: "a", Name: "Server A"}, ResultsDir: dirA},
				{ServerRef: smweb.ServerRef{ID: "b", Name: "Server B"}, ResultsDir: dirB},
			}
		}
	}

	// a driver who is in both files
	results, err := env.Results().ListAll()

	if err != nil {
		t.Fatal(err)
	}

	count := make(map[string]int)

	for _, result := range results {
		for _, car := range result.Cars {
			count[car.Driver.GUID]++
		}
	}

	for g, n := range count {
		if d, ok := index.Driver(g); ok && n >= 2 && len(d.PersonalBests) > 0 && (guid == "" || g < guid) {
			guid = g
		}
	}

	if guid == "" {
		t.Fatal("no driver in both fixture files")
	}

	return &DriverHandler{index: index, servers: serversFunc}, guid, func() { _ = os.RemoveAll(dir) }
}

func getDriver(t *testing.T, dh *DriverHandler, target string) (*httptest.ResponseRecorder, *APIDriver) {
	t.Helper()

	r := chi.NewRouter()
	r.Get("/api/driver/{guid}", dh.ViewAPI)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))

	var out APIDriver

	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
	}

	return rec, &out
}

func TestDriverAPI(t *testing.T) {
	dh, guid, cleanup := driverSetup(t, true)
	defer cleanup()

	rec, driver := getDriver(t, dh, "/api/driver/"+guid)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	if driver.GUID != guid || driver.Name == "" || len(driver.Names) == 0 {
		t.Fatalf("unexpected driver %+v", driver)
	}

	if len(driver.Results) < 2 || len(driver.PersonalBests) == 0 || driver.Laps == 0 {
		t.Fatalf("expected results, personal bests and laps: %+v", driver)
	}

	if driver.Championships == nil {
		t.Fatal("championships must be an empty list, not null")
	}

	for _, r := range driver.Results {
		if r.Server == nil || r.FileName == "" {
			t.Fatalf("expected a server and file name: %+v", r)
		}
	}

	t.Run("filterServer", func(t *testing.T) {
		_, onA := getDriver(t, dh, "/api/driver/"+guid+"?filterServer=a")
		_, unknown := getDriver(t, dh, "/api/driver/"+guid+"?filterServer=nope")

		if len(onA.Results) == 0 || len(onA.Results) >= len(driver.Results) {
			t.Fatalf("expected the results of server a only, got %d of %d", len(onA.Results), len(driver.Results))
		}

		for _, r := range onA.Results {
			if r.Server.ID != "a" {
				t.Fatalf("result of server %s in the list of a", r.Server.ID)
			}
		}

		for _, pb := range onA.PersonalBests {
			if pb.Server.ID != "a" {
				t.Fatalf("personal best of server %s in the list of a", pb.Server.ID)
			}
		}

		if len(unknown.Results) != 0 || len(unknown.PersonalBests) != 0 || unknown.Laps != driver.Laps {
			t.Fatal("an unknown server matches no results, the totals stay")
		}
	})

	t.Run("unknown and invalid", func(t *testing.T) {
		for target, status := range map[string]int{
			"/api/driver/1234":           http.StatusNotFound,
			"/api/driver/..%2Fetc":       http.StatusNotFound,
			"/api/driver/" + guid + ";x": http.StatusNotFound,
		} {
			if rec, _ := getDriver(t, dh, target); rec.Code != status {
				t.Errorf("%s: expected %d, got %d", target, status, rec.Code)
			}
		}
	})
}

func TestDriverAPISingleServerHasNoServer(t *testing.T) {
	dh, guid, cleanup := driverSetup(t, false)
	defer cleanup()

	_, driver := getDriver(t, dh, "/api/driver/"+guid)

	for _, r := range driver.Results {
		if r.Server != nil {
			t.Fatal("a single server is not attributed")
		}
	}
}

func TestSessionCounts(t *testing.T) {
	counts := sessionCounts(map[string]int{"RACE": 2, "QUALIFY": 5, "PRACTICE": 2})

	if len(counts) != 3 || counts[0].Type != "QUALIFY" || counts[1].Type != "PRACTICE" || counts[2].Type != "RACE" {
		t.Fatalf("unexpected order %+v", counts)
	}
}

func raceResults(class *smmodel.ChampionshipClass, guids ...string) *smresults.SessionResults {
	results := &smresults.SessionResults{Type: serverconfig.SessionTypeRace}

	for i, guid := range guids {
		results.Result = append(results.Result, &smresults.SessionResult{
			CarID: i, DriverGUID: guid, DriverName: guid, TotalTime: 1000 + i, BestLap: 100 + i, ClassID: class.ID,
		})
		results.Cars = append(results.Cars, &smresults.SessionCar{
			CarID: i, Driver: smresults.SessionDriver{GUID: guid, Name: guid, ClassID: class.ID},
		})
		results.Laps = append(results.Laps, &smresults.SessionLap{
			CarID: i, DriverGUID: guid, ClassID: class.ID, LapTime: 100 + i, Timestamp: 100 + i,
		})
	}

	return results
}

func TestChampionshipEntries(t *testing.T) {
	newChampionship := func(name string, entrants ...string) (*smmodel.Championship, *smmodel.ChampionshipClass) {
		championship := smmodel.NewChampionship(name)
		class := smmodel.NewChampionshipClass("Class")
		class.Points.Places = []int{10, 6, 3}

		for _, guid := range entrants {
			class.Entrants[guid] = &serverconfig.Entrant{GUID: guid, Name: guid}
		}

		championship.AddClass(class)

		return championship, class
	}

	addEvent := func(championship *smmodel.Championship, class *smmodel.ChampionshipClass, finish ...string) {
		event := smmodel.NewChampionshipEvent()
		event.StartedTime = time.Now().Add(-time.Hour)
		event.CompletedTime = time.Now()
		event.Sessions[serverconfig.SessionTypeRace] = &smmodel.ChampionshipSession{
			StartedTime:   event.StartedTime,
			CompletedTime: event.CompletedTime,
			Results:       raceResults(class, finish...),
		}

		championship.Events = append(championship.Events, event)
	}

	first, firstClass := newChampionship("First", "A", "B")
	addEvent(first, firstClass, "A", "B")
	addEvent(first, firstClass, "B", "A")
	addEvent(first, firstClass, "B", "A")

	second, secondClass := newChampionship("Second", "C", "B")
	addEvent(second, secondClass, "C", "B")

	deleted, deletedClass := newChampionship("Deleted", "B")
	addEvent(deleted, deletedClass, "B")
	deleted.Deleted = time.Now()

	other, otherClass := newChampionship("Other", "C", "D")
	addEvent(other, otherClass, "C", "D")

	all := []*smmodel.Championship{first, second, deleted, other, nil}

	entries := ChampionshipEntries(all, "B")

	if len(entries) != 2 {
		t.Fatalf("expected B in two championships, got %+v", entries)
	}

	if e := entries[0]; e.Name != "First" || e.Position != 1 || e.Points != 26 || e.Entrants != 2 || e.ID != first.ID.String() || e.Class != "" {
		t.Errorf("unexpected first entry %+v", e)
	}

	if e := entries[1]; e.Name != "Second" || e.Position != 2 || e.Points != 6 {
		t.Errorf("unexpected second entry %+v", e)
	}

	if entries := ChampionshipEntries(all, "A"); len(entries) != 1 || entries[0].Position != 2 || entries[0].Points != 22 {
		t.Errorf("unexpected entries of A: %+v", entries)
	}

	if entries := ChampionshipEntries(all, "Z"); len(entries) != 0 {
		t.Errorf("expected no entries for an unknown driver, got %+v", entries)
	}

	// a driver swap entrant is found by any of its GUIDs
	if !guidMatches("A;B", "B") || guidMatches("A;B", "C") || !guidMatches("A", "A") {
		t.Error("unexpected guidMatches")
	}
}

func TestValidDriverGUID(t *testing.T) {
	for guid, valid := range map[string]bool{
		"76561198020046073":  true,
		"S76561198020046073": true,
		"":                   false,
		"a;b":                false,
		"../x":               false,
		"a b":                false,
	} {
		if smweb.ValidDriverGUID(guid) != valid {
			t.Errorf("%q: expected valid=%v", guid, valid)
		}
	}

	if smweb.DriverURL("1") != "/driver/1" || smweb.DriverURL("a;b") != "" {
		t.Error("unexpected DriverURL")
	}
}
