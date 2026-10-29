package servermanager

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"
)

type dropTestDriver struct {
	guid, team string
}

var (
	dropDriverA = dropTestDriver{"A", "X"}
	dropDriverB = dropTestDriver{"B", "X"}
	dropDriverC = dropTestDriver{"C", "Y"}
	dropDriverD = dropTestDriver{"D", "Z"}
)

func dropTestResults(class *ChampionshipClass, finishOrder ...dropTestDriver) *smresults.SessionResults {
	results := &smresults.SessionResults{}

	for i, d := range finishOrder {
		results.Result = append(results.Result, &smresults.SessionResult{
			CarID:      i,
			DriverGUID: d.guid,
			DriverName: d.guid,
			TotalTime:  1000 + i,
			BestLap:    100 + i,
			ClassID:    class.ID,
		})

		results.Cars = append(results.Cars, &smresults.SessionCar{
			CarID:  i,
			Driver: smresults.SessionDriver{GUID: d.guid, Name: d.guid, Team: d.team, ClassID: class.ID},
		})

		results.Laps = append(results.Laps, &smresults.SessionLap{
			CarID:      i,
			DriverGUID: d.guid,
			ClassID:    class.ID,
			LapTime:    100 + i,
			Timestamp:  100 + i,
		})
	}

	return results
}

func dropTestEvent(class *ChampionshipClass, finishOrder ...dropTestDriver) *ChampionshipEvent {
	event := NewChampionshipEvent()
	event.StartedTime = time.Now().Add(-time.Hour)
	event.CompletedTime = time.Now()
	event.Sessions[serverconfig.SessionTypeRace] = &ChampionshipSession{
		StartedTime:   event.StartedTime,
		CompletedTime: event.CompletedTime,
		Results:       dropTestResults(class, finishOrder...),
	}

	return event
}

// dropTestChampionship has four completed events. Points are 10/6/3 for 1st/2nd/3rd.
//
// Event scores: E1 E2 E3 E4 total
// A:            10  6 10  6  32
// B:             6 10  3 10  29
// C:             3  0  6  3  12  (C missed E2)
// Team X (A+B): 16 16 13 16  61
// Team Y (C):    3  0  6  3  12
func dropTestChampionship(drop int) (*Championship, *ChampionshipClass) {
	championship := NewChampionship("Drop Results")
	class := NewChampionshipClass("Class")
	class.Points.Places = []int{10, 6, 3}
	championship.AddClass(class)

	championship.Events = []*ChampionshipEvent{
		dropTestEvent(class, dropDriverA, dropDriverB, dropDriverC),
		dropTestEvent(class, dropDriverB, dropDriverA),
		dropTestEvent(class, dropDriverA, dropDriverC, dropDriverB),
		dropTestEvent(class, dropDriverB, dropDriverA, dropDriverC),
	}
	championship.DropWorstResults = drop

	return championship, class
}

func standingFor(t *testing.T, standings []*ChampionshipStanding, guid string) *ChampionshipStanding {
	t.Helper()

	for _, s := range standings {
		if s.Car.Driver.GUID == guid {
			return s
		}
	}

	t.Fatalf("no standing for driver %s", guid)

	return nil
}

func teamStandingFor(t *testing.T, standings []*TeamStanding, team string) *TeamStanding {
	t.Helper()

	for _, s := range standings {
		if s.Team == team {
			return s
		}
	}

	t.Fatalf("no standing for team %s", team)

	return nil
}

func TestChampionshipClass_Standings_DropWorstResults(t *testing.T) {
	tests := []struct {
		drop           int
		a, b, c        float64
		numDroppedA    int
		numDroppedC    int
		teamX, teamY   float64
		droppedPointsC float64
	}{
		{drop: 0, a: 32, b: 29, c: 12, numDroppedA: 0, numDroppedC: 0, teamX: 61, teamY: 12},
		// N=1: A drops a 6, B drops its 3, C drops the missed event (0).
		{drop: 1, a: 26, b: 26, c: 12, numDroppedA: 1, numDroppedC: 1, teamX: 48, teamY: 12},
		{drop: 2, a: 20, b: 20, c: 9, numDroppedA: 2, numDroppedC: 2, teamX: 32, teamY: 9, droppedPointsC: 3},
		// N >= number of completed events: the best result always counts.
		{drop: 4, a: 10, b: 10, c: 6, numDroppedA: 3, numDroppedC: 3, teamX: 16, teamY: 6, droppedPointsC: 6},
		{drop: 100, a: 10, b: 10, c: 6, numDroppedA: 3, numDroppedC: 3, teamX: 16, teamY: 6, droppedPointsC: 6},
	}

	for _, tc := range tests {
		championship, class := dropTestChampionship(tc.drop)

		standings := class.Standings(championship, championship.Events)

		a, b, c := standingFor(t, standings, "A"), standingFor(t, standings, "B"), standingFor(t, standings, "C")

		if a.Points != tc.a || b.Points != tc.b || c.Points != tc.c {
			t.Errorf("drop %d: expected points A=%v B=%v C=%v, got A=%v B=%v C=%v", tc.drop, tc.a, tc.b, tc.c, a.Points, b.Points, c.Points)
		}

		if len(a.DroppedEvents) != tc.numDroppedA || len(c.DroppedEvents) != tc.numDroppedC {
			t.Errorf("drop %d: expected %d/%d dropped events for A/C, got %d/%d", tc.drop, tc.numDroppedA, tc.numDroppedC, len(a.DroppedEvents), len(c.DroppedEvents))
		}

		if c.DroppedPoints != tc.droppedPointsC {
			t.Errorf("drop %d: expected C to drop %v points, got %v", tc.drop, tc.droppedPointsC, c.DroppedPoints)
		}

		teams := class.TeamStandings(championship, championship.Events)

		if x, y := teamStandingFor(t, teams, "X"), teamStandingFor(t, teams, "Y"); x.Points != tc.teamX || y.Points != tc.teamY {
			t.Errorf("drop %d: expected team points X=%v Y=%v, got X=%v Y=%v", tc.drop, tc.teamX, tc.teamY, x.Points, y.Points)
		}
	}
}

func TestChampionshipClass_Standings_DropWorstResultsMissedEventDroppedFirst(t *testing.T) {
	championship, class := dropTestChampionship(1)
	standings := class.Standings(championship, championship.Events)

	c := standingFor(t, standings, "C")

	if !c.IsDropped(championship.Events[1].ID) || len(c.DroppedEvents) != 1 {
		t.Errorf("expected only the missed event (E2) to be dropped for C, got %v", c.DroppedEvents)
	}

	// A's lowest results are 6 (E2 and E4); exactly one of them is dropped.
	a := standingFor(t, standings, "A")

	if len(a.DroppedEvents) != 1 || a.DroppedPoints != 6 {
		t.Errorf("expected one dropped 6-point result for A, got %v (%v points)", a.DroppedEvents, a.DroppedPoints)
	}
}

func TestChampionshipClass_Standings_DropWorstResultsIgnoresIncompleteEvents(t *testing.T) {
	championship, class := dropTestChampionship(1)

	// an upcoming event and an event that is still running must not count as droppable.
	championship.Events = append(championship.Events, NewChampionshipEvent())

	inProgress := dropTestEvent(class, dropDriverC, dropDriverA, dropDriverB)
	inProgress.CompletedTime = time.Time{}
	inProgress.Sessions[serverconfig.SessionTypeRace].CompletedTime = time.Time{}
	championship.Events = append(championship.Events, inProgress)

	standings := class.Standings(championship, championship.Events)

	if a := standingFor(t, standings, "A"); a.Points != 26 {
		t.Errorf("expected A to have 26 points, got %v", a.Points)
	}

	// a single completed event never drops anything
	championship.Events = championship.Events[:1]

	if a := standingFor(t, class.Standings(championship, championship.Events), "A"); a.Points != 10 || len(a.DroppedEvents) != 0 {
		t.Errorf("expected nothing dropped with one event, got %v points, %v", a.Points, a.DroppedEvents)
	}
}

func TestChampionshipClass_Standings_DropWorstResultsPenaltiesNotDropped(t *testing.T) {
	championship, class := dropTestChampionship(1)

	class.DriverPenalties = map[string]int{"A": 5}
	class.TeamPenalties = map[string]int{"X": 4}

	a := standingFor(t, class.Standings(championship, championship.Events), "A")

	if a.Points != 21 {
		t.Errorf("expected A to have 26-5=21 points, got %v", a.Points)
	}

	if x := teamStandingFor(t, class.TeamStandings(championship, championship.Events), "X"); x.Points != 44 {
		t.Errorf("expected team X to have 48-4=44 points, got %v", x.Points)
	}

	// the event standings (used for the per-event columns) never include drops or penalties
	if p := standingFor(t, class.StandingsForEvent(championship, championship.Events[0]), "A"); p.Points != 10 {
		t.Errorf("expected A to have 10 points in E1, got %v", p.Points)
	}
}

func TestChampionshipClass_Standings_DropWorstResultsRaceWeekend(t *testing.T) {
	championship := NewChampionship("Drop Results Weekend")
	class := NewChampionshipClass("Class")
	class.Points.Places = []int{10, 6, 3}
	championship.AddClass(class)

	e1 := dropTestEvent(class, dropDriverB, dropDriverA) // A 6, B 10

	rwEvent := NewChampionshipEvent()
	rwEvent.RaceWeekendID = uuid.New()
	rwEvent.CompletedTime = time.Now()
	rwEvent.RaceWeekend = &RaceWeekend{ID: rwEvent.RaceWeekendID}

	// two sessions that both award points: A wins both (20), B is second in both (12)
	for i := 0; i < 2; i++ {
		rwEvent.RaceWeekend.Sessions = append(rwEvent.RaceWeekend.Sessions, &RaceWeekendSession{
			ID:            uuid.New(),
			StartedTime:   time.Now().Add(-time.Hour),
			CompletedTime: time.Now(),
			RaceConfig:    serverconfig.CurrentRaceConfig{Sessions: serverconfig.Sessions{serverconfig.SessionTypeRace: &serverconfig.SessionConfig{Laps: 5}}},
			Results:       dropTestResults(class, dropDriverA, dropDriverB),
			Points:        map[uuid.UUID]*ChampionshipPoints{class.ID: {Places: []int{10, 6}}},
		})
	}

	championship.Events = []*ChampionshipEvent{e1, rwEvent}
	championship.DropWorstResults = 1

	standings := class.Standings(championship, championship.Events)
	a, b := standingFor(t, standings, "A"), standingFor(t, standings, "B")

	// the race weekend is one event (both sessions together), not two.
	if a.Points != 20 || !a.IsDropped(e1.ID) || len(a.DroppedEvents) != 1 {
		t.Errorf("unexpected standing for A: %v points, dropped %v", a.Points, a.DroppedEvents)
	}

	// B: E1 10, race weekend 12 -> drops E1
	if b.Points != 12 || !b.IsDropped(e1.ID) || b.IsDropped(rwEvent.ID) || len(b.DroppedEvents) != 1 {
		t.Errorf("unexpected standing for B: %v points, dropped %v", b.Points, b.DroppedEvents)
	}

	// team X: E1 16, race weekend 32 -> drops E1
	teams := class.TeamStandings(championship, championship.Events)

	if x := teamStandingFor(t, teams, "X"); x.Points != 32 || len(x.DroppedEvents) != 1 || !x.IsDropped(e1.ID) {
		t.Errorf("unexpected team standing for X: %v points, dropped %v", x.Points, x.DroppedEvents)
	}
}

func TestChampionshipClass_Standings_DropWorstResultsPerClass(t *testing.T) {
	championship := NewChampionship("Drop Results Classes")
	class1 := NewChampionshipClass("One")
	class1.Points.Places = []int{10, 6, 3}
	class2 := NewChampionshipClass("Two")
	class2.Points.Places = []int{10, 6, 3}
	championship.AddClass(class1)
	championship.AddClass(class2)

	// E1: A (class 1) wins, D (class 2) wins. E2: B wins, A second, D misses the event.
	for _, order := range [][]dropTestDriver{{dropDriverA, dropDriverD}, {dropDriverB, dropDriverA}} {
		event := NewChampionshipEvent()
		event.StartedTime = time.Now().Add(-time.Hour)
		event.CompletedTime = time.Now()

		results := &smresults.SessionResults{}

		for _, d := range order {
			class := class1

			if d == dropDriverD {
				class = class2
			}

			r := dropTestResults(class, d)
			results.Result = append(results.Result, r.Result...)
			results.Cars = append(results.Cars, r.Cars...)
			results.Laps = append(results.Laps, r.Laps...)
		}

		event.Sessions[serverconfig.SessionTypeRace] = &ChampionshipSession{StartedTime: event.StartedTime, CompletedTime: event.CompletedTime, Results: results}
		championship.Events = append(championship.Events, event)
	}

	championship.DropWorstResults = 1

	// class 1: A has 10 + 6 -> drops 6 = 10
	if a := standingFor(t, class1.Standings(championship, championship.Events), "A"); a.Points != 10 {
		t.Errorf("expected A to have 10 points, got %v", a.Points)
	}

	// class 2: D has 10 + missed event -> drops the missed event = 10
	d := standingFor(t, class2.Standings(championship, championship.Events), "D")

	if d.Points != 10 || !d.IsDropped(championship.Events[1].ID) {
		t.Errorf("expected D to have 10 points with E2 dropped, got %v, %v", d.Points, d.DroppedEvents)
	}
}

func TestParseDropWorstResults(t *testing.T) {
	for value, expected := range map[string]int{"": 0, "0": 0, " 3 ": 3} {
		if n, err := parseDropWorstResults(value); err != nil || n != expected {
			t.Errorf("parseDropWorstResults(%q) = %d, %v; expected %d", value, n, err, expected)
		}
	}

	for _, value := range []string{"-1", "abc", "1.5"} {
		if _, err := parseDropWorstResults(value); err == nil {
			t.Errorf("parseDropWorstResults(%q) should fail", value)
		}
	}
}

func TestChampionshipManager_HandleCreateChampionship_DropWorstResults(t *testing.T) {
	championshipManager := newTestChampionshipManager()

	newRequest := func(value string) *http.Request {
		form := url.Values{}
		form.Set("ChampionshipName", "Drop Form Test")
		form.Set("ChampionshipDropWorstResults", value)

		r, err := http.NewRequest(http.MethodPost, "/championship/new", strings.NewReader(form.Encode()))

		if err != nil {
			t.Fatal(err)
		}

		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		return r
	}

	championship, _, err := championshipManager.HandleCreateChampionship(newRequest("2"))

	if err != nil {
		t.Fatal(err)
	}

	if championship.DropWorstResults != 2 {
		t.Errorf("expected DropWorstResults 2, got %d", championship.DropWorstResults)
	}

	if championship, _, err = championshipManager.HandleCreateChampionship(newRequest("")); err != nil || championship.DropWorstResults != 0 {
		t.Errorf("expected empty value to mean 0, got %v, %v", championship, err)
	}

	if _, _, err = championshipManager.HandleCreateChampionship(newRequest("-1")); err == nil {
		t.Error("expected a negative value to be rejected")
	}
}
