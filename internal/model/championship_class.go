package model

import (
	"errors"
	"math/rand"
	"sort"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smresults "github.com/wernersbacher/ACPitwall/internal/results"
)

var ErrClassNotFound = errors.New("servermanager: championship class not found")

// FindClassForCarModel returns the class a car belongs to. If no class has the car and a class refers to a car class
// that cannot be resolved, ErrCarClassUnresolved is returned instead of ErrClassNotFound: the car might belong to
// that class, so callers must not treat it as "no class" quietly (and nothing is ever assigned to another class).
func (c *Championship) FindClassForCarModel(model string) (*ChampionshipClass, error) {
	unresolved := false

	for _, class := range c.Classes {
		cars, err := class.ResolveCarIDs(c.carClassLookupFor(class))

		if err != nil {
			unresolved = true
		}

		for _, car := range cars {
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

	if unresolved {
		return nil, ErrCarClassUnresolved
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

	// CarClassIDs refer to global car classes (CarClass). The cars of these classes count as available cars of this
	// class in addition to AvailableCars, always as they are now (no snapshot), so that changes to a car class apply
	// to the championships using it. Stored data from before this field existed decodes to none.
	CarClassIDs []uuid.UUID

	// carClasses resolves CarClassIDs. It is set at runtime (Championship.SetCarClassLookup, done by the store when
	// it loads a championship) and never persisted.
	carClasses CarClassLookup

	DriverPenalties, TeamPenalties map[string]int
}

// ValidCarIDs returns all cars chosen within the given class: AvailableCars (or, if none are chosen and no car class is
// referenced, the models of the entrants) plus the cars of the referenced car classes, without duplicates, in a stable
// order. A reference that cannot be resolved is logged and contributes no cars; use ResolveCarIDs to get the error.
func (c *ChampionshipClass) ValidCarIDs() []string {
	cars, err := c.ResolveCarIDs(c.carClasses)

	if err != nil {
		logrus.WithError(err).Errorf("Could not resolve all cars of championship class %q (%s)", c.Name, c.ID)
	}

	return cars
}

// OwnCarIDs returns the cars that are chosen in the class itself, not through car classes: AvailableCars or, if none
// are chosen and no car class is referenced, the models of the entrants (sorted). This is what the championship form
// offers as single cars, so that saving the form does not turn the cars of a referenced car class into single cars.
func (c *ChampionshipClass) OwnCarIDs() []string {
	if len(c.AvailableCars) > 0 {
		return c.AvailableCars
	}

	if len(c.CarClassIDs) > 0 {
		return nil
	}

	var models []string

	for _, e := range c.Entrants {
		models = append(models, e.Model)
	}

	sort.Strings(models)

	return models
}

// ResolveCarIDs is ValidCarIDs with an explicit lookup (nil falls back to the lookup of the class). The cars that could
// be resolved are returned together with an error (an i18n.Message) if a referenced car class does not exist or no
// lookup is set.
func (c *ChampionshipClass) ResolveCarIDs(lookup CarClassLookup) ([]string, error) {
	if lookup == nil {
		lookup = c.carClasses
	}

	var cars []string
	seen := make(map[string]bool)

	add := func(car string) {
		if !seen[car] {
			seen[car] = true
			cars = append(cars, car)
		}
	}

	for _, car := range c.OwnCarIDs() {
		add(car)
	}

	var firstErr error

	for _, id := range c.CarClassIDs {
		var carClass *CarClass
		var ok bool

		if lookup != nil {
			carClass, ok = lookup.CarClass(id)
		}

		if !ok || carClass == nil {
			if firstErr == nil {
				firstErr = ErrCarClassReferenceMissing(c.Name, id)
			}

			continue
		}

		for _, car := range carClass.Cars {
			add(car)
		}
	}

	return cars, firstErr
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
	if class.carClasses == nil {
		class.carClasses = c.carClasses
	}

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

// ReferencedCarClasses returns the global car classes the class refers to, as they are now, in the order of CarClassIDs.
// References that cannot be resolved are left out, see MissingCarClassIDs.
func (c *ChampionshipClass) ReferencedCarClasses() []*CarClass {
	var found []*CarClass

	for _, id := range c.CarClassIDs {
		if carClass, ok := c.lookupCarClass(id); ok {
			found = append(found, carClass)
		}
	}

	return found
}

// MissingCarClassIDs returns the references to car classes that cannot be resolved. This cannot happen as long as
// referenced car classes cannot be deleted; it is shown so that it is never silent.
func (c *ChampionshipClass) MissingCarClassIDs() []uuid.UUID {
	var missing []uuid.UUID

	for _, id := range c.CarClassIDs {
		if _, ok := c.lookupCarClass(id); !ok {
			missing = append(missing, id)
		}
	}

	return missing
}

func (c *ChampionshipClass) lookupCarClass(id uuid.UUID) (*CarClass, bool) {
	if c.carClasses == nil {
		return nil, false
	}

	carClass, ok := c.carClasses.CarClass(id)

	return carClass, ok && carClass != nil
}
