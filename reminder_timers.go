package servermanager

import (
	"github.com/JustaPenguin/assetto-server-manager/pkg/when"
)

// reminderTimers holds the notification reminder timers of scheduled events, keyed by event ID. An event has one
// timer per configured reminder (e.g. 120 and 15 minutes before the start), all of which need to be stopped when
// the event is rescheduled.
type reminderTimers map[string][]*when.Timer

func (rt reminderTimers) add(id string, timer *when.Timer) {
	if timer == nil {
		return
	}

	rt[id] = append(rt[id], timer)
}

func (rt reminderTimers) stop(id string) {
	for _, timer := range rt[id] {
		timer.Stop()
	}

	delete(rt, id)
}

func (rt reminderTimers) stopAll() {
	for id := range rt {
		rt.stop(id)
	}
}
