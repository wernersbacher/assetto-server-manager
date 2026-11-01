package championship

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/wernersbacher/ACPitwall/internal/i18n"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

// ExportedCarClass is the definition of a global car class in a championship export. The export only carries the
// classes the championship refers to, so that it can be imported on a server which does not have them.
type ExportedCarClass struct {
	ID   uuid.UUID
	Name string
	Cars []string
}

// ChampionshipExport is the export format of a championship: the championship JSON as it always was, plus
// CarClassDefinitions. The additional field is omitted if the championship refers to no car class, and importers
// that do not know it (older versions) ignore it, as encoding/json skips unknown fields.
type ChampionshipExport struct {
	*smmodel.Championship

	CarClassDefinitions []ExportedCarClass `json:",omitempty"`
}

// CarClassValidator checks a car class that an import wants to create with the rules of the car class manager (a
// name and valid cars, name not used by another class). It runs while the import holds the car class lock, so it must
// not take it (carclasses.Manager.ValidateImported). The resolver injects it, because the carclasses package imports
// this one.
type CarClassValidator interface {
	ValidateImported(class *smmodel.CarClass, existing []*smmodel.CarClass) error
}

// SetCarClassValidator sets the validator for car classes created by ImportChampionship. Without one, only
// CarClass.Validate runs.
func (cm *ChampionshipManager) SetCarClassValidator(v CarClassValidator) {
	cm.carClassValidator = v
}

// ExportChampionship returns the export of the championship including the definitions of the global car classes its
// classes refer to (in the order of the references, each once). References that cannot be resolved are left out.
func (cm *ChampionshipManager) ExportChampionship(championship *smmodel.Championship) (*ChampionshipExport, error) {
	export := &ChampionshipExport{Championship: championship}

	if !championship.HasCarClassReferences() {
		return export, nil
	}

	carClasses, err := cm.store.ListCarClasses()

	if err != nil {
		return nil, err
	}

	lookup := smmodel.NewCarClassMap(carClasses)
	seen := make(map[uuid.UUID]bool)

	for _, class := range championship.Classes {
		for _, id := range class.CarClassIDs {
			carClass, ok := lookup[id]

			if !ok || seen[id] {
				continue
			}

			seen[id] = true

			export.CarClassDefinitions = append(export.CarClassDefinitions, ExportedCarClass{
				ID:   carClass.ID,
				Name: carClass.Name,
				Cars: append([]string(nil), carClass.Cars...),
			})
		}
	}

	return export, nil
}

// ImportWarning tells that a car class of the export exists here already with other cars. The existing class is used
// unchanged.
type ImportWarning struct {
	ClassName string
}

// ImportResult describes what ImportChampionship did.
type ImportResult struct {
	ChampionshipID string

	// CreatedCarClasses are the names of the car classes the import created.
	CreatedCarClasses []string

	// Warnings are the car classes which exist here with other cars than in the export.
	Warnings []ImportWarning
}

// ErrNoChampionshipInImport is returned if the imported JSON is no championship (e.g. "null").
var ErrNoChampionshipInImport = ValidationError{i18n.Msg("validation.championship.import_empty", "The import does not contain a championship.")}

// ImportChampionship imports a championship from the JSON of an export.
//
// All checks run before anything is written, under the car class lock (LockCarClassChanges), so that car classes and
// championships cannot change in between:
//
//   - a car class of the export that does not exist here (ID unknown) is created with the same ID,
//   - one with the ID of an existing car class is used unchanged (a different list of cars is reported as a warning),
//   - one whose name is used by a car class with another ID refuses the import,
//   - after that, the championship must pass Championship.CheckCarClasses with the resulting car classes (every
//     reference exists, no car in two classes).
//
// Then the new car classes, the race weekends and the championship are written (the championship last, because it is
// what makes the rest reachable). If one of the writes fails, everything written before is removed again.
func (cm *ChampionshipManager) ImportChampionship(jsonData string) (*ImportResult, error) {
	var championship *smmodel.Championship

	if err := json.Unmarshal([]byte(jsonData), &championship); err != nil {
		return nil, err
	}

	if championship == nil {
		return nil, ErrNoChampionshipInImport
	}

	var definitions struct {
		CarClassDefinitions []ExportedCarClass
	}

	if err := json.Unmarshal([]byte(jsonData), &definitions); err != nil {
		return nil, err
	}

	// the lock is held until the championship is written. It is not reentrant, so nothing below may call
	// UpsertChampionship (which holds it for reading); the check under the lock replaces the one of that function.
	defer LockCarClassChanges()()

	result := &ImportResult{ChampionshipID: championship.ID.String()}

	existing, err := cm.store.ListCarClasses()

	if err != nil {
		return nil, err
	}

	created, err := cm.prepareImportedCarClasses(championship, definitions.CarClassDefinitions, existing, result)

	if err != nil {
		return nil, err
	}

	if championship.HasCarClassReferences() {
		lookup := smmodel.NewCarClassMap(append(append([]*smmodel.CarClass(nil), existing...), created...))

		championship.SetCarClassLookup(lookup)

		if err := championship.CheckCarClasses(lookup); err != nil {
			if msg, ok := err.(i18n.Message); ok {
				return nil, ValidationError{msg}
			}

			return nil, err
		}
	}

	if err := cm.writeImport(championship, created); err != nil {
		return nil, err
	}

	sort.Strings(result.CreatedCarClasses)

	return result, nil
}

// prepareImportedCarClasses decides, without writing, which car classes of the export have to be created. Only classes
// that the championship refers to count. A reference without a definition is left to CheckCarClasses.
func (cm *ChampionshipManager) prepareImportedCarClasses(championship *smmodel.Championship, definitions []ExportedCarClass, existing []*smmodel.CarClass, result *ImportResult) ([]*smmodel.CarClass, error) {
	referenced := make(map[uuid.UUID]bool)

	for _, class := range championship.Classes {
		for _, id := range class.CarClassIDs {
			referenced[id] = true
		}
	}

	existingByID := smmodel.NewCarClassMap(existing)
	now := time.Now()

	var created []*smmodel.CarClass

	seen := make(map[uuid.UUID]bool)

	for _, definition := range definitions {
		if !referenced[definition.ID] || seen[definition.ID] {
			continue
		}

		seen[definition.ID] = true

		if current, ok := existingByID[definition.ID]; ok {
			if !sameCars(current.Cars, definition.Cars) {
				result.Warnings = append(result.Warnings, ImportWarning{current.Name})
			}

			continue
		}

		class := &smmodel.CarClass{
			ID:      definition.ID,
			Name:    definition.Name,
			Cars:    append([]string(nil), definition.Cars...),
			Created: now,
			Updated: now,
		}

		for _, other := range append(append([]*smmodel.CarClass(nil), existing...), created...) {
			if smmodel.NormalizeCarClassName(other.Name) == smmodel.NormalizeCarClassName(class.Name) {
				return nil, ValidationError{i18n.Msg("validation.championship.import_class_name_taken", "The car class \"{class}\" of the import cannot be created: this server already has a car class with that name but a different ID.", "class", class.Name)}
			}
		}

		if err := cm.validateImportedCarClass(class, existing); err != nil {
			return nil, err
		}

		created = append(created, class)
		result.CreatedCarClasses = append(result.CreatedCarClasses, class.Name)
	}

	return created, nil
}

func (cm *ChampionshipManager) validateImportedCarClass(class *smmodel.CarClass, existing []*smmodel.CarClass) error {
	var err error

	if cm.carClassValidator != nil {
		err = cm.carClassValidator.ValidateImported(class, existing)
	} else {
		err = class.Validate()
	}

	if err == nil {
		return nil
	}

	var reason interface{} = err.Error()

	if translatable, ok := err.(i18n.Translatable); ok {
		reason = translatable.I18nMessage()
	}

	return ValidationError{i18n.Msg("validation.championship.import_class_invalid", "The car class \"{class}\" of the import is invalid: {reason}", "class", class.Name, "reason", reason)}
}

// writeImport writes the new car classes, the race weekends and the championship and removes what was written if a
// step fails. The caller holds the car class lock and has validated everything.
func (cm *ChampionshipManager) writeImport(championship *smmodel.Championship, classes []*smmodel.CarClass) error {
	var writtenClasses []*smmodel.CarClass
	var writtenWeekends []string

	rollback := func() {
		for _, id := range writtenWeekends {
			if err := cm.store.DeleteRaceWeekend(id); err != nil {
				logrus.WithError(err).Errorf("could not remove race weekend %s after a failed championship import", id)
			}
		}

		for _, class := range writtenClasses {
			if err := cm.store.DeleteCarClass(class.ID.String()); err != nil {
				logrus.WithError(err).Errorf("could not remove car class %s after a failed championship import", class.ID)
			}
		}
	}

	for _, class := range classes {
		if err := cm.store.UpsertCarClass(class); err != nil {
			rollback()

			return err
		}

		writtenClasses = append(writtenClasses, class)
	}

	for _, event := range championship.Events {
		if event.IsRaceWeekend() {
			if err := cm.store.UpsertRaceWeekend(event.RaceWeekend); err != nil {
				rollback()

				return err
			}

			writtenWeekends = append(writtenWeekends, event.RaceWeekend.ID.String())
		}
	}

	// the lock is held and the car classes were checked above
	if err := cm.store.UpsertChampionship(championship); err != nil {
		rollback()

		return err
	}

	return nil
}

func sameCars(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	set := make(map[string]bool, len(a))

	for _, car := range a {
		set[car] = true
	}

	for _, car := range b {
		if !set[car] {
			return false
		}
	}

	return true
}
