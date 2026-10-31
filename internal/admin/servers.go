package admin

import (
	"net/http"

	"github.com/wernersbacher/ACPitwall/internal/api"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

// NamedSlot is a server slot together with how templates refer to it.
type NamedSlot struct {
	smweb.ServerRef
	*Slot
}

// SlotsFunc returns all server slots. The resolver provides it, as the handlers cannot know the slots.
type SlotsFunc func() []NamedSlot

// ServerSummary is what the overview and the servers API show about one server slot.
type ServerSummary struct {
	ID   string
	Name string

	// Running is false if no event is running. The event fields are then empty.
	Running bool

	// EventType is the kind of event ("Championship", "Race Weekend", "Looping event", "Time attack" or "Running"),
	// EventName its name, e.g. the track.
	EventType string `json:",omitempty"`
	EventName string `json:",omitempty"`

	IsChampionship bool
	IsRaceWeekend  bool
	IsPractice     bool

	DriversOnline int
}

// NewServerSummary summarises the state of a slot's server process and race control.
func NewServerSummary(slot NamedSlot) *ServerSummary {
	out := &ServerSummary{
		ID:      string(slot.ID),
		Name:    slot.Name,
		Running: slot.Process.IsRunning(),
	}

	if !out.Running {
		return out
	}

	event := slot.Process.Event()

	out.EventType = eventType(event.IsChampionship(), event.IsRaceWeekend(), event.IsPractice(), event.IsTimeAttack(), event.IsLooping(slot.Env.ServerID()))
	out.EventName = event.EventName(slot.Env.TrackSummary)
	out.IsChampionship = event.IsChampionship()
	out.IsRaceWeekend = event.IsRaceWeekend()
	out.IsPractice = event.IsPractice()

	if slot.RaceControl != nil {
		out.DriversOnline = slot.RaceControl.ConnectedDrivers.Len()
	}

	return out
}

// eventType mirrors the template "ServerEventType" of the base layout.
func eventType(championship, raceWeekend, practice, timeAttack, looping bool) string {
	suffix := ""

	if practice {
		suffix = " practice"
	}

	switch {
	case championship:
		return "Championship" + suffix
	case raceWeekend:
		return "Race Weekend" + suffix
	case looping:
		return "Looping event"
	case timeAttack:
		return "Time attack"
	default:
		return "Running"
	}
}

func (sah *ServerAdministrationHandler) serverSummaries() []*ServerSummary {
	slots := sah.slots()

	summaries := make([]*ServerSummary, 0, len(slots))

	for _, slot := range slots {
		summaries = append(summaries, NewServerSummary(slot))
	}

	return summaries
}

func (sah *ServerAdministrationHandler) slots() []NamedSlot {
	if sah.slotsFunc == nil {
		return nil
	}

	return sah.slotsFunc()
}

// ServersAPI lists all servers with their running event and the number of drivers online, as JSON. It does not
// depend on the server the request selects. For details of a single server see /api/server-status?server=<id>.
func (sah *ServerAdministrationHandler) ServersAPI(w http.ResponseWriter, r *http.Request) {
	api.WriteJSON(w, sah.serverSummaries())
}
