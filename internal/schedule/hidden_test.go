package schedule

import (
	"bytes"
	"testing"
	"time"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

func TestHiddenEventsAreOnlyScheduledForLoggedInViewers(t *testing.T) {
	handler, store, cleanup := setupUpcomingAPITest(t)
	defer cleanup()

	now := time.Now().UTC().Truncate(time.Second)

	championship := smmodel.NewChampionship("Secret Championship")
	championship.Hidden = true

	event := smmodel.NewChampionshipEvent()
	event.RaceSetup = serverconfig.ConfigIniDefault().CurrentRaceConfig
	event.Scheduled = now.Add(time.Hour)
	event.ScheduledServerID = serverID
	championship.Events = []*smmodel.ChampionshipEvent{event}

	raceWeekend := smmodel.NewRaceWeekend()
	raceWeekend.Name = "Secret Weekend"
	raceWeekend.Hidden = true

	session := smmodel.NewRaceWeekendSession()
	session.RaceConfig.Sessions = serverconfig.Sessions{serverconfig.SessionTypeRace: &serverconfig.SessionConfig{Name: "Race", Laps: 5}}
	session.ScheduledTime = now.Add(2 * time.Hour)
	session.ScheduledServerID = serverID
	raceWeekend.Sessions = []*smmodel.RaceWeekendSession{session}

	for _, err := range []error{store.UpsertChampionship(championship), store.UpsertRaceWeekend(raceWeekend)} {
		if err != nil {
			t.Fatal(err)
		}
	}

	manager := handler.scheduledRacesManager

	upcoming, err := manager.UpcomingRaces(now, 0, "", false)

	if err != nil || len(upcoming) != 0 {
		t.Fatalf("hidden events must not be upcoming for anonymous viewers: %v, %v", upcoming, err)
	}

	if upcoming, err = manager.UpcomingRaces(now, 0, "", true); err != nil || len(upcoming) != 2 {
		t.Fatalf("logged in viewers see hidden events: %v, %v", upcoming, err)
	}

	calendar, err := manager.BuildCalendar(now.Add(-time.Hour), now.Add(24*time.Hour), "", false)

	if err != nil {
		t.Fatal(err)
	}

	for _, object := range calendar {
		if object.ID != "no-events" {
			t.Errorf("hidden event in the calendar: %+v", object)
		}
	}

	var feed bytes.Buffer

	if err := manager.buildScheduledRaces(&feed, "", false, nil); err != nil || bytes.Contains(feed.Bytes(), []byte("VEVENT")) {
		t.Fatalf("hidden events must not be in the feed: %v\n%s", err, feed.String())
	}

	feed.Reset()

	if err := manager.buildScheduledRaces(&feed, "", true, nil); err != nil || !bytes.Contains(feed.Bytes(), []byte("VEVENT")) {
		t.Fatalf("logged in viewers get hidden events in the feed: %v", err)
	}
}
