package schedule

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	"github.com/wernersbacher/assetto-server-manager/internal/api"
	smcontent "github.com/wernersbacher/assetto-server-manager/internal/content"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
)

// UpcomingRaces returns the scheduled events that start at or after now, soonest first. A recurring event whose
// scheduled time has passed is listed with its next recurrence. A limit of 0 returns all events.
func (srm *Manager) UpcomingRaces(now time.Time, limit int) ([]*api.UpcomingRace, error) {
	scheduled, err := srm.GetScheduledRaces(true)

	if err != nil {
		return nil, err
	}

	out := make([]*api.UpcomingRace, 0, len(scheduled))

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

		if upcoming := srm.newUpcomingRace(event, start); upcoming != nil {
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

func (srm *Manager) newUpcomingRace(event Event, start time.Time) *api.UpcomingRace {
	setup := event.GetRaceSetup()
	scheduledServerID := event.GetScheduledServerID()

	out := &api.UpcomingRace{
		ID:           event.GetID(),
		Start:        start,
		OnThisServer: scheduledServerID == "" || scheduledServerID == srm.env.ServerID(),
		Track:        setup.Track,
		TrackLayout:  setup.TrackLayout,
		TrackName:    api.TrackName(srm.env.InstallPath(), setup.Track, setup.TrackLayout),
		Cars:         []*api.Car{},
		Sessions:     []*api.Session{},
	}

	switch e := event.(type) {
	case *smmodel.CustomRace:
		out.Kind = api.UpcomingKindCustomRace
		out.Name = e.EventName(srm.env.TrackSummary)
		out.Recurrence = e.Recurrence
	case *smmodel.ChampionshipEvent:
		out.Kind = api.UpcomingKindChampionshipEvent
		out.Name = srm.env.TrackSummary(setup.Track, setup.TrackLayout)
		out.Recurrence = e.Recurrence

		if e.Championship() != nil {
			out.ChampionshipID = &e.Championship().ID
			out.ChampionshipName = e.Championship().Name
		}
	case raceWeekendSession:
		out.Kind = api.UpcomingKindRaceWeekendSession
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
			out.Cars = append(out.Cars, &api.Car{ID: car, Name: smcontent.PrettifyName(car, true)})
		}
	}

	// sessions run back to back, so the start of each session can be calculated from the start of the event as long
	// as all sessions before it are timed. A session with a number of laps has an unknown end.
	sessionStart := start
	startKnown := true

	sessions, sessionTypes := setup.Sessions.AsSliceWithSessionTypes()

	for i, session := range sessions {
		sessionType := sessionTypes[i]

		apiSession := api.NewSession(sessionType, sessionType.String(), *session, setup.RaceExtraLap == 1)
		apiSession.Status = api.EventStatusScheduled

		if startKnown {
			apiSession.Start = api.Time(sessionStart)
		}

		if session.Time > 0 {
			sessionStart = sessionStart.Add(time.Duration(session.Time) * time.Minute)
		} else {
			startKnown = false
		}

		out.Sessions = append(out.Sessions, apiSession)

		if sessionType == serverconfig.SessionTypeRace && setup.HasMultipleRaces() {
			secondRace := api.NewSession(serverconfig.SessionTypeSecondRace, "2nd Race", *session, setup.RaceExtraLap == 1)
			secondRace.Status = api.EventStatusScheduled

			out.Sessions = append(out.Sessions, secondRace)
		}
	}

	return out
}

// upcomingAPI returns the upcoming scheduled events in the API format. The optional query parameter limit caps the
// number of events.
func (rs *Handler) UpcomingAPI(w http.ResponseWriter, r *http.Request) {
	var limit int

	if limitParam := r.URL.Query().Get("limit"); limitParam != "" {
		var err error

		limit, err = strconv.Atoi(limitParam)

		if err != nil || limit < 1 {
			http.Error(w, "limit must be a positive number", http.StatusBadRequest)
			return
		}
	}

	upcoming, err := rs.scheduledRacesManager.UpcomingRaces(time.Now(), limit)

	if err != nil {
		logrus.WithError(err).Errorf("couldn't list upcoming races")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	api.WriteJSON(w, upcoming)
}
