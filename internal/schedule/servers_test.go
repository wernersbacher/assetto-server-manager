package schedule

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smconfig "github.com/wernersbacher/ACPitwall/internal/config"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

const otherServerID smmodel.ServerID = "other-server"

func TestScheduleServerNamesAndFilter(t *testing.T) {
	handler, store, cleanup := setupUpcomingAPITest(t)
	defer cleanup()

	now := time.Now().UTC().Truncate(time.Second)

	newRace := func(name string, id smmodel.ServerID, start time.Time) *smmodel.CustomRace {
		race := &smmodel.CustomRace{UUID: uuid.New(), Name: name, HasCustomName: true}
		race.RaceConfig = serverconfig.ConfigIniDefault().CurrentRaceConfig
		race.Scheduled = start
		race.ScheduledServerID = id

		if err := store.UpsertCustomRace(race); err != nil {
			t.Fatal(err)
		}

		return race
	}

	newRace("On the default server", serverID, now.Add(time.Hour))
	newRace("On the other server", otherServerID, now.Add(2*time.Hour))
	newRace("On a removed server", "removed-server", now.Add(3*time.Hour))

	servers := []smweb.ServerRef{{ID: serverID, Name: "Main"}, {ID: otherServerID, Name: "Endurance"}}

	manager := NewManager(&smconfig.Configuration{}, store, handler.scheduledRacesManager.env, handler.scheduledRacesManager.raceWeekendEntryList, func() []smweb.ServerRef { return servers })
	handler = NewHandler(&smweb.BaseHandler{}, manager)

	calendar := func(t *testing.T, filter string) []CalendarObject {
		target := "/calendar.json?start=" + now.Add(-time.Hour).Format(time.RFC3339) + "&end=" + now.Add(24*time.Hour).Format(time.RFC3339)

		if filter != "" {
			target += "&" + FilterServerParam + "=" + filter
		}

		rec := httptest.NewRecorder()
		handler.CalendarJSON(rec, httptest.NewRequest(http.MethodGet, target, nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var objects []CalendarObject

		if err := json.Unmarshal(rec.Body.Bytes(), &objects); err != nil {
			t.Fatal(err)
		}

		return objects
	}

	t.Run("names", func(t *testing.T) {
		objects := calendar(t, "")

		names := make(map[string]bool)

		for _, object := range objects {
			names[object.ScheduledServerName] = true
		}

		if !names["Main"] || !names["Endurance"] || !names[""] {
			t.Errorf("events must show the name of their server, unknown servers none: %v", names)
		}
	})

	t.Run("filter", func(t *testing.T) {
		objects := calendar(t, string(otherServerID))

		if len(objects) == 0 {
			t.Fatal("expected the events of the other server")
		}

		for _, object := range objects {
			if object.ScheduledServerID != otherServerID || object.ScheduledServerName != "Endurance" {
				t.Errorf("unexpected event %+v", object)
			}
		}

		if objects := calendar(t, "unknown"); len(objects) != 1 {
			// the "no events" placeholder
			t.Errorf("expected only the placeholder for a server without events, got %d", len(objects))
		}
	})

	t.Run("upcoming", func(t *testing.T) {
		upcoming, err := manager.UpcomingRaces(now, 0, otherServerID, false)

		if err != nil {
			t.Fatal(err)
		}

		if len(upcoming) != 1 || upcoming[0].ServerID != otherServerID || upcoming[0].ServerName != "Endurance" || upcoming[0].OnThisServer {
			t.Errorf("unexpected upcoming races for the other server: %+v", upcoming)
		}

		all, err := manager.UpcomingRaces(now, 0, "", false)

		if err != nil {
			t.Fatal(err)
		}

		if len(all) != 3 {
			t.Errorf("expected the upcoming races of all servers, got %d", len(all))
		}
	})

	t.Run("single server", func(t *testing.T) {
		single := NewManager(&smconfig.Configuration{}, store, manager.env, manager.raceWeekendEntryList, nil)

		upcoming, err := single.UpcomingRaces(now, 0, "", false)

		if err != nil {
			t.Fatal(err)
		}

		for _, race := range upcoming {
			if race.ServerName != "" {
				t.Errorf("a single server setup does not show server names: %+v", race)
			}
		}
	})
}
