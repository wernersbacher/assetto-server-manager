// Package carclasses manages the global car classes (model.CarClass): named groups of cars that are independent of any
// championship and shared by all server slots. Championship classes refer to them live, so every change is checked
// against all championships that use the class.
//
// The package sits above championship because it needs the championship lock (championship.LockCarClassChanges) and
// the rules championships follow; the handlers (xe0.4) and the championship form only talk to the Manager.
package carclasses

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wernersbacher/ACPitwall/internal/championship"
	"github.com/wernersbacher/ACPitwall/internal/i18n"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smresults "github.com/wernersbacher/ACPitwall/internal/results"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
)

// ErrNotFound is returned for an unknown car class (or an ID that is no UUID).
var ErrNotFound = smmodel.ErrCarClassNotFound

// Error is a refused change of a car class, with a message the UI shows in the language of the request
// (smweb.ErrorText). Error returns the English text.
type Error struct {
	i18n.Message
}

func (e Error) Error() string {
	return e.Message.String()
}

// Usage is one championship class that refers to a car class.
type Usage struct {
	ChampionshipID   uuid.UUID
	ChampionshipName string
	ClassID          uuid.UUID
	ClassName        string
}

// Manager is the one manager of the global car classes, shared by all slots. All changes hold
// championship.LockCarClassChanges, which serialises them with each other, with every sign up and with the saving of
// championships (every slot has its own ChampionshipManager, but the championships and car classes are shared).
type Manager struct {
	store smstore.Store
	now   func() time.Time
}

// NewManager creates the manager.
func NewManager(store smstore.Store) *Manager {
	return &Manager{store: store, now: time.Now}
}

// List returns all car classes sorted by name.
func (m *Manager) List() ([]*smmodel.CarClass, error) {
	classes, err := m.store.ListCarClasses()

	if err != nil {
		return nil, err
	}

	sort.SliceStable(classes, func(i, j int) bool {
		return smmodel.NormalizeCarClassName(classes[i].Name) < smmodel.NormalizeCarClassName(classes[j].Name)
	})

	return classes, nil
}

// Get returns the car class with the ID (ErrNotFound if there is none).
func (m *Manager) Get(id string) (*smmodel.CarClass, error) {
	return m.store.FindCarClassByID(id)
}

// Create adds a car class: the name must not be empty or used by another class, the cars must be valid (see
// model.CarClass.Validate).
func (m *Manager) Create(name string, cars []string) (*smmodel.CarClass, error) {
	defer championship.LockCarClassChanges()()

	class, err := smmodel.NewCarClass(name, cars, m.now())

	if err != nil {
		return nil, translateModelError(err)
	}

	if err := m.checkName(class); err != nil {
		return nil, err
	}

	if err := m.store.UpsertCarClass(class); err != nil {
		return nil, err
	}

	return class, nil
}

// Update changes name and cars of a car class. Every championship that refers to the class is checked against the
// changed class before anything is saved:
//
//  1. no car may end up in two classes of a championship (adding a car which the championship has in another class),
//  2. a car that the class no longer gives to a championship class must not be in use there, see CarUsage.
//
// The first violation is returned as an Error naming the championship.
func (m *Manager) Update(id string, name string, cars []string) (*smmodel.CarClass, error) {
	defer championship.LockCarClassChanges()()

	current, err := m.store.FindCarClassByID(id)

	if err != nil {
		return nil, err
	}

	updated := *current
	updated.Name = strings.TrimSpace(name)
	updated.Cars = append([]string(nil), cars...)
	updated.Updated = m.now()

	if err := updated.Validate(); err != nil {
		return nil, translateModelError(err)
	}

	if err := m.checkName(&updated); err != nil {
		return nil, err
	}

	if err := m.checkChampionships(current, &updated); err != nil {
		return nil, err
	}

	if err := m.store.UpsertCarClass(&updated); err != nil {
		return nil, err
	}

	return &updated, nil
}

// Delete removes a car class which no championship refers to. Deleting a referenced class is refused with an Error
// that names the championships.
func (m *Manager) Delete(id string) error {
	defer championship.LockCarClassChanges()()

	class, err := m.store.FindCarClassByID(id)

	if err != nil {
		return err
	}

	usages, err := m.usages(class.ID)

	if err != nil {
		return err
	}

	if len(usages) > 0 {
		return Error{i18n.Msg("validation.car_class.in_use", "The car class \"{class}\" is used by: {championships}. Remove it from these championships first.", "class", class.Name, "championships", usageNames(usages))}
	}

	return m.store.DeleteCarClass(id)
}

// Usages lists the championship classes that refer to the car class (deleted championships are ignored), for the UI.
func (m *Manager) Usages(id string) ([]Usage, error) {
	class, err := m.store.FindCarClassByID(id)

	if err != nil {
		return nil, err
	}

	return m.usages(class.ID)
}

// usages lists the live championships that refer to the car class. Deleted championships are not listed by the store
// and do not block changes of a car class; they are checked when they are saved again (UpsertChampionshipChecked).
func (m *Manager) usages(id uuid.UUID) ([]Usage, error) {
	championships, err := m.store.ListChampionships()

	if err != nil {
		return nil, err
	}

	var usages []Usage

	for _, c := range championships {
		for _, class := range c.Classes {
			if referencesCarClass(class, id) {
				usages = append(usages, Usage{c.ID, c.Name, class.ID, class.Name})
			}
		}
	}

	return usages, nil
}

func referencesCarClass(class *smmodel.ChampionshipClass, id uuid.UUID) bool {
	for _, ref := range class.CarClassIDs {
		if ref == id {
			return true
		}
	}

	return false
}

// usageNames lists the championships of the usages once each, in the order of the usages.
func usageNames(usages []Usage) string {
	var names []string
	seen := make(map[uuid.UUID]bool)

	for _, usage := range usages {
		if !seen[usage.ChampionshipID] {
			seen[usage.ChampionshipID] = true
			names = append(names, "\""+usage.ChampionshipName+"\"")
		}
	}

	return strings.Join(names, ", ")
}

func (m *Manager) checkName(class *smmodel.CarClass) error {
	classes, err := m.store.ListCarClasses()

	if err != nil {
		return err
	}

	return translateModelError(smmodel.CheckCarClassNameUnique(class, classes))
}

// checkChampionships simulates the change (before -> after) for every championship that refers to the class.
func (m *Manager) checkChampionships(before, after *smmodel.CarClass) error {
	classes, err := m.store.ListCarClasses()

	if err != nil {
		return err
	}

	oldMap := smmodel.NewCarClassMap(classes)

	// a copy of the map in which only the changed class differs, the stored one is not touched
	newMap := make(smmodel.CarClassMap, len(oldMap))

	for id, class := range oldMap {
		newMap[id] = class
	}

	newMap[after.ID] = after

	championships, err := m.store.ListChampionships()

	if err != nil {
		return err
	}

	for _, c := range championships {
		if !refersTo(c, after.ID) {
			continue
		}

		if err := c.CheckCarClasses(newMap); err != nil {
			return conflictError(c, after, err)
		}

		for _, class := range c.Classes {
			if !referencesCarClass(class, after.ID) {
				continue
			}

			oldCars, _ := class.ResolveCarIDs(oldMap)
			newCars, _ := class.ResolveCarIDs(newMap)

			still := make(map[string]bool, len(newCars))

			for _, car := range newCars {
				still[car] = true
			}

			for _, car := range oldCars {
				if still[car] {
					continue
				}

				if err := CarUsage(c, class, car); err != nil {
					return carInUseError(c, class, after, car, err)
				}
			}
		}
	}

	return nil
}

func refersTo(c *smmodel.Championship, id uuid.UUID) bool {
	for _, class := range c.Classes {
		if referencesCarClass(class, id) {
			return true
		}
	}

	return false
}

// conflictError names the championship in the error of Championship.CheckCarClasses.
func conflictError(c *smmodel.Championship, class *smmodel.CarClass, err error) error {
	var msg i18n.Message

	if translatable, ok := err.(i18n.Translatable); ok {
		msg = translatable.I18nMessage()
	} else {
		msg = i18n.Msg("validation.car_class.unknown_problem", "{error}", "error", err.Error())
	}

	if msg.Key == "validation.championship.car_in_two_classes" && len(msg.Args) == 6 {
		return Error{i18n.Msg("validation.car_class.car_in_two_classes", "The change of the car class \"{class}\" would put the car {car} into two classes of the championship \"{championship}\" (\"{first}\" and \"{second}\").",
			"class", class.Name, "car", msg.Args[1], "championship", c.Name, "first", msg.Args[3], "second", msg.Args[5])}
	}

	return Error{i18n.Msg("validation.car_class.championship_invalid", "The change of the car class \"{class}\" would make the championship \"{championship}\" invalid: {reason}", "class", class.Name, "championship", c.Name, "reason", msg)}
}

// usageError says why a car is in use, see CarUsage.
type usageError struct {
	kind string
}

func (u usageError) Error() string { return "car in use: " + u.kind }

// CarUsage says whether the championship uses the car in its class, in which case the car must stay available to the
// class. It returns nil if the car is unused, else a usageError. "In use" means, for this class:
//
//   - an entrant of the class drives the car (Entrants[].Model),
//   - a registration for the championship with this car that is accepted or pending (rejected ones are ignored; the
//     class of a registration is the class that has its car, so the car being in the class makes it one of the class),
//   - a result of any event of the championship (including the sessions of race weekends) which the class has
//     (SessionResult/SessionLap with this car model and the ID of the class, or a car with this model whose driver has
//     the class ID). This holds for finished championships too: their results keep assigning the car to the class.
//
// The cars of events that are planned but did not run (entry lists) are not looked at: they are rebuilt from the
// classes when an event starts. AnyCarModel is no car and never in use.
func CarUsage(c *smmodel.Championship, class *smmodel.ChampionshipClass, car string) error {
	for _, entrant := range class.Entrants {
		if entrant.Model == car {
			return usageError{"entrant"}
		}
	}

	for _, response := range c.SignUpForm.Responses {
		if response.Car == car && response.Status != smmodel.ChampionshipEntrantRejected {
			return usageError{"sign_up"}
		}
	}

	if classResultsUseCar(c, class.ID, car) {
		return usageError{"results"}
	}

	return nil
}

func classResultsUseCar(c *smmodel.Championship, classID uuid.UUID, car string) bool {
	for _, event := range c.Events {
		for _, session := range event.Sessions {
			if session != nil && resultsUseCar(session.Results, classID, car) {
				return true
			}
		}

		if event.RaceWeekend != nil {
			for _, session := range event.RaceWeekend.Sessions {
				if session != nil && resultsUseCar(session.Results, classID, car) {
					return true
				}
			}
		}
	}

	return false
}

func resultsUseCar(results *smresults.SessionResults, classID uuid.UUID, car string) bool {
	if results == nil {
		return false
	}

	for _, result := range results.Result {
		if result.CarModel == car && result.ClassID == classID {
			return true
		}
	}

	for _, lap := range results.Laps {
		if lap.CarModel == car && lap.ClassID == classID {
			return true
		}
	}

	for _, sessionCar := range results.Cars {
		if sessionCar.Model == car && sessionCar.Driver.ClassID == classID {
			return true
		}
	}

	return false
}

func carInUseError(c *smmodel.Championship, class *smmodel.ChampionshipClass, carClass *smmodel.CarClass, car string, err error) error {
	kind := ""

	var usage usageError

	if errors.As(err, &usage) {
		kind = usage.kind
	}

	switch kind {
	case "entrant":
		return Error{i18n.Msg("validation.car_class.car_in_use_entrant", "The car {car} cannot be removed from the car class \"{class}\": it is driven by entrants of the class \"{championshipClass}\" in the championship \"{championship}\".", "car", car, "class", carClass.Name, "championshipClass", class.Name, "championship", c.Name)}
	case "sign_up":
		return Error{i18n.Msg("validation.car_class.car_in_use_sign_up", "The car {car} cannot be removed from the car class \"{class}\": drivers have registered with it for the class \"{championshipClass}\" in the championship \"{championship}\".", "car", car, "class", carClass.Name, "championshipClass", class.Name, "championship", c.Name)}
	default:
		return Error{i18n.Msg("validation.car_class.car_in_use_results", "The car {car} cannot be removed from the car class \"{class}\": the championship \"{championship}\" has results that assign it to the class \"{championshipClass}\".", "car", car, "class", carClass.Name, "championshipClass", class.Name, "championship", c.Name)}
	}
}

// translateModelError turns the plain errors of the model into messages.
func translateModelError(err error) error {
	switch err {
	case nil:
		return nil
	case smmodel.ErrCarClassNameEmpty:
		return Error{i18n.Msg("validation.car_class.name_empty", "The name of the car class must not be empty.")}
	case smmodel.ErrCarClassNameTaken:
		return Error{i18n.Msg("validation.car_class.name_taken", "There is already a car class with this name.")}
	case smmodel.ErrCarClassNoCars:
		return Error{i18n.Msg("validation.car_class.no_cars", "Choose at least one car for the car class.")}
	case smmodel.ErrCarClassEmptyCar:
		return Error{i18n.Msg("validation.car_class.empty_car", "The car class contains an empty car.")}
	case smmodel.ErrCarClassDuplicateCar:
		return Error{i18n.Msg("validation.car_class.duplicate_car", "The car class contains a car more than once.")}
	}

	return err
}

// ValidateImported checks a car class that a championship import wants to create (with the ID of the export) like
// Create does: name and cars must be valid and the name must not be used by another class of existing. It does not
// take the car class lock, the import holds it already (championship.CarClassValidator).
func (m *Manager) ValidateImported(class *smmodel.CarClass, existing []*smmodel.CarClass) error {
	if err := class.Validate(); err != nil {
		return translateModelError(err)
	}

	return translateModelError(smmodel.CheckCarClassNameUnique(class, existing))
}
