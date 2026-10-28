package servermanager

import (
	"encoding/json"
	"html/template"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// The championship API exposes the information that is publicly visible on the championship pages (events,
// schedule, tracks, cars) in a stable JSON format, so that other systems can read it. Entrants, sign up responses,
// passwords and server configuration are intentionally left out.

type APIEventStatus string

const (
	APIEventStatusUnscheduled APIEventStatus = "unscheduled"
	APIEventStatusScheduled   APIEventStatus = "scheduled"
	APIEventStatusInProgress  APIEventStatus = "in_progress"
	APIEventStatusCompleted   APIEventStatus = "completed"
)

type APIChampionship struct {
	ID         uuid.UUID
	Name       string
	URL        string        `json:",omitempty"`
	Info       template.HTML `json:",omitempty"`
	Completed  bool
	Progress   float64
	SignUpOpen bool

	// DropWorstResults is the number of worst event results dropped per driver in the standings (0 = off).
	DropWorstResults int

	Classes []*APIChampionshipClass
	Events  []*APIChampionshipEvent
}

type APIChampionshipClass struct {
	ID   uuid.UUID
	Name string
	Cars []*APICar
}

type APICar struct {
	ID   string
	Name string
}

type APIChampionshipEvent struct {
	ID     uuid.UUID
	Status APIEventStatus

	// RaceWeekendName is set if the event is a Race Weekend.
	RaceWeekendName string `json:",omitempty"`

	Track       string
	TrackLayout string
	TrackName   string

	Scheduled     *time.Time `json:",omitempty"`
	Recurrence    string     `json:",omitempty"`
	StartedTime   *time.Time `json:",omitempty"`
	CompletedTime *time.Time `json:",omitempty"`

	Sessions []*APISession
}

type APISession struct {
	Type   SessionType
	Name   string
	Status APIEventStatus

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

func NewAPIChampionship(championship *Championship) *APIChampionship {
	out := &APIChampionship{
		ID:               championship.ID,
		Name:             championship.Name,
		URL:              championship.GetURL(),
		Info:             championship.Info,
		Completed:        championship.Progress() == 100,
		Progress:         championship.Progress(),
		SignUpOpen:       championship.SignUpAvailable(),
		DropWorstResults: championship.DropWorstResults,
		Classes:          []*APIChampionshipClass{},
		Events:           []*APIChampionshipEvent{},
	}

	for _, class := range championship.Classes {
		apiClass := &APIChampionshipClass{
			ID:   class.ID,
			Name: class.Name,
			Cars: []*APICar{},
		}

		for _, car := range class.ValidCarIDs() {
			apiClass.Cars = append(apiClass.Cars, &APICar{ID: car, Name: prettifyName(car, true)})
		}

		out.Classes = append(out.Classes, apiClass)
	}

	for _, event := range championship.Events {
		if event.IsRaceWeekend() {
			if event.RaceWeekend == nil {
				continue
			}

			out.Events = append(out.Events, newAPIRaceWeekendEvent(event))
		} else {
			out.Events = append(out.Events, newAPIChampionshipEvent(event))
		}
	}

	return out
}

func newAPIChampionshipEvent(event *ChampionshipEvent) *APIChampionshipEvent {
	setup := event.RaceSetup

	out := &APIChampionshipEvent{
		ID:            event.ID,
		Track:         setup.Track,
		TrackLayout:   setup.TrackLayout,
		TrackName:     apiTrackName(setup.Track, setup.TrackLayout),
		Scheduled:     apiTime(event.Scheduled),
		Recurrence:    event.Recurrence,
		StartedTime:   apiTime(event.StartedTime),
		CompletedTime: apiTime(event.CompletedTime),
		Sessions:      []*APISession{},
	}

	switch {
	case event.Completed():
		out.Status = APIEventStatusCompleted
	case event.InProgress():
		out.Status = APIEventStatusInProgress
	case !event.Scheduled.IsZero():
		out.Status = APIEventStatusScheduled
	default:
		out.Status = APIEventStatusUnscheduled
	}

	// sessions run back to back, so the start of each session can be calculated from the event's scheduled time as
	// long as all sessions before it are timed. A session with a number of laps has an unknown end.
	start := event.Scheduled
	startKnown := !event.Scheduled.IsZero()

	sessions, sessionTypes := setup.Sessions.AsSliceWithSessionTypes()

	for i, session := range sessions {
		sessionType := sessionTypes[i]

		apiSession := newAPISession(sessionType, sessionType.String(), *session, setup.RaceExtraLap == 1)

		if startKnown {
			apiSession.Start = apiTime(start)
		}

		if session.Time > 0 {
			start = start.Add(time.Duration(session.Time) * time.Minute)
		} else {
			startKnown = false
		}

		apiSession.Status = apiChampionshipSessionStatus(event, sessionType, out.Status)
		out.Sessions = append(out.Sessions, apiSession)

		if sessionType == SessionTypeRace && setup.HasMultipleRaces() {
			secondRace := newAPISession(SessionTypeSecondRace, "2nd Race", *session, setup.RaceExtraLap == 1)
			secondRace.Status = apiChampionshipSessionStatus(event, SessionTypeSecondRace, out.Status)

			out.Sessions = append(out.Sessions, secondRace)
		}
	}

	return out
}

func apiChampionshipSessionStatus(event *ChampionshipEvent, sessionType SessionType, eventStatus APIEventStatus) APIEventStatus {
	if session, ok := event.Sessions[sessionType]; ok {
		if session.Completed() {
			return APIEventStatusCompleted
		} else if session.InProgress() {
			return APIEventStatusInProgress
		}
	}

	switch eventStatus {
	case APIEventStatusCompleted:
		// sessions without results of their own, e.g. booking
		return APIEventStatusCompleted
	case APIEventStatusScheduled, APIEventStatusInProgress:
		return APIEventStatusScheduled
	default:
		return APIEventStatusUnscheduled
	}
}

func newAPIRaceWeekendEvent(event *ChampionshipEvent) *APIChampionshipEvent {
	raceWeekend := event.RaceWeekend

	out := &APIChampionshipEvent{
		ID:              event.ID,
		RaceWeekendName: raceWeekend.Name,
		Sessions:        []*APISession{},
	}

	var scheduled, started, completed time.Time

	for _, session := range raceWeekend.SortedSessions() {
		setup := session.RaceConfig

		apiSession := newAPISession(session.SessionType(), session.Name(), *session.SessionInfo(), setup.RaceExtraLap == 1)
		apiSession.Start = apiTime(session.ScheduledTime)
		apiSession.Track = setup.Track
		apiSession.TrackLayout = setup.TrackLayout
		apiSession.TrackName = apiTrackName(setup.Track, setup.TrackLayout)

		switch {
		case session.Completed():
			apiSession.Status = APIEventStatusCompleted
		case session.InProgress():
			apiSession.Status = APIEventStatusInProgress
		case !session.ScheduledTime.IsZero():
			apiSession.Status = APIEventStatusScheduled
		default:
			apiSession.Status = APIEventStatusUnscheduled
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

	out.Scheduled = apiTime(scheduled)
	out.StartedTime = apiTime(started)

	switch {
	case raceWeekend.Completed():
		out.Status = APIEventStatusCompleted
		out.CompletedTime = apiTime(completed)
	case raceWeekend.Progress() > 0:
		out.Status = APIEventStatusInProgress
	case !scheduled.IsZero():
		out.Status = APIEventStatusScheduled
	default:
		out.Status = APIEventStatusUnscheduled
	}

	return out
}

func newAPISession(sessionType SessionType, name string, session SessionConfig, extraLap bool) *APISession {
	out := &APISession{
		Type: sessionType,
		Name: name,
	}

	if session.Time > 0 {
		out.Time = session.Time
		out.ExtraLap = extraLap && (sessionType == SessionTypeRace || sessionType == SessionTypeSecondRace)
	} else {
		out.Laps = session.Laps
	}

	return out
}

func apiTrackName(track, layout string) string {
	if info := trackInfo(track, layout); info != nil && info.Name != "" {
		return info.Name
	}

	name := prettifyName(track, false)

	if layout != "" {
		name += " (" + prettifyName(layout, true) + ")"
	}

	return name
}

func apiTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}

	return &t
}

// listAPI returns all championships in the API format.
func (ch *ChampionshipsHandler) listAPI(w http.ResponseWriter, r *http.Request) {
	championships, err := ch.championshipManager.ListChampionships()

	if err != nil {
		logrus.WithError(err).Errorf("couldn't list championships")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	out := make([]*APIChampionship, 0, len(championships))

	for _, championship := range championships {
		out = append(out, NewAPIChampionship(championship))
	}

	writeAPIJSON(w, out)
}

// viewAPI returns a single championship in the API format.
func (ch *ChampionshipsHandler) viewAPI(w http.ResponseWriter, r *http.Request) {
	championship, err := ch.championshipManager.LoadChampionship(chi.URLParam(r, "championshipID"))

	if err == ErrChampionshipNotFound || os.IsNotExist(err) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		logrus.WithError(err).Errorf("couldn't load championship")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	writeAPIJSON(w, NewAPIChampionship(championship))
}

func writeAPIJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")

	if err := enc.Encode(data); err != nil {
		logrus.WithError(err).Error("couldn't encode api response")
	}
}
