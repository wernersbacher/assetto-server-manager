package store

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

func TestStoreContract_CarClasses(t *testing.T) {
	runStoreContract(t, func(t *testing.T, store Store) {
		if classes, err := store.ListCarClasses(); err != nil || len(classes) != 0 {
			t.Fatalf("list on empty store: %v, %d classes", err, len(classes))
		}

		for _, id := range []string{uuid.New().String(), "../etc/passwd", ""} {
			if _, err := store.FindCarClassByID(id); err != smmodel.ErrCarClassNotFound {
				t.Errorf("find %q: expected ErrCarClassNotFound, got %v", id, err)
			}

			if err := store.DeleteCarClass(id); err != smmodel.ErrCarClassNotFound {
				t.Errorf("delete %q: expected ErrCarClassNotFound, got %v", id, err)
			}
		}

		now := time.Now()
		zed, _ := smmodel.NewCarClass("zed", []string{"car_b", "car_a"}, now)
		alpha, _ := smmodel.NewCarClass("Alpha", []string{"car_c"}, now)

		for _, class := range []*smmodel.CarClass{zed, alpha} {
			if err := store.UpsertCarClass(class); err != nil {
				t.Fatal(err)
			}

			if class.Updated.IsZero() {
				t.Error("expected Updated to be set")
			}
		}

		found, err := store.FindCarClassByID(zed.ID.String())

		if err != nil {
			t.Fatal(err)
		}

		if found.ID != zed.ID || found.Name != "zed" || len(found.Cars) != 2 || found.Cars[0] != "car_b" || found.Cars[1] != "car_a" {
			t.Errorf("round trip lost data: %+v", found)
		}

		classes, err := store.ListCarClasses()

		if err != nil || len(classes) != 2 || classes[0].Name != "Alpha" || classes[1].Name != "zed" {
			t.Fatalf("expected classes sorted by name, got %v, %v", classes, err)
		}

		zed.Name = "Beta"
		zed.Cars = []string{"car_z"}

		if err := store.UpsertCarClass(zed); err != nil {
			t.Fatal(err)
		}

		found, _ = store.FindCarClassByID(zed.ID.String())

		if found.Name != "Beta" || len(found.Cars) != 1 {
			t.Errorf("update not stored: %+v", found)
		}

		if classes, _ := store.ListCarClasses(); len(classes) != 2 {
			t.Errorf("update must not add a class, got %d", len(classes))
		}

		if err := store.DeleteCarClass(alpha.ID.String()); err != nil {
			t.Fatal(err)
		}

		if _, err := store.FindCarClassByID(alpha.ID.String()); err != smmodel.ErrCarClassNotFound {
			t.Errorf("expected ErrCarClassNotFound after delete, got %v", err)
		}

		if classes, _ := store.ListCarClasses(); len(classes) != 1 {
			t.Errorf("expected 1 class, got %d", len(classes))
		}

		if err := store.DeleteCarClass(alpha.ID.String()); err != smmodel.ErrCarClassNotFound {
			t.Errorf("second delete: %v", err)
		}
	})
}

// TestStoreContract_ChampionshipCarClassLookup checks that loaded championships resolve their car class references
// live, from the car classes as they are when the championship is loaded.
func TestStoreContract_ChampionshipCarClassLookup(t *testing.T) {
	runStoreContract(t, func(t *testing.T, store Store) {
		gt3, _ := smmodel.NewCarClass("GT3", []string{"bmw", "audi"}, time.Now())

		if err := store.UpsertCarClass(gt3); err != nil {
			t.Fatal(err)
		}

		championship := smmodel.NewChampionship("c")
		class := smmodel.NewChampionshipClass("A")
		class.AvailableCars = []string{"fiat"}
		class.CarClassIDs = []uuid.UUID{gt3.ID, uuid.New()}
		championship.AddClass(class)

		if err := store.UpsertChampionship(championship); err != nil {
			t.Fatal(err)
		}

		check := func(what string, c *smmodel.Championship, expected string) {
			t.Helper()

			cars, err := c.Classes[0].ResolveCarIDs(nil)

			if err == nil {
				t.Errorf("%s: the unknown reference must be reported", what)
			}

			if strings.Join(cars, ",") != expected {
				t.Errorf("%s: expected %s, got %v", what, expected, cars)
			}
		}

		loaded, err := store.LoadChampionship(championship.ID.String())

		if err != nil {
			t.Fatal(err)
		}

		check("load", loaded, "fiat,bmw,audi")

		// the car class changes: the next load sees it
		gt3.Cars = []string{"porsche"}

		if err := store.UpsertCarClass(gt3); err != nil {
			t.Fatal(err)
		}

		listed, err := store.ListChampionships()

		if err != nil || len(listed) != 1 {
			t.Fatalf("list: %v, %d", err, len(listed))
		}

		check("list", listed[0], "fiat,porsche")

		loaded, err = store.LoadChampionship(championship.ID.String())

		if err != nil {
			t.Fatal(err)
		}

		check("reload", loaded, "fiat,porsche")
	})
}
