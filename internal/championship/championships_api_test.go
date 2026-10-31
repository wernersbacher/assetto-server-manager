package championship

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	"github.com/wernersbacher/ACPitwall/internal/api"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smresults "github.com/wernersbacher/ACPitwall/internal/results"
)

func TestNewAPIChampionship(t *testing.T) {
	scheduled := time.Date(2026, 11, 7, 19, 0, 0, 0, time.UTC)

	championship := smmodel.NewChampionship("Test Championship")

	class := smmodel.NewChampionshipClass("GT3")
	class.AvailableCars = []string{"dev_car_gt3"}
	championship.AddClass(class)

	// a scheduled event with timed practice and qualifying, a race with laps and a second race
	scheduledEvent := smmodel.NewChampionshipEvent()
	scheduledEvent.Scheduled = scheduled
	scheduledEvent.RaceSetup.Track = "api_test_track"
	scheduledEvent.RaceSetup.ReversedGridRacePositions = -1
	scheduledEvent.RaceSetup.Sessions = serverconfig.Sessions{
		serverconfig.SessionTypePractice:   &serverconfig.SessionConfig{Name: "Practice", Time: 30},
		serverconfig.SessionTypeQualifying: &serverconfig.SessionConfig{Name: "Qualify", Time: 15},
		serverconfig.SessionTypeRace:       &serverconfig.SessionConfig{Name: "Race", Laps: 20},
	}

	// a completed event with a timed race
	completedEvent := smmodel.NewChampionshipEvent()
	completedEvent.RaceSetup.Track = "api_test_ring"
	completedEvent.RaceSetup.TrackLayout = "gp"
	completedEvent.RaceSetup.RaceExtraLap = 1
	completedEvent.RaceSetup.Sessions = serverconfig.Sessions{
		serverconfig.SessionTypeRace: &serverconfig.SessionConfig{Name: "Race", Time: 45},
	}
	completedEvent.StartedTime = scheduled.Add(-time.Hour)
	completedEvent.CompletedTime = scheduled
	completedEvent.Sessions[serverconfig.SessionTypeRace] = &smmodel.ChampionshipSession{
		StartedTime:   scheduled.Add(-time.Hour),
		CompletedTime: scheduled,
		Results:       &smresults.SessionResults{},
	}

	// a race weekend event with a completed qualifying and an unscheduled race
	raceWeekend := smmodel.NewRaceWeekend()
	raceWeekend.Name = "Endurance"

	qualifying := smmodel.NewRaceWeekendSession()
	qualifying.RaceConfig.Track = "api_test_ring"
	qualifying.RaceConfig.TrackLayout = "short"
	qualifying.RaceConfig.Sessions = serverconfig.Sessions{serverconfig.SessionTypeQualifying: &serverconfig.SessionConfig{Name: "Qualy", Time: 20}}
	qualifying.ScheduledTime = scheduled
	qualifying.StartedTime = scheduled
	qualifying.CompletedTime = scheduled.Add(20 * time.Minute)
	qualifying.Results = &smresults.SessionResults{}

	race := smmodel.NewRaceWeekendSession()
	race.ParentIDs = []uuid.UUID{qualifying.ID}
	race.RaceConfig.Track = "api_test_ring"
	race.RaceConfig.TrackLayout = "short"
	race.RaceConfig.Sessions = serverconfig.Sessions{serverconfig.SessionTypeRace: &serverconfig.SessionConfig{Name: "Main Race", Laps: 50}}

	// added in reverse order, sessions must be sorted by their dependencies
	raceWeekend.Sessions = []*smmodel.RaceWeekendSession{race, qualifying}

	raceWeekendEvent := smmodel.NewChampionshipEvent()
	raceWeekendEvent.RaceWeekendID = raceWeekend.ID
	raceWeekendEvent.RaceWeekend = raceWeekend

	championship.Events = []*smmodel.ChampionshipEvent{scheduledEvent, completedEvent, raceWeekendEvent}

	out := api.NewChampionship(testInstallPath, championship, "")

	if out.Name != "Test Championship" || out.Completed || len(out.Classes) != 1 || len(out.Events) != 3 {
		t.Fatalf("unexpected championship: %+v", out)
	}

	if cars := out.Classes[0].Cars; len(cars) != 1 || cars[0].ID != "dev_car_gt3" {
		t.Errorf("unexpected class cars: %+v", cars)
	}

	t.Run("scheduled event", func(t *testing.T) {
		event := out.Events[0]

		if event.Status != api.EventStatusScheduled || event.Scheduled == nil || !event.Scheduled.Equal(scheduled) {
			t.Fatalf("unexpected event: %+v", event)
		}

		if event.TrackName != "Api Test Track" {
			t.Errorf("expected track name Api Test Track, got %s", event.TrackName)
		}

		expected := []struct {
			sessionType serverconfig.SessionType
			start       *time.Time
		}{
			{serverconfig.SessionTypePractice, api.Time(scheduled)},
			{serverconfig.SessionTypeQualifying, api.Time(scheduled.Add(30 * time.Minute))},
			{serverconfig.SessionTypeRace, api.Time(scheduled.Add(45 * time.Minute))},
			// the race has a number of laps, so the second race's start is unknown
			{serverconfig.SessionTypeSecondRace, nil},
		}

		if len(event.Sessions) != len(expected) {
			t.Fatalf("expected %d sessions, got %d", len(expected), len(event.Sessions))
		}

		for i, e := range expected {
			session := event.Sessions[i]

			if session.Type != e.sessionType {
				t.Errorf("session %d: expected type %s, got %s", i, e.sessionType, session.Type)
			}

			if (e.start == nil) != (session.Start == nil) || (e.start != nil && !e.start.Equal(*session.Start)) {
				t.Errorf("session %s: expected start %v, got %v", session.Type, e.start, session.Start)
			}

			if session.Status != api.EventStatusScheduled {
				t.Errorf("session %s: expected status scheduled, got %s", session.Type, session.Status)
			}
		}

		if race := event.Sessions[2]; race.Laps != 20 || race.Time != 0 {
			t.Errorf("unexpected race session: %+v", race)
		}
	})

	t.Run("completed event", func(t *testing.T) {
		event := out.Events[1]

		if event.Status != api.EventStatusCompleted || event.Scheduled != nil || event.CompletedTime == nil {
			t.Fatalf("unexpected event: %+v", event)
		}

		if event.TrackName != "Api Test Ring (GP)" {
			t.Errorf("expected track name Api Test Ring (GP), got %s", event.TrackName)
		}

		if race := event.Sessions[0]; race.Status != api.EventStatusCompleted || race.Time != 45 || !race.ExtraLap {
			t.Errorf("unexpected race session: %+v", race)
		}
	})

	t.Run("race weekend event", func(t *testing.T) {
		event := out.Events[2]

		if event.RaceWeekendName != "Endurance" || event.Status != api.EventStatusInProgress {
			t.Fatalf("unexpected event: %+v", event)
		}

		if event.Scheduled == nil || !event.Scheduled.Equal(scheduled) || event.Track != "api_test_ring" || event.TrackLayout != "short" {
			t.Errorf("unexpected event schedule or track: %+v", event)
		}

		if len(event.Sessions) != 2 {
			t.Fatalf("expected 2 sessions, got %d", len(event.Sessions))
		}

		if qualifying := event.Sessions[0]; qualifying.Name != "Qualy" || qualifying.Status != api.EventStatusCompleted || qualifying.Start == nil {
			t.Errorf("unexpected qualifying session: %+v", qualifying)
		}

		if race := event.Sessions[1]; race.Name != "Main Race" || race.Status != api.EventStatusUnscheduled || race.Start != nil || race.Laps != 50 {
			t.Errorf("unexpected race session: %+v", race)
		}
	})
}
