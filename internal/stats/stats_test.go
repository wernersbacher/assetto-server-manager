package stats

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wernersbacher/assetto-server-manager/internal/results"
)

func tempDirs(t *testing.T, n int) ([]string, func()) {
	t.Helper()

	root, err := ioutil.TempDir("", "asm-test-stats")

	if err != nil {
		t.Fatal(err)
	}

	var dirs []string

	for i := 0; i < n; i++ {
		dir := filepath.Join(root, fmt.Sprintf("d%d", i))

		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}

		dirs = append(dirs, dir)
	}

	return dirs, func() { _ = os.RemoveAll(root) }
}

func newIndex(dirs []string) *Index {
	return New(&results.Loader{Dir: dirs[0], ExtraDirs: dirs[1:]}, 0)
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()

	if err := ioutil.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

type lapSpec struct {
	guid, name, car string
	time, cuts      int
}

// session builds a result JSON; drivers finish in the order of the results (names "!" prefix: disqualified).
func session(typ, track, layout string, laps []lapSpec, finish ...string) string {
	var cars, lapJSON, res []string
	carIDs := make(map[string]int)

	for _, l := range laps {
		key := l.guid + l.car

		if _, ok := carIDs[key]; !ok {
			id := len(carIDs)
			carIDs[key] = id
			cars = append(cars, fmt.Sprintf(`{"CarId":%d,"Model":%q,"Driver":{"Guid":%q,"GuidsList":[%q],"Name":%q,"Team":"T","Nation":"GER"}}`,
				id, l.car, l.guid, l.guid, l.name))
		}

		lapJSON = append(lapJSON, fmt.Sprintf(`{"CarId":%d,"CarModel":%q,"Cuts":%d,"DriverGuid":%q,"DriverName":%q,"LapTime":%d,"Sectors":[1000,2000,3000],"Timestamp":5,"Tyre":"SM"}`,
			carIDs[key], l.car, l.cuts, l.guid, l.name, l.time))
	}

	for _, f := range finish {
		dq := strings.HasPrefix(f, "!")
		guid := strings.TrimPrefix(f, "!")

		for _, l := range laps {
			if l.guid == guid {
				res = append(res, fmt.Sprintf(`{"CarId":%d,"CarModel":%q,"DriverGuid":%q,"DriverName":%q,"TotalTime":100,"Disqualified":%v}`,
					carIDs[l.guid+l.car], l.car, l.guid, l.name, dq))
				break
			}
		}
	}

	return fmt.Sprintf(`{"TrackName":%q,"TrackConfig":%q,"Type":%q,"Cars":[%s],"Laps":[%s],"Result":[%s],"Events":[
{"CarId":0,"Type":"COLLISION_WITH_CAR","Driver":{"Guid":%q}},
{"CarId":0,"Type":"COLLISION_WITH_ENV","Driver":{"Guid":%q}},
{"CarId":0,"Type":"COLLISION_WITH_ENV","Driver":{"Guid":%q}}]}`,
		track, layout, typ, strings.Join(cars, ","), strings.Join(lapJSON, ","), strings.Join(res, ","),
		laps[0].guid, laps[0].guid, laps[0].guid)
}

func TestRecordsExcludeCutsAndFilter(t *testing.T) {
	dirs, cleanup := tempDirs(t, 2)
	defer cleanup()

	write(t, dirs[0], "2020_1_1_10_0_PRACTICE.json", session("PRACTICE", "monza", "", []lapSpec{
		{"1", "Alice", "ferrari", 90000, 0},
		{"1", "Alice", "ferrari", 80000, 1}, // cut, ignored
		{"2", "Bob", "ferrari", 91000, 0},
		{"2", "Bob", "bmw", 95000, 0},
	}))
	write(t, dirs[1], "2020_6_1_10_0_RACE.json", session("RACE", "monza", "", []lapSpec{
		{"1", "Alice", "bmw", 94000, 0},
		{"2", "Bob", "ferrari", 89000, 0},
		{"3", "Carl", "ferrari", 0, 0}, // no time
	}, "2", "1", "3"))
	write(t, dirs[0], "2020_6_2_10_0_RACE.json", session("RACE", "monza", "gp", []lapSpec{
		{"1", "Alice", "ferrari", 50000, 0},
	}, "1"))

	idx := newIndex(dirs)

	rec := idx.TrackRecords("Monza", "", Filter{})

	if len(rec) != 2 || rec[0].Car != "ferrari" || rec[0].LapTime != 89*time.Second || rec[0].DriverGUID != "2" ||
		rec[0].Dir != dirs[1] || rec[0].SessionType != "RACE" || rec[0].ResultFile != "2020_6_1_10_0_RACE.json" ||
		len(rec[0].Sectors) != 3 || rec[0].Sectors[1] != 2*time.Second || rec[0].Tyre != "SM" {
		t.Fatalf("unexpected track records: %+v", rec)
	}

	if rec[1].Car != "bmw" || rec[1].LapTime != 94*time.Second || rec[1].DriverGUID != "1" {
		t.Fatalf("unexpected second record: %+v", rec[1])
	}

	if lb := idx.Leaderboard("monza", "", Filter{}); len(lb) != 2 || lb[0].DriverGUID != "2" || lb[1].DriverGUID != "1" ||
		lb[1].LapTime != 90*time.Second {
		t.Fatalf("unexpected leaderboard: %+v", lb)
	}

	if lb := idx.Leaderboard("monza", "", Filter{PerCar: true}); len(lb) != 4 {
		t.Fatalf("expected driver+car entries (1/ferrari, 1/bmw, 2/ferrari, 2/bmw), got %+v", lb)
	}

	if lb := idx.Leaderboard("monza", "gp", Filter{}); len(lb) != 1 || lb[0].LapTime != 50*time.Second {
		t.Fatalf("layout not separated: %+v", lb)
	}

	if lb := idx.Leaderboard("monza", "", Filter{Cars: []string{"bmw"}}); len(lb) != 2 || lb[0].DriverGUID != "1" {
		t.Fatalf("car filter: %+v", lb)
	}

	if lb := idx.Leaderboard("monza", "", Filter{Since: time.Date(2020, 3, 1, 0, 0, 0, 0, time.Local)}); len(lb) != 2 || lb[1].LapTime != 94*time.Second {
		t.Fatalf("since filter: %+v", lb)
	}

	if lb := idx.Leaderboard("monza", "", Filter{SessionTypes: []string{"practice"}}); len(lb) != 2 || lb[0].LapTime != 90*time.Second {
		t.Fatalf("session type filter: %+v", lb)
	}

	if lb := idx.Leaderboard("monza", "", Filter{Dir: dirs[0]}); len(lb) != 2 || lb[0].LapTime != 90*time.Second {
		t.Fatalf("dir filter: %+v", lb)
	}

	if lb := idx.Leaderboard("monza", "", Filter{Dir: dirs[1], SessionTypes: []string{"practice"}}); len(lb) != 0 {
		t.Fatalf("expected no entries: %+v", lb)
	}

	if cr := idx.CarRecords("ferrari", Filter{}); len(cr) != 2 || cr[0].Layout != "" || cr[1].Layout != "gp" {
		t.Fatalf("car records: %+v", cr)
	}

	if tr := idx.Tracks(); len(tr) != 2 || tr[0] != (TrackRef{"monza", ""}) || tr[1] != (TrackRef{"monza", "gp"}) {
		t.Fatalf("tracks: %+v", tr)
	}
}

func TestDriverProfile(t *testing.T) {
	dirs, cleanup := tempDirs(t, 1)
	defer cleanup()

	write(t, dirs[0], "2020_1_1_10_0_PRACTICE.json", session("PRACTICE", "monza", "", []lapSpec{
		{"1", "Old Name", "ferrari", 90000, 0},
		{"1", "Old Name", "ferrari", 80000, 2},
	}))
	write(t, dirs[0], "2020_1_2_10_0_RACE.json", session("RACE", "monza", "", []lapSpec{
		{"1", "Alice", "ferrari", 91000, 0},
		{"2", "Bob", "ferrari", 89000, 0},
		{"3", "Carl", "ferrari", 88000, 0},
	}, "!3", "2", "1")) // Carl disqualified: Bob wins, Alice second
	write(t, dirs[0], "2020_1_3_10_0_RACE.json", session("RACE", "spa", "", []lapSpec{
		{"1", "Alice", "bmw", 130000, 0},
		{"2", "Bob", "bmw", 131000, 0},
	}, "1", "2"))
	write(t, dirs[0], "2020_1_4_10_0_RACEx2.json", session("RACEx2", "spa", "", []lapSpec{
		{"1", "Alice", "bmw", 129000, 0},
		{"2", "Bob", "bmw", 131000, 0},
	}, "2", "1"))

	idx := newIndex(dirs)

	if _, ok := idx.Driver("nobody"); ok {
		t.Fatal("unknown driver found")
	}

	if _, ok := idx.Driver(""); ok {
		t.Fatal("empty guid found")
	}

	a, ok := idx.Driver("1")

	if !ok {
		t.Fatal("driver not found")
	}

	if a.Name != "Alice" || len(a.Names) != 2 || a.Names[1] != "Old Name" || a.Nation != "GER" || a.Team != "T" {
		t.Fatalf("names: %+v", a)
	}

	if a.Sessions["RACE"] != 2 || a.Sessions["PRACTICE"] != 1 || a.Sessions["RACEx2"] != 1 {
		t.Fatalf("sessions: %v", a.Sessions)
	}

	if a.Races != 3 || a.Wins != 1 || a.Podiums != 3 {
		t.Fatalf("races/wins/podiums: %d/%d/%d", a.Races, a.Wins, a.Podiums)
	}

	if a.Laps != 5 || a.ValidLaps != 4 || a.Cuts != 2 || a.CollisionsCar != 4 || a.CollisionsEnv != 8 {
		t.Fatalf("totals: %+v", a)
	}

	if len(a.Results) != 4 || a.Results[0].ResultFile != "2020_1_4_10_0_RACEx2.json" || a.Results[0].Position != 2 ||
		a.Results[3].SessionType != "PRACTICE" || a.Results[3].BestLap != 90*time.Second || a.Results[3].Position != 0 ||
		a.Results[2].Position != 2 {
		t.Fatalf("results: %+v", a.Results)
	}

	if len(a.PersonalBests) != 2 || a.PersonalBests[0].Track != "monza" || a.PersonalBests[0].LapTime != 90*time.Second ||
		a.PersonalBests[1].Track != "spa" || a.PersonalBests[1].LapTime != 129*time.Second {
		t.Fatalf("personal bests: %+v", a.PersonalBests)
	}

	carl, _ := idx.Driver("3")

	if carl.Races != 1 || carl.Wins != 0 || carl.Podiums != 0 || !carl.Results[0].Disqualified || carl.Results[0].Position != 0 {
		t.Fatalf("disqualified driver: %+v", carl)
	}

	bob, _ := idx.Driver("2")

	if bob.Wins != 2 || bob.Podiums != 3 {
		t.Fatalf("bob: %+v", bob)
	}
}

func TestDriverSwapUsesLapGUID(t *testing.T) {
	dirs, cleanup := tempDirs(t, 1)
	defer cleanup()

	write(t, dirs[0], "2020_1_1_10_0_RACE.json", `{"TrackName":"monza","Type":"RACE",
"Cars":[{"CarId":0,"Model":"ferrari","Driver":{"Guid":"a","GuidsList":["a","b"],"Name":"A","Team":"Duo"}}],
"Laps":[{"CarId":0,"CarModel":"ferrari","DriverGuid":"a","DriverName":"A","LapTime":90000},
{"CarId":0,"CarModel":"ferrari","DriverGuid":"b","DriverName":"B","LapTime":85000}],
"Result":[{"CarId":0,"CarModel":"ferrari","DriverGuid":"a;b","DriverName":"A","TotalTime":1}]}`)

	idx := newIndex(dirs)

	lb := idx.Leaderboard("monza", "", Filter{})

	if len(lb) != 2 || lb[0].DriverGUID != "b" || lb[0].DriverName != "B" {
		t.Fatalf("swap leaderboard: %+v", lb)
	}

	b, ok := idx.Driver("b")

	if !ok || b.Wins != 1 || b.Team != "Duo" || b.Name != "B" {
		t.Fatalf("swap driver: %+v", b)
	}
}

func TestRefreshIsIncremental(t *testing.T) {
	dirs, cleanup := tempDirs(t, 2)
	defer cleanup()

	write(t, dirs[0], "2020_1_1_10_0_PRACTICE.json", session("PRACTICE", "monza", "", []lapSpec{{"1", "Alice", "ferrari", 90000, 0}}))
	write(t, dirs[1], "2020_1_2_10_0_PRACTICE.json", session("PRACTICE", "monza", "", []lapSpec{{"2", "Bob", "ferrari", 91000, 0}}))
	write(t, dirs[1], "notes.txt", "ignored")
	write(t, dirs[1], "broken.json", "{")

	idx := newIndex(dirs)

	if err := idx.Refresh(); err != nil {
		t.Fatal(err)
	}

	if idx.parses != 3 {
		t.Fatalf("expected 3 parses (2 results and a broken one), got %d", idx.parses)
	}

	if len(idx.Leaderboard("monza", "", Filter{})) != 2 {
		t.Fatal("expected two drivers")
	}

	// the broken file is retried, the good ones are not parsed again
	if idx.parses != 4 {
		t.Fatalf("expected only the broken file to be parsed again, got %d parses", idx.parses)
	}

	// add
	write(t, dirs[0], "2020_1_3_10_0_PRACTICE.json", session("PRACTICE", "monza", "", []lapSpec{{"3", "Carl", "ferrari", 80000, 0}}))

	if lb := idx.Leaderboard("monza", "", Filter{}); len(lb) != 3 || lb[0].DriverGUID != "3" {
		t.Fatalf("added file not picked up: %+v", lb)
	}

	if idx.parses != 6 {
		t.Fatalf("expected new + broken file parsed, got %d", idx.parses)
	}

	// change (same size, new mod time)
	path := filepath.Join(dirs[0], "2020_1_3_10_0_PRACTICE.json")
	later := time.Now().Add(time.Hour)

	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}

	before := idx.parses
	idx.Leaderboard("monza", "", Filter{})

	if idx.parses != before+2 {
		t.Fatalf("changed file not parsed again: %d -> %d", before, idx.parses)
	}

	// change content
	write(t, dirs[0], "2020_1_3_10_0_PRACTICE.json", session("PRACTICE", "monza", "", []lapSpec{{"3", "Carl", "ferrari", 95000, 0}}))

	if lb := idx.Leaderboard("monza", "", Filter{}); lb[len(lb)-1].DriverGUID != "3" || lb[len(lb)-1].LapTime != 95*time.Second {
		t.Fatalf("changed file not picked up: %+v", lb)
	}

	// remove
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	if _, ok := idx.Driver("3"); ok {
		t.Fatal("removed file still indexed")
	}
}

func TestMinInterval(t *testing.T) {
	dirs, cleanup := tempDirs(t, 1)
	defer cleanup()

	idx := New(&results.Loader{Dir: dirs[0]}, time.Hour)

	if len(idx.Tracks()) != 0 {
		t.Fatal("expected no tracks")
	}

	write(t, dirs[0], "2020_1_1_10_0_PRACTICE.json", session("PRACTICE", "monza", "", []lapSpec{{"1", "Alice", "ferrari", 90000, 0}}))

	if len(idx.Tracks()) != 0 {
		t.Fatal("query refreshed within the minimum interval")
	}

	if err := idx.Refresh(); err != nil {
		t.Fatal(err)
	}

	if len(idx.Tracks()) != 1 {
		t.Fatal("explicit refresh not applied")
	}
}

func TestFixtures(t *testing.T) {
	dirs, cleanup := tempDirs(t, 1)
	defer cleanup()

	fixtures, err := filepath.Glob(filepath.Join("..", "..", "fixtures", "results", "*.json"))

	if err != nil || len(fixtures) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}

	for _, f := range fixtures {
		data, err := ioutil.ReadFile(f)

		if err != nil {
			t.Fatal(err)
		}

		write(t, dirs[0], filepath.Base(f), string(data))
	}

	idx := newIndex(dirs)
	tracks := idx.Tracks()

	if len(tracks) == 0 {
		t.Fatal("no tracks in fixtures")
	}

	var drivers int

	for _, tr := range tracks {
		lb := idx.Leaderboard(tr.Track, tr.Layout, Filter{})

		for i := 1; i < len(lb); i++ {
			if lb[i].LapTime < lb[i-1].LapTime {
				t.Fatalf("leaderboard not sorted: %+v", lb)
			}
		}

		for _, r := range lb {
			if r.LapTime <= 0 || r.DriverGUID == "" {
				t.Fatalf("bad record %+v", r)
			}

			if _, ok := idx.Driver(r.DriverGUID); !ok {
				t.Fatalf("driver %s missing", r.DriverGUID)
			}

			drivers++
		}
	}

	if drivers == 0 {
		t.Fatal("no drivers in fixtures")
	}

	// the best lap of the index agrees with SessionResults.FastestLap of a file
	loader := &results.Loader{Dir: dirs[0]}
	r, err := loader.Load("2019_2_15_21_16_RACE.json", results.WithoutPluginFire)

	if err != nil {
		t.Fatal(err)
	}

	fastest := r.FastestLap()
	rec := idx.TrackRecords(r.TrackName, r.TrackConfig, Filter{Dir: dirs[0], SessionTypes: []string{"race"}})

	if fastest == nil || len(rec) == 0 {
		t.Fatalf("no fastest lap: %v %v", fastest, rec)
	}
}

func TestConcurrentQueries(t *testing.T) {
	dirs, cleanup := tempDirs(t, 1)
	defer cleanup()

	write(t, dirs[0], "2020_1_1_10_0_PRACTICE.json", session("PRACTICE", "monza", "", []lapSpec{{"1", "Alice", "ferrari", 90000, 0}}))

	idx := newIndex(dirs)

	var wg sync.WaitGroup

	for n := 0; n < 8; n++ {
		wg.Add(1)

		go func(n int) {
			defer wg.Done()

			for i := 0; i < 20; i++ {
				if n == 0 {
					write(t, dirs[0], fmt.Sprintf("2020_2_%d_10_0_PRACTICE.json", i+1),
						session("PRACTICE", "monza", "", []lapSpec{{"2", "Bob", "ferrari", 91000 + i, 0}}))
				}

				idx.Leaderboard("monza", "", Filter{})
				idx.Driver("1")
				_ = idx.Refresh()
			}
		}(n)
	}

	wg.Wait()
}
