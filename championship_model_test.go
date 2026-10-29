package servermanager

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"
)

func modelTestSlot(model, name, guid, team string) *serverconfig.Entrant {
	e := serverconfig.NewEntrant()
	e.Model = model
	e.Name = name
	e.GUID = guid
	e.Team = team

	return e
}

func modelTestClass(name string, slots ...*serverconfig.Entrant) *ChampionshipClass {
	class := NewChampionshipClass(name)

	for _, slot := range slots {
		class.Entrants.AddToBackOfGrid(slot)
	}

	return class
}

func TestChampionship_AddEntrantInFirstFreeSlot(t *testing.T) {
	t.Run("free slot is taken", func(t *testing.T) {
		championship := NewChampionship("Test")
		class := modelTestClass("A", modelTestSlot("car_a", "", "", ""))
		championship.AddClass(class)

		found, entrant, entrantClass, err := championship.AddEntrantInFirstFreeSlot(ChampionshipSignUpResponse{Name: "Driver", GUID: "1", Team: "Team"})

		if err != nil || !found {
			t.Fatalf("expected free slot, got found=%v err=%v", found, err)
		}

		if entrantClass != class {
			t.Errorf("expected class A")
		}

		if entrant.Name != "Driver" || entrant.GUID != "1" || entrant.Team != "Team" || entrant.Model != "car_a" {
			t.Errorf("unexpected entrant: %+v", entrant)
		}
	})

	t.Run("slots with a name but no guid are not free", func(t *testing.T) {
		championship := NewChampionship("Test")
		named := modelTestSlot("car_a", "Reserved", "", "")
		free := modelTestSlot("car_a", "", "", "")
		championship.AddClass(modelTestClass("A", named, free))

		found, entrant, _, err := championship.AddEntrantInFirstFreeSlot(ChampionshipSignUpResponse{Name: "Driver", GUID: "1"})

		if err != nil || !found {
			t.Fatalf("expected free slot, got found=%v err=%v", found, err)
		}

		if entrant != free {
			t.Errorf("expected the unnamed slot to be taken")
		}

		if named.Name != "Reserved" || named.GUID != "" {
			t.Errorf("reserved slot was modified: %+v", named)
		}
	})

	t.Run("multi class fills first class with a free slot", func(t *testing.T) {
		championship := NewChampionship("Test")
		classA := modelTestClass("A", modelTestSlot("car_a", "Full", "10", ""))
		classB := modelTestClass("B", modelTestSlot("car_b", "", "", ""))
		classC := modelTestClass("C", modelTestSlot("car_c", "", "", ""))
		championship.AddClass(classA)
		championship.AddClass(classB)
		championship.AddClass(classC)

		found, entrant, entrantClass, err := championship.AddEntrantInFirstFreeSlot(ChampionshipSignUpResponse{Name: "Driver", GUID: "1"})

		if err != nil || !found {
			t.Fatalf("expected free slot, got found=%v err=%v", found, err)
		}

		if entrantClass != classB || entrant.Model != "car_b" {
			t.Errorf("expected slot in class B, got class %s model %s", entrantClass.Name, entrant.Model)
		}

		_, _, entrantClass, err = championship.AddEntrantInFirstFreeSlot(ChampionshipSignUpResponse{Name: "Driver 2", GUID: "2"})

		if err != nil || entrantClass != classC {
			t.Errorf("expected second driver in class C, got err=%v", err)
		}
	})

	t.Run("existing guid updates the entrant instead of taking a new slot", func(t *testing.T) {
		championship := NewChampionship("Test")
		existing := modelTestSlot("car_b", "Old Name", "1", "Old Team")
		free := modelTestSlot("car_a", "", "", "")
		classA := modelTestClass("A", free)
		classB := modelTestClass("B", existing)
		championship.AddClass(classA)
		championship.AddClass(classB)

		found, entrant, entrantClass, err := championship.AddEntrantInFirstFreeSlot(ChampionshipSignUpResponse{Name: "New Name", GUID: "1", Team: "New Team"})

		if err != nil || !found {
			t.Fatalf("expected existing slot, got found=%v err=%v", found, err)
		}

		if entrant != existing || entrantClass != classB {
			t.Errorf("expected the existing entrant in class B to be returned")
		}

		if existing.Name != "New Name" || existing.Team != "New Team" || existing.Model != "car_b" {
			t.Errorf("existing entrant not updated: %+v", existing)
		}

		if free.GUID != "" || free.Name != "" {
			t.Errorf("free slot should not have been taken: %+v", free)
		}
	})

	t.Run("existing guid without a team keeps the old team", func(t *testing.T) {
		championship := NewChampionship("Test")
		existing := modelTestSlot("car_a", "Old Name", "1", "Old Team")
		championship.AddClass(modelTestClass("A", existing))

		if _, _, _, err := championship.AddEntrantInFirstFreeSlot(ChampionshipSignUpResponse{Name: "New Name", GUID: "1"}); err != nil {
			t.Fatal(err)
		}

		if existing.Name != "New Name" || existing.Team != "Old Team" {
			t.Errorf("expected name to be updated and team to be kept, got %+v", existing)
		}
	})

	t.Run("full entry list", func(t *testing.T) {
		championship := NewChampionship("Test")
		championship.AddClass(modelTestClass("A", modelTestSlot("car_a", "X", "10", "")))
		championship.AddClass(modelTestClass("B", modelTestSlot("car_b", "Y", "11", "")))

		found, entrant, entrantClass, err := championship.AddEntrantInFirstFreeSlot(ChampionshipSignUpResponse{Name: "Driver", GUID: "1"})

		if err != ErrEntryListFull || found || entrant != nil || entrantClass != nil {
			t.Errorf("expected ErrEntryListFull, got found=%v entrant=%v class=%v err=%v", found, entrant, entrantClass, err)
		}
	})

	t.Run("free slots are taken in grid order", func(t *testing.T) {
		// the class entry list is a map, so run it a few times to rule out lucky map iteration orders
		for run := 0; run < 50; run++ {
			championship := NewChampionship("Test")
			slots := []*serverconfig.Entrant{
				modelTestSlot("car_a", "", "", ""),
				modelTestSlot("car_a", "", "", ""),
				modelTestSlot("car_a", "", "", ""),
			}
			championship.AddClass(modelTestClass("A", slots...))

			for i, guid := range []string{"1", "2", "3"} {
				_, entrant, _, err := championship.AddEntrantInFirstFreeSlot(ChampionshipSignUpResponse{Name: "Driver " + guid, GUID: guid})

				if err != nil {
					t.Fatal(err)
				}

				if entrant != slots[i] {
					t.Fatalf("run %d: expected driver %s in pit box %d, got pit box %d", run, guid, i, entrant.PitBox)
				}
			}
		}
	})

	t.Run("entrant without guid is rejected", func(t *testing.T) {
		championship := NewChampionship("Test")
		free := modelTestSlot("car_a", "", "", "")
		championship.AddClass(modelTestClass("A", free))

		found, entrant, entrantClass, err := championship.AddEntrantInFirstFreeSlot(ChampionshipSignUpResponse{Name: "Driver", Team: "Team"})

		if err != ErrEntrantHasNoGUID || found || entrant != nil || entrantClass != nil {
			t.Errorf("expected ErrEntrantHasNoGUID, got found=%v entrant=%v class=%v err=%v", found, entrant, entrantClass, err)
		}

		if free.Name != "" || free.GUID != "" || free.Team != "" {
			t.Errorf("free slot was modified: %+v", free)
		}
	})

	t.Run("no classes", func(t *testing.T) {
		championship := NewChampionship("Test")

		if _, _, _, err := championship.AddEntrantInFirstFreeSlot(ChampionshipSignUpResponse{GUID: "1"}); err != ErrEntryListFull {
			t.Errorf("expected ErrEntryListFull, got %v", err)
		}
	})
}

func TestChampionship_ClearEntrant(t *testing.T) {
	championship := NewChampionship("Test")
	a := modelTestSlot("car_a", "A", "1", "X")
	b := modelTestSlot("car_b", "B", "2", "Y")
	championship.AddClass(modelTestClass("A", a))
	championship.AddClass(modelTestClass("B", b))

	championship.ClearEntrant("2")

	if b.Name != "" || b.GUID != "" || b.Team != "" {
		t.Errorf("expected entrant to be cleared, got %+v", b)
	}

	if b.Model != "car_b" {
		t.Errorf("expected car to be kept, got %s", b.Model)
	}

	if a.Name != "A" || a.GUID != "1" || a.Team != "X" {
		t.Errorf("other entrant was modified: %+v", a)
	}

	// unknown guid is a no-op
	championship.ClearEntrant("unknown")

	if a.GUID != "1" {
		t.Errorf("unknown guid modified an entrant")
	}
}

func TestChampionship_ClassByID(t *testing.T) {
	championship := NewChampionship("Test")
	classA := NewChampionshipClass("A")
	classB := NewChampionshipClass("B")
	championship.AddClass(classA)
	championship.AddClass(classB)

	class, err := championship.ClassByID(classB.ID.String())

	if err != nil || class != classB {
		t.Errorf("expected class B, got %v (err %v)", class, err)
	}

	if _, err := championship.ClassByID(uuid.New().String()); err != ErrInvalidChampionshipClass {
		t.Errorf("expected ErrInvalidChampionshipClass, got %v", err)
	}
}

func TestChampionship_NumEntrants(t *testing.T) {
	newChampionship := func() *Championship {
		championship := NewChampionship("Test")
		placeholder := modelTestSlot("car_a", "P", "3", "")
		placeholder.IsPlaceHolder = true

		championship.AddClass(modelTestClass("A", modelTestSlot("car_a", "A", "1", ""), modelTestSlot("car_a", "", "", ""), placeholder))
		championship.AddClass(modelTestClass("B", modelTestSlot("car_b", "B", "2", ""), modelTestSlot("car_b", "", "", "")))

		return championship
	}

	championship := newChampionship()

	if n := championship.NumEntrants(); n != 5 {
		t.Errorf("expected all 5 slots to be counted, got %d", n)
	}

	championship = newChampionship()
	championship.SignUpForm.Enabled = true

	if n := championship.NumEntrants(); n != 2 {
		t.Errorf("closed sign up championship: expected 2 taken non-placeholder slots, got %d", n)
	}

	championship = newChampionship()
	championship.SignUpForm.Enabled = true
	championship.OpenEntrants = true

	if n := championship.NumEntrants(); n != 5 {
		t.Errorf("open sign up championship: expected 5 slots, got %d", n)
	}
}

func TestChampionship_EntrantAttendance(t *testing.T) {
	championship, _ := dropTestChampionship(0)

	// an event that is not completed does not count
	pending := NewChampionshipEvent()
	championship.Events = append(championship.Events, pending)

	for guid, expected := range map[string]int{"A": 4, "B": 4, "C": 3, "D": 0} {
		if n := championship.EntrantAttendance(guid); n != expected {
			t.Errorf("attendance %s: expected %d, got %d", guid, expected, n)
		}
	}
}

func modelTestRaceWeekendEvent(sessions ...*RaceWeekendSession) *ChampionshipEvent {
	rw := NewRaceWeekend()
	rw.Sessions = sessions

	event := NewChampionshipEvent()
	event.RaceWeekendID = rw.ID
	event.RaceWeekend = rw

	return event
}

func TestChampionship_NumCompletedEvents(t *testing.T) {
	championship, class := dropTestChampionship(0)
	championship.Events = append(championship.Events, NewChampionshipEvent())

	if n := championship.NumCompletedEvents(); n != 4 {
		t.Errorf("expected 4 completed events, got %d", n)
	}

	completedSession := NewRaceWeekendSession()
	completedSession.CompletedTime = time.Now()
	completedSession.Results = dropTestResults(class, dropDriverA)

	championship.Events = append(championship.Events, modelTestRaceWeekendEvent(completedSession, NewRaceWeekendSession()))

	// each completed race weekend session counts as an event
	if n := championship.NumCompletedEvents(); n != 5 {
		t.Errorf("expected 5 completed events with race weekend, got %d", n)
	}

	if n := NewChampionship("Empty").NumCompletedEvents(); n != 0 {
		t.Errorf("expected 0 completed events, got %d", n)
	}
}

func TestChampionship_NumPendingSignUps(t *testing.T) {
	championship := NewChampionship("Test")
	championship.SignUpForm.Responses = []*ChampionshipSignUpResponse{
		{GUID: "1", Status: ChampionshipEntrantPending},
		{GUID: "2", Status: ChampionshipEntrantAccepted},
		{GUID: "3", Status: ChampionshipEntrantPending},
		{GUID: "4", Status: ChampionshipEntrantRejected},
	}

	if n := championship.NumPendingSignUps(); n != 2 {
		t.Errorf("expected 2 pending sign ups, got %d", n)
	}

	if n := NewChampionship("Empty").NumPendingSignUps(); n != 0 {
		t.Errorf("expected 0 pending sign ups, got %d", n)
	}
}

func TestChampionship_IsMultiClass(t *testing.T) {
	championship := NewChampionship("Test")

	if championship.IsMultiClass() {
		t.Errorf("no classes: expected not multi class")
	}

	championship.AddClass(NewChampionshipClass("A"))

	if championship.IsMultiClass() {
		t.Errorf("one class: expected not multi class")
	}

	championship.AddClass(NewChampionshipClass("B"))

	if !championship.IsMultiClass() {
		t.Errorf("two classes: expected multi class")
	}
}

func TestChampionship_HasTeamNames(t *testing.T) {
	t.Run("no teams", func(t *testing.T) {
		championship := NewChampionship("Test")
		class := modelTestClass("A", modelTestSlot("car_a", "A", "1", ""))
		championship.AddClass(class)
		championship.Events = []*ChampionshipEvent{dropTestEvent(class, dropTestDriver{guid: "A"})}

		if championship.HasTeamNames() {
			t.Errorf("expected no team names")
		}
	})

	t.Run("team in entry list", func(t *testing.T) {
		championship := NewChampionship("Test")
		championship.AddClass(modelTestClass("A", modelTestSlot("car_a", "A", "1", "")))
		championship.AddClass(modelTestClass("B", modelTestSlot("car_b", "B", "2", "Team")))

		if !championship.HasTeamNames() {
			t.Errorf("expected team names from entry list")
		}
	})

	t.Run("team in completed results", func(t *testing.T) {
		championship := NewChampionship("Test")
		class := modelTestClass("A", modelTestSlot("car_a", "", "", ""))
		championship.AddClass(class)
		championship.Events = []*ChampionshipEvent{dropTestEvent(class, dropDriverA)}

		if !championship.HasTeamNames() {
			t.Errorf("expected team names from results")
		}
	})

	t.Run("team only in non-completed session", func(t *testing.T) {
		championship := NewChampionship("Test")
		class := modelTestClass("A", modelTestSlot("car_a", "", "", ""))
		championship.AddClass(class)
		event := dropTestEvent(class, dropDriverA)
		event.Sessions[serverconfig.SessionTypeRace].CompletedTime = time.Time{}
		championship.Events = []*ChampionshipEvent{event}

		if championship.HasTeamNames() {
			t.Errorf("expected results of non-completed sessions to be ignored")
		}
	})
}

func TestChampionship_HasScheduledEvents(t *testing.T) {
	championship := NewChampionship("Test")
	championship.Events = []*ChampionshipEvent{NewChampionshipEvent(), NewChampionshipEvent()}

	if championship.HasScheduledEvents() {
		t.Errorf("expected no scheduled events")
	}

	championship.Events[1].Scheduled = time.Now().Add(time.Hour)

	if !championship.HasScheduledEvents() {
		t.Errorf("expected scheduled events")
	}
}

func TestChampionshipEvent_InProgress(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name                 string
		started, completed   time.Time
		inProgress, complete bool
	}{
		{name: "not started"},
		{name: "started", started: now, inProgress: true},
		{name: "completed", started: now, completed: now.Add(time.Minute), complete: true},
	}

	for _, tc := range tests {
		event := &ChampionshipEvent{StartedTime: tc.started, CompletedTime: tc.completed}

		if event.InProgress() != tc.inProgress || event.Completed() != tc.complete {
			t.Errorf("event %s: expected inProgress=%v completed=%v, got %v/%v", tc.name, tc.inProgress, tc.complete, event.InProgress(), event.Completed())
		}

		session := &ChampionshipSession{StartedTime: tc.started, CompletedTime: tc.completed}

		if session.InProgress() != tc.inProgress {
			t.Errorf("session %s: expected inProgress=%v", tc.name, tc.inProgress)
		}

		// sessions are only completed if they have results
		if session.Completed() {
			t.Errorf("session %s: expected session without results not to be completed", tc.name)
		}

		session.Results = &smresults.SessionResults{}

		if session.Completed() != tc.complete {
			t.Errorf("session %s: expected completed=%v", tc.name, tc.complete)
		}
	}
}

func TestChampionship_FindLastResultForDriver(t *testing.T) {
	championship, _ := dropTestChampionship(0)
	base := time.Now().Add(-24 * time.Hour)

	// make completion order explicit: E1 < E2 < E3 < E4, but store them out of order
	for i, event := range championship.Events {
		event.CompletedTime = base.Add(time.Duration(i) * time.Hour)
		event.Sessions[serverconfig.SessionTypeRace].StartedTime = event.CompletedTime.Add(-time.Minute)
	}

	e1, e2, e3, e4 := championship.Events[0], championship.Events[1], championship.Events[2], championship.Events[3]
	championship.Events = []*ChampionshipEvent{e4, e2, e1, e3}

	result, team := championship.FindLastResultForDriver("C")

	if result == nil || result != e4.Sessions[serverconfig.SessionTypeRace].Results.Result[2] {
		t.Fatalf("expected C's result from the last event, got %+v", result)
	}

	if team != "Y" {
		t.Errorf("expected team Y, got %q", team)
	}

	// a later qualifying session in the last event takes precedence
	qualiResults := &smresults.SessionResults{
		Result: []*smresults.SessionResult{{DriverGUID: "C", DriverName: "C"}},
		Cars:   []*smresults.SessionCar{{Driver: smresults.SessionDriver{GUID: "C", Team: "Q"}}},
	}
	e4.Sessions[serverconfig.SessionTypeQualifying] = &ChampionshipSession{
		StartedTime:   e4.Sessions[serverconfig.SessionTypeRace].StartedTime.Add(time.Second),
		CompletedTime: e4.CompletedTime,
		Results:       qualiResults,
	}

	result, team = championship.FindLastResultForDriver("C")

	if result != qualiResults.Result[0] || team != "Q" {
		t.Errorf("expected result from the most recently started session, got %+v team %q", result, team)
	}

	// events that are not completed are ignored
	e4.CompletedTime = time.Time{}

	result, _ = championship.FindLastResultForDriver("C")

	if result != e3.Sessions[serverconfig.SessionTypeRace].Results.Result[1] {
		t.Errorf("expected C's result from E3 when E4 is not complete, got %+v", result)
	}

	// the most recently completed event (E2) doesn't contain C, result and team come from C's last event (E4)
	e4.CompletedTime = base.Add(3 * time.Hour)
	delete(e4.Sessions, serverconfig.SessionTypeQualifying)
	e2.CompletedTime = base.Add(5 * time.Hour)
	e2.Sessions[serverconfig.SessionTypeRace].StartedTime = e2.CompletedTime.Add(-time.Minute)

	result, team = championship.FindLastResultForDriver("C")

	if result != e4.Sessions[serverconfig.SessionTypeRace].Results.Result[2] || team != "Y" {
		t.Errorf("expected C's result and team from E4, got %+v team %q", result, team)
	}

	// a later session of the same event without the driver doesn't change result or team
	e4.Sessions[serverconfig.SessionTypePractice] = &ChampionshipSession{
		StartedTime:   e4.Sessions[serverconfig.SessionTypeRace].StartedTime.Add(time.Second),
		CompletedTime: e4.CompletedTime,
		Results:       dropTestResults(NewChampionshipClass("Other"), dropDriverA),
	}

	result, team = championship.FindLastResultForDriver("C")

	if result != e4.Sessions[serverconfig.SessionTypeRace].Results.Result[2] || team != "Y" {
		t.Errorf("expected C's result and team from E4's race, got %+v team %q", result, team)
	}

	if result, team := championship.FindLastResultForDriver("unknown"); result != nil || team != "" {
		t.Errorf("expected no result for unknown driver, got %+v %q", result, team)
	}

	if result, _ := NewChampionship("Empty").FindLastResultForDriver("A"); result != nil {
		t.Errorf("expected no result for empty championship")
	}
}

func modelTestRaceSession(results *smresults.SessionResults) *ChampionshipSession {
	return &ChampionshipSession{
		StartedTime:   time.Now().Add(-time.Hour),
		CompletedTime: time.Now(),
		Results:       results,
	}
}

func TestChampionshipClass_CountPositionsForTeam(t *testing.T) {
	class := NewChampionshipClass("Class")
	teamX1 := dropTestDriver{"A", "X"}
	teamX2 := dropTestDriver{"B", "X"}
	teamY := dropTestDriver{"C", "Y"}

	// E1: X, Y, X  E2: Y, X
	e1 := dropTestEvent(class, teamX1, teamY, teamX2)
	e2 := dropTestEvent(class, teamY, teamX1)

	// qualifying sessions and incomplete races are ignored
	e2.Sessions[serverconfig.SessionTypeQualifying] = modelTestRaceSession(dropTestResults(class, teamX1, teamX2, teamY))
	e3 := dropTestEvent(class, teamX1, teamX2)
	e3.Sessions[serverconfig.SessionTypeRace].CompletedTime = time.Time{}

	events := []*ChampionshipEvent{e1, e2, e3}

	tests := []struct {
		pos  int
		team string
		want int
	}{
		{1, "X", 1}, {1, "Y", 1},
		{2, "X", 1}, {2, "Y", 1},
		{3, "X", 1}, {3, "Y", 0},
		{1, "Z", 0},
	}

	for _, tc := range tests {
		if got := class.CountPositionsForTeam(tc.pos, tc.team, events); got != tc.want {
			t.Errorf("CountPositionsForTeam(%d, %s): expected %d, got %d", tc.pos, tc.team, tc.want, got)
		}
	}

	// second races count as well
	e1.Sessions[serverconfig.SessionTypeSecondRace] = modelTestRaceSession(dropTestResults(class, teamX1))

	if got := class.CountPositionsForTeam(1, "X", events); got != 2 {
		t.Errorf("expected second race win to count, got %d", got)
	}
}

func TestChampionshipClass_CountPositionsForTeam_MultiClass(t *testing.T) {
	classA := NewChampionshipClass("A")
	classB := NewChampionshipClass("B")

	// overall order: B1 (class B, team Y), A1 (class A, team X), A2 (class A, team Y)
	results := dropTestResults(classB, dropTestDriver{"B1", "Y"})
	classAResults := dropTestResults(classA, dropTestDriver{"A1", "X"}, dropTestDriver{"A2", "Y"})
	results.Result = append(results.Result, classAResults.Result...)
	results.Cars = append(results.Cars, classAResults.Cars...)

	event := NewChampionshipEvent()
	event.CompletedTime = time.Now()
	event.Sessions[serverconfig.SessionTypeRace] = modelTestRaceSession(results)
	events := []*ChampionshipEvent{event}

	tests := []struct {
		class *ChampionshipClass
		pos   int
		team  string
		want  int
	}{
		{classA, 1, "X", 1}, {classA, 2, "Y", 1}, {classA, 1, "Y", 0}, {classA, 2, "X", 0}, {classA, 3, "Y", 0},
		{classB, 1, "Y", 1}, {classB, 2, "X", 0}, {classB, 3, "Y", 0},
	}

	for _, tc := range tests {
		if got := tc.class.CountPositionsForTeam(tc.pos, tc.team, events); got != tc.want {
			t.Errorf("class %s CountPositionsForTeam(%d, %s): expected %d, got %d", tc.class.Name, tc.pos, tc.team, tc.want, got)
		}
	}
}

func TestChampionshipClass_TeamHadBetterFinishingPositions(t *testing.T) {
	class := NewChampionshipClass("Class")
	teamX1 := dropTestDriver{"A", "X"}
	teamX2 := dropTestDriver{"B", "X"}
	teamY := dropTestDriver{"C", "Y"}

	// one win each, one second place each; X has an extra third place
	events := []*ChampionshipEvent{
		dropTestEvent(class, teamX1, teamY, teamX2),
		dropTestEvent(class, teamY, teamX1),
	}

	if !class.TeamHadBetterFinishingPositions("X", "Y", events, 3) {
		t.Errorf("expected X to win the tie-break on third places")
	}

	if class.TeamHadBetterFinishingPositions("Y", "X", events, 3) {
		t.Errorf("expected Y to lose the tie-break on third places")
	}

	// only looking at first and second places it's a tie, which falls back to alphabetical order
	if !class.TeamHadBetterFinishingPositions("X", "Y", events, 2) {
		t.Errorf("expected alphabetical fallback X < Y")
	}

	if class.TeamHadBetterFinishingPositions("Y", "X", events, 2) {
		t.Errorf("expected alphabetical fallback Y > X")
	}

	// more wins beat everything else
	events = append(events, dropTestEvent(class, teamY, teamX1, teamX2))

	if !class.TeamHadBetterFinishingPositions("Y", "X", events, 3) {
		t.Errorf("expected Y to win the tie-break on wins")
	}
}

func TestChampionshipClass_TeamStandings_TieBreak(t *testing.T) {
	championship := NewChampionship("Tie")
	class := NewChampionshipClass("Class")
	class.Points.Places = []int{10, 6, 4}
	championship.AddClass(class)

	// both teams score 20 points, team Z has two wins and ranks first despite the alphabetical order.
	z := dropTestDriver{"Z1", "Z"}
	a1 := dropTestDriver{"A1", "A"}
	a2 := dropTestDriver{"A2", "A"}

	championship.Events = []*ChampionshipEvent{
		dropTestEvent(class, z, a1, a2),
		dropTestEvent(class, z, a1, a2),
	}

	standings := class.TeamStandings(championship, championship.Events)

	if len(standings) != 2 || standings[0].Points != 20 || standings[1].Points != 20 {
		t.Fatalf("expected two teams with 20 points, got %+v", standings)
	}

	if standings[0].Team != "Z" || standings[1].Team != "A" {
		t.Errorf("expected Z ahead of A on wins, got %s, %s", standings[0].Team, standings[1].Team)
	}
}

func TestChampionshipClass_TeamStandings_TieBreakMoreCarsThanTeams(t *testing.T) {
	championship := NewChampionship("Tie")
	class := NewChampionshipClass("Class")
	class.Points.Places = []int{10, 5, 0}
	championship.AddClass(class)

	z1 := dropTestDriver{"Z1", "Z"}
	z2 := dropTestDriver{"Z2", "Z"}
	a1 := dropTestDriver{"A1", "A"}

	// Z: 1st + 3rd, then 2nd. A: 2nd, then 1st. Both have 15 points, Z has an extra 3rd place.
	championship.Events = []*ChampionshipEvent{
		dropTestEvent(class, z1, a1, z2),
		dropTestEvent(class, a1, z1),
	}

	standings := class.TeamStandings(championship, championship.Events)

	if len(standings) != 2 || standings[0].Points != 15 || standings[1].Points != 15 {
		t.Fatalf("expected two teams with 15 points, got %+v", standings)
	}

	if standings[0].Team != "Z" || standings[1].Team != "A" {
		t.Errorf("expected Z ahead of A on third places, got %s, %s", standings[0].Team, standings[1].Team)
	}
}

func modelTestRaceWeekendRace(class *ChampionshipClass, finishOrder ...dropTestDriver) *RaceWeekendSession {
	session := NewRaceWeekendSession()
	session.RaceConfig.Sessions = serverconfig.Sessions{serverconfig.SessionTypeRace: &serverconfig.SessionConfig{}}
	session.StartedTime = time.Now().Add(-time.Hour)
	session.CompletedTime = time.Now()
	session.Results = dropTestResults(class, finishOrder...)
	session.Points[class.ID] = &class.Points

	return session
}

func TestChampionshipClass_Standings_TieBreakRaceWeekend(t *testing.T) {
	newChampionship := func() (*Championship, *ChampionshipClass) {
		championship := NewChampionship("Tie")
		class := NewChampionshipClass("Class")
		class.Points.Places = []int{5, 3, 2, 0}
		championship.AddClass(class)

		z := dropTestDriver{"Z", "Z"}
		a := dropTestDriver{"A", "A"}
		f := dropTestDriver{"F", "F"}
		g := dropTestDriver{"G", "G"}

		// a single race weekend with two races. Z: 1st + 4th, A: 2nd + 3rd, both have 5 points but Z has a win.
		championship.Events = []*ChampionshipEvent{
			modelTestRaceWeekendEvent(
				modelTestRaceWeekendRace(class, z, a, f, g),
				modelTestRaceWeekendRace(class, f, g, a, z),
			),
		}

		return championship, class
	}

	championship, class := newChampionship()

	if got := class.CountPositionsForDriver(1, "Z", championship.Events); got != 1 {
		t.Errorf("expected race weekend win to be counted for Z, got %d", got)
	}

	if got := class.CountPositionsForTeam(4, "Z", championship.Events); got != 1 {
		t.Errorf("expected race weekend 4th place to be counted for team Z, got %d", got)
	}

	// standings are built from a map, repeat to rule out lucky orders
	for run := 0; run < 20; run++ {
		championship, class := newChampionship()

		standings := class.Standings(championship, championship.Events)

		if len(standings) != 4 || standingFor(t, standings, "Z").Points != 5 || standingFor(t, standings, "A").Points != 5 {
			t.Fatalf("unexpected standings: %+v", standings)
		}

		if standings[1].Car.Driver.GUID != "Z" || standings[2].Car.Driver.GUID != "A" {
			t.Fatalf("run %d: expected Z ahead of A on wins, got %s, %s", run, standings[1].Car.Driver.GUID, standings[2].Car.Driver.GUID)
		}

		teamStandings := class.TeamStandings(championship, championship.Events)

		if len(teamStandings) != 4 || teamStandings[1].Team != "Z" || teamStandings[2].Team != "A" {
			t.Fatalf("run %d: expected team Z ahead of team A on wins, got %+v", run, teamStandings)
		}
	}
}

func TestChampionshipClass_StandingsForEntrantAtEvent(t *testing.T) {
	championship := NewChampionship("Test")
	class := NewChampionshipClass("Class")
	class.Points.Places = []int{10, 6, 3, 2}
	championship.AddClass(class)

	drivers := []dropTestDriver{{"P1", ""}, {"P2", ""}, {"P3", ""}, {"P4", ""}, {"P5", ""}}
	event := dropTestEvent(class, drivers...)
	championship.Events = []*ChampionshipEvent{event}

	tests := []struct {
		guid           string
		colour, reason string
		points         float64
	}{
		{"P1", "#fefebe", "DNS", 10},
		{"P2", "#dce0e1", "DNS", 6},
		{"P3", "#ffde9e", "DNS", 3},
		{"P4", "rgb(214, 245, 214)", "DNS", 2},
		{"P5", "#ebcaff", "0", 0},
		{"absent", "#ffffff", "DNS", 0},
	}

	for _, tc := range tests {
		entrant := NewChampionshipStanding(&smresults.SessionCar{Driver: smresults.SessionDriver{GUID: tc.guid, Name: tc.guid}})
		entrant.Points = 99

		info := class.StandingsForEntrantAtEvent(championship, event, entrant)

		if string(info.BackgroundColour) != tc.colour || info.NoFinishReason != tc.reason || info.Points != tc.points {
			t.Errorf("%s: expected %s/%s/%v, got %s/%s/%v", tc.guid, tc.colour, tc.reason, tc.points, info.BackgroundColour, info.NoFinishReason, info.Points)
		}

		if info.Car.GetGUID() != tc.guid {
			t.Errorf("%s: wrong car in standing: %s", tc.guid, info.Car.GetGUID())
		}
	}
}

func TestChampionshipSignUpForm_EmailList(t *testing.T) {
	form := ChampionshipSignUpForm{
		Responses: []*ChampionshipSignUpResponse{
			{Email: "a@example.com", Status: ChampionshipEntrantAccepted},
			{Email: "b@example.com", Status: ChampionshipEntrantRejected},
			{Email: "c@example.com", Status: ChampionshipEntrantPending},
			{Email: "d@example.com", Status: ChampionshipEntrantAccepted},
			{Email: "", Status: ChampionshipEntrantPending},
			{Email: "", Status: ChampionshipEntrantAccepted},
			{Email: "", Status: ChampionshipEntrantRejected},
		},
	}

	// responses without an email address are skipped in every group
	tests := map[string]string{
		"accepted": "a@example.com,d@example.com",
		"rejected": "b@example.com",
		"pending":  "c@example.com",
		"all":      "a@example.com,b@example.com,c@example.com,d@example.com",
	}

	for group, expected := range tests {
		if got := form.EmailList(group); got != expected {
			t.Errorf("group %s: expected %q, got %q", group, expected, got)
		}
	}

	if got := (ChampionshipSignUpForm{}).EmailList("all"); got != "" {
		t.Errorf("expected empty list, got %q", got)
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Errorf("expected unknown group to panic")
			}
		}()

		form.EmailList("unknown")
	}()
}

func TestActiveChampionship_Password(t *testing.T) {
	a := &ActiveChampionship{Name: "Champ", OverridePassword: true, ReplacementPassword: "secret"}

	if !a.OverrideServerPassword() || a.ReplacementServerPassword() != "secret" {
		t.Errorf("expected override password secret, got %v %q", a.OverrideServerPassword(), a.ReplacementServerPassword())
	}

	if a.EventName() != "Champ" {
		t.Errorf("unexpected event name %q", a.EventName())
	}

	a.IsPracticeSession = true

	if a.EventName() != "Champ - Looping Practice" {
		t.Errorf("unexpected practice event name %q", a.EventName())
	}

	if (&ActiveChampionship{}).OverrideServerPassword() {
		t.Errorf("expected no override by default")
	}
}

func TestChampionship_ImportEvent(t *testing.T) {
	newChampionship := func() (*Championship, *ChampionshipClass) {
		championship := NewChampionship("Import")
		class := modelTestClass("A", modelTestSlot("car_a", "A", "1", ""), modelTestSlot("car_a", "B", "2", ""))
		class.Points.Places = []int{10, 6, 3}
		championship.AddClass(class)
		championship.AddClass(modelTestClass("B", modelTestSlot("car_b", "C", "3", "")))

		return championship, class
	}

	t.Run("custom race", func(t *testing.T) {
		championship, _ := newChampionship()
		race := &CustomRace{UUID: uuid.New(), RaceConfig: serverconfig.CurrentRaceConfig{Track: "monza"}}

		event, err := championship.ImportEvent(race)

		if err != nil {
			t.Fatal(err)
		}

		if event.ID == uuid.Nil || event.ID == race.UUID {
			t.Errorf("expected a new event id")
		}

		if event.RaceSetup.Track != "monza" || len(event.EntryList) != 3 {
			t.Errorf("expected race setup and all 3 championship entrants, got track %q, %d entrants", event.RaceSetup.Track, len(event.EntryList))
		}

		if len(championship.Events) != 1 || championship.Events[0] != event {
			t.Errorf("expected event to be appended to the championship")
		}
	})

	t.Run("championship event", func(t *testing.T) {
		championship, _ := newChampionship()
		source := NewChampionshipEvent()
		source.RaceSetup.Track = "spa"
		source.EntryList = serverconfig.EntryList{}
		source.EntryList.AddToBackOfGrid(modelTestSlot("car_x", "X", "9", ""))
		source.CompletedTime = time.Now()

		event, err := championship.ImportEvent(source)

		if err != nil {
			t.Fatal(err)
		}

		if event.ID == source.ID || event.RaceSetup.Track != "spa" || len(event.EntryList) != 1 {
			t.Errorf("unexpected imported event: %+v", event)
		}

		if event.Completed() {
			t.Errorf("imported event should not be completed")
		}
	})

	t.Run("unknown type", func(t *testing.T) {
		championship, _ := newChampionship()

		if _, err := championship.ImportEvent("nope"); err == nil {
			t.Errorf("expected error for unknown event type")
		}

		if len(championship.Events) != 0 {
			t.Errorf("expected no event to be added")
		}
	})

	t.Run("race weekend", func(t *testing.T) {
		championship, class := newChampionship()

		rw := NewRaceWeekend()
		oldID := rw.ID

		quali := NewRaceWeekendSession()
		quali.RaceConfig.Sessions = serverconfig.Sessions{serverconfig.SessionTypeQualifying: &serverconfig.SessionConfig{}}
		quali.ParentIDs = []uuid.UUID{oldID}
		quali.StartedTime = time.Now()
		quali.CompletedTime = time.Now()
		quali.ScheduledTime = time.Now()
		quali.Results = &smresults.SessionResults{}

		race := NewRaceWeekendSession()
		race.RaceConfig.Sessions = serverconfig.Sessions{serverconfig.SessionTypeRace: &serverconfig.SessionConfig{}}
		race.ParentIDs = []uuid.UUID{quali.ID}

		rw.Sessions = []*RaceWeekendSession{quali, race}
		filter := map[string]*RaceWeekendSessionToSessionFilter{quali.ID.String(): {}}
		rw.Filters = map[string]map[string]*RaceWeekendSessionToSessionFilter{oldID.String(): filter}

		event, err := championship.ImportEvent(rw)

		if err != nil {
			t.Fatal(err)
		}

		if rw.ID == oldID || event.RaceWeekendID != rw.ID || event.RaceWeekend != rw || !event.IsRaceWeekend() {
			t.Errorf("expected race weekend to get a new id linked to the event")
		}

		if rw.Championship != championship || rw.ChampionshipID != championship.ID {
			t.Errorf("expected race weekend to be linked to the championship")
		}

		if _, ok := rw.Filters[oldID.String()]; ok {
			t.Errorf("expected old filter key to be removed")
		}

		if f := rw.Filters[rw.ID.String()]; len(f) != 1 || f[quali.ID.String()] == nil {
			t.Errorf("expected filter to be moved to the new race weekend id")
		}

		if quali.ParentIDs[0] != rw.ID || race.ParentIDs[0] != quali.ID {
			t.Errorf("expected entry list parent ids to be replaced, got %v / %v", quali.ParentIDs, race.ParentIDs)
		}

		if !quali.StartedTime.IsZero() || !quali.CompletedTime.IsZero() || !quali.ScheduledTime.IsZero() || quali.Results != nil {
			t.Errorf("expected session progress to be reset")
		}

		if race.Points[class.ID] != &class.Points {
			t.Errorf("expected race session to use class points")
		}

		qualiPoints := quali.Points[class.ID]

		if qualiPoints == nil || len(qualiPoints.Places) != 3 {
			t.Fatalf("expected zero points for non-race sessions, got %+v", qualiPoints)
		}

		for _, p := range qualiPoints.Places {
			if p != 0 {
				t.Errorf("expected zero points for non-race sessions, got %v", qualiPoints.Places)
			}
		}

		if len(quali.Points) != 2 || len(race.Points) != 2 {
			t.Errorf("expected points for both classes")
		}

		if len(event.EntryList) != 3 || len(championship.Events) != 1 {
			t.Errorf("expected event with all entrants appended to championship")
		}
	})
}

// Championships and server options stored by versions that still had the ACSR integration must stay loadable.
func TestChampionship_LegacyACSRFieldsAreIgnored(t *testing.T) {
	legacy := struct {
		Name                 string
		ACSR                 bool
		ACSRSkillGate        string
		ACSRSafetyGate       int
		EnableACSRSafetyGate bool
		EnableACSRSkillGate  bool
	}{Name: "Legacy", ACSR: true, ACSRSkillGate: "E", ACSRSafetyGate: 50, EnableACSRSafetyGate: true}

	t.Run("json", func(t *testing.T) {
		data, err := json.Marshal(legacy)

		if err != nil {
			t.Fatal(err)
		}

		var championship Championship

		if err := json.Unmarshal(data, &championship); err != nil {
			t.Fatal(err)
		}

		if championship.Name != "Legacy" {
			t.Errorf("expected name Legacy, got %q", championship.Name)
		}
	})

	t.Run("gob", func(t *testing.T) {
		buf := new(bytes.Buffer)

		if err := gob.NewEncoder(buf).Encode(legacy); err != nil {
			t.Fatal(err)
		}

		var championship Championship

		if err := gob.NewDecoder(buf).Decode(&championship); err != nil {
			t.Fatal(err)
		}

		if championship.Name != "Legacy" {
			t.Errorf("expected name Legacy, got %q", championship.Name)
		}
	})
}
