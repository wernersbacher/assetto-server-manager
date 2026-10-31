package statshttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-chi/chi"

	"github.com/wernersbacher/ACPitwall/internal/stats"
)

func leaderboardSetup(t *testing.T, servers bool) (lh *LeaderboardHandler, track, layout string, cleanup func()) {
	t.Helper()

	dh, _, cleanup := driverSetup(t, servers)
	tracks := dh.index.Tracks()

	if len(tracks) == 0 {
		cleanup()
		t.Fatal("no tracks in the fixtures")
	}

	lh = &LeaderboardHandler{
		index:       dh.index,
		servers:     dh.servers,
		installPath: func() string { return "." },
		now:         time.Now,
	}

	return lh, tracks[0].Track, tracks[0].Layout, cleanup
}

func getLeaderboard(t *testing.T, lh *LeaderboardHandler, query url.Values) (*httptest.ResponseRecorder, *APILeaderboard) {
	t.Helper()

	r := chi.NewRouter()
	r.Get("/api/leaderboard", lh.ViewAPI)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/leaderboard?"+query.Encode(), nil))

	var out APILeaderboard

	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
	}

	return rec, &out
}

func TestLeaderboardAPI(t *testing.T) {
	lh, track, layout, cleanup := leaderboardSetup(t, true)
	defer cleanup()

	rec, lb := getLeaderboard(t, lh, url.Values{"track": {track}, "layout": {layout}})

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}

	if len(lb.Entries) == 0 {
		t.Fatal("no entries")
	}

	seen := make(map[string]bool)

	for i, e := range lb.Entries {
		if e.Position != i+1 {
			t.Errorf("entry %d has position %d", i, e.Position)
		}

		if e.Gap != e.LapTime-lb.Entries[0].LapTime {
			t.Errorf("entry %d: gap %d, lap %d, leader %d", i, e.Gap, e.LapTime, lb.Entries[0].LapTime)
		}

		if i > 0 && e.LapTime < lb.Entries[i-1].LapTime {
			t.Errorf("entry %d is faster than the one before", i)
		}

		if seen[e.DriverGUID] {
			t.Errorf("driver %s listed twice without perCar", e.DriverGUID)
		}

		seen[e.DriverGUID] = true

		if e.Server == nil {
			t.Errorf("entry %d has no server with two servers", i)
		}
	}

	if lb.Entries[0].Gap != 0 {
		t.Error("the leader has a gap")
	}

	if lb.Period != "all" || lb.Cars == nil {
		t.Errorf("unexpected request echo: %+v", lb)
	}
}

func TestLeaderboardAPIFilters(t *testing.T) {
	lh, track, layout, cleanup := leaderboardSetup(t, true)
	defer cleanup()

	base := func() url.Values { return url.Values{"track": {track}, "layout": {layout}} }
	_, all := getLeaderboard(t, lh, base())

	// per car never has fewer rows
	q := base()
	q.Set("perCar", "1")
	_, perCar := getLeaderboard(t, lh, q)

	if len(perCar.Entries) < len(all.Entries) || !perCar.PerCar {
		t.Errorf("perCar: %d rows, %d without", len(perCar.Entries), len(all.Entries))
	}

	// a car filter only returns that car
	car := all.Entries[0].CarModel
	q = base()
	q.Set("car", car)
	_, byCar := getLeaderboard(t, lh, q)

	if len(byCar.Entries) == 0 {
		t.Fatal("no entries for the car")
	}

	for _, e := range byCar.Entries {
		if e.CarModel != car {
			t.Errorf("car filter returned %s", e.CarModel)
		}
	}

	// the fixtures are from 2019
	q = base()
	q.Set("period", "7d")
	_, recent := getLeaderboard(t, lh, q)

	if len(recent.Entries) != 0 {
		t.Errorf("%d entries in the last 7 days", len(recent.Entries))
	}

	// session type: the fixtures have a race and a qualifying
	for _, sessionType := range []string{"race", "qualify"} {
		q = base()
		q.Set("sessionType", sessionType)
		_, typed := getLeaderboard(t, lh, q)

		for _, e := range typed.Entries {
			if e.SessionType == "" {
				t.Errorf("entry without session type")
			}
		}
	}

	// server filter: both servers together hold all entries, an unknown server none
	var total int

	for _, id := range []string{"a", "b"} {
		q = base()
		q.Set("filterServer", id)
		_, filtered := getLeaderboard(t, lh, q)

		for _, e := range filtered.Entries {
			if e.Server == nil || e.Server.ID != id {
				t.Errorf("server filter %s returned %+v", id, e.Server)
			}
		}

		total += len(filtered.Entries)
	}

	if total < len(all.Entries) {
		t.Errorf("servers have %d entries together, %d overall", total, len(all.Entries))
	}

	q = base()
	q.Set("filterServer", "nope")
	_, none := getLeaderboard(t, lh, q)

	if len(none.Entries) != 0 {
		t.Error("unknown server matched entries")
	}
}

func TestLeaderboardAPIInvalid(t *testing.T) {
	lh, track, layout, cleanup := leaderboardSetup(t, false)
	defer cleanup()

	for name, q := range map[string]url.Values{
		"no track":     {},
		"bad track":    {"track": {"../x"}},
		"bad layout":   {"track": {track}, "layout": {"a/b"}},
		"bad car":      {"track": {track}, "car": {".."}},
		"bad period":   {"track": {track}, "period": {"year"}},
		"bad type":     {"track": {track}, "sessionType": {"drift"}},
		"bad perCar":   {"track": {track}, "perCar": {"maybe"}},
		"bad page car": {"track": {track}, "layout": {layout}, "car": {"a/b"}},
	} {
		if rec, _ := getLeaderboard(t, lh, q); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, rec.Code)
		}
	}

	// single server: no server attribution
	_, lb := getLeaderboard(t, lh, url.Values{"track": {track}, "layout": {layout}})

	for _, e := range lb.Entries {
		if e.Server != nil {
			t.Error("server set with a single server")
		}
	}

	// unknown track: empty list
	rec, lb := getLeaderboard(t, lh, url.Values{"track": {"no_such_track"}})

	if rec.Code != http.StatusOK || len(lb.Entries) != 0 {
		t.Errorf("unknown track: %d, %d entries", rec.Code, len(lb.Entries))
	}
}

func TestFormatGap(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0:                                   "",
		1234 * time.Millisecond:             "+1.234",
		62*time.Second + 5*time.Millisecond: "+1:02.005",
	} {
		if got := formatGap(d); got != want {
			t.Errorf("formatGap(%s) = %q, want %q", d, got, want)
		}
	}
}

func TestLeaderboardRowsBestSectors(t *testing.T) {
	lh := &LeaderboardHandler{}
	ms := time.Millisecond
	rows := lh.rows([]stats.Record{
		{LapTime: 60000 * ms, Sectors: []time.Duration{20000 * ms, 21000 * ms, 19000 * ms}},
		{LapTime: 60500 * ms, Sectors: []time.Duration{20500 * ms, 20000 * ms, 20000 * ms}},
	})

	if len(rows) != 2 || rows[0].Position != 1 || rows[1].Gap != 500*ms || rows[1].GapText != "+0.500" {
		t.Fatalf("unexpected rows %+v", rows)
	}

	want := [][]bool{{true, false, true}, {false, true, false}}

	for i := range rows {
		for j := range want[i] {
			if rows[i].BestSectors[j] != want[i][j] {
				t.Errorf("row %d sector %d: best %v, want %v", i, j, rows[i].BestSectors[j], want[i][j])
			}
		}
	}
}
