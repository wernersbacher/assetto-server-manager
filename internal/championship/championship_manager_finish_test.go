package championship

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"
)

// partiallyDrivenChampionship stores a championship whose first event has a second race (reversed grid) of which only
// the first race was driven, i.e. the event waits for the second race.
func partiallyDrivenChampionship(t *testing.T, cm *ChampionshipManager) (*smmodel.Championship, *smmodel.ChampionshipEvent) {
	t.Helper()

	var champ *smmodel.Championship

	f, err := os.Open(filepath.Join("..", "..", "fixtures", "open-championship", "championship-setup.json"))

	if err != nil {
		t.Fatal(err)
	}

	defer f.Close()

	if err := json.NewDecoder(f).Decode(&champ); err != nil {
		t.Fatal(err)
	}

	rf, err := os.Open(filepath.Join("..", "..", "fixtures", "results", "2019_2_15_22_10_RACE.json"))

	if err != nil {
		t.Fatal(err)
	}

	defer rf.Close()

	var results smresults.SessionResults

	if err := json.NewDecoder(rf).Decode(&results); err != nil {
		t.Fatal(err)
	}

	event := champ.Events[0]
	event.RaceSetup.ReversedGridRacePositions = 1

	if !event.RaceSetup.HasSession(serverconfig.SessionTypeRace) {
		t.Fatal("fixture event has no race session")
	}

	start := time.Now().Add(-time.Hour)

	event.StartedTime = start
	event.CompletedTime = time.Time{}
	event.Sessions = map[serverconfig.SessionType]*smmodel.ChampionshipSession{
		serverconfig.SessionTypeRace: {StartedTime: start, CompletedTime: start.Add(20 * time.Minute), Results: &results},
		// race 2 was started, but never finished
		serverconfig.SessionTypeSecondRace: {StartedTime: start.Add(30 * time.Minute)},
	}

	if err := cm.UpsertChampionship(champ); err != nil {
		t.Fatal(err)
	}

	return champ, event
}

func TestChampionshipManager_FinishEvent(t *testing.T) {
	cm := newTestChampionshipManager()
	champ, event := partiallyDrivenChampionship(t, cm)

	if !event.InProgress() || event.LastSession() != serverconfig.SessionTypeSecondRace || !event.HasCompletedSessions() {
		t.Fatal("expected an event in progress that waits for the second race")
	}

	if err := cm.FinishEvent(champ.ID.String(), event.ID.String()); err != nil {
		t.Fatal(err)
	}

	loaded, err := cm.LoadChampionship(champ.ID.String())

	if err != nil {
		t.Fatal(err)
	}

	got, _, err := loaded.EventByID(event.ID.String())

	if err != nil {
		t.Fatal(err)
	}

	if !got.Completed() || got.InProgress() {
		t.Error("event should be completed")
	}

	race, ok := got.Sessions[serverconfig.SessionTypeRace]

	if !ok || race.Results == nil || !race.Completed() {
		t.Fatal("results of race 1 must be kept")
	}

	if _, ok := got.Sessions[serverconfig.SessionTypeSecondRace]; ok {
		t.Error("the unfinished second race should be dropped")
	}

	// the results count for the standings
	total := 0.0

	for _, class := range loaded.Classes {
		for _, standing := range class.Standings(loaded, loaded.Events) {
			total += standing.Points
		}
	}

	if total <= 0 {
		t.Error("the results of race 1 should give standings points")
	}
}

func TestChampionshipManager_FinishEventWithoutResults(t *testing.T) {
	cm := newTestChampionshipManager()
	champ, event := partiallyDrivenChampionship(t, cm)

	loaded, _ := cm.LoadChampionship(champ.ID.String())
	e, _, _ := loaded.EventByID(event.ID.String())
	e.Sessions = map[serverconfig.SessionType]*smmodel.ChampionshipSession{}

	if err := cm.UpsertChampionship(loaded); err != nil {
		t.Fatal(err)
	}

	if err := cm.FinishEvent(champ.ID.String(), event.ID.String()); err != ErrNoSessionsDriven {
		t.Errorf("expected ErrNoSessionsDriven, got %v", err)
	}
}

// cancelling still resets the whole event including race 1.
func TestChampionshipManager_CancelEventDiscardsResults(t *testing.T) {
	cm := newTestChampionshipManager()
	champ, event := partiallyDrivenChampionship(t, cm)

	if err := cm.CancelEvent(champ.ID.String(), event.ID.String()); err != nil {
		t.Fatal(err)
	}

	loaded, _ := cm.LoadChampionship(champ.ID.String())
	got, _, _ := loaded.EventByID(event.ID.String())

	if len(got.Sessions) != 0 || got.InProgress() || got.Completed() {
		t.Error("cancel should reset the event and discard all results")
	}
}
