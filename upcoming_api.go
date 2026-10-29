package servermanager

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
)

// The upcoming races API exposes what the calendar shows (the scheduled custom races, championship events and race
// weekend sessions) in a stable JSON format, so that other systems can read it. Recurring events are not expanded:
// an entry is the next start of the event, with its recurrence rule. Passwords, entrants and the server
// configuration are intentionally left out.

type APIUpcomingKind string

const (
	APIUpcomingKindCustomRace         APIUpcomingKind = "custom_race"
	APIUpcomingKindChampionshipEvent  APIUpcomingKind = "championship_event"
	APIUpcomingKindRaceWeekendSession APIUpcomingKind = "race_weekend_session"
)

type APIUpcomingRace struct {
	ID   uuid.UUID
	Kind APIUpcomingKind
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

	Cars     []*APICar
	Sessions []*APISession
}

// upcomingRaces returns the scheduled events that start at or after now, soonest first. A recurring event whose
// scheduled time has passed is listed with its next recurrence. A limit of 0 returns all events.
func (srm *ScheduledRacesManager) upcomingRaces(now time.Time, limit int) ([]*APIUpcomingRace, error) {
	scheduled, err := srm.getScheduledRaces(true)

	if err != nil {
		return nil, err
	}

	out := make([]*APIUpcomingRace, 0, len(scheduled))

	for _, event := range scheduled {
		start := event.GetScheduledTime()

		if start.Before(now) {
			if !event.HasRecurrenceRule() {
				continue
			}

			rule, err := event.GetRecurrenceRule()

			if err != nil {
				logrus.WithError(err).Errorf("couldn't parse recurrence rule of scheduled event %s", event.GetID())
				continue
			}

			start = rule.After(now, true)

			if start.IsZero() {
				continue
			}
		}

		if upcoming := newAPIUpcomingRace(event, start); upcoming != nil {
			out = append(out, upcoming)
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Start.Before(out[j].Start)
	})

	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}

	return out, nil
}

func newAPIUpcomingRace(event ScheduledEvent, start time.Time) *APIUpcomingRace {
	setup := event.GetRaceSetup()
	scheduledServerID := event.GetScheduledServerID()

	out := &APIUpcomingRace{
		ID:           event.GetID(),
		Start:        start,
		OnThisServer: scheduledServerID == "" || scheduledServerID == serverID,
		Track:        setup.Track,
		TrackLayout:  setup.TrackLayout,
		TrackName:    apiTrackName(setup.Track, setup.TrackLayout),
		Cars:         []*APICar{},
		Sessions:     []*APISession{},
	}

	switch e := event.(type) {
	case *smmodel.CustomRace:
		out.Kind = APIUpcomingKindCustomRace
		out.Name = e.EventName(trackSummary)
		out.Recurrence = e.Recurrence
	case *smmodel.ChampionshipEvent:
		out.Kind = APIUpcomingKindChampionshipEvent
		out.Name = trackSummary(setup.Track, setup.TrackLayout)
		out.Recurrence = e.Recurrence

		if e.Championship() != nil {
			out.ChampionshipID = &e.Championship().ID
			out.ChampionshipName = e.Championship().Name
		}
	case scheduledRaceWeekendSession:
		out.Kind = APIUpcomingKindRaceWeekendSession
		out.Name = e.Name()

		if e.RaceWeekend() != nil {
			out.RaceWeekendID = &e.RaceWeekend().ID
			out.RaceWeekendName = e.RaceWeekend().Name
		}
	default:
		return nil
	}

	for _, car := range strings.Split(setup.Cars, ";") {
		if car != "" {
			out.Cars = append(out.Cars, &APICar{ID: car, Name: prettifyName(car, true)})
		}
	}

	// sessions run back to back, so the start of each session can be calculated from the start of the event as long
	// as all sessions before it are timed. A session with a number of laps has an unknown end.
	sessionStart := start
	startKnown := true

	sessions, sessionTypes := setup.Sessions.AsSliceWithSessionTypes()

	for i, session := range sessions {
		sessionType := sessionTypes[i]

		apiSession := newAPISession(sessionType, sessionType.String(), *session, setup.RaceExtraLap == 1)
		apiSession.Status = APIEventStatusScheduled

		if startKnown {
			apiSession.Start = apiTime(sessionStart)
		}

		if session.Time > 0 {
			sessionStart = sessionStart.Add(time.Duration(session.Time) * time.Minute)
		} else {
			startKnown = false
		}

		out.Sessions = append(out.Sessions, apiSession)

		if sessionType == serverconfig.SessionTypeRace && setup.HasMultipleRaces() {
			secondRace := newAPISession(serverconfig.SessionTypeSecondRace, "2nd Race", *session, setup.RaceExtraLap == 1)
			secondRace.Status = APIEventStatusScheduled

			out.Sessions = append(out.Sessions, secondRace)
		}
	}

	return out
}

// upcomingAPI returns the upcoming scheduled events in the API format. The optional query parameter limit caps the
// number of events.
func (rs *ScheduledRacesHandler) upcomingAPI(w http.ResponseWriter, r *http.Request) {
	var limit int

	if limitParam := r.URL.Query().Get("limit"); limitParam != "" {
		var err error

		limit, err = strconv.Atoi(limitParam)

		if err != nil || limit < 1 {
			http.Error(w, "limit must be a positive number", http.StatusBadRequest)
			return
		}
	}

	upcoming, err := rs.scheduledRacesManager.upcomingRaces(time.Now(), limit)

	if err != nil {
		logrus.WithError(err).Errorf("couldn't list upcoming races")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	writeAPIJSON(w, upcoming)
}
