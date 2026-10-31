package model

import (
	"errors"
	"math/rand"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smresults "github.com/wernersbacher/ACPitwall/internal/results"
)

var ErrClassNotFound = errors.New("servermanager: championship class not found")

func (c *Championship) FindClassForCarModel(model string) (*ChampionshipClass, error) {
	for _, class := range c.Classes {
		for _, car := range class.ValidCarIDs() {
			if car == model {
				return class, nil
			}
		}
	}

	if model == serverconfig.AnyCarModel {
		// randomly assign a class based from whatever classes we have with AnyCarModel in their entrylist.
		classes := make([]*ChampionshipClass, len(c.Classes))

		copy(classes, c.Classes)

		rand.Shuffle(len(classes), func(i, j int) {
			classes[i], classes[j] = classes[j], classes[i]
		})

		for _, class := range classes {
			for _, entrant := range class.Entrants {
				if entrant.Model == serverconfig.AnyCarModel {
					return class, nil
				}
			}
		}
	}

	return nil, ErrClassNotFound
}

// NewChampionshipClass creates a championship class with the default points
func NewChampionshipClass(name string) *ChampionshipClass {
	return &ChampionshipClass{
		ID:       uuid.New(),
		Name:     name,
		Points:   DefaultChampionshipPoints,
		Entrants: make(serverconfig.EntryList),
	}
}

// ChampionshipClass contains a Name, Entrants (including Cars, Skins) and Points for those Entrants
type ChampionshipClass struct {
	ID   uuid.UUID
	Name string

	Entrants      serverconfig.EntryList
	Points        ChampionshipPoints
	AvailableCars []string

	DriverPenalties, TeamPenalties map[string]int
}

// ValidCarIDs returns a set of all cars chosen within the given class
func (c *ChampionshipClass) ValidCarIDs() []string {
	if len(c.AvailableCars) == 0 {
		cars := make(map[string]bool)

		for _, e := range c.Entrants {
			cars[e.Model] = true
		}

		var availableCars []string

		for car := range cars {
			availableCars = append(availableCars, car)
		}

		return availableCars
	}

	return c.AvailableCars
}

// ClassByID finds a ChampionshipClass by its ID string.
func (c *Championship) ClassByID(id string) (*ChampionshipClass, error) {
	for _, x := range c.Classes {
		if x.ID.String() == id {
			return x, nil
		}
	}

	return nil, ErrInvalidChampionshipClass
}

// AddClass to the championship
func (c *Championship) AddClass(class *ChampionshipClass) {
	c.Classes = append(c.Classes, class)
}

func (c *ChampionshipClass) DriverInClass(result *smresults.SessionResult) bool {
	return result.ClassID == c.ID
}

func (c *ChampionshipClass) AssignToFreeEntrantSlot(entrant *serverconfig.Entrant, potentialEntrant PotentialChampionshipEntrant) {
	entrant.Name = potentialEntrant.GetName()
	entrant.GUID = potentialEntrant.GetGUID()
	entrant.Model = potentialEntrant.GetCar()
	entrant.Skin = potentialEntrant.GetSkin()

	// #386: don't replace a team with no team.
	if potentialEntrant.GetTeam() != "" {
		entrant.Team = potentialEntrant.GetTeam()
	}
}

func (c *ChampionshipClass) AttachEntrantToResult(entrant *serverconfig.Entrant, results *smresults.SessionResults) {
	for _, car := range results.Cars {
		if car.Driver.GUID == entrant.GUID {
			car.Driver.AssignEntrant(entrant, c.ID)
		}
	}

	for _, event := range results.Events {
		if event.Driver.GUID == entrant.GUID {
			event.Driver.AssignEntrant(entrant, c.ID)
		}

		if event.OtherDriver.GUID == entrant.GUID {
			event.OtherDriver.AssignEntrant(entrant, c.ID)
		}
	}

	for _, lap := range results.Laps {
		if lap.DriverGUID == entrant.GUID {
			lap.DriverName = entrant.Name
			lap.ClassID = c.ID
		}
	}

	for _, result := range results.Result {
		if result.DriverGUID == entrant.GUID {
			result.DriverName = entrant.Name
			result.ClassID = c.ID
		}
	}
}

func (c *Championship) AttachClassIDToResults(results *smresults.SessionResults) {
	for _, lap := range results.Laps {
		class, err := c.FindClassForCarModel(lap.CarModel)

		if err != nil {
			logrus.Warnf("Couldn't find class for car model: %s (entrant: %s/%s)", lap.CarModel, lap.DriverName, lap.DriverGUID)
			continue
		}

		lap.ClassID = class.ID
	}

	for _, result := range results.Result {
		class, err := c.FindClassForCarModel(result.CarModel)

		if err != nil {
			logrus.Warnf("Couldn't find class for car model: %s (entrant: %s/%s)", result.CarModel, result.DriverName, result.DriverGUID)
			continue
		}

		result.ClassID = class.ID
	}
}
