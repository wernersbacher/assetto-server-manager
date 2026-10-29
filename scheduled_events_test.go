package servermanager

import (
	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smcontent "github.com/wernersbacher/assetto-server-manager/internal/content"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	"github.com/wernersbacher/assetto-server-manager/internal/reminder"
	smstore "github.com/wernersbacher/assetto-server-manager/internal/store"

	smconfig "github.com/wernersbacher/assetto-server-manager/internal/config"

	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wernersbacher/assetto-server-manager/pkg/when"
)

// interruptibleServerProcess behaves like AssettoServerProcess when it comes to starting events: starting an event
// while another one is running stops the running one first.
type interruptibleServerProcess struct {
	dummyServerProcess

	event   smmodel.RaceEvent
	started []smmodel.RaceEvent
	stopped int
}

func (p *interruptibleServerProcess) Start(event smmodel.RaceEvent, udpPluginAddress string, udpPluginLocalPort int, forwardingAddress string, forwardListenPort int) error {
	if p.IsRunning() {
		_ = p.Stop()
	}

	p.event = event
	p.started = append(p.started, event)

	return nil
}

func (p *interruptibleServerProcess) Stop() error {
	p.stopped++
	p.event = nil

	return nil
}

func (p *interruptibleServerProcess) IsRunning() bool {
	return p.event != nil
}

func (p *interruptibleServerProcess) Event() smmodel.RaceEvent {
	if p.event == nil {
		return smmodel.QuickRace{}
	}

	return p.event
}

// setupScheduledEventTest builds a ChampionshipManager on top of a temporary store and fake server install.
func setupScheduledEventTest(t *testing.T, process ServerProcess) (*ChampionshipManager, smstore.Store, func()) {
	dir, err := ioutil.TempDir("", "scheduled-events")

	if err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(dir, ServerConfigPath), 0755); err != nil {
		t.Fatal(err)
	}

	oldInstallPath, oldServerID := ServerInstallPath, serverID
	ServerInstallPath = dir
	serverID = "test-server"

	store := smstore.NewJSONStore(filepath.Join(dir, "store"), filepath.Join(dir, "store-shared"))

	cm := NewChampionshipManager(
		NewRaceManager(
			&smconfig.Configuration{},
			store,
			process,
			smcontent.NewCarManager(ServerInstallPath, false, false),
			smcontent.NewTrackManager(ServerInstallPath),
			&dummyNotificationManager{},
			NewRaceControl(&smconfig.Configuration{}, NilBroadcaster{}, nilTrackData{}, process, store, NewPenaltiesManager(store)),
		),
	)

	return cm, store, func() {
		ServerInstallPath, serverID = oldInstallPath, oldServerID

		_ = os.RemoveAll(dir)
	}
}

func newScheduledTestChampionship(t *testing.T, store smstore.Store, scheduled ...time.Time) *smmodel.Championship {
	championship := smmodel.NewChampionship("Scheduled Championship")

	for _, scheduledTime := range scheduled {
		event := smmodel.NewChampionshipEvent()
		event.RaceSetup = serverconfig.ConfigIniDefault().CurrentRaceConfig
		event.Scheduled = scheduledTime
		event.ScheduledServerID = serverID

		championship.Events = append(championship.Events, event)
	}

	if err := store.UpsertChampionship(championship); err != nil {
		t.Fatal(err)
	}

	return championship
}

// A scheduled championship event that is due while a championship practice session is running must stop the practice
// session and start the event.
func TestChampionshipManager_StartScheduledEventInterruptsPractice(t *testing.T) {
	process := &interruptibleServerProcess{}
	cm, store, cleanup := setupScheduledEventTest(t, process)
	defer cleanup()

	championship := newScheduledTestChampionship(t, store, time.Now().Add(time.Hour))
	event := championship.Events[0]

	// a practice session of the very same event is running
	if err := cm.StartPracticeEvent(championship.ID.String(), event.ID.String()); err != nil {
		t.Fatal(err)
	}

	if !process.IsRunning() || !process.Event().IsPractice() {
		t.Fatal("expected a practice session to be running")
	}

	if err := cm.StartScheduledEvent(championship, event); err != nil {
		t.Fatalf("scheduled event could not be started whilst practice was running: %s", err)
	}

	if process.stopped != 1 {
		t.Errorf("expected the practice session to be stopped once, was stopped %d times", process.stopped)
	}

	if len(process.started) != 2 {
		t.Fatalf("expected practice and scheduled event to have been started, got %d starts", len(process.started))
	}

	running, ok := process.Event().(*smmodel.ActiveChampionship)

	if !ok || running.IsPractice() || running.EventID != event.ID || running.ChampionshipID != championship.ID {
		t.Fatalf("expected the scheduled championship event to be running, got: %#v", process.Event())
	}

	if cm.activeChampionship != running {
		t.Error("expected the scheduled event to be the active championship event")
	}

	loaded, loadedEvent, err := cm.GetChampionshipAndEvent(championship.ID.String(), event.ID.String())

	if err != nil || loaded == nil {
		t.Fatal(err)
	}

	if !loadedEvent.Scheduled.IsZero() {
		t.Error("expected the schedule to be cleared once the event has started")
	}
}

// Every scheduled championship event needs a start timer after a server manager restart, not only the first one.
func TestChampionshipManager_InitScheduledChampionships(t *testing.T) {
	cm, store, cleanup := setupScheduledEventTest(t, &interruptibleServerProcess{})
	defer cleanup()

	// the first event was missed whilst the server was offline, the others are in the future
	past := time.Now().Add(-time.Hour)
	first, second := time.Now().Add(time.Hour), time.Now().Add(2*time.Hour)

	championship := newScheduledTestChampionship(t, store, first, second, past)
	other := newScheduledTestChampionship(t, store, first)

	if err := cm.InitScheduledChampionships(); err != nil {
		t.Fatal(err)
	}

	defer func() {
		for _, timer := range cm.championshipEventStartTimers {
			timer.Stop()
		}
	}()

	for _, c := range []*smmodel.Championship{championship, other} {
		for _, event := range c.Events {
			timer, ok := cm.championshipEventStartTimers[event.ID.String()]

			if event.Scheduled.Equal(past) {
				if ok {
					t.Errorf("event %s was scheduled in the past, it must not get a timer", event.ID)
				}

				continue
			}

			if !ok || timer == nil {
				t.Errorf("scheduled event %s (at %s) has no start timer", event.ID, event.Scheduled)
			}
		}
	}

	loaded, err := store.LoadChampionship(championship.ID.String())

	if err != nil {
		t.Fatal(err)
	}

	if !loaded.Events[2].Scheduled.IsZero() {
		t.Error("expected the schedule of the missed event to be cleared")
	}

	if loaded.Events[0].Scheduled.IsZero() || loaded.Events[1].Scheduled.IsZero() {
		t.Error("expected the schedule of future events to be kept")
	}
}

// newRecurringTestChampionship creates a championship with one recurring event that is due at the given time.
func newRecurringTestChampionship(t *testing.T, store smstore.Store, due time.Time) *smmodel.Championship {
	championship := newScheduledTestChampionship(t, store, due)

	if err := championship.Events[0].SetRecurrenceRule("FREQ=DAILY"); err != nil {
		t.Fatal(err)
	}

	if err := store.UpsertChampionship(championship); err != nil {
		t.Fatal(err)
	}

	return championship
}

// The timer captures the championship as it was when the event was scheduled. Changes saved after that (results, name,
// entrants) must survive the recurrence handling when the timer fires.
func TestChampionshipManager_StartScheduledEventRecurrenceKeepsNewerChanges(t *testing.T) {
	process := &interruptibleServerProcess{}
	cm, store, cleanup := setupScheduledEventTest(t, process)
	defer cleanup()

	cm.championshipEventStartTimers = make(map[string]*when.Timer)
	cm.championshipEventReminderTimers = make(reminder.Timers)

	due := time.Now().Add(-time.Minute)
	stale := newRecurringTestChampionship(t, store, due)
	staleEvent := stale.Events[0]

	// a stale copy, as captured by the timer closure
	captured, err := store.LoadChampionship(stale.ID.String())

	if err != nil {
		t.Fatal(err)
	}

	// changes saved after the timer was created
	fresh, err := store.LoadChampionship(stale.ID.String())

	if err != nil {
		t.Fatal(err)
	}

	fresh.Name = "Renamed Since"
	fresh.Events[0].CompletedTime = time.Now()
	fresh.Classes = append(fresh.Classes, smmodel.NewChampionshipClass("Added Since"))

	if err := store.UpsertChampionship(fresh); err != nil {
		t.Fatal(err)
	}

	defer func() {
		for _, timer := range cm.championshipEventStartTimers {
			timer.Stop()
		}
	}()

	if err := cm.StartScheduledEvent(captured, captured.Events[0]); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.LoadChampionship(stale.ID.String())

	if err != nil {
		t.Fatal(err)
	}

	if loaded.Name != "Renamed Since" {
		t.Errorf("name change was overwritten by stale copy, got %q", loaded.Name)
	}

	if len(loaded.Classes) != 1 {
		t.Errorf("class added since was overwritten by stale copy, got %d classes", len(loaded.Classes))
	}

	original, _, err := loaded.EventByID(staleEvent.ID.String())

	if err != nil {
		t.Fatal(err)
	}

	if original.CompletedTime.IsZero() {
		t.Error("completed time saved since was overwritten by stale copy")
	}

	if len(loaded.Events) != 2 {
		t.Fatalf("expected the next recurrence to have been added, got %d events", len(loaded.Events))
	}

	next := loaded.Events[1]

	if !next.Scheduled.After(time.Now()) || next.ID == staleEvent.ID || !next.CompletedTime.IsZero() {
		t.Errorf("unexpected next recurrence: %#v", next)
	}

	if len(process.started) != 1 {
		t.Errorf("expected the due event to be started once, got %d starts", len(process.started))
	}
}

// A timer for an event that no longer exists, whose championship was deleted or whose schedule changed must neither
// panic, resurrect the championship nor start anything.
func TestChampionshipManager_StartScheduledEventOutdated(t *testing.T) {
	process := &interruptibleServerProcess{}
	cm, store, cleanup := setupScheduledEventTest(t, process)
	defer cleanup()

	cm.championshipEventStartTimers = make(map[string]*when.Timer)
	cm.championshipEventReminderTimers = make(reminder.Timers)

	due := time.Now().Add(-time.Minute)

	deleted := newRecurringTestChampionship(t, store, due)

	if err := store.DeleteChampionship(deleted.ID.String()); err != nil {
		t.Fatal(err)
	}

	if err := cm.StartScheduledEvent(deleted, deleted.Events[0]); err != nil {
		t.Errorf("deleted championship: unexpected error: %s", err)
	}

	if c, err := store.LoadChampionship(deleted.ID.String()); err != nil || c.Deleted.IsZero() || len(c.Events) != 1 {
		t.Error("deleted championship was resurrected or modified")
	}

	removed := newRecurringTestChampionship(t, store, due)
	captured, _ := store.LoadChampionship(removed.ID.String())
	removed.Events = nil

	if err := store.UpsertChampionship(removed); err != nil {
		t.Fatal(err)
	}

	if err := cm.StartScheduledEvent(captured, captured.Events[0]); err != nil {
		t.Errorf("removed event: unexpected error: %s", err)
	}

	rescheduled := newRecurringTestChampionship(t, store, due)
	captured, _ = store.LoadChampionship(rescheduled.ID.String())
	rescheduled.Events[0].Scheduled = time.Time{}

	if err := store.UpsertChampionship(rescheduled); err != nil {
		t.Fatal(err)
	}

	if err := cm.StartScheduledEvent(captured, captured.Events[0]); err != nil {
		t.Errorf("descheduled event: unexpected error: %s", err)
	}

	loaded, _ := store.LoadChampionship(rescheduled.ID.String())

	if len(loaded.Events) != 1 {
		t.Errorf("descheduled event must not be recurred, got %d events", len(loaded.Events))
	}

	if len(process.started) != 0 {
		t.Errorf("nothing should have been started, got %d starts", len(process.started))
	}
}
