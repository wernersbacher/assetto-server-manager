package schedule

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cj123/caldav-go/icalendar"
	"github.com/cj123/caldav-go/icalendar/components"
	"github.com/cj123/caldav-go/icalendar/values"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/teambition/rrule-go"
	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smconfig "github.com/wernersbacher/ACPitwall/internal/config"
	smcontent "github.com/wernersbacher/ACPitwall/internal/content"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smresults "github.com/wernersbacher/ACPitwall/internal/results"
	"github.com/wernersbacher/ACPitwall/internal/serverenv"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

// Event is an event which has a scheduled start (custom race, championship event or race weekend session).
type Event interface {
	GetID() uuid.UUID
	GetScheduledServerID() smmodel.ServerID
	GetRaceSetup() serverconfig.CurrentRaceConfig
	GetScheduledTime() time.Time
	GetSummary() string
	GetURL() string
	HasSignUpForm() bool
	ReadOnlyEntryList() serverconfig.EntryList
	HasRecurrenceRule() bool
	GetRecurrenceRule() (*rrule.RRule, error)
	SetRecurrenceRule(input string) error
	ClearRecurrenceRule()
}

// BuildICalEvent builds the calendar entry of a scheduled event.
func BuildICalEvent(env *serverenv.Env, event Event, baseURL string) *components.Event {
	icalEvent := components.NewEvent(event.GetID().String(), event.GetScheduledTime().UTC())

	raceSetup := event.GetRaceSetup()

	trackInfo := env.TrackInfo(raceSetup.Track, raceSetup.TrackLayout)

	if trackInfo == nil {
		icalEvent.Summary = "Race at " + smcontent.PrettifyName(raceSetup.Track, false)

		if raceSetup.TrackLayout != "" {
			icalEvent.Summary += fmt.Sprintf(" (%s)", smcontent.PrettifyName(raceSetup.TrackLayout, true))
		}
	} else {
		icalEvent.Summary = fmt.Sprintf("Race at %s, %s, %s", trackInfo.Name, trackInfo.City, trackInfo.Country)
		icalEvent.Location = values.NewLocation(fmt.Sprintf("%s, %s", trackInfo.City, trackInfo.Country))
	}

	icalEvent.Summary += " " + event.GetSummary()

	if baseURL != "" {
		u, err := url.Parse(baseURL + event.GetURL())

		if err == nil {
			icalEvent.Url = values.NewUrl(*u)
		}
	}

	var totalDuration time.Duration
	var description string

	for _, session := range raceSetup.Sessions.AsSlice() {
		if session.Time > 0 {
			sessionDuration := time.Minute * time.Duration(session.Time)

			totalDuration += sessionDuration
			description += fmt.Sprintf("%s: %s\n", session.Name, sessionDuration.String())
		} else if session.Laps > 0 {
			description += fmt.Sprintf("%s: %d laps\n", session.Name, session.Laps)
			totalDuration += time.Minute * 30 // just add a 30 min buffer so it shows in the calendar
		}
	}

	entryList := event.ReadOnlyEntryList()

	description += fmt.Sprintf("\n%d entrants in: %s", len(entryList), smcontent.CarList(entryList))

	icalEvent.Description = description
	icalEvent.Duration = values.NewDuration(totalDuration)

	return icalEvent
}

// Handler serves the calendar and the iCal feed of the scheduled events.
type Handler struct {
	*smweb.BaseHandler

	scheduledRacesManager *Manager
}

// NewHandler creates the handler for the calendar.
func NewHandler(baseHandler *smweb.BaseHandler, scheduledRacesManager *Manager) *Handler {
	return &Handler{
		BaseHandler:           baseHandler,
		scheduledRacesManager: scheduledRacesManager,
	}
}

// FilterServerParam is the query parameter of the calendar, the calendar feed and the upcoming races API which limits
// the events to those scheduled for one server. It is not the server parameter, which selects the server of the request.
const FilterServerParam = "filterServer"

type calendarTemplateVars struct {
	smweb.BaseTemplateVars

	// FilterServer is the id of the server the calendar is limited to, empty for all servers.
	FilterServer smmodel.ServerID
}

func (rs *Handler) Calendar(w http.ResponseWriter, r *http.Request) {
	rs.ViewRenderer.MustLoadTemplate(w, r, "calendar.html", &calendarTemplateVars{
		BaseTemplateVars: smweb.BaseTemplateVars{WideContainer: true},
		FilterServer:     filterServer(r),
	})
}

func filterServer(r *http.Request) smmodel.ServerID {
	return smmodel.ServerID(r.URL.Query().Get(FilterServerParam))
}

func (rs *Handler) CalendarJSON(w http.ResponseWriter, r *http.Request) {
	err := rs.generateJSON(w, r)

	if err != nil {
		logrus.WithError(err).Errorf("could not find scheduled events")
		return
	}
}

func (rs *Handler) AllScheduledRacesICalHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Add("Content-Disposition", "inline; filename=championship.ics")

	err := rs.scheduledRacesManager.buildScheduledRaces(w, filterServer(r), smweb.CanSeeHidden(r))

	if err != nil {
		logrus.WithError(err).Error("could not build scheduled races feed")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
}

func (rs *Handler) generateJSON(w io.Writer, r *http.Request) error {
	start, err := time.Parse(time.RFC3339, r.URL.Query().Get("start"))

	if err != nil {
		return err
	}

	end, err := time.Parse(time.RFC3339, r.URL.Query().Get("end"))

	if err != nil {
		return err
	}

	calendarObjects, err := rs.scheduledRacesManager.BuildCalendar(start, end, filterServer(r), smweb.CanSeeHidden(r))

	if err != nil {
		return err
	}

	for i := range calendarObjects {
		calendarObjects[i] = rs.hideInvisibleLinks(r, calendarObjects[i])
	}

	return json.NewEncoder(w).Encode(calendarObjects)
}

// EntryListFunc returns the entry list a race weekend session will be run with.
type EntryListFunc func(session *smmodel.RaceWeekendSession) (serverconfig.EntryList, error)

// ServersFunc returns the server slots. The schedule only needs their names; the resolver provides the list, as this
// package does not know the slots.
type ServersFunc func() []smweb.ServerRef

// Manager finds the scheduled events of all types.
type Manager struct {
	config *smconfig.Configuration
	store  smstore.Store
	env    *serverenv.Env

	raceWeekendEntryList EntryListFunc
	servers              ServersFunc
}

// NewManager creates a Manager. raceWeekendEntryList is used to show the entrants of race weekend sessions, servers to
// show the names of the servers events are scheduled for (nil for a single server).
func NewManager(config *smconfig.Configuration, store smstore.Store, env *serverenv.Env, raceWeekendEntryList EntryListFunc, servers ServersFunc) *Manager {
	return &Manager{
		config:               config,
		store:                store,
		env:                  env,
		raceWeekendEntryList: raceWeekendEntryList,
		servers:              servers,
	}
}

// GetScheduledRaces returns all scheduled events, or only those of this server if forAllServers is false.
func (srm *Manager) GetScheduledRaces(forAllServers bool) ([]Event, error) {
	customRaces, err := srm.store.ListCustomRaces()

	if err != nil {
		return nil, err
	}

	var scheduled []Event

	for _, race := range customRaces {
		for _, scheduledEvent := range race.ScheduledEvents {

			if scheduledEvent.Scheduled.IsZero() || (!forAllServers && scheduledEvent.ScheduledServerID != srm.env.ServerID()) {
				continue
			}

			race.ScheduledServerID = scheduledEvent.ScheduledServerID
			race.Scheduled = scheduledEvent.Scheduled
			race.ScheduledInitial = scheduledEvent.Scheduled
			race.Recurrence = scheduledEvent.Recurrence

			break
		}

		if race.Scheduled.IsZero() || (!forAllServers && race.ScheduledServerID != srm.env.ServerID()) {
			continue
		}

		scheduled = append(scheduled, race)
	}

	championships, err := srm.store.ListChampionships()

	if err != nil {
		return nil, err
	}

	for _, championship := range championships {
		for _, event := range smmodel.ExtractRaceWeekendSessionsIntoIndividualEvents(championship.Events) {
			if event.Scheduled.IsZero() || (!forAllServers && event.ScheduledServerID != srm.env.ServerID()) {
				continue
			}

			event.SetChampionship(championship)
			scheduled = append(scheduled, event)
		}
	}

	raceWeekends, err := srm.store.ListRaceWeekends()

	if err != nil {
		return nil, err
	}

	for _, raceWeekend := range raceWeekends {
		if raceWeekend.HasLinkedChampionship() {
			continue // they'll be picked up from championship events
		}

		for _, session := range raceWeekend.Sessions {
			if session.ScheduledTime.IsZero() || (!forAllServers && session.ScheduledServerID != srm.env.ServerID()) {
				continue
			}

			session.SetRaceWeekend(raceWeekend)
			scheduled = append(scheduled, raceWeekendSession{RaceWeekendSession: session, entryList: srm.raceWeekendEntryList})
		}
	}

	return scheduled, nil
}

// serverName returns the name of the server slot with the given id, or an empty string if it is not a slot.
func (srm *Manager) serverName(id smmodel.ServerID) string {
	if srm.servers == nil {
		return ""
	}

	for _, server := range srm.servers() {
		if server.ID == id {
			return server.Name
		}
	}

	return ""
}

// multipleServers tells if events can be scheduled for several servers.
func (srm *Manager) multipleServers() bool {
	return srm.servers != nil && len(srm.servers()) > 1
}

// GetScheduledRacesFor returns all scheduled events, or only those scheduled for the server with the given id if it is
// not empty. Events of hidden championships and race weekends are only included if includeHidden is set.
func (srm *Manager) GetScheduledRacesFor(server smmodel.ServerID, includeHidden bool) ([]Event, error) {
	scheduled, err := srm.GetScheduledRaces(true)

	if err != nil {
		return nil, err
	}

	filtered := make([]Event, 0, len(scheduled))

	for _, event := range scheduled {
		if !includeHidden && eventHidden(event) {
			continue
		}

		if server != "" && event.GetScheduledServerID() != server {
			continue
		}

		filtered = append(filtered, event)
	}

	return filtered, nil
}

func (srm *Manager) buildScheduledRaces(w io.Writer, server smmodel.ServerID, includeHidden bool) error {
	scheduled, err := srm.GetScheduledRacesFor(server, includeHidden)

	if err != nil {
		return err
	}

	cal := components.NewCalendar()

	for _, event := range scheduled {
		icalEvent := BuildICalEvent(srm.env, event, srm.config.HTTP.BaseURL)

		cal.Events = append(cal.Events, icalEvent)
	}

	str, err := icalendar.Marshal(cal)

	if err != nil {
		return err
	}

	_, err = fmt.Fprint(w, str)

	return err
}

type CalendarObject struct {
	ID                string           `json:"id"`
	ScheduledServerID smmodel.ServerID `json:"scheduledServerID"`

	// ScheduledServerName is the name of the server the event is scheduled for, if there are several servers.
	ScheduledServerName string    `json:"scheduledServerName"`
	GroupID             string    `json:"groupId"`
	AllDay              bool      `json:"allDay"`
	Start               time.Time `json:"start"`
	End                 time.Time `json:"end"`
	Title               string    `json:"title"`
	Description         string    `json:"description"`
	URL                 string    `json:"url"`
	SignUpURL           string    `json:"signUpURL"`
	ClassNames          []string  `json:"classNames"`
	Editable            bool      `json:"editable"`
	StartEditable       bool      `json:"startEditable"`
	DurationEditable    bool      `json:"durationEditable"`
	ResourceEditable    bool      `json:"resourceEditable"`
	Rendering           string    `json:"rendering"`
	Overlap             bool      `json:"overlap"`

	Constraint      string `json:"constraint"`
	BackgroundColor string `json:"backgroundColor"`
	BorderColor     string `json:"borderColor"`
	TextColor       string `json:"textColor"`
}

// BuildCalendar returns the calendar entries for the scheduled events between start and end, limited to the events
// scheduled for the server with the given id if it is not empty.
func (srm *Manager) BuildCalendar(start time.Time, end time.Time, server smmodel.ServerID, includeHidden bool) ([]CalendarObject, error) {
	scheduled, err := srm.GetScheduledRacesFor(server, includeHidden)

	if err != nil {
		return nil, err
	}

	var calendarObjects []CalendarObject

	if len(scheduled) == 0 {
		calendarObjects = append(calendarObjects, CalendarObject{
			ID:               "no-events",
			GroupID:          "no-events",
			AllDay:           false,
			Start:            time.Now(),
			End:              time.Now().Add(time.Hour * 3),
			Title:            "Looks like there are no scheduled events!",
			URL:              "",
			ClassNames:       nil,
			Editable:         false,
			StartEditable:    false,
			DurationEditable: false,
			ResourceEditable: false,
			Rendering:        "",
			Overlap:          true,
			Constraint:       "",
			BackgroundColor:  "#c480ff",
			BorderColor:      "#c480ff",
			TextColor:        "#303030",
		})
	}

	var recurring []Event

	for _, scheduledEvent := range scheduled {
		if scheduledEvent.HasRecurrenceRule() {
			customRace, ok := scheduledEvent.(*smmodel.CustomRace)

			if !ok {
				continue
			}

			rule, err := customRace.GetRecurrenceRule()

			if err != nil {
				return nil, err
			}

			for _, startTime := range rule.Between(start, end, true) {
				newEvent := *customRace
				newEvent.Scheduled = startTime
				newEvent.UUID = uuid.New()

				if customRace.GetScheduledTime() == newEvent.GetScheduledTime() {
					continue
				}

				recurring = append(recurring, &newEvent)
			}
		}
	}

	scheduled = append(scheduled, recurring...)

	return srm.buildCalObject(scheduled, calendarObjects)
}

func (srm *Manager) buildCalObject(scheduled []Event, calendarObjects []CalendarObject) ([]CalendarObject, error) {
	for _, scheduledEvent := range scheduled {

		var prevSessionTime time.Duration
		start := scheduledEvent.GetScheduledTime()
		end := scheduledEvent.GetScheduledTime()

		var sessionTypes []serverconfig.SessionType

		if _, ok := scheduledEvent.GetRaceSetup().Sessions[serverconfig.SessionTypeBooking]; ok {
			sessionTypes = append(sessionTypes, serverconfig.SessionTypeBooking)
		}

		if _, ok := scheduledEvent.GetRaceSetup().Sessions[serverconfig.SessionTypePractice]; ok {
			sessionTypes = append(sessionTypes, serverconfig.SessionTypePractice)
		}

		if _, ok := scheduledEvent.GetRaceSetup().Sessions[serverconfig.SessionTypeQualifying]; ok {
			sessionTypes = append(sessionTypes, serverconfig.SessionTypeQualifying)
		}

		if _, ok := scheduledEvent.GetRaceSetup().Sessions[serverconfig.SessionTypeRace]; ok {
			sessionTypes = append(sessionTypes, serverconfig.SessionTypeRace)
		}

		sessionTypes = append(sessionTypes, "Default")

		for x, session := range scheduledEvent.GetRaceSetup().Sessions.AsSlice() {
			// calculate session start/end
			start = start.Add(prevSessionTime)

			if session.Time > 0 {
				prevSessionTime = time.Minute * time.Duration(session.Time)
			} else {
				// approximate, probably fine
				prevSessionTime = 3 * time.Minute * time.Duration(session.Laps)
			}

			end = end.Add(prevSessionTime)

			var signUpURL string
			pageURL := scheduledEvent.GetURL()

			// get correct URL
			if scheduledEvent.HasSignUpForm() {
				signUpURL = pageURL + "/sign-up"
			}

			// select colours
			var backgroundColor, borderColor, textColor string

			var classNames []string
			classNames = append(classNames, "calendar-card")

			switch sessionTypes[x] {
			case serverconfig.SessionTypeBooking:
				borderColor = "#c480ff"
			case serverconfig.SessionTypePractice:
				borderColor = "#5dc972"
			case serverconfig.SessionTypeQualifying:
				borderColor = "#ffd080"
			case serverconfig.SessionTypeRace:
				borderColor = "#ff8080"
			case "Default":
				borderColor = "#5dc972"
			}

			if scheduledEvent.GetURL() != "" {
				classNames = append(classNames, "calendar-link")

				switch sessionTypes[x] {
				case serverconfig.SessionTypeBooking:
					backgroundColor = "#c480ff"
				case serverconfig.SessionTypePractice:
					backgroundColor = "#5dc972"
				case serverconfig.SessionTypeQualifying:
					backgroundColor = "#ffd080"
				case serverconfig.SessionTypeRace:
					backgroundColor = "#ff8080"
				case "Default":
					backgroundColor = "#5dc972"
				}
			} else {
				backgroundColor = "white"
			}

			textColor = "#303030"

			scheduledServerID := scheduledEvent.GetScheduledServerID()
			var scheduledServerName string

			if srm.multipleServers() {
				scheduledServerName = srm.serverName(scheduledServerID)
			}

			if srm.env.ServerID() == scheduledServerID {
				scheduledServerID = ""
			}

			calendarObjects = append(calendarObjects, CalendarObject{
				ID:                  scheduledEvent.GetID().String() + session.Name,
				ScheduledServerID:   scheduledServerID,
				ScheduledServerName: scheduledServerName,
				GroupID:             scheduledEvent.GetID().String(),
				AllDay:              false,
				Start:               start,
				End:                 end,
				Title:               generateSummary(srm.env, scheduledEvent.GetRaceSetup(), session.Name) + " " + scheduledEvent.GetSummary(),
				Description:         smcontent.CarList(scheduledEvent.GetRaceSetup().Cars) + ": " + scheduledEvent.ReadOnlyEntryList().Entrants(smresults.DriverName),
				URL:                 pageURL,
				SignUpURL:           signUpURL,
				ClassNames:          classNames,
				Editable:            false,
				StartEditable:       false,
				DurationEditable:    false,
				ResourceEditable:    false,
				Rendering:           "",
				Overlap:             true,
				Constraint:          "",
				BackgroundColor:     backgroundColor,
				BorderColor:         borderColor,
				TextColor:           textColor,
			})
		}
	}

	return calendarObjects, nil
}

func generateSummary(env *serverenv.Env, raceSetup serverconfig.CurrentRaceConfig, eventType string) string {
	var summary string

	trackInfo := env.TrackInfo(raceSetup.Track, raceSetup.TrackLayout)

	if trackInfo == nil {
		summary = eventType + " at " + smcontent.PrettifyName(raceSetup.Track, false)

		if raceSetup.TrackLayout != "" {
			summary += fmt.Sprintf(" (%s)", smcontent.PrettifyName(raceSetup.TrackLayout, true))
		}
	} else {
		summary = fmt.Sprintf(eventType+" at %s", trackInfo.Name)
	}

	return summary
}

// raceWeekendSession makes a RaceWeekendSession usable as an Event, which needs the entry list that the session will
// be run with.
type raceWeekendSession struct {
	*smmodel.RaceWeekendSession

	entryList EntryListFunc
}

func (s raceWeekendSession) ReadOnlyEntryList() serverconfig.EntryList {
	entryList, err := s.entryList(s.RaceWeekendSession)

	if err != nil {
		logrus.WithError(err).Error("Could not get race weekend entry list")
		return make(serverconfig.EntryList)
	}

	return entryList
}

// eventHidden tells whether the event belongs to a hidden championship or race weekend.
func eventHidden(event Event) bool {
	switch e := event.(type) {
	case *smmodel.ChampionshipEvent:
		return e.Championship() != nil && e.Championship().Hidden
	case raceWeekendSession:
		return e.RaceWeekend() != nil && e.RaceWeekend().Hidden
	}

	return false
}

// hideInvisibleLinks removes the links of a calendar entry to pages of areas the requester may not see (the
// championship or race weekend the event belongs to); the entry itself belongs to the calendar and stays.
func (rs *Handler) hideInvisibleLinks(r *http.Request, object CalendarObject) CalendarObject {
	var area smmodel.Area

	switch {
	case strings.HasPrefix(object.URL, "/championship/"):
		area = smmodel.AreaChampionships
	case strings.HasPrefix(object.URL, "/race-weekend/"):
		area = smmodel.AreaRaceWeekends
	default:
		return object
	}

	if smweb.AreaAccess(r, area) {
		return object
	}

	object.URL = ""
	object.SignUpURL = ""

	classNames := make([]string, 0, len(object.ClassNames))

	for _, class := range object.ClassNames {
		if class != "calendar-link" {
			classNames = append(classNames, class)
		}
	}

	object.ClassNames = classNames

	return object
}
