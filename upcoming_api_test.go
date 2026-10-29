package servermanager

import (
	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smconfig "github.com/wernersbacher/assetto-server-manager/internal/config"

	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func setupUpcomingAPITest(t *testing.T) (*ScheduledRacesHandler, Store, func()) {
	dir, err := ioutil.TempDir("", "upcoming-api")

	if err != nil {
		t.Fatal(err)
	}

	oldServerID := serverID
	serverID = "test-server"

	store := NewJSONStore(filepath.Join(dir, "store"), filepath.Join(dir, "store-shared"))

	handler := NewScheduledRacesHandler(&BaseHandler{}, NewScheduledRacesManager(&smconfig.Configuration{}, store))

	return handler, store, func() {
		serverID = oldServerID

		_ = os.RemoveAll(dir)
	}
}

func getUpcomingAPI(handler *ScheduledRacesHandler, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	handler.upcomingAPI(rec, httptest.NewRequest(http.MethodGet, target, nil))

	return rec
}

func TestScheduledRacesHandlerUpcomingAPI(t *testing.T) {
	handler, store, cleanup := setupUpcomingAPITest(t)
	defer cleanup()

	t.Run("nothing scheduled", func(t *testing.T) {
		rec := getUpcomingAPI(handler, "/api/upcoming-races")

		if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
			t.Fatalf("expected an empty list, got %d %q", rec.Code, rec.Body.String())
		}
	})

	now := time.Now().UTC().Truncate(time.Second)

	// a recurring custom race on another server
	customRace := &CustomRace{UUID: uuid.New(), Name: "Sunday Cruise", HasCustomName: true}
	customRace.RaceConfig = serverconfig.ConfigIniDefault().CurrentRaceConfig
	customRace.RaceConfig.Track = "api_test_track"
	customRace.RaceConfig.Cars = "dev_car_gt3;dev_car_gt4"
	customRace.RaceConfig.Sessions = serverconfig.Sessions{
		serverconfig.SessionTypePractice: &serverconfig.SessionConfig{Name: "Practice", Time: 10},
		serverconfig.SessionTypeRace:     &serverconfig.SessionConfig{Name: "Race", Laps: 5},
	}
	customRace.Scheduled = now.Add(3 * time.Hour)
	customRace.ScheduledInitial = customRace.Scheduled
	customRace.ScheduledServerID = "other-server"

	if err := customRace.SetRecurrenceRule("FREQ=WEEKLY"); err != nil {
		t.Fatal(err)
	}

	// a custom race that is not scheduled
	unscheduled := &CustomRace{UUID: uuid.New(), Name: "Not scheduled"}
	unscheduled.RaceConfig = serverconfig.ConfigIniDefault().CurrentRaceConfig

	// a championship with an event in the past and one in the future
	championship := NewChampionship("Test Championship")

	pastEvent := NewChampionshipEvent()
	pastEvent.RaceSetup = serverconfig.ConfigIniDefault().CurrentRaceConfig
	pastEvent.Scheduled = now.Add(-time.Hour)
	pastEvent.ScheduledServerID = serverID

	futureEvent := NewChampionshipEvent()
	futureEvent.RaceSetup = serverconfig.ConfigIniDefault().CurrentRaceConfig
	futureEvent.RaceSetup.Track = "api_test_ring"
	futureEvent.RaceSetup.TrackLayout = "gp"
	futureEvent.Scheduled = now.Add(time.Hour)
	futureEvent.ScheduledServerID = serverID

	championship.Events = []*ChampionshipEvent{pastEvent, futureEvent}

	// a race weekend with a scheduled and an unscheduled session
	raceWeekend := NewRaceWeekend()
	raceWeekend.Name = "Endurance"

	qualifying := NewRaceWeekendSession()
	qualifying.RaceConfig.Track = "api_test_ring"
	qualifying.RaceConfig.Sessions = serverconfig.Sessions{serverconfig.SessionTypeQualifying: &serverconfig.SessionConfig{Name: "Qualy", Time: 20}}
	qualifying.ScheduledTime = now.Add(2 * time.Hour)
	qualifying.ScheduledServerID = serverID

	race := NewRaceWeekendSession()
	race.ParentIDs = []uuid.UUID{qualifying.ID}
	race.RaceConfig.Sessions = serverconfig.Sessions{serverconfig.SessionTypeRace: &serverconfig.SessionConfig{Name: "Main Race", Laps: 50}}

	raceWeekend.Sessions = []*RaceWeekendSession{qualifying, race}

	for _, err := range []error{
		store.UpsertCustomRace(customRace),
		store.UpsertCustomRace(unscheduled),
		store.UpsertChampionship(championship),
		store.UpsertRaceWeekend(raceWeekend),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}

	get := func(t *testing.T, target string) []*APIUpcomingRace {
		rec := getUpcomingAPI(handler, target)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		if strings.Contains(rec.Body.String(), "null") {
			t.Errorf("expected no null values in %s", rec.Body.String())
		}

		var out []*APIUpcomingRace

		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}

		return out
	}

	t.Run("sorted and filtered", func(t *testing.T) {
		out := get(t, "/api/upcoming-races")

		if len(out) != 3 {
			t.Fatalf("expected 3 upcoming races, got %d: %+v", len(out), out)
		}

		// championship event in 1h, race weekend session in 2h, custom race in 3h
		kinds := []APIUpcomingKind{APIUpcomingKindChampionshipEvent, APIUpcomingKindRaceWeekendSession, APIUpcomingKindCustomRace}

		for i, kind := range kinds {
			if out[i].Kind != kind {
				t.Errorf("entry %d: expected kind %s, got %s", i, kind, out[i].Kind)
			}
		}

		championshipEvent := out[0]

		if !championshipEvent.Start.Equal(futureEvent.Scheduled) || championshipEvent.ChampionshipID == nil ||
			*championshipEvent.ChampionshipID != championship.ID || championshipEvent.ChampionshipName != "Test Championship" ||
			championshipEvent.Track != "api_test_ring" || championshipEvent.TrackLayout != "gp" || championshipEvent.TrackName == "" ||
			!championshipEvent.OnThisServer || championshipEvent.RaceWeekendID != nil {
			t.Errorf("unexpected championship event: %+v", championshipEvent)
		}

		weekendSession := out[1]

		if !weekendSession.Start.Equal(qualifying.ScheduledTime) || weekendSession.RaceWeekendID == nil ||
			*weekendSession.RaceWeekendID != raceWeekend.ID || weekendSession.RaceWeekendName != "Endurance" ||
			weekendSession.Name != "Qualy" || weekendSession.ChampionshipID != nil ||
			len(weekendSession.Sessions) != 1 || weekendSession.Sessions[0].Time != 20 {
			t.Errorf("unexpected race weekend session: %+v", weekendSession)
		}

		custom := out[2]

		if custom.ID != customRace.UUID || custom.Name != "Sunday Cruise" || !custom.Start.Equal(customRace.Scheduled) ||
			!strings.Contains(custom.Recurrence, "FREQ=WEEKLY") || custom.OnThisServer {
			t.Errorf("unexpected custom race: %+v", custom)
		}

		if len(custom.Cars) != 2 || custom.Cars[0].ID != "dev_car_gt3" || custom.Cars[0].Name == "" {
			t.Errorf("unexpected cars: %+v", custom.Cars)
		}

		if len(custom.Sessions) != 2 || custom.Sessions[0].Time != 10 || custom.Sessions[0].Start == nil ||
			custom.Sessions[1].Laps != 5 || custom.Sessions[1].Start == nil || !custom.Sessions[1].Start.Equal(customRace.Scheduled.Add(10*time.Minute)) {
			t.Errorf("unexpected sessions: %+v", custom.Sessions)
		}
	})

	t.Run("limit", func(t *testing.T) {
		out := get(t, "/api/upcoming-races?limit=2")

		if len(out) != 2 || out[0].Kind != APIUpcomingKindChampionshipEvent || out[1].Kind != APIUpcomingKindRaceWeekendSession {
			t.Errorf("unexpected limited list: %+v", out)
		}

		if out = get(t, "/api/upcoming-races?limit=100"); len(out) != 3 {
			t.Errorf("expected 3 entries, got %d", len(out))
		}
	})

	t.Run("invalid limit", func(t *testing.T) {
		for _, limit := range []string{"0", "-1", "abc"} {
			if rec := getUpcomingAPI(handler, "/api/upcoming-races?limit="+limit); rec.Code != http.StatusBadRequest {
				t.Errorf("limit %q: expected 400, got %d", limit, rec.Code)
			}
		}
	})

	t.Run("recurring event in the past", func(t *testing.T) {
		stale := &CustomRace{UUID: uuid.New(), Name: "Daily"}
		stale.RaceConfig = serverconfig.ConfigIniDefault().CurrentRaceConfig
		stale.Scheduled = now.Add(-36 * time.Hour)
		stale.ScheduledInitial = stale.Scheduled
		stale.ScheduledServerID = serverID

		if err := stale.SetRecurrenceRule("FREQ=DAILY"); err != nil {
			t.Fatal(err)
		}

		if err := store.UpsertCustomRace(stale); err != nil {
			t.Fatal(err)
		}

		out := get(t, "/api/upcoming-races")

		if len(out) != 4 {
			t.Fatalf("expected 4 upcoming races, got %d", len(out))
		}

		var found *APIUpcomingRace

		for _, entry := range out {
			if entry.ID == stale.UUID {
				found = entry
			}
		}

		if found == nil || found.Start.Before(now) || found.Start.After(now.Add(24*time.Hour)) {
			t.Errorf("expected the next recurrence within a day, got %+v", found)
		}
	})
}
