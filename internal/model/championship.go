package model

import (
	"html/template"
	"time"

	"github.com/google/uuid"
	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
)

// NewChampionship creates a Championship with a given name, creating a UUID for the championship as well.
func NewChampionship(name string) *Championship {
	return &Championship{
		ID:                  uuid.New(),
		Name:                name,
		Created:             time.Now(),
		OpenEntrants:        false,
		PersistOpenEntrants: true,
		DefaultTab:          ChampionshipTabDriverStandings,
	}
}

// A Championship is a collection of ChampionshipEvents for a group of Entrants. Each Entrant in a Championship
// is awarded Points for their position in a ChampionshipEvent.
type Championship struct {
	ID                  uuid.UUID
	Name                string
	Created             time.Time
	Updated             time.Time
	Deleted             time.Time
	OverridePassword    bool
	ReplacementPassword string

	// Raw html can be attached to championships, used to share tracks/cars etc.
	Info template.HTML

	// URL to a specific OG Image for the championship
	OGImage string

	// OpenEntrants indicates that entrant names do not need to be specified in the EntryList.
	// As Entrants join a championship, the available Entrant slots will be filled by the information
	// provided by a join message. The EntryList for each class will still need creating, but
	// can omit names/GUIDs/teams as necessary. These can then be edited after the fact.
	OpenEntrants bool

	// PersistOpenEntrants (used with OpenEntrants) indicates that drivers who join the Championship
	// should be added to the Championship EntryList. This is ON by default.
	PersistOpenEntrants bool

	// SignUpForm gives anyone on the web access to a Championship Sign Up Form so that they can
	// mark themselves for participation in this Championship.
	SignUpForm ChampionshipSignUpForm

	Classes []*ChampionshipClass
	Events  []*ChampionshipEvent

	// SpectatorCar is a car defined in the Championship settings that if set up is added to the back of the grid
	// of every event in the Championship.
	SpectatorCar        serverconfig.Entrant
	SpectatorCarEnabled bool

	DefaultTab ChampionshipTab

	// DropWorstResults is the number of worst event results that are dropped from each driver's (and team's)
	// total when calculating standings. 0 (the default) means no results are dropped.
	DropWorstResults int

	// RaceSettings are race settings (fuel, tyre wear, damage, assists, dynamic track...) which are shared by all
	// events of the championship that have UseChampionshipRaceSettings set. nil means the feature is off.
	RaceSettings *ChampionshipRaceSettings
}

func (c *Championship) HasSpectatorCar() bool {
	return !c.OpenEntrants && c.SpectatorCarEnabled && c.SpectatorCar.GUID != "" && c.SpectatorCar.Model != ""
}

func (c *Championship) GetURL(baseURL string) string {
	if baseURL != "" {
		return baseURL + "/championship/" + c.ID.String()
	}

	return ""
}

// IsMultiClass is true if the Championship has more than one Class
func (c *Championship) IsMultiClass() bool {
	return len(c.Classes) > 1
}

func (c *Championship) HasTeamNames() bool {
	for _, class := range c.Classes {
		for _, entrant := range class.Entrants {
			if entrant.Team != "" {
				return true
			}
		}
	}
	for _, event := range c.Events {
		for _, session := range event.Sessions {
			if session.Completed() && session.Results != nil {
				for _, car := range session.Results.Cars {
					if car.Driver.Team != "" {
						return true
					}
				}
			}
		}
	}

	return false
}

// ValidCarIDs returns a set of all of the valid cars in the Championship - that is, the smallest possible list
// of Cars driven by the Entrants.
func (c *Championship) ValidCarIDs() []string {
	cars := make(map[string]bool)

	for _, class := range c.Classes {
		for _, model := range class.ValidCarIDs() {
			cars[model] = true
		}
	}

	if c.HasSpectatorCar() {
		cars[c.SpectatorCar.Model] = true
	}

	var out []string

	for car := range cars {
		out = append(out, car)
	}

	return out
}

// Progress of the Championship as a percentage
func (c *Championship) Progress() float64 {
	numEvents := float64(len(c.Events))

	for _, event := range c.Events {
		if event.IsRaceWeekend() && event.RaceWeekend != nil {
			numEvents += float64(len(event.RaceWeekend.Sessions)) - 1
		}
	}

	if numEvents == 0 {
		return 0
	}

	numCompletedEvents := float64(0)

	for _, event := range c.Events {

		if event.Completed() {
			numCompletedEvents++
		}

		if event.IsRaceWeekend() && event.RaceWeekend != nil {
			for _, session := range event.RaceWeekend.Sessions {
				if session.Completed() {
					numCompletedEvents++
				}
			}
		}
	}

	return (numCompletedEvents / numEvents) * 100
}
