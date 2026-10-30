package model

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"
)

func (c *Championship) HasScheduledEvents() bool {
	for _, event := range c.Events {
		if !event.Scheduled.IsZero() {
			return true
		}
	}

	return false
}

func (c *Championship) MostRecentScheduledDateFormat(format string) string {
	scheduledDate := time.Now()

	for _, event := range c.Events {
		if event.GetScheduledTime().After(scheduledDate) {
			scheduledDate = event.GetScheduledTime()
		}
	}

	return scheduledDate.Format(format)
}

func (c *Championship) ImportEvent(eventToImport interface{}) (*ChampionshipEvent, error) {
	var newEvent *ChampionshipEvent

	switch event := eventToImport.(type) {
	case *CustomRace:
		newEvent = &ChampionshipEvent{
			ID:        uuid.New(),
			RaceSetup: event.RaceConfig,
			EntryList: c.AllEntrants(),
		}
	case *RaceWeekend:
		// the filter between Entry List and any events uses the race weekend ID in the map
		// as we are updating this ID we need to update it in the map too or filters between
		// the Entry List and any event will fail!
		oldID := event.ID.String()
		event.ID = uuid.New()

		if event.Filters != nil {
			oldFilter := event.Filters[oldID]
			delete(event.Filters, oldID)
			event.Filters[event.ID.String()] = oldFilter
		}

		newEvent = &ChampionshipEvent{
			ID:            uuid.New(),
			EntryList:     c.AllEntrants(),
			RaceWeekendID: event.ID,
			RaceWeekend:   event,
		}

		event.Championship = c
		event.ChampionshipID = c.ID

		// reset session progress
		for _, session := range event.Sessions {
			session.CompletedTime = time.Time{}
			session.StartedTime = time.Time{}
			session.Results = nil
			session.ScheduledTime = time.Time{}

			// if a session has the Entry List as a parent we need to replace the old
			// Race Weekend ID with the new one
			for i, parentID := range session.ParentIDs {
				if parentID.String() == oldID {
					session.ParentIDs[i] = event.ID
				}
			}

			for _, class := range c.Classes {
				if session.SessionType() == serverconfig.SessionTypeRace {
					session.Points[class.ID] = &class.Points
				} else {
					var points []int

					for range class.Points.Places {
						points = append(points, 0)
					}

					session.Points[class.ID] = &ChampionshipPoints{
						Places:               points,
						BestLap:              0,
						PolePosition:         0,
						CollisionWithDriver:  0,
						CollisionWithEnv:     0,
						CutTrack:             0,
						SecondRaceMultiplier: 0,
					}
				}
			}
		}

	case *ChampionshipEvent:
		newEvent = &ChampionshipEvent{
			ID:        uuid.New(),
			RaceSetup: event.RaceSetup,
			EntryList: event.EntryList,
		}
	default:
		return nil, errors.New("servermanager: unknown event type")
	}

	c.Events = append(c.Events, newEvent)

	return newEvent, nil
}

// EventByID finds a ChampionshipEvent by its ID string.
func (c *Championship) EventByID(id string) (*ChampionshipEvent, int, error) {
	for i, e := range c.Events {
		if e.ID.String() == id {
			return e, i, nil
		}
	}

	return nil, 0, ErrInvalidChampionshipEvent
}

func (c *Championship) NumCompletedEvents() int {
	i := 0

	for _, event := range ExtractRaceWeekendSessionsIntoIndividualEvents(c.Events) {
		if event.Completed() {
			i++
		}
	}

	return i
}

// extractRaceWeekendSessionsIntoIndividualEvents looks for race weekend events, and makes each indiivdual session of that
// race weekend a Championship Event, to aide with points tallying
func ExtractRaceWeekendSessionsIntoIndividualEvents(inEvents []*ChampionshipEvent) []*ChampionshipEvent {
	events := make([]*ChampionshipEvent, 0)

	for _, event := range inEvents {
		if !event.IsRaceWeekend() {
			events = append(events, event)
		} else if event.RaceWeekend != nil {
			for _, session := range event.RaceWeekend.Sessions {
				e := NewChampionshipEvent()

				e.ID = session.ID
				e.RaceSetup = session.RaceConfig
				e.CompletedTime = session.CompletedTime
				e.StartedTime = session.StartedTime
				e.Scheduled = session.ScheduledTime

				e.Sessions[session.SessionType()] = &ChampionshipSession{
					StartedTime:        session.StartedTime,
					CompletedTime:      session.CompletedTime,
					Results:            session.Results,
					RaceWeekendSession: session,
				}

				events = append(events, e)
			}
		}
	}

	return events
}

// NewChampionshipEvent creates a ChampionshipEvent with an ID
func NewChampionshipEvent() *ChampionshipEvent {
	return &ChampionshipEvent{
		ID:       uuid.New(),
		Sessions: make(map[serverconfig.SessionType]*ChampionshipSession),
	}
}

// copied an existing ChampionshipEvent but assigns a new ID
func DuplicateChampionshipEvent(event *ChampionshipEvent) *ChampionshipEvent {
	newEvent := *event

	newEvent.ID = uuid.New()
	newEvent.CompletedTime = time.Time{}

	return &newEvent
}

// A ChampionshipEvent is a given RaceSetup with Sessions.
type ChampionshipEvent struct {
	ScheduledEventBase

	ID uuid.UUID

	RaceSetup serverconfig.CurrentRaceConfig
	EntryList serverconfig.EntryList

	// UseChampionshipRaceSettings makes the event use the Championship's RaceSettings (if the championship has
	// any) instead of the corresponding values in RaceSetup. RaceSetup always keeps the event's own values.
	UseChampionshipRaceSettings bool

	Sessions map[serverconfig.SessionType]*ChampionshipSession `json:",omitempty"`

	// RaceWeekendID is the ID of the linked RaceWeekend for this ChampionshipEvent
	RaceWeekendID uuid.UUID
	// If RaceWeekendID is non-nil, RaceWeekend will be populated on loading the Championship.
	RaceWeekend *RaceWeekend

	StartedTime   time.Time
	CompletedTime time.Time

	championship *Championship
}

// SetChampionship attaches the Championship that the event belongs to. It is runtime only information (it is not
// persisted) which the event needs when it is used as a ScheduledEvent.
func (cr *ChampionshipEvent) SetChampionship(championship *Championship) {
	cr.championship = championship
}

// Championship returns the Championship attached with SetChampionship, if any.
func (cr *ChampionshipEvent) Championship() *Championship {
	return cr.championship
}

func (cr *ChampionshipEvent) IsRaceWeekend() bool {
	return cr.RaceWeekendID != uuid.Nil
}

func (cr *ChampionshipEvent) GetSummary() string {
	return fmt.Sprintf("(%s)", cr.championship.Name)
}

func (cr *ChampionshipEvent) GetURL() string {
	return "/championship/" + cr.championship.ID.String()
}

func (cr *ChampionshipEvent) HasSignUpForm() bool {
	return cr.championship.SignUpForm.Enabled
}

func (cr *ChampionshipEvent) ReadOnlyEntryList() serverconfig.EntryList {
	return cr.CombineEntryLists(cr.championship)
}

func (cr *ChampionshipEvent) GetID() uuid.UUID {
	return cr.ID
}

func (cr *ChampionshipEvent) GetScheduledServerID() ServerID {
	return cr.ScheduledServerID
}

// GetRaceSetup returns the effective race setup of the event (including championship wide race settings).
func (cr *ChampionshipEvent) GetRaceSetup() serverconfig.CurrentRaceConfig {
	if cr.championship != nil {
		return cr.championship.EffectiveRaceSetup(cr)
	}

	return cr.RaceSetup
}

func (cr *ChampionshipEvent) Cars(c *Championship) []string {
	if cr.Completed() {
		cars := make(map[string]bool)

		// look for cars in the session results
		for _, session := range cr.Sessions {
			if session.Results != nil {
				for _, car := range session.Results.Cars {
					cars[car.Model] = true
				}
			}
		}

		var out []string

		for car := range cars {
			out = append(out, car)
		}

		return out
	}

	return c.ValidCarIDs()
}

func (cr *ChampionshipEvent) CombineEntryLists(championship *Championship) serverconfig.EntryList {
	entryList := championship.AllEntrants()

	if cr.EntryList == nil {
		// no specific entry list for this event, just use the default
		return entryList
	}

	for _, entrant := range entryList {
		for _, eventEntrant := range cr.EntryList {
			if entrant.InternalUUID != uuid.Nil && entrant.InternalUUID == eventEntrant.InternalUUID && entrant.Model == eventEntrant.Model {
				entrant.OverwriteProperties(eventEntrant)

				break
			}
		}
	}

	return entryList
}

// LastSession returns the last configured session in the championship, in the following order:
// Race, Qualifying, Practice, Booking
func (cr *ChampionshipEvent) LastSession() serverconfig.SessionType {
	switch {
	case cr.RaceSetup.HasMultipleRaces():
		// there must be two races as reversed grid positions are on
		return serverconfig.SessionTypeSecondRace
	case cr.RaceSetup.HasSession(serverconfig.SessionTypeRace):
		return serverconfig.SessionTypeRace
	case cr.RaceSetup.HasSession(serverconfig.SessionTypeQualifying):
		return serverconfig.SessionTypeQualifying
	case cr.RaceSetup.HasSession(serverconfig.SessionTypePractice):
		return serverconfig.SessionTypePractice
	default:
		return serverconfig.SessionTypeBooking
	}
}

// InProgress indicates whether a ChampionshipEvent has been started but not stopped
func (cr *ChampionshipEvent) InProgress() bool {
	return !cr.StartedTime.IsZero() && cr.CompletedTime.IsZero()
}

// Completed ChampionshipEvents have a non-zero CompletedTime
func (cr *ChampionshipEvent) Completed() bool {
	return !cr.CompletedTime.IsZero()
}

// A ChampionshipSession contains information found from the live portion of the Championship tool
type ChampionshipSession struct {
	StartedTime   time.Time
	CompletedTime time.Time

	Results *smresults.SessionResults

	RaceWeekendSession *RaceWeekendSession
}

func (ce *ChampionshipSession) IsRaceWeekend() bool {
	return ce.RaceWeekendSession != nil
}

// InProgress indicates whether a ChampionshipSession has been started but not stopped
func (ce *ChampionshipSession) InProgress() bool {
	return !ce.StartedTime.IsZero() && ce.CompletedTime.IsZero()
}

// Completed ChampionshipSessions have a non-zero CompletedTime
func (ce *ChampionshipSession) Completed() bool {
	return !ce.CompletedTime.IsZero() && ce.Results != nil
}

// HasCompletedSessions is true if at least one session of the event was driven to its end and has results, so
// that a partially driven event can be finished instead of cancelled.
func (cr *ChampionshipEvent) HasCompletedSessions() bool {
	for _, session := range cr.Sessions {
		if session.Completed() && session.Results != nil {
			return true
		}
	}

	return false
}
