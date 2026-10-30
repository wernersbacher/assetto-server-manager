package raceweekend

import (
	"testing"
	"time"

	"github.com/google/uuid"

	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"
)

func TestRaceWeekendListSplit(t *testing.T) {
	active := &smmodel.RaceWeekend{ID: uuid.New(), Sessions: []*smmodel.RaceWeekendSession{{}}}
	done := &smmodel.RaceWeekend{ID: uuid.New(), Sessions: []*smmodel.RaceWeekendSession{{CompletedTime: time.Now(), Results: &smresults.SessionResults{}}}}
	linked := &smmodel.RaceWeekend{ID: uuid.New(), ChampionshipID: uuid.New()}

	vars := &raceWeekendListTemplateVars{RaceWeekends: []*smmodel.RaceWeekend{active, done, linked}}

	if got := vars.Active(); len(got) != 1 || got[0] != active {
		t.Errorf("unexpected active weekends: %v", got)
	}

	if got := vars.Completed(); len(got) != 1 || got[0] != done {
		t.Errorf("unexpected completed weekends: %v", got)
	}
}
