package servermanager

import (
	"github.com/wernersbacher/assetto-server-manager/internal/reminder"

	smconfig "github.com/wernersbacher/assetto-server-manager/internal/config"

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

func TestChampionshipManager_ScheduleEventKeepsAllReminders(t *testing.T) {
	cm, store, cleanup := setupScheduledEventTest(t, &interruptibleServerProcess{})
	defer cleanup()

	cm.notificationManager = &reminderNotificationManager{reminders: []int{120, 15}}
	cm.championshipEventStartTimers = make(map[string]*when.Timer)
	cm.championshipEventReminderTimers = make(reminder.Timers)

	championship := newScheduledTestChampionship(t, store, time.Time{})
	eventID := championship.Events[0].ID.String()

	defer func() {
		for _, timer := range cm.championshipEventStartTimers {
			timer.Stop()
		}

		cm.championshipEventReminderTimers.StopAll()
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

		cm.championshipEventReminderTimers.StopAll()
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

func TestRaceWeekendManager_DeScheduleSessionStopsReminders(t *testing.T) {
	cm, store, cleanup := setupScheduledEventTest(t, &interruptibleServerProcess{})
	defer cleanup()

	notificationManager := &reminderNotificationManager{reminders: []int{120, 15}}
	rwm := NewRaceWeekendManager(&smconfig.Configuration{}, cm.RaceManager, cm, store, cm.process, notificationManager, cm.carManager)

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
