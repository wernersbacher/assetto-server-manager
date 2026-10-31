package reminder

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/wernersbacher/ACPitwall/pkg/when"
)

func TestReminderTimers(t *testing.T) {
	rt := make(Timers)

	var fired int32

	for i := 0; i < 2; i++ {
		timer, err := when.When(time.Now().Add(time.Second), func() {
			atomic.AddInt32(&fired, 1)
		})

		if err != nil {
			t.Fatal(err)
		}

		rt.Add("event", timer)
	}

	// failed timers (e.g. reminders in the past) are not kept
	rt.Add("event", nil)

	if len(rt["event"]) != 2 {
		t.Fatalf("expected 2 reminder timers, got %d", len(rt["event"]))
	}

	rt.Stop("event")

	if _, ok := rt["event"]; ok {
		t.Error("expected stopped timers to be removed")
	}

	time.Sleep(2500 * time.Millisecond)

	if n := atomic.LoadInt32(&fired); n != 0 {
		t.Errorf("expected no stopped reminder to fire, %d fired", n)
	}
}

func TestReminderTimers_StopAll(t *testing.T) {
	rt := make(Timers)

	for _, id := range []string{"a", "a", "b"} {
		timer, err := when.When(time.Now().Add(time.Hour), func() {})

		if err != nil {
			t.Fatal(err)
		}

		rt.Add(id, timer)
	}

	rt.StopAll()

	if len(rt) != 0 {
		t.Errorf("expected all timers to be removed, %d events left", len(rt))
	}
}
