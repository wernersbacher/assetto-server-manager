package raceweekend

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smchampionship "github.com/wernersbacher/assetto-server-manager/internal/championship"
	smconfig "github.com/wernersbacher/assetto-server-manager/internal/config"
	smcontent "github.com/wernersbacher/assetto-server-manager/internal/content"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	"github.com/wernersbacher/assetto-server-manager/internal/notification/notificationtest"
	smrace "github.com/wernersbacher/assetto-server-manager/internal/race"
	racecontrol "github.com/wernersbacher/assetto-server-manager/internal/racecontrol"
	smserver "github.com/wernersbacher/assetto-server-manager/internal/server"
	"github.com/wernersbacher/assetto-server-manager/internal/server/servertest"
	"github.com/wernersbacher/assetto-server-manager/internal/serverenv"
	"github.com/wernersbacher/assetto-server-manager/internal/serverenv/envtest"
	smstore "github.com/wernersbacher/assetto-server-manager/internal/store"
)

// The tests of the race weekend manager need a championship manager. They are temporarily duplicating the test
// helpers of the championship package until the race weekend manager is extracted as well.

// reminderNotificationManager has notification reminders configured.
type reminderNotificationManager struct {
	notificationtest.Dummy

	reminders []int
}

func (r *reminderNotificationManager) HasNotificationReminders() bool {
	return len(r.reminders) > 0
}

func (r *reminderNotificationManager) GetNotificationReminders() []int {
	return r.reminders
}

// setupChampionshipManagerTest builds a ChampionshipManager on top of a temporary store and fake server install.
func setupChampionshipManagerTest(t *testing.T, process smserver.ServerProcess) (*smchampionship.ChampionshipManager, smstore.Store, *serverenv.Env, func()) {
	dir, err := ioutil.TempDir("", "race-weekend-manager")

	if err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(dir, serverenv.ConfigPath), 0755); err != nil {
		t.Fatal(err)
	}

	env := testEnv(dir)

	store := smstore.NewJSONStore(filepath.Join(dir, "store"), filepath.Join(dir, "store-shared"))

	config := &smconfig.Configuration{}
	notifications := &notificationtest.Dummy{}

	cm := smchampionship.NewChampionshipManager(
		smrace.NewRaceManager(
			config,
			store,
			process,
			smcontent.NewCarManager(dir, false, false),
			smcontent.NewTrackManager(dir),
			notifications,
			racecontrol.NewRaceControl(config, racecontrol.NilBroadcaster{}, racecontrol.NilTrackData{}, process, store, racecontrol.NewPenaltiesManager(store, env), env),
			env,
		),
		config,
		store,
		process,
		env,
		notifications,
	)

	return cm, store, env, func() {
		_ = os.RemoveAll(dir)
	}
}

func testRaceWeekendEventSetup() serverconfig.CurrentRaceConfig {
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

func testRaceWeekendChampionshipSettings() *smmodel.ChampionshipRaceSettings {
	return &smmodel.ChampionshipRaceSettings{
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

func assertRaceWeekendSettingsApplied(t *testing.T, got serverconfig.CurrentRaceConfig, settings *smmodel.ChampionshipRaceSettings) {
	t.Helper()

	if !reflect.DeepEqual(smmodel.NewChampionshipRaceSettingsFromConfig(got), settings) {
		t.Errorf("expected championship race settings to be applied, got %+v want %+v", smmodel.NewChampionshipRaceSettingsFromConfig(got), settings)
	}
}

func TestRaceWeekendManager_StartSessionUsesEffectiveRaceSetup(t *testing.T) {
	process := &servertest.Interruptible{}
	cm, store, env, cleanup := setupChampionshipManagerTest(t, process)
	defer cleanup()

	rwm := NewRaceWeekendManager(&smconfig.Configuration{}, cm.RaceManager, cm, store, process, &notificationtest.Dummy{}, smcontent.NewCarManager(env.InstallPath(), false, false), env)

	for _, useSettings := range []bool{false, true} {
		process.ClearEvent()
		process.Started = nil

		championship := smmodel.NewChampionship("Race Weekend Championship")
		championship.RaceSettings = testRaceWeekendChampionshipSettings()

		class := smmodel.NewChampionshipClass("Class")
		class.AvailableCars = []string{"event_car"}
		class.Entrants = serverconfig.EntryList{"CAR_0": &serverconfig.Entrant{Name: "Driver", GUID: "123", Model: "event_car"}}
		championship.Classes = append(championship.Classes, class)

		raceWeekend := smmodel.NewRaceWeekend()
		raceWeekend.ChampionshipID = championship.ID

		session := smmodel.NewRaceWeekendSession()
		session.RaceConfig = testRaceWeekendEventSetup()
		session.ParentIDs = []uuid.UUID{raceWeekend.ID}
		raceWeekend.AddSession(session, nil)

		event := smmodel.NewChampionshipEvent()
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

		if len(process.Started) != 1 {
			t.Fatalf("expected one started session, got %d", len(process.Started))
		}

		started := process.Started[0].GetRaceConfig()

		if useSettings {
			assertRaceWeekendSettingsApplied(t, started, championship.RaceSettings)
		} else {
			assertRaceWeekendSettingsApplied(t, started, smmodel.NewChampionshipRaceSettingsFromConfig(testRaceWeekendEventSetup()))
		}

		if started.Track != "event_track" {
			t.Errorf("track must come from the session, got %s", started.Track)
		}

		loaded, err := store.LoadRaceWeekend(raceWeekend.ID.String())

		if err != nil {
			t.Fatal(err)
		}

		stored := loaded.Sessions[0].RaceConfig

		assertRaceWeekendSettingsApplied(t, stored, smmodel.NewChampionshipRaceSettingsFromConfig(testRaceWeekendEventSetup()))
	}
}

func TestRaceWeekendManager_DeScheduleSessionStopsReminders(t *testing.T) {
	process := &servertest.Interruptible{}
	cm, store, env, cleanup := setupChampionshipManagerTest(t, process)
	defer cleanup()

	notificationManager := &reminderNotificationManager{reminders: []int{120, 15}}
	rwm := NewRaceWeekendManager(&smconfig.Configuration{}, cm.RaceManager, cm, store, process, notificationManager, smcontent.NewCarManager(env.InstallPath(), false, false), env)

	raceWeekend := smmodel.NewRaceWeekend()
	session := smmodel.NewRaceWeekendSession()
	raceWeekend.AddSession(session, nil)

	if err := store.UpsertRaceWeekend(raceWeekend); err != nil {
		t.Fatal(err)
	}

	defer func() {
		for _, timer := range rwm.scheduledSessionTimers {
			timer.Stop()
		}

		rwm.scheduledSessionReminderTimers.StopAll()
	}()

	if err := rwm.ScheduleSession(raceWeekend.ID.String(), session.ID.String(), time.Now().Add(3*time.Hour), false); err != nil {
		t.Fatal(err)
	}

	if n := len(rwm.scheduledSessionReminderTimers[session.ID.String()]); n != 2 {
		t.Fatalf("expected a reminder timer per configured reminder, got %d", n)
	}

	if err := rwm.DeScheduleSession(raceWeekend.ID.String(), session.ID.String()); err != nil {
		t.Fatal(err)
	}

	if n := len(rwm.scheduledSessionReminderTimers[session.ID.String()]); n != 0 {
		t.Errorf("expected all reminder timers to be stopped after removing the schedule, got %d", n)
	}
}

func testEnv(installPath string) *serverenv.Env {
	return envtest.New(installPath)
}
