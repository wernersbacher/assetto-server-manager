package servermanager

import (
	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smconfig "github.com/wernersbacher/assetto-server-manager/internal/config"

	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/go-chi/chi"
	"github.com/google/uuid"
)

func testChampionshipRaceSettings() *ChampionshipRaceSettings {
	return &ChampionshipRaceSettings{
		FuelRate:                150,
		DamageMultiplier:        25,
		TyreWearRate:            200,
		AllowedTyresOut:         3,
		ABSAllowed:              2,
		TractionControlAllowed:  1,
		StabilityControlAllowed: 1,
		AutoClutchAllowed:       1,
		TyreBlanketsAllowed:     1,
		ForceVirtualMirror:      1,
		DynamicTrack: serverconfig.DynamicTrackConfig{
			SessionStart:    95,
			Randomness:      3,
			SessionTransfer: 50,
			LapGain:         10,
		},
	}
}

// testEventSetup returns a config with values that differ from testChampionshipRaceSettings everywhere.
func testEventSetup() serverconfig.CurrentRaceConfig {
	setup := serverconfig.ConfigIniDefault().CurrentRaceConfig

	setup.Track = "event_track"
	setup.TrackLayout = "event_layout"
	setup.Cars = "event_car"
	setup.MaxClients = 7
	setup.SunAngle = 42
	setup.FuelRate = 1
	setup.DamageMultiplier = 2
	setup.TyreWearRate = 3
	setup.AllowedTyresOut = -1
	setup.ABSAllowed = 0
	setup.TractionControlAllowed = 0
	setup.StabilityControlAllowed = 0
	setup.AutoClutchAllowed = 0
	setup.TyreBlanketsAllowed = 0
	setup.ForceVirtualMirror = 0
	setup.DynamicTrack = serverconfig.DynamicTrackConfig{SessionStart: 1, Randomness: 2, SessionTransfer: 3, LapGain: 4}
	setup.Sessions = serverconfig.Sessions{
		serverconfig.SessionTypeRace: &serverconfig.SessionConfig{Name: "Race", Laps: 5},
	}

	return setup
}

func assertChampionshipSettingsApplied(t *testing.T, got serverconfig.CurrentRaceConfig, settings *ChampionshipRaceSettings) {
	t.Helper()

	if !reflect.DeepEqual(NewChampionshipRaceSettingsFromConfig(got), settings) {
		t.Errorf("expected championship race settings to be applied, got %+v want %+v", NewChampionshipRaceSettingsFromConfig(got), settings)
	}
}

func assertEventSpecificFieldsKept(t *testing.T, got, want serverconfig.CurrentRaceConfig) {
	t.Helper()

	if got.Track != want.Track || got.TrackLayout != want.TrackLayout || got.Cars != want.Cars ||
		got.MaxClients != want.MaxClients || got.SunAngle != want.SunAngle ||
		!reflect.DeepEqual(got.Sessions, want.Sessions) || !reflect.DeepEqual(got.Weather, want.Weather) {
		t.Errorf("event specific fields were changed: got %+v want %+v", got, want)
	}
}

func TestChampionship_EffectiveRaceSetup(t *testing.T) {
	raw := testEventSetup()
	settings := testChampionshipRaceSettings()

	t.Run("nil settings", func(t *testing.T) {
		championship := NewChampionship("c")
		event := NewChampionshipEvent()
		event.RaceSetup = raw
		event.UseChampionshipRaceSettings = true // has no effect without championship settings

		if got := championship.EffectiveRaceSetup(event); !reflect.DeepEqual(got, raw) {
			t.Errorf("expected unchanged setup, got %+v", got)
		}
	})

	t.Run("event does not use settings", func(t *testing.T) {
		championship := NewChampionship("c")
		championship.RaceSettings = settings
		event := NewChampionshipEvent()
		event.RaceSetup = raw

		if got := championship.EffectiveRaceSetup(event); !reflect.DeepEqual(got, raw) {
			t.Errorf("expected unchanged setup, got %+v", got)
		}
	})

	t.Run("event uses settings", func(t *testing.T) {
		championship := NewChampionship("c")
		championship.RaceSettings = settings
		event := NewChampionshipEvent()
		event.RaceSetup = raw
		event.UseChampionshipRaceSettings = true

		got := championship.EffectiveRaceSetup(event)

		assertChampionshipSettingsApplied(t, got, settings)
		assertEventSpecificFieldsKept(t, got, raw)

		if !reflect.DeepEqual(event.RaceSetup, raw) {
			t.Error("the stored event setup must not be modified")
		}
	})

	t.Run("event GetRaceSetup", func(t *testing.T) {
		championship := NewChampionship("c")
		championship.RaceSettings = settings
		event := NewChampionshipEvent()
		event.RaceSetup = raw
		event.UseChampionshipRaceSettings = true

		if !reflect.DeepEqual(event.GetRaceSetup(), raw) {
			t.Error("without a championship the raw setup is returned")
		}

		event.championship = championship

		assertChampionshipSettingsApplied(t, event.GetRaceSetup(), settings)
	})
}

func TestChampionshipRaceSettings_ZeroValueMeansUnchanged(t *testing.T) {
	// existing stored championships have no settings and events no flag: nothing may change for them.
	championship := NewChampionship("c")
	event := NewChampionshipEvent()
	event.RaceSetup = testEventSetup()

	if championship.HasRaceSettings() || event.UseChampionshipRaceSettings {
		t.Fatal("new championships and events must not use race settings")
	}

	var settings *ChampionshipRaceSettings

	if got := settings.ApplyTo(event.RaceSetup); !reflect.DeepEqual(got, event.RaceSetup) {
		t.Error("nil settings must not change a config")
	}
}

func TestChampionshipRaceSettings_StoreRoundTrip(t *testing.T) {
	_, store, cleanup := setupScheduledEventTest(t, &interruptibleServerProcess{})
	defer cleanup()

	championship := NewChampionship("Round Trip")
	championship.RaceSettings = testChampionshipRaceSettings()

	event := NewChampionshipEvent()
	event.RaceSetup = testEventSetup()
	event.UseChampionshipRaceSettings = true
	championship.Events = append(championship.Events, event)

	if err := store.UpsertChampionship(championship); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.LoadChampionship(championship.ID.String())

	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(loaded.RaceSettings, championship.RaceSettings) {
		t.Errorf("race settings were not persisted: %+v", loaded.RaceSettings)
	}

	if !loaded.Events[0].UseChampionshipRaceSettings {
		t.Error("event flag was not persisted")
	}
}

func TestParseChampionshipRaceSettings(t *testing.T) {
	newRequest := func(values url.Values) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/championships/new/submit", strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}

		return r
	}

	if ParseChampionshipRaceSettings(newRequest(url.Values{"ChampionshipRaceSettings.FuelRate": {"100"}})) != nil {
		t.Error("settings must be nil if the section is not enabled")
	}

	got := ParseChampionshipRaceSettings(newRequest(url.Values{
		"ChampionshipRaceSettings.Enabled":                 {"1"},
		"ChampionshipRaceSettings.FuelRate":                {"150"},
		"ChampionshipRaceSettings.DamageMultiplier":        {"25"},
		"ChampionshipRaceSettings.TyreWearRate":            {"200"},
		"ChampionshipRaceSettings.AllowedTyresOut":         {"3"},
		"ChampionshipRaceSettings.ABSAllowed":              {"2"},
		"ChampionshipRaceSettings.TractionControlAllowed":  {"1"},
		"ChampionshipRaceSettings.StabilityControlAllowed": {"on"},
		"ChampionshipRaceSettings.AutoClutchAllowed":       {"on"},
		"ChampionshipRaceSettings.TyreBlanketsAllowed":     {"on"},
		"ChampionshipRaceSettings.ForceVirtualMirror":      {"on"},
		"ChampionshipRaceSettings.SessionStart":            {"95"},
		"ChampionshipRaceSettings.Randomness":              {"3"},
		"ChampionshipRaceSettings.SessionTransfer":         {"50"},
		"ChampionshipRaceSettings.LapGain":                 {"10"},
	}))

	if !reflect.DeepEqual(got, testChampionshipRaceSettings()) {
		t.Errorf("unexpected settings: %+v", got)
	}

	// unchecked checkboxes are not submitted
	got = ParseChampionshipRaceSettings(newRequest(url.Values{"ChampionshipRaceSettings.Enabled": {"1"}}))

	if got == nil || !reflect.DeepEqual(*got, ChampionshipRaceSettings{}) {
		t.Errorf("expected enabled, empty settings, got %+v", got)
	}
}

func TestChampionship_RaceSettingsForForm(t *testing.T) {
	championship := NewChampionship("c")

	if got := championship.RaceSettingsForForm(); !reflect.DeepEqual(got, *NewChampionshipRaceSettingsFromConfig(serverconfig.ConfigIniDefault().CurrentRaceConfig)) {
		t.Errorf("expected defaults, got %+v", got)
	}

	event := NewChampionshipEvent()
	event.RaceSetup = testEventSetup()
	championship.Events = append(championship.Events, event)

	if got := championship.RaceSettingsForForm(); !reflect.DeepEqual(got, *NewChampionshipRaceSettingsFromConfig(event.RaceSetup)) {
		t.Errorf("expected values of the latest event, got %+v", got)
	}

	championship.RaceSettings = testChampionshipRaceSettings()

	if got := championship.RaceSettingsForForm(); !reflect.DeepEqual(got, *championship.RaceSettings) {
		t.Errorf("expected the saved settings, got %+v", got)
	}
}

func newRaceSettingsTestChampionship(t *testing.T, store Store, useSettings bool) *Championship {
	championship := NewChampionship("Race Settings")
	championship.RaceSettings = testChampionshipRaceSettings()

	event := NewChampionshipEvent()
	event.RaceSetup = testEventSetup()
	event.UseChampionshipRaceSettings = useSettings

	championship.Events = append(championship.Events, event)

	if err := store.UpsertChampionship(championship); err != nil {
		t.Fatal(err)
	}

	return championship
}

func TestChampionshipManager_StartEventUsesEffectiveRaceSetup(t *testing.T) {
	for _, practice := range []bool{false, true} {
		for _, useSettings := range []bool{false, true} {
			process := &interruptibleServerProcess{}
			cm, store, cleanup := setupScheduledEventTest(t, process)

			championship := newRaceSettingsTestChampionship(t, store, useSettings)
			event := championship.Events[0]
			raw := event.RaceSetup

			if err := cm.StartEvent(championship.ID.String(), event.ID.String(), practice); err != nil {
				cleanup()
				t.Fatal(err)
			}

			if len(process.started) != 1 {
				cleanup()
				t.Fatalf("expected one started event, got %d", len(process.started))
			}

			started := process.started[0].GetRaceConfig()

			if useSettings {
				assertChampionshipSettingsApplied(t, started, championship.RaceSettings)
			} else {
				assertChampionshipSettingsApplied(t, started, NewChampionshipRaceSettingsFromConfig(raw))
			}

			if started.Track != raw.Track || started.TrackLayout != raw.TrackLayout {
				t.Errorf("practice=%t use=%t: track must come from the event, got %s/%s", practice, useSettings, started.Track, started.TrackLayout)
			}

			if race := started.Sessions[serverconfig.SessionTypeRace]; !practice && (race == nil || race.Laps != 5) {
				t.Errorf("sessions must come from the event, got %+v", started.Sessions)
			}

			// the stored event keeps its own values
			loaded, loadedEvent, err := cm.GetChampionshipAndEvent(championship.ID.String(), event.ID.String())

			if err != nil {
				cleanup()
				t.Fatal(err)
			}

			if !reflect.DeepEqual(loadedEvent.RaceSetup, raw) {
				t.Errorf("practice=%t use=%t: stored event setup was modified: %+v", practice, useSettings, loadedEvent.RaceSetup)
			}

			if loadedEvent.UseChampionshipRaceSettings != useSettings || !reflect.DeepEqual(loaded.RaceSettings, championship.RaceSettings) {
				t.Error("stored flag or championship settings were modified")
			}

			cleanup()
		}
	}
}

func TestChampionshipManager_ChangingSettingsAffectsEvents(t *testing.T) {
	process := &interruptibleServerProcess{}
	cm, store, cleanup := setupScheduledEventTest(t, process)
	defer cleanup()

	championship := newRaceSettingsTestChampionship(t, store, true)
	event := championship.Events[0]

	championship.RaceSettings.FuelRate = 333

	if err := store.UpsertChampionship(championship); err != nil {
		t.Fatal(err)
	}

	if err := cm.StartEvent(championship.ID.String(), event.ID.String(), false); err != nil {
		t.Fatal(err)
	}

	if got := process.started[0].GetRaceConfig().FuelRate; got != 333 {
		t.Errorf("expected the changed championship fuel rate, got %d", got)
	}
}

func TestChampionshipManager_SaveChampionshipEventSavesFlag(t *testing.T) {
	cm, store, cleanup := setupScheduledEventTest(t, &interruptibleServerProcess{})
	defer cleanup()

	if err := os.MkdirAll(filepath.Join(ServerInstallPath, "content", "cars"), 0755); err != nil {
		t.Fatal(err)
	}

	championship := NewChampionship("Flag")
	championship.RaceSettings = testChampionshipRaceSettings()

	if err := store.UpsertChampionship(championship); err != nil {
		t.Fatal(err)
	}

	submit := func(values url.Values) *ChampionshipEvent {
		values.Set("Track", "some_track")
		values.Set("Race.Enabled", "1")
		values.Set("Race.Laps", "3")
		values.Set("FuelRate", "10")

		r := httptest.NewRequest(http.MethodPost, "/championship/x/event/submit", strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("championshipID", championship.ID.String())
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

		_, event, _, err := cm.SaveChampionshipEvent(r)

		if err != nil {
			t.Fatal(err)
		}

		return event
	}

	event := submit(url.Values{"UseChampionshipRaceSettings": {"1"}})

	if !event.UseChampionshipRaceSettings {
		t.Error("expected the flag to be saved for a new event")
	}

	if event.RaceSetup.FuelRate != 10 {
		t.Errorf("the event keeps its own values, got fuel rate %d", event.RaceSetup.FuelRate)
	}

	loaded, err := store.LoadChampionship(championship.ID.String())

	if err != nil || !loaded.Events[0].UseChampionshipRaceSettings {
		t.Fatalf("flag was not persisted: %v", err)
	}

	// editing the event with the checkbox unchecked turns the flag off
	event = submit(url.Values{"Editing": {event.ID.String()}})

	if event.UseChampionshipRaceSettings {
		t.Error("expected the flag to be turned off when editing")
	}

	loaded, err = store.LoadChampionship(championship.ID.String())

	if err != nil || len(loaded.Events) != 1 || loaded.Events[0].UseChampionshipRaceSettings {
		t.Fatalf("flag change was not persisted: %v", err)
	}
}

func TestChampionship_EffectiveRaceWeekendSessionSetup(t *testing.T) {
	raw := testEventSetup()
	raceWeekendID := uuid.New()

	championship := NewChampionship("c")
	championship.RaceSettings = testChampionshipRaceSettings()

	event := NewChampionshipEvent()
	event.RaceWeekendID = raceWeekendID
	championship.Events = append(championship.Events, event)

	if got := championship.EffectiveRaceWeekendSessionSetup(raceWeekendID, raw); !reflect.DeepEqual(got, raw) {
		t.Error("race weekend events which do not use the settings must not change")
	}

	event.UseChampionshipRaceSettings = true

	got := championship.EffectiveRaceWeekendSessionSetup(raceWeekendID, raw)

	assertChampionshipSettingsApplied(t, got, championship.RaceSettings)
	assertEventSpecificFieldsKept(t, got, raw)

	if got := championship.EffectiveRaceWeekendSessionSetup(uuid.New(), raw); !reflect.DeepEqual(got, raw) {
		t.Error("other race weekends must not change")
	}

	var none *Championship

	if got := none.EffectiveRaceWeekendSessionSetup(raceWeekendID, raw); !reflect.DeepEqual(got, raw) {
		t.Error("no championship must not change the config")
	}
}

func TestRaceWeekendManager_StartSessionUsesEffectiveRaceSetup(t *testing.T) {
	process := &interruptibleServerProcess{}
	cm, store, cleanup := setupScheduledEventTest(t, process)
	defer cleanup()

	rwm := NewRaceWeekendManager(&smconfig.Configuration{}, cm.RaceManager, cm, store, process, &dummyNotificationManager{}, NewCarManager(NewTrackManager(), false, false))

	for _, useSettings := range []bool{false, true} {
		process.event = nil
		process.started = nil

		championship := NewChampionship("Race Weekend Championship")
		championship.RaceSettings = testChampionshipRaceSettings()

		class := NewChampionshipClass("Class")
		class.AvailableCars = []string{"event_car"}
		class.Entrants = serverconfig.EntryList{"CAR_0": &serverconfig.Entrant{Name: "Driver", GUID: "123", Model: "event_car"}}
		championship.Classes = append(championship.Classes, class)

		raceWeekend := NewRaceWeekend()
		raceWeekend.ChampionshipID = championship.ID

		session := NewRaceWeekendSession()
		session.RaceConfig = testEventSetup()
		session.ParentIDs = []uuid.UUID{raceWeekend.ID}
		raceWeekend.AddSession(session, nil)

		event := NewChampionshipEvent()
		event.RaceWeekendID = raceWeekend.ID
		event.UseChampionshipRaceSettings = useSettings
		championship.Events = append(championship.Events, event)

		if err := store.UpsertChampionship(championship); err != nil {
			t.Fatal(err)
		}

		if err := store.UpsertRaceWeekend(raceWeekend); err != nil {
			t.Fatal(err)
		}

		if err := rwm.StartSession(raceWeekend.ID.String(), session.ID.String(), false); err != nil {
			t.Fatal(err)
		}

		if len(process.started) != 1 {
			t.Fatalf("expected one started session, got %d", len(process.started))
		}

		started := process.started[0].GetRaceConfig()

		if useSettings {
			assertChampionshipSettingsApplied(t, started, championship.RaceSettings)
		} else {
			assertChampionshipSettingsApplied(t, started, NewChampionshipRaceSettingsFromConfig(testEventSetup()))
		}

		if started.Track != "event_track" {
			t.Errorf("track must come from the session, got %s", started.Track)
		}

		loaded, err := store.LoadRaceWeekend(raceWeekend.ID.String())

		if err != nil {
			t.Fatal(err)
		}

		stored := loaded.Sessions[0].RaceConfig

		assertChampionshipSettingsApplied(t, stored, NewChampionshipRaceSettingsFromConfig(testEventSetup()))
	}
}
