package carclasses

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.etcd.io/bbolt"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	"github.com/wernersbacher/ACPitwall/internal/championship"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smresults "github.com/wernersbacher/ACPitwall/internal/results"
	"github.com/wernersbacher/ACPitwall/internal/serverenv/envtest"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
)

// forEachStore runs the test against both Store implementations.
func forEachStore(t *testing.T, fn func(t *testing.T, store smstore.Store)) {
	t.Run("JSONStore", func(t *testing.T) {
		store, cleanup := envtest.TempJSONStore()
		defer cleanup()

		fn(t, store)
	})

	t.Run("BoltStore", func(t *testing.T) {
		db, err := bbolt.Open(filepath.Join(t.TempDir(), "test.db"), 0644, &bbolt.Options{Timeout: 2 * time.Second})

		if err != nil {
			t.Fatal(err)
		}

		defer db.Close()

		fn(t, smstore.NewBoltStore(db))
	})
}

func mustCreate(t *testing.T, m *Manager, name string, cars ...string) *smmodel.CarClass {
	t.Helper()

	class, err := m.Create(name, cars)

	if err != nil {
		t.Fatalf("create %q: %v", name, err)
	}

	return class
}

// saveChampionship stores a championship with one championship class per given car class (named "class-<n>") and
// returns it. Further changes of the championship are made by the caller before it is stored again.
func saveChampionship(t *testing.T, store smstore.Store, name string, refs ...*smmodel.CarClass) *smmodel.Championship {
	t.Helper()

	c := smmodel.NewChampionship(name)

	for _, ref := range refs {
		class := smmodel.NewChampionshipClass("for-" + ref.Name)
		class.CarClassIDs = []uuid.UUID{ref.ID}
		c.AddClass(class)
	}

	if err := store.UpsertChampionship(c); err != nil {
		t.Fatal(err)
	}

	return c
}

func expectError(t *testing.T, what string, err error, contains ...string) {
	t.Helper()

	var managerErr Error

	if !errors.As(err, &managerErr) {
		t.Fatalf("%s: expected an Error, got %v", what, err)
	}

	for _, s := range contains {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("%s: error %q does not mention %q", what, err, s)
		}
	}
}

func TestCreateValidatesNameAndCars(t *testing.T) {
	forEachStore(t, func(t *testing.T, store smstore.Store) {
		m := NewManager(store)

		mustCreate(t, m, "GT3", "bmw", "audi")

		for _, c := range []struct {
			what string
			name string
			cars []string
		}{
			{"empty name", "  ", []string{"a"}},
			{"taken name", " gt3 ", []string{"a"}},
			{"no cars", "X", nil},
			{"empty car", "X", []string{"a", " "}},
			{"duplicate car", "X", []string{"a", "a"}},
		} {
			_, err := m.Create(c.name, c.cars)
			expectError(t, c.what, err)
		}

		classes, err := m.List()

		if err != nil || len(classes) != 1 {
			t.Fatalf("invalid classes must not be stored: %v, %d classes", err, len(classes))
		}
	})
}

func TestUpdateRenameAndListOrder(t *testing.T) {
	forEachStore(t, func(t *testing.T, store smstore.Store) {
		m := NewManager(store)

		zed := mustCreate(t, m, "zed", "a")
		mustCreate(t, m, "Alpha", "b")

		updated, err := m.Update(zed.ID.String(), "Beta", []string{"a", "c"})

		if err != nil {
			t.Fatal(err)
		}

		if updated.Name != "Beta" || len(updated.Cars) != 2 || !updated.Created.Equal(zed.Created) {
			t.Errorf("unexpected update result: %+v", updated)
		}

		classes, _ := m.List()

		if len(classes) != 2 || classes[0].Name != "Alpha" || classes[1].Name != "Beta" {
			t.Errorf("expected classes sorted by name, got %+v", classes)
		}

		_, err = m.Update(zed.ID.String(), "alpha", []string{"a"})
		expectError(t, "rename to a taken name", err)

		if _, err := m.Update(uuid.New().String(), "x", []string{"a"}); err != ErrNotFound {
			t.Errorf("expected ErrNotFound, got %v", err)
		}

		if _, err := m.Get(zed.ID.String()); err != nil {
			t.Error(err)
		}
	})
}

func TestDeleteReferencedClassIsRefused(t *testing.T) {
	forEachStore(t, func(t *testing.T, store smstore.Store) {
		m := NewManager(store)

		used := mustCreate(t, m, "Used", "a")
		free := mustCreate(t, m, "Free", "b")

		saveChampionship(t, store, "Season 1", used)
		saveChampionship(t, store, "Season 2", used)

		err := m.Delete(used.ID.String())
		expectError(t, "delete referenced", err, "Season 1", "Season 2", "Used")

		if _, err := m.Get(used.ID.String()); err != nil {
			t.Errorf("a refused delete must keep the class: %v", err)
		}

		if err := m.Delete(free.ID.String()); err != nil {
			t.Fatal(err)
		}

		if err := m.Delete(free.ID.String()); err != ErrNotFound {
			t.Errorf("expected ErrNotFound, got %v", err)
		}

		usages, err := m.Usages(used.ID.String())

		if err != nil || len(usages) != 2 {
			t.Fatalf("expected 2 usages, got %v, %v", usages, err)
		}

		if usages[0].ClassName != "for-Used" {
			t.Errorf("unexpected usage %+v", usages[0])
		}

		if usages, _ := m.Usages(free.ID.String()); err == nil && usages != nil {
			t.Error("deleted class has no usages")
		}
	})
}

func TestUpdateCannotPutACarIntoTwoClasses(t *testing.T) {
	forEachStore(t, func(t *testing.T, store smstore.Store) {
		m := NewManager(store)

		gt3 := mustCreate(t, m, "GT3", "bmw", "audi")
		gt4 := mustCreate(t, m, "GT4", "mini")

		saveChampionship(t, store, "Both", gt3, gt4)
		// a championship which only uses GT3 does not mind that GT4 gets the BMW
		saveChampionship(t, store, "Only GT3", gt3)

		_, err := m.Update(gt4.ID.String(), "GT4", []string{"mini", "bmw"})
		expectError(t, "car in two classes", err, "bmw", "Both")

		if strings.Contains(err.Error(), "Only GT3") {
			t.Errorf("unrelated championship named: %v", err)
		}

		stored, _ := m.Get(gt4.ID.String())

		if len(stored.Cars) != 1 {
			t.Errorf("refused update changed the class: %v", stored.Cars)
		}

		// a car which no championship has in another class can be added
		if _, err := m.Update(gt4.ID.String(), "GT4", []string{"mini", "porsche"}); err != nil {
			t.Errorf("valid update refused: %v", err)
		}
	})
}

func TestUpdateCannotRemoveCarsInUse(t *testing.T) {
	forEachStore(t, func(t *testing.T, store smstore.Store) {
		m := NewManager(store)

		class := mustCreate(t, m, "GT3", "bmw", "audi", "ferrari", "porsche")

		c := saveChampionship(t, store, "Season", class)
		cc := c.Classes[0]

		entrant := serverconfig.NewEntrant()
		entrant.Model = "bmw"
		cc.Entrants["0"] = entrant

		c.SignUpForm.Responses = []*smmodel.ChampionshipSignUpResponse{
			{GUID: "1", Car: "audi", Status: smmodel.ChampionshipEntrantAccepted},
			{GUID: "2", Car: "ferrari", Status: smmodel.ChampionshipEntrantPending},
			{GUID: "3", Car: "porsche", Status: smmodel.ChampionshipEntrantRejected},
		}

		if err := store.UpsertChampionship(c); err != nil {
			t.Fatal(err)
		}

		update := func(cars ...string) error {
			_, err := m.Update(class.ID.String(), "GT3", cars)

			return err
		}

		expectError(t, "entrant", update("audi", "ferrari", "porsche"), "bmw", "Season", "entrants")
		expectError(t, "accepted sign up", update("bmw", "ferrari", "porsche"), "audi", "registered")
		expectError(t, "pending sign up", update("bmw", "audi", "porsche"), "ferrari", "registered")

		// a rejected registration is no use, so is a car nobody has chosen
		if err := update("bmw", "audi", "ferrari"); err != nil {
			t.Errorf("removing an unused car refused: %v", err)
		}

		// results keep a car in the class, also in a championship that is finished
		class, _ = m.Get(class.ID.String())

		c, _ = store.LoadChampionship(c.ID.String())
		c.Classes[0].Entrants = make(serverconfig.EntryList)
		c.SignUpForm.Responses = nil

		event := smmodel.NewChampionshipEvent()
		event.CompletedTime = time.Now()
		event.Sessions = map[serverconfig.SessionType]*smmodel.ChampionshipSession{
			serverconfig.SessionTypeRace: {Results: &smresults.SessionResults{
				Result: []*smresults.SessionResult{{CarModel: "ferrari", ClassID: c.Classes[0].ID}},
			}},
		}
		c.Events = append(c.Events, event)

		if err := store.UpsertChampionship(c); err != nil {
			t.Fatal(err)
		}

		expectError(t, "results", update("bmw", "audi"), "ferrari", "results")

		// results of the same car in another class do not matter
		event.Sessions[serverconfig.SessionTypeRace].Results.Result[0].ClassID = uuid.New()

		if err := store.UpsertChampionship(c); err != nil {
			t.Fatal(err)
		}

		if err := update("bmw", "audi"); err != nil {
			t.Errorf("results of another class must not block: %v", err)
		}
	})
}

func TestUpdateKeepsCarsOfTheClassThatAreAlsoAvailableDirectly(t *testing.T) {
	forEachStore(t, func(t *testing.T, store smstore.Store) {
		m := NewManager(store)

		class := mustCreate(t, m, "GT3", "bmw", "audi")

		c := saveChampionship(t, store, "Season", class)
		c.Classes[0].AvailableCars = []string{"bmw"}

		entrant := serverconfig.NewEntrant()
		entrant.Model = "bmw"
		c.Classes[0].Entrants["0"] = entrant

		if err := store.UpsertChampionship(c); err != nil {
			t.Fatal(err)
		}

		// the BMW stays available through the championship class itself
		if _, err := m.Update(class.ID.String(), "GT3", []string{"audi"}); err != nil {
			t.Errorf("car which stays available was treated as removed: %v", err)
		}
	})
}

// A championship save that was validated before a car class change must not make the championship invalid. Saves and
// changes run concurrently; whatever the order is, the stored championship must satisfy CheckCarClasses afterwards.
func TestConcurrentSavesAndUpdatesKeepChampionshipsValid(t *testing.T) {
	forEachStore(t, func(t *testing.T, store smstore.Store) {
		m := NewManager(store)
		cm := championship.NewChampionshipManager(nil, nil, store, nil, nil, nil)

		gt3 := mustCreate(t, m, "GT3", "bmw")
		gt4 := mustCreate(t, m, "GT4", "mini")

		saved := saveChampionship(t, store, "Season", gt3, gt4)

		var wg sync.WaitGroup

		for i := 0; i < 20; i++ {
			wg.Add(2)

			go func() {
				defer wg.Done()

				_, _ = m.Update(gt4.ID.String(), "GT4", []string{"mini", "bmw"})
				_, _ = m.Update(gt4.ID.String(), "GT4", []string{"mini"})
			}()

			go func() {
				defer wg.Done()

				c, err := store.LoadChampionship(saved.ID.String())

				if err != nil {
					t.Error(err)
					return
				}

				// the class references are the same, only their cars change underneath
				_ = cm.UpsertChampionship(c)
			}()
		}

		wg.Wait()

		c, err := store.LoadChampionship(saved.ID.String())

		if err != nil {
			t.Fatal(err)
		}

		classes, _ := store.ListCarClasses()

		if err := c.CheckCarClasses(smmodel.NewCarClassMap(classes)); err != nil {
			t.Errorf("championship is invalid after concurrent changes: %v", err)
		}
	})
}

// A save which refers to a car class that now conflicts is refused by the championship manager itself.
func TestChampionshipSaveIsCheckedAgainstCurrentCarClasses(t *testing.T) {
	forEachStore(t, func(t *testing.T, store smstore.Store) {
		m := NewManager(store)
		cm := championship.NewChampionshipManager(nil, nil, store, nil, nil, nil)

		gt3 := mustCreate(t, m, "GT3", "bmw")
		gt4 := mustCreate(t, m, "GT4", "mini")

		c := saveChampionship(t, store, "Season", gt3)

		// stale copy: loaded before GT4 was added to the championship and changed
		stale, err := store.LoadChampionship(c.ID.String())

		if err != nil {
			t.Fatal(err)
		}

		other := smmodel.NewChampionshipClass("other")
		other.CarClassIDs = []uuid.UUID{gt4.ID}
		stale.AddClass(other)

		if _, err := m.Update(gt4.ID.String(), "GT4", []string{"mini", "bmw"}); err != nil {
			t.Fatal(err) // no championship uses GT4 yet
		}

		if err := cm.UpsertChampionship(stale); err == nil {
			t.Error("expected the save to be refused: the BMW is now in two classes")
		}
	})
}

// A deleted championship (soft delete) is no longer listed, so it does not block changing or deleting a car class it
// refers to. Saving it again (it can still be opened by its ID) is checked like any other save.
func TestDeletedChampionshipDoesNotBlockCarClassChanges(t *testing.T) {
	forEachStore(t, func(t *testing.T, store smstore.Store) {
		m := NewManager(store)
		cm := championship.NewChampionshipManager(nil, nil, store, nil, nil, nil)

		class := mustCreate(t, m, "Used", "a")
		c := saveChampionship(t, store, "Old season", class)

		if err := m.Delete(class.ID.String()); err == nil {
			t.Fatal("a class of a live championship must not be deleted")
		}

		if err := store.DeleteChampionship(c.ID.String()); err != nil {
			t.Fatal(err)
		}

		if _, err := m.Update(class.ID.String(), "Used", []string{"b"}); err != nil {
			t.Fatalf("update with only a deleted championship: %v", err)
		}

		if err := m.Delete(class.ID.String()); err != nil {
			t.Fatalf("delete with only a deleted championship: %v", err)
		}

		// the deleted championship loads, but cannot be saved with the missing car class
		loaded, err := store.LoadChampionship(c.ID.String())

		if err != nil {
			t.Fatal(err)
		}

		if err := cm.UpsertChampionship(loaded); err == nil {
			t.Error("saving a championship that refers to a missing car class must be refused")
		}
	})
}
