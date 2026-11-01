package store

import (
	"testing"
	"time"

	"github.com/google/uuid"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

func TestUpsertChampionshipChecked(t *testing.T) {
	runStoreContract(t, func(t *testing.T, store Store) {
		now := time.Now()
		gt3, _ := smmodel.NewCarClass("GT3", []string{"bmw", "audi"}, now)
		gt4, _ := smmodel.NewCarClass("GT4", []string{"mini"}, now)

		for _, class := range []*smmodel.CarClass{gt3, gt4} {
			if err := store.UpsertCarClass(class); err != nil {
				t.Fatal(err)
			}
		}

		a := smmodel.NewChampionshipClass("A")
		a.CarClassIDs = []uuid.UUID{gt3.ID}
		b := smmodel.NewChampionshipClass("B")
		b.CarClassIDs = []uuid.UUID{gt4.ID}

		championship := smmodel.NewChampionship("c")
		championship.AddClass(a)
		championship.AddClass(b)

		if err := UpsertChampionshipChecked(store, championship); err != nil {
			t.Fatalf("valid championship refused: %v", err)
		}

		// a stale copy (loaded before the car class changed) must not be written back: the car class now has a car
		// of the other class
		stale, err := store.LoadChampionship(championship.ID.String())

		if err != nil {
			t.Fatal(err)
		}

		gt4.Cars = append(gt4.Cars, "audi")

		if err := store.UpsertCarClass(gt4); err != nil {
			t.Fatal(err)
		}

		err = UpsertChampionshipChecked(store, stale)

		if _, ok := err.(interface{ String() string }); !ok || err == nil {
			t.Fatalf("expected the violation of the car class check, got %v", err)
		}

		// without references nothing is checked
		plain := smmodel.NewChampionship("plain")

		if err := UpsertChampionshipChecked(store, plain); err != nil {
			t.Fatal(err)
		}
	})
}

func TestUpsertChampionshipCheckedWaitsForCarClassChange(t *testing.T) {
	runStoreContract(t, func(t *testing.T, store Store) {
		unlock := LockCarClassWrites()
		done := make(chan error, 1)

		go func() { done <- UpsertChampionshipChecked(store, smmodel.NewChampionship("c")) }()

		select {
		case <-done:
			unlock()
			t.Fatal("the save did not wait for the car class change")
		case <-time.After(100 * time.Millisecond):
		}

		unlock()

		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
}
