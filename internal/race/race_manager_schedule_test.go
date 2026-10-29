package race

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smconfig "github.com/wernersbacher/assetto-server-manager/internal/config"
	smcontent "github.com/wernersbacher/assetto-server-manager/internal/content"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	"github.com/wernersbacher/assetto-server-manager/internal/notification/notificationtest"
	"github.com/wernersbacher/assetto-server-manager/internal/racecontrol"
	"github.com/wernersbacher/assetto-server-manager/internal/server/servertest"
	"github.com/wernersbacher/assetto-server-manager/internal/serverenv"
	smstore "github.com/wernersbacher/assetto-server-manager/internal/store"
)

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

func testEnv(dir string) *serverenv.Env {
	return &serverenv.Env{
		ServerID:        func() smmodel.ServerID { return "test-server" },
		InstallPath:     func() string { return dir },
		FallBackSorting: func() bool { return false },
		PanicCapture:    func(fn func()) { fn() },
	}
}

// setupTestRaceManager builds a RaceManager on top of a temporary store and fake server install.
func setupTestRaceManager(t *testing.T) (*RaceManager, smstore.Store, func()) {
	dir, err := ioutil.TempDir("", "race-manager")

	if err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(dir, serverenv.ConfigPath), 0755); err != nil {
		t.Fatal(err)
	}

	env := testEnv(dir)
	config := &smconfig.Configuration{}
	store := smstore.NewJSONStore(filepath.Join(dir, "store"), filepath.Join(dir, "store-shared"))
	process := servertest.Dummy{}

	raceControl := racecontrol.NewRaceControl(config, racecontrol.NilBroadcaster{}, racecontrol.NilTrackData{}, process, store, racecontrol.NewPenaltiesManager(store, env), env)

	rm := NewRaceManager(
		config,
		store,
		process,
		smcontent.NewCarManager(dir, false, false),
		smcontent.NewTrackManager(dir),
		&reminderNotificationManager{reminders: []int{120, 15}},
		raceControl,
		env,
	)

	return rm, store, func() {
		_ = os.RemoveAll(dir)
	}
}

func TestRaceManager_ScheduleRaceKeepsAllReminders(t *testing.T) {
	rm, store, cleanup := setupTestRaceManager(t)
	defer cleanup()

	race := &smmodel.CustomRace{
		Name:            "Scheduled Race",
		UUID:            uuid.New(),
		RaceConfig:      serverconfig.ConfigIniDefault().CurrentRaceConfig,
		EntryList:       serverconfig.EntryList{},
		ScheduledEvents: make(map[smmodel.ServerID]*smmodel.ScheduledEventBase),
	}

	if err := store.UpsertCustomRace(race); err != nil {
		t.Fatal(err)
	}

	id := race.UUID.String()

	defer func() {
		for _, timer := range rm.customRaceStartTimers {
			timer.Stop()
		}

		rm.customRaceReminderTimers.StopAll()
	}()

	if err := rm.ScheduleRace(id, time.Now().Add(3*time.Hour), "add", ""); err != nil {
		t.Fatal(err)
	}

	if n := len(rm.customRaceReminderTimers[id]); n != 2 {
		t.Fatalf("expected a reminder timer per configured reminder, got %d", n)
	}

	if err := rm.ScheduleRace(id, time.Time{}, "remove", ""); err != nil {
		t.Fatal(err)
	}

	if n := len(rm.customRaceReminderTimers[id]); n != 0 {
		t.Errorf("expected all reminder timers to be stopped after removing the schedule, got %d", n)
	}
}
