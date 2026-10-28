package servermanager

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// interruptibleServerProcess behaves like AssettoServerProcess when it comes to starting events: starting an event
// while another one is running stops the running one first.
type interruptibleServerProcess struct {
	dummyServerProcess

	event   RaceEvent
	started []RaceEvent
	stopped int
}

func (p *interruptibleServerProcess) Start(event RaceEvent, udpPluginAddress string, udpPluginLocalPort int, forwardingAddress string, forwardListenPort int) error {
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

func (p *interruptibleServerProcess) Event() RaceEvent {
	if p.event == nil {
		return QuickRace{}
	}

	return p.event
}

// setupScheduledEventTest builds a ChampionshipManager on top of a temporary store and fake server install.
func setupScheduledEventTest(t *testing.T, process ServerProcess) (*ChampionshipManager, Store, func()) {
	dir, err := ioutil.TempDir("", "scheduled-events")

	if err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(dir, ServerConfigPath), 0755); err != nil {
		t.Fatal(err)
	}

	oldInstallPath, oldConfig, oldServerID := ServerInstallPath, config, serverID
	ServerInstallPath = dir
	config = &Configuration{}
	serverID = "test-server"

	store := NewJSONStore(filepath.Join(dir, "store"), filepath.Join(dir, "store-shared"))

	cm := NewChampionshipManager(
		NewRaceManager(
			store,
			process,
			NewCarManager(NewTrackManager(), false, false),
			NewTrackManager(),
			&dummyNotificationManager{},
			NewRaceControl(NilBroadcaster{}, nilTrackData{}, process, store, NewPenaltiesManager(store)),
		),
		&ACSRClient{Enabled: false},
	)

	return cm, store, func() {
		ServerInstallPath, config, serverID = oldInstallPath, oldConfig, oldServerID

		_ = os.RemoveAll(dir)
	}
}

func newScheduledTestChampionship(t *testing.T, store Store, scheduled ...time.Time) *Championship {
	championship := NewChampionship("Scheduled Championship")

	for _, scheduledTime := range scheduled {
		event := NewChampionshipEvent()
		event.RaceSetup = ConfigIniDefault().CurrentRaceConfig
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

	running, ok := process.Event().(*ActiveChampionship)

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

	for _, c := range []*Championship{championship, other} {
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
