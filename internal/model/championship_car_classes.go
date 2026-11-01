package model

import (
	"errors"
	"sort"

	"github.com/google/uuid"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	"github.com/wernersbacher/ACPitwall/internal/i18n"
)

// ErrCarClassUnresolved is returned by FindClassForCarModel when no class has the car but a class refers to a car
// class that cannot be resolved, so the car may belong to it.
var ErrCarClassUnresolved = errors.New("servermanager: championship class refers to a car class that cannot be resolved")

// CarClassLookup finds global car classes by ID. The model has no store access, whoever loads a championship (the
// store) hands it one with Championship.SetCarClassLookup.
type CarClassLookup interface {
	CarClass(id uuid.UUID) (*CarClass, bool)
}

// CarClassMap is a CarClassLookup over a fixed set of car classes.
type CarClassMap map[uuid.UUID]*CarClass

// NewCarClassMap indexes car classes by ID.
func NewCarClassMap(classes []*CarClass) CarClassMap {
	m := make(CarClassMap, len(classes))

	for _, class := range classes {
		m[class.ID] = class
	}

	return m
}

// CarClass implements CarClassLookup.
func (m CarClassMap) CarClass(id uuid.UUID) (*CarClass, bool) {
	class, ok := m[id]

	return class, ok && class != nil
}

// ErrCarClassReferenceMissing is the error for a championship class that refers to a car class that does not exist.
func ErrCarClassReferenceMissing(className string, id uuid.UUID) i18n.Message {
	return i18n.Msg("validation.championship.car_class_missing", "Class \"{class}\": the car class {id} does not exist.", "class", className, "id", id.String())
}

// SetCarClassLookup sets the lookup which resolves the CarClassIDs of all classes of the championship (and of classes
// added later). It is not persisted. Without one, references cannot be resolved: ValidCarIDs logs that and
// CheckCarClasses reports it.
func (c *Championship) SetCarClassLookup(lookup CarClassLookup) {
	c.carClasses = lookup

	for _, class := range c.Classes {
		class.carClasses = lookup
	}
}

// HasCarClassReferences tells whether any class refers to a global car class.
func (c *Championship) HasCarClassReferences() bool {
	for _, class := range c.Classes {
		if len(class.CarClassIDs) > 0 {
			return true
		}
	}

	return false
}

// carClassLookupFor is the lookup to resolve the references of a class: the one of the class, else the one of the championship.
func (c *Championship) carClassLookupFor(class *ChampionshipClass) CarClassLookup {
	if class.carClasses != nil {
		return class.carClasses
	}

	return c.carClasses
}

// CheckCarClasses checks the invariants of the car classes of the championship against lookup (nil: the championship's
// own, see SetCarClassLookup):
//
//  1. no class lists a car class more than once,
//  2. every referenced car class exists,
//  3. no car is in two classes of the championship, counting AvailableCars, the cars of referenced car classes and the
//     models of entrants (AnyCarModel is no car and ignored).
//
// The first violation is returned as an i18n.Message.
func (c *Championship) CheckCarClasses(lookup CarClassLookup) error {
	owner := make(map[string]*ChampionshipClass)

	for _, class := range c.Classes {
		ids := make(map[uuid.UUID]bool, len(class.CarClassIDs))

		for _, id := range class.CarClassIDs {
			if ids[id] {
				return i18n.Msg("validation.championship.car_class_duplicate", "Class \"{class}\" lists the car class {id} more than once.", "class", class.Name, "id", id.String())
			}

			ids[id] = true
		}

		classLookup := lookup

		if classLookup == nil {
			classLookup = c.carClassLookupFor(class)
		}

		cars, err := class.ResolveCarIDs(classLookup)

		if err != nil {
			return err
		}

		// the models of entrants are cars of the class as well, even if they are not in the list of available cars
		for _, entrant := range class.Entrants {
			if entrant.Model != "" {
				cars = append(cars, entrant.Model)
			}
		}

		sort.Strings(cars)

		for _, car := range cars {
			if car == serverconfig.AnyCarModel {
				continue
			}

			if other, ok := owner[car]; ok && other != class {
				return i18n.Msg("validation.championship.car_in_two_classes", "The car {car} is in more than one class of this championship (\"{first}\" and \"{second}\").", "car", car, "first", other.Name, "second", class.Name)
			}

			owner[car] = class
		}
	}

	return nil
}
