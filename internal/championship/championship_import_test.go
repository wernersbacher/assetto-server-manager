package championship

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.etcd.io/bbolt"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
)

// importTestStores runs fn for both store implementations; newStore creates a fresh, empty store each time.
func importTestStores(t *testing.T, fn func(t *testing.T, newStore func() smstore.Store)) {
	t.Run("JSONStore", func(t *testing.T) {
		dir := t.TempDir()
		n := 0

		fn(t, func() smstore.Store {
			n++
			sub := filepath.Join(dir, string(rune('a'+n)))

			return smstore.NewJSONStore(filepath.Join(sub, "store"), filepath.Join(sub, "shared"))
		})
	})

	t.Run("BoltStore", func(t *testing.T) {
		dir := t.TempDir()
		n := 0

		fn(t, func() smstore.Store {
			n++

			db, err := bbolt.Open(filepath.Join(dir, string(rune('a'+n))+".db"), 0644, &bbolt.Options{Timeout: 2 * time.Second})

			if err != nil {
				t.Fatal(err)
			}

			t.Cleanup(func() { _ = db.Close() })

			return smstore.NewBoltStore(db)
		})
	})
}

func importTestChampionship(name string, classes map[string][]uuid.UUID, order ...string) *smmodel.Championship {
	championship := smmodel.NewChampionship(name)

	for _, className := range order {
		class := smmodel.NewChampionshipClass(className)
		class.CarClassIDs = classes[className]
		championship.AddClass(class)
	}

	return championship
}

func exportJSON(t *testing.T, cm *ChampionshipManager, c *smmodel.Championship) string {
	t.Helper()

	export, err := cm.ExportChampionship(c)

	if err != nil {
		t.Fatal(err)
	}

	data, err := json.Marshal(export)

	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

func newImportTestManager(store smstore.Store) *ChampionshipManager {
	return NewChampionshipManager(nil, nil, store, nil, nil, nil)
}

func carClassNames(t *testing.T, store smstore.Store) []string {
	t.Helper()

	classes, err := store.ListCarClasses()

	if err != nil {
		t.Fatal(err)
	}

	var names []string

	for _, class := range classes {
		names = append(names, class.Name)
	}

	return names
}

func TestChampionshipExportImportRoundtrip(t *testing.T) {
	importTestStores(t, func(t *testing.T, newStore func() smstore.Store) {
		source, target := newStore(), newStore()
		sourceCM, targetCM := newImportTestManager(source), newImportTestManager(target)

		gt3 := storeCarClasses(t, source, map[string][]string{"GT3": {"bmw", "audi"}})["GT3"]
		classes := map[string][]uuid.UUID{"A": {gt3.ID}}

		championship := importTestChampionship("Export", classes, "A", "B")

		if err := source.UpsertChampionship(championship); err != nil {
			t.Fatal(err)
		}

		data := exportJSON(t, sourceCM, championship)

		var raw map[string]json.RawMessage

		if err := json.Unmarshal([]byte(data), &raw); err != nil {
			t.Fatal(err)
		}

		if _, ok := raw["CarClassDefinitions"]; !ok || raw["Name"] == nil {
			t.Fatalf("export lacks definitions or the championship fields: %v", raw)
		}

		result, err := targetCM.ImportChampionship(data)

		if err != nil {
			t.Fatal(err)
		}

		if len(result.CreatedCarClasses) != 1 || result.CreatedCarClasses[0] != "GT3" || len(result.Warnings) != 0 {
			t.Errorf("result: %+v", result)
		}

		created, err := target.FindCarClassByID(gt3.ID.String())

		if err != nil {
			t.Fatalf("car class not created with the same ID: %v", err)
		}

		if !sameCars(created.Cars, gt3.Cars) {
			t.Errorf("cars: %v", created.Cars)
		}

		loaded, err := target.LoadChampionship(championship.ID.String())

		if err != nil {
			t.Fatal(err)
		}

		if cars, _ := loaded.Classes[0].ResolveCarIDs(smmodel.NewCarClassMap([]*smmodel.CarClass{created})); len(cars) != 2 {
			t.Errorf("imported class does not resolve its cars: %v", cars)
		}

		// importing again uses the now existing class: no new class, no warning
		again, err := targetCM.ImportChampionship(data)

		if err != nil || len(again.CreatedCarClasses) != 0 || len(again.Warnings) != 0 {
			t.Errorf("second import: %+v, %v", again, err)
		}
	})
}

func TestChampionshipImportExistingClassIsUsedUnchanged(t *testing.T) {
	importTestStores(t, func(t *testing.T, newStore func() smstore.Store) {
		source, target := newStore(), newStore()

		exported := storeCarClasses(t, source, map[string][]string{"GT3": {"bmw", "audi"}})["GT3"]

		// the same ID with another name and other cars exists on the target
		local := *exported
		local.Name = "Local GT3"
		local.Cars = []string{"bmw", "porsche"}

		if err := target.UpsertCarClass(&local); err != nil {
			t.Fatal(err)
		}

		championship := importTestChampionship("Same ID", map[string][]uuid.UUID{"A": {exported.ID}}, "A")
		data := exportJSON(t, newImportTestManager(source), championship)

		result, err := newImportTestManager(target).ImportChampionship(data)

		if err != nil {
			t.Fatal(err)
		}

		if len(result.CreatedCarClasses) != 0 || len(result.Warnings) != 1 || result.Warnings[0].ClassName != "Local GT3" {
			t.Errorf("result: %+v", result)
		}

		after, _ := target.FindCarClassByID(exported.ID.String())

		if after.Name != "Local GT3" || !sameCars(after.Cars, []string{"bmw", "porsche"}) {
			t.Errorf("existing class was changed: %+v", after)
		}
	})
}

func TestChampionshipImportNameCollisionWritesNothing(t *testing.T) {
	importTestStores(t, func(t *testing.T, newStore func() smstore.Store) {
		source, target := newStore(), newStore()

		storeCarClasses(t, target, map[string][]string{"gt3 ": {"mini"}})

		gt3 := storeCarClasses(t, source, map[string][]string{"GT3": {"bmw"}, "GT4": {"audi"}})
		championship := importTestChampionship("Collision", map[string][]uuid.UUID{"A": {gt3["GT4"].ID}, "B": {gt3["GT3"].ID}}, "A", "B")
		data := exportJSON(t, newImportTestManager(source), championship)

		_, err := newImportTestManager(target).ImportChampionship(data)

		var validation ValidationError

		if !errors.As(err, &validation) || validation.Key != "validation.championship.import_class_name_taken" {
			t.Fatalf("expected the name collision, got %v", err)
		}

		if names := carClassNames(t, target); len(names) != 1 {
			t.Errorf("car classes after refused import: %v", names)
		}

		if all, _ := target.ListChampionships(); len(all) != 0 {
			t.Errorf("championship saved: %d", len(all))
		}
	})
}

func TestChampionshipImportCarInTwoClassesWritesNothing(t *testing.T) {
	importTestStores(t, func(t *testing.T, newStore func() smstore.Store) {
		source, target := newStore(), newStore()

		classes := storeCarClasses(t, source, map[string][]string{"GT3": {"bmw", "audi"}, "GT4": {"audi", "mini"}})
		championship := importTestChampionship("Overlap", map[string][]uuid.UUID{"A": {classes["GT3"].ID}, "B": {classes["GT4"].ID}}, "A", "B")

		data := exportJSON(t, newImportTestManager(source), championship)

		_, err := newImportTestManager(target).ImportChampionship(data)

		if _, ok := err.(ValidationError); !ok {
			t.Fatalf("expected a ValidationError, got %v", err)
		}

		if names := carClassNames(t, target); len(names) != 0 {
			t.Errorf("car classes left behind: %v", names)
		}

		if all, _ := target.ListChampionships(); len(all) != 0 {
			t.Errorf("championship saved")
		}
	})
}

func TestChampionshipImportWithoutDefinitions(t *testing.T) {
	importTestStores(t, func(t *testing.T, newStore func() smstore.Store) {
		target := newStore()
		cm := newImportTestManager(target)

		// old export without references
		plain, _ := json.Marshal(importTestChampionship("Old", nil, "A"))

		if _, err := cm.ImportChampionship(string(plain)); err != nil {
			t.Fatalf("old export: %v", err)
		}

		// old export that refers to a class the target lacks: refused as before
		missing, _ := json.Marshal(importTestChampionship("Old2", map[string][]uuid.UUID{"A": {uuid.New()}}, "A"))

		if _, err := cm.ImportChampionship(string(missing)); err == nil {
			t.Fatal("expected an error for a missing car class without definition")
		} else if _, ok := err.(ValidationError); !ok {
			t.Fatalf("expected a ValidationError, got %v", err)
		}

		// old export referring to an existing class
		existing := storeCarClasses(t, target, map[string][]string{"GT3": {"bmw"}})["GT3"]
		known, _ := json.Marshal(importTestChampionship("Old3", map[string][]uuid.UUID{"A": {existing.ID}}, "A"))

		if _, err := cm.ImportChampionship(string(known)); err != nil {
			t.Fatalf("old export with existing class: %v", err)
		}

		if _, err := cm.ImportChampionship("null"); err == nil {
			t.Error("null must be refused")
		}
	})
}

// failingStore fails the championship save.
type failingStore struct {
	smstore.Store
}

func (failingStore) UpsertChampionship(*smmodel.Championship) error {
	return errors.New("disk full")
}

func TestChampionshipImportFailedSaveLeavesNothingBehind(t *testing.T) {
	importTestStores(t, func(t *testing.T, newStore func() smstore.Store) {
		source, target := newStore(), newStore()

		gt3 := storeCarClasses(t, source, map[string][]string{"GT3": {"bmw"}})["GT3"]
		championship := importTestChampionship("Fail", map[string][]uuid.UUID{"A": {gt3.ID}}, "A")

		weekend := smmodel.NewRaceWeekend()
		weekend.Name = "Weekend"

		if _, err := championship.ImportEvent(weekend); err != nil {
			t.Fatal(err)
		}

		data := exportJSON(t, newImportTestManager(source), championship)

		cm := newImportTestManager(failingStore{target})

		if _, err := cm.ImportChampionship(data); err == nil || err.Error() != "disk full" {
			t.Fatalf("expected the save error, got %v", err)
		}

		if names := carClassNames(t, target); len(names) != 0 {
			t.Errorf("new car classes left behind: %v", names)
		}

		if weekends, _ := target.ListRaceWeekends(); len(weekends) != 0 {
			t.Errorf("orphaned race weekends: %d", len(weekends))
		}

		// the locks were released: a regular import works afterwards
		if _, err := newImportTestManager(target).ImportChampionship(data); err != nil {
			t.Fatalf("import after failure: %v", err)
		}

		if weekends, _ := target.ListRaceWeekends(); len(weekends) != 1 {
			t.Errorf("race weekends after the successful import: %d", len(weekends))
		}
	})
}

func TestChampionshipImportInvalidClassDefinition(t *testing.T) {
	target := smstore.NewJSONStore(t.TempDir(), t.TempDir())
	cm := newImportTestManager(target)

	id := uuid.New()
	data := `{"ID":"` + uuid.New().String() + `","Name":"Bad","Classes":[{"ID":"` + uuid.New().String() + `","Name":"A","CarClassIDs":["` + id.String() + `"]}],"CarClassDefinitions":[{"ID":"` + id.String() + `","Name":"","Cars":["bmw"]}]}`

	_, err := cm.ImportChampionship(data)

	var validation ValidationError

	if !errors.As(err, &validation) || validation.Key != "validation.championship.import_class_invalid" {
		t.Fatalf("expected import_class_invalid, got %v", err)
	}
}
