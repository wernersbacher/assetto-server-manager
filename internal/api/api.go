// Package api holds the data types of the public JSON API (championships, upcoming races), which are shared by the
// championship, schedule and race control packages.
package api

import (
	"encoding/json"
	"html/template"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smcontent "github.com/wernersbacher/assetto-server-manager/internal/content"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
)

// The championship API exposes the information that is publicly visible on the championship pages (events,
// schedule, tracks, cars) in a stable JSON format, so that other systems can read it. Entrants, sign up responses,
// passwords and server configuration are intentionally left out.

// EventStatus is the progress of an event or session.
type EventStatus string

const (
	EventStatusUnscheduled EventStatus = "unscheduled"
	EventStatusScheduled   EventStatus = "scheduled"
	EventStatusInProgress  EventStatus = "in_progress"
	EventStatusCompleted   EventStatus = "completed"
)

type Championship struct {
	ID         uuid.UUID
	Name       string
	URL        string        `json:",omitempty"`
	Info       template.HTML `json:",omitempty"`
	Completed  bool
	Progress   float64
	SignUpOpen bool

	// DropWorstResults is the number of worst event results dropped per driver in the standings (0 = off).
	DropWorstResults int

	Classes []*ChampionshipClass
	Events  []*ChampionshipEvent
}

type ChampionshipClass struct {
	ID   uuid.UUID
	Name string
	Cars []*Car
}

type Car struct {
	ID   string
	Name string
}

type ChampionshipEvent struct {
	ID     uuid.UUID
	Status EventStatus

	// RaceWeekendName is set if the event is a Race Weekend.
	RaceWeekendName string `json:",omitempty"`

	Track       string
	TrackLayout string
	TrackName   string

	Scheduled     *time.Time `json:",omitempty"`
	Recurrence    string     `json:",omitempty"`
	StartedTime   *time.Time `json:",omitempty"`
	CompletedTime *time.Time `json:",omitempty"`

	Sessions []*Session
}

type Session struct {
	Type   serverconfig.SessionType
	Name   string
	Status EventStatus

	// Time is the length of the session in minutes, Laps the number of laps. Only one of them is set.
	Time     int  `json:",omitempty"`
	Laps     int  `json:",omitempty"`
	ExtraLap bool `json:",omitempty"`

	// Start is the scheduled start of the session, if it is known.
	Start *time.Time `json:",omitempty"`

	// Track details are only set for Race Weekend sessions, as they can differ between sessions.
	Track       string `json:",omitempty"`
	TrackLayout string `json:",omitempty"`
	TrackName   string `json:",omitempty"`
}

// NewChampionship returns the API representation of a championship. The install path of the Assetto Corsa server is
// used to look up the track names.
func NewChampionship(installPath string, championship *smmodel.Championship, baseURL string) *Championship {
	out := &Championship{
		ID:               championship.ID,
		Name:             championship.Name,
		URL:              championship.GetURL(baseURL),
		Info:             championship.Info,
		Completed:        championship.Progress() == 100,
		Progress:         championship.Progress(),
		SignUpOpen:       championship.SignUpAvailable(),
		DropWorstResults: championship.DropWorstResults,
		Classes:          []*ChampionshipClass{},
		Events:           []*ChampionshipEvent{},
	}

	for _, class := range championship.Classes {
		apiClass := &ChampionshipClass{
			ID:   class.ID,
			Name: class.Name,
			Cars: []*Car{},
		}

		for _, car := range class.ValidCarIDs() {
			apiClass.Cars = append(apiClass.Cars, &Car{ID: car, Name: smcontent.PrettifyName(car, true)})
		}

		out.Classes = append(out.Classes, apiClass)
	}

	for _, event := range championship.Events {
		if event.IsRaceWeekend() {
			if event.RaceWeekend == nil {
				continue
			}

			out.Events = append(out.Events, newRaceWeekendEvent(installPath, event))
		} else {
			out.Events = append(out.Events, newChampionshipEvent(installPath, event))
		}
	}

	return out
}

func newChampionshipEvent(installPath string, event *smmodel.ChampionshipEvent) *ChampionshipEvent {
	setup := event.RaceSetup

	out := &ChampionshipEvent{
		ID:            event.ID,
		Track:         setup.Track,
		TrackLayout:   setup.TrackLayout,
		TrackName:     TrackName(installPath, setup.Track, setup.TrackLayout),
		Scheduled:     Time(event.Scheduled),
		Recurrence:    event.Recurrence,
		StartedTime:   Time(event.StartedTime),
		CompletedTime: Time(event.CompletedTime),
		Sessions:      []*Session{},
	}

	switch {
	case event.Completed():
		out.Status = EventStatusCompleted
	case event.InProgress():
		out.Status = EventStatusInProgress
	case !event.Scheduled.IsZero():
		out.Status = EventStatusScheduled
	default:
		out.Status = EventStatusUnscheduled
	}

	// sessions run back to back, so the start of each session can be calculated from the event's scheduled time as
	// long as all sessions before it are timed. A session with a number of laps has an unknown end.
	start := event.Scheduled
	startKnown := !event.Scheduled.IsZero()

	sessions, sessionTypes := setup.Sessions.AsSliceWithSessionTypes()

	for i, session := range sessions {
		sessionType := sessionTypes[i]

		apiSession := NewSession(sessionType, sessionType.String(), *session, setup.RaceExtraLap == 1)

		if startKnown {
			apiSession.Start = Time(start)
		}

		if session.Time > 0 {
			start = start.Add(time.Duration(session.Time) * time.Minute)
		} else {
			startKnown = false
		}

		apiSession.Status = championshipSessionStatus(event, sessionType, out.Status)
		out.Sessions = append(out.Sessions, apiSession)

		if sessionType == serverconfig.SessionTypeRace && setup.HasMultipleRaces() {
			secondRace := NewSession(serverconfig.SessionTypeSecondRace, "2nd Race", *session, setup.RaceExtraLap == 1)
			secondRace.Status = championshipSessionStatus(event, serverconfig.SessionTypeSecondRace, out.Status)

			out.Sessions = append(out.Sessions, secondRace)
		}
	}

	return out
}

func championshipSessionStatus(event *smmodel.ChampionshipEvent, sessionType serverconfig.SessionType, eventStatus EventStatus) EventStatus {
	if session, ok := event.Sessions[sessionType]; ok {
		if session.Completed() {
			return EventStatusCompleted
		} else if session.InProgress() {
			return EventStatusInProgress
		}
	}

	switch eventStatus {
	case EventStatusCompleted:
		// sessions without results of their own, e.g. booking
		return EventStatusCompleted
	case EventStatusScheduled, EventStatusInProgress:
		return EventStatusScheduled
	default:
		return EventStatusUnscheduled
	}
}

func newRaceWeekendEvent(installPath string, event *smmodel.ChampionshipEvent) *ChampionshipEvent {
	raceWeekend := event.RaceWeekend

	out := &ChampionshipEvent{
		ID:              event.ID,
		RaceWeekendName: raceWeekend.Name,
		Sessions:        []*Session{},
	}

	var scheduled, started, completed time.Time

	for _, session := range raceWeekend.SortedSessions() {
		setup := session.RaceConfig

		apiSession := NewSession(session.SessionType(), session.Name(), *session.SessionInfo(), setup.RaceExtraLap == 1)
		apiSession.Start = Time(session.ScheduledTime)
		apiSession.Track = setup.Track
		apiSession.TrackLayout = setup.TrackLayout
		apiSession.TrackName = TrackName(installPath, setup.Track, setup.TrackLayout)

		switch {
		case session.Completed():
			apiSession.Status = EventStatusCompleted
		case session.InProgress():
			apiSession.Status = EventStatusInProgress
		case !session.ScheduledTime.IsZero():
			apiSession.Status = EventStatusScheduled
		default:
			apiSession.Status = EventStatusUnscheduled
		}

		if !session.ScheduledTime.IsZero() && (scheduled.IsZero() || session.ScheduledTime.Before(scheduled)) {
			scheduled = session.ScheduledTime
		}

		if !session.StartedTime.IsZero() && (started.IsZero() || session.StartedTime.Before(started)) {
			started = session.StartedTime
		}

		if session.CompletedTime.After(completed) {
			completed = session.CompletedTime
		}

		out.Sessions = append(out.Sessions, apiSession)
	}

	// the event takes its track from the first session, like the championship page does
	if len(out.Sessions) > 0 {
		out.Track = out.Sessions[0].Track
		out.TrackLayout = out.Sessions[0].TrackLayout
		out.TrackName = out.Sessions[0].TrackName
	}

	out.Scheduled = Time(scheduled)
	out.StartedTime = Time(started)

	switch {
	case raceWeekend.Completed():
		out.Status = EventStatusCompleted
		out.CompletedTime = Time(completed)
	case raceWeekend.Progress() > 0:
		out.Status = EventStatusInProgress
	case !scheduled.IsZero():
		out.Status = EventStatusScheduled
	default:
		out.Status = EventStatusUnscheduled
	}

	return out
}

// NewSession returns the API representation of a session. The status and start are set by the caller.
func NewSession(sessionType serverconfig.SessionType, name string, session serverconfig.SessionConfig, extraLap bool) *Session {
	out := &Session{
		Type: sessionType,
		Name: name,
	}

	if session.Time > 0 {
		out.Time = session.Time
		out.ExtraLap = extraLap && (sessionType == serverconfig.SessionTypeRace || sessionType == serverconfig.SessionTypeSecondRace)
	} else {
		out.Laps = session.Laps
	}

	return out
}

// TrackName returns the name of a track including its layout, from the track details in the install path if they exist.
func TrackName(installPath string, track, layout string) string {
	if info := smcontent.CachedTrackInfo(installPath, track, layout); info != nil && info.Name != "" {
		return info.Name
	}

	name := smcontent.PrettifyName(track, false)

	if layout != "" {
		name += " (" + smcontent.PrettifyName(layout, true) + ")"
	}

	return name
}

// Time returns a pointer to t, or nil for the zero time (which is then left out of the JSON).
func Time(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}

	return &t
}

// WriteJSON writes data as the indented JSON response of an API request.
func WriteJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")

	if err := enc.Encode(data); err != nil {
		logrus.WithError(err).Error("couldn't encode api response")
	}
}

// The upcoming races API exposes what the calendar shows (the scheduled custom races, championship events and race
// weekend sessions) in a stable JSON format, so that other systems can read it. Recurring events are not expanded:
// an entry is the next start of the event, with its recurrence rule. Passwords, entrants and the server
// configuration are intentionally left out.

// UpcomingKind is the type of an upcoming event.
type UpcomingKind string

const (
	UpcomingKindCustomRace         UpcomingKind = "custom_race"
	UpcomingKindChampionshipEvent  UpcomingKind = "championship_event"
	UpcomingKindRaceWeekendSession UpcomingKind = "race_weekend_session"
)

// UpcomingRace is one entry of the upcoming races API.
type UpcomingRace struct {
	ID   uuid.UUID
	Kind UpcomingKind
	Name string

	// Start is the next start of the event. For a recurring event Recurrence is the recurrence rule.
	Start      time.Time
	Recurrence string `json:",omitempty"`

	// OnThisServer is false if the event is scheduled on another server of a multiserver setup.
	OnThisServer bool

	// ChampionshipID and ChampionshipName are set for championship events, RaceWeekendID and RaceWeekendName for
	// race weekend sessions.
	ChampionshipID   *uuid.UUID `json:",omitempty"`
	ChampionshipName string     `json:",omitempty"`
	RaceWeekendID    *uuid.UUID `json:",omitempty"`
	RaceWeekendName  string     `json:",omitempty"`

	Track       string
	TrackLayout string
	TrackName   string

	Cars     []*Car
	Sessions []*Session
}

// upcomingRaces returns the scheduled events that start at or after now, soonest first. A recurring event whose
