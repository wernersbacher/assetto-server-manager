package servermanager

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/wernersbacher/assetto-server-manager/pkg/when"
	"github.com/google/uuid"
)

// reminderNotificationManager has notification reminders configured.
type reminderNotificationManager struct {
	dummyNotificationManager

	reminders []int
}

func (r *reminderNotificationManager) HasNotificationReminders() bool {
	return len(r.reminders) > 0
}

func (r *reminderNotificationManager) GetNotificationReminders() []int {
	return r.reminders
}

func TestReminderTimers(t *testing.T) {
	rt := make(reminderTimers)

	var fired int32

	for i := 0; i < 2; i++ {
		timer, err := when.When(time.Now().Add(time.Second), func() {
			atomic.AddInt32(&fired, 1)
		})

		if err != nil {
			t.Fatal(err)
		}

		rt.add("event", timer)
	}

	// failed timers (e.g. reminders in the past) are not kept
	rt.add("event", nil)

	if len(rt["event"]) != 2 {
		t.Fatalf("expected 2 reminder timers, got %d", len(rt["event"]))
	}

	rt.stop("event")

	if _, ok := rt["event"]; ok {
		t.Error("expected stopped timers to be removed")
	}

	time.Sleep(2500 * time.Millisecond)

	if n := atomic.LoadInt32(&fired); n != 0 {
		t.Errorf("expected no stopped reminder to fire, %d fired", n)
	}
}

func TestReminderTimers_StopAll(t *testing.T) {
	rt := make(reminderTimers)

	for _, id := range []string{"a", "a", "b"} {
		timer, err := when.When(time.Now().Add(time.Hour), func() {})

		if err != nil {
			t.Fatal(err)
		}

		rt.add(id, timer)
	}

	rt.stopAll()

	if len(rt) != 0 {
		t.Errorf("expected all timers to be removed, %d events left", len(rt))
	}
}

func TestChampionshipManager_ScheduleEventKeepsAllReminders(t *testing.T) {
	cm, store, cleanup := setupScheduledEventTest(t, &interruptibleServerProcess{})
	defer cleanup()

	cm.notificationManager = &reminderNotificationManager{reminders: []int{120, 15}}
	cm.championshipEventStartTimers = make(map[string]*when.Timer)
	cm.championshipEventReminderTimers = make(reminderTimers)

	championship := newScheduledTestChampionship(t, store, time.Time{})
	eventID := championship.Events[0].ID.String()

	defer func() {
		for _, timer := range cm.championshipEventStartTimers {
			timer.Stop()
		}

		cm.championshipEventReminderTimers.stopAll()
	}()

	if err := cm.ScheduleEvent(championship.ID.String(), eventID, time.Now().Add(3*time.Hour), "add", ""); err != nil {
		t.Fatal(err)
	}

	if n := len(cm.championshipEventReminderTimers[eventID]); n != 2 {
		t.Fatalf("expected a reminder timer per configured reminder, got %d", n)
	}

	// rescheduling replaces the reminders instead of adding to them
	if err := cm.ScheduleEvent(championship.ID.String(), eventID, time.Now().Add(4*time.Hour), "add", ""); err != nil {
		t.Fatal(err)
	}

	if n := len(cm.championshipEventReminderTimers[eventID]); n != 2 {
		t.Fatalf("expected 2 reminder timers after rescheduling, got %d", n)
	}

	if err := cm.ScheduleEvent(championship.ID.String(), eventID, time.Time{}, "remove", ""); err != nil {
		t.Fatal(err)
	}

	if n := len(cm.championshipEventReminderTimers[eventID]); n != 0 {
		t.Errorf("expected all reminder timers to be stopped after removing the schedule, got %d", n)
	}
}

func TestChampionshipManager_InitScheduledChampionshipsKeepsAllReminders(t *testing.T) {
	cm, store, cleanup := setupScheduledEventTest(t, &interruptibleServerProcess{})
	defer cleanup()

	// the 300 minute reminder is already in the past
	cm.notificationManager = &reminderNotificationManager{reminders: []int{300, 120, 15}}

	championship := newScheduledTestChampionship(t, store, time.Now().Add(3*time.Hour))

	if err := cm.InitScheduledChampionships(); err != nil {
		t.Fatal(err)
	}

	defer func() {
		for _, timer := range cm.championshipEventStartTimers {
			timer.Stop()
		}

		cm.championshipEventReminderTimers.stopAll()
	}()

	if n := len(cm.championshipEventReminderTimers[championship.Events[0].ID.String()]); n != 2 {
		t.Errorf("expected 2 reminder timers, got %d", n)
	}
}

func TestRaceManager_ScheduleRaceKeepsAllReminders(t *testing.T) {
	cm, store, cleanup := setupScheduledEventTest(t, &interruptibleServerProcess{})
	defer cleanup()

	rm := cm.RaceManager
	rm.notificationManager = &reminderNotificationManager{reminders: []int{120, 15}}

	race := &CustomRace{
		Name:            "Scheduled Race",
		UUID:            uuid.New(),
		RaceConfig:      ConfigIniDefault().CurrentRaceConfig,
		EntryList:       EntryList{},
		ScheduledEvents: make(map[ServerID]*ScheduledEventBase),
	}

	if err := store.UpsertCustomRace(race); err != nil {
		t.Fatal(err)
	}

	id := race.UUID.String()

	defer func() {
		for _, timer := range rm.customRaceStartTimers {
			timer.Stop()
		}

		rm.customRaceReminderTimers.stopAll()
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

func TestRaceWeekendManager_DeScheduleSessionStopsReminders(t *testing.T) {
	cm, store, cleanup := setupScheduledEventTest(t, &interruptibleServerProcess{})
	defer cleanup()

	notificationManager := &reminderNotificationManager{reminders: []int{120, 15}}
	rwm := NewRaceWeekendManager(cm.RaceManager, cm, store, cm.process, notificationManager, cm.carManager)

	raceWeekend := NewRaceWeekend()
	session := NewRaceWeekendSession()
	raceWeekend.AddSession(session, nil)

	if err := store.UpsertRaceWeekend(raceWeekend); err != nil {
		t.Fatal(err)
	}

	defer func() {
		for _, timer := range rwm.scheduledSessionTimers {
			timer.Stop()
		}

		rwm.scheduledSessionReminderTimers.stopAll()
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
