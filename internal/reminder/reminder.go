// Package reminder keeps track of the notification reminder timers of scheduled events.
package reminder

import (
	"github.com/wernersbacher/ACPitwall/pkg/when"
)

// Timers holds the notification reminder timers of scheduled events, keyed by event ID. An event has one
// timer per configured reminder (e.g. 120 and 15 minutes before the start), all of which need to be stopped when
// the event is rescheduled.
type Timers map[string][]*when.Timer

// Add registers timer for the event id. A nil timer is ignored.
func (rt Timers) Add(id string, timer *when.Timer) {
	if timer == nil {
		return
	}

	rt[id] = append(rt[id], timer)
}

// Stop stops and forgets all timers of the event id.
func (rt Timers) Stop(id string) {
	for _, timer := range rt[id] {
		timer.Stop()
	}

	delete(rt, id)
}

// StopAll stops all timers of all events.
func (rt Timers) StopAll() {
	for id := range rt {
		rt.Stop(id)
	}
}
