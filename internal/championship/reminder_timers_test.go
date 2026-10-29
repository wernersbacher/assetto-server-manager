package championship

import (
	"github.com/wernersbacher/assetto-server-manager/internal/notification/notificationtest"
	"github.com/wernersbacher/assetto-server-manager/internal/reminder"

	"testing"
	"time"

	"github.com/wernersbacher/assetto-server-manager/pkg/when"
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
