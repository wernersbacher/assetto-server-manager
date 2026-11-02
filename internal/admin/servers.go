package admin

import (
	"net/http"

	"github.com/sirupsen/logrus"

	"github.com/wernersbacher/ACPitwall/internal/api"
	smserver "github.com/wernersbacher/ACPitwall/internal/server"
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

	// EventTypeKey is the message key of the event type (event_type.*), for the translated UI.
	EventTypeKey string `json:"-"`

	IsChampionship bool
	IsRaceWeekend  bool
	IsPractice     bool

	DriversOnline int

	// JoinLink opens the running server in Content Manager. It is empty if the server is not running or the
	// server options hide the link.
	JoinLink string `json:",omitempty"`
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

	out.EventTypeKey = eventTypeKey(event.IsChampionship(), event.IsRaceWeekend(), event.IsPractice(), event.IsTimeAttack(), event.IsLooping(slot.Env.ServerID()))
	out.EventType = eventTypeNames[out.EventTypeKey]
	out.EventName = event.EventName(slot.Env.TrackSummary)
	out.IsChampionship = event.IsChampionship()
	out.IsRaceWeekend = event.IsRaceWeekend()
	out.IsPractice = event.IsPractice()

	if slot.RaceControl != nil {
		out.DriversOnline = slot.RaceControl.ConnectedDrivers.Len()
	}

	return out
}

// eventTypeNames are the English names of the event types, which the API returns.
var eventTypeNames = map[string]string{
	"event_type.championship":          "Championship",
	"event_type.championship_practice": "Championship practice",
	"event_type.race_weekend":          "Race Weekend",
	"event_type.race_weekend_practice": "Race Weekend practice",
	"event_type.looping":               "Looping event",
	"event_type.time_attack":           "Time attack",
	"event_type.running":               "Running",
}

// eventTypeKey mirrors the template "ServerEventType" of the base layout and returns the message key of the type.
func eventTypeKey(championship, raceWeekend, practice, timeAttack, looping bool) string {
	switch {
	case championship && practice:
		return "event_type.championship_practice"
	case championship:
		return "event_type.championship"
	case raceWeekend && practice:
		return "event_type.race_weekend_practice"
	case raceWeekend:
		return "event_type.race_weekend"
	case looping:
		return "event_type.looping"
	case timeAttack:
		return "event_type.time_attack"
	default:
		return "event_type.running"
	}
}

// eventType returns the English name of the event type.
func eventType(championship, raceWeekend, practice, timeAttack, looping bool) string {
	return eventTypeNames[eventTypeKey(championship, raceWeekend, practice, timeAttack, looping)]
}

func (sah *ServerAdministrationHandler) serverSummaries() []*ServerSummary {
	slots := sah.slots()

	summaries := make([]*ServerSummary, 0, len(slots))

	for _, slot := range slots {
		summary := NewServerSummary(slot)
		summary.JoinLink = joinLink(slot, summary.Running)

		summaries = append(summaries, summary)
	}

	return summaries
}

// joinLink returns the Content Manager link of a running slot, or "" if there is none to show.
func joinLink(slot NamedSlot, running bool) string {
	if !running || slot.Store == nil {
		return ""
	}

	opts, err := slot.Store.LoadServerOptions()

	if err != nil {
		logrus.WithError(err).Errorf("couldn't load server options of %q", slot.ID)
		return ""
	}

	if opts.ShowContentManagerJoinLink != 1 {
		return ""
	}

	link, err := smserver.GetContentManagerJoinLink(*opts)

	if err != nil {
		logrus.WithError(err).Errorf("could not get content manager join link of %q", slot.ID)
		return ""
	}

	return link.String()
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
