package championship

import (
	"testing"
	"time"

	"github.com/google/uuid"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/server/servertest"
)

func TestCheckCarClassesOnSave(t *testing.T) {
	cm, store, cleanup := setupScheduledEventTest(t, &servertest.Dummy{})
	defer cleanup()

	gt3, _ := smmodel.NewCarClass("GT3", []string{"bmw", "audi"}, time.Now())
	gt4, _ := smmodel.NewCarClass("GT4", []string{"audi", "mini"}, time.Now())

	for _, class := range []*smmodel.CarClass{gt3, gt4} {
		if err := store.UpsertCarClass(class); err != nil {
			t.Fatal(err)
		}
	}

	build := func(a, b *smmodel.ChampionshipClass) *smmodel.Championship {
		championship := smmodel.NewChampionship("c")
		championship.AddClass(a)
		championship.AddClass(b)

		return championship
	}

	classWith := func(name string, ids ...uuid.UUID) *smmodel.ChampionshipClass {
		class := smmodel.NewChampionshipClass(name)
		class.CarClassIDs = ids

		return class
	}

	expectValidation := func(what string, championship *smmodel.Championship) {
		t.Helper()

		if _, ok := cm.checkCarClasses(championship).(ValidationError); !ok {
			t.Errorf("%s: expected a ValidationError", what)
		}
	}

	if err := cm.checkCarClasses(build(classWith("A", gt3.ID), classWith("B"))); err != nil {
		t.Errorf("valid championship refused: %v", err)
	}

	expectValidation("unknown reference", build(classWith("A", uuid.New()), classWith("B")))
	expectValidation("duplicate reference", build(classWith("A", gt3.ID, gt3.ID), classWith("B")))
	expectValidation("car in two classes", build(classWith("A", gt3.ID), classWith("B", gt4.ID)))

	// a deleted car class turns the reference of an existing championship into an error, never into an empty list
	championship := build(classWith("A", gt3.ID), classWith("B"))

	if err := store.UpsertChampionship(championship); err != nil {
		t.Fatal(err)
	}

	if err := store.DeleteCarClass(gt3.ID.String()); err != nil {
		t.Fatal(err)
	}

	loaded, err := cm.LoadChampionship(championship.ID.String())

	if err != nil {
		t.Fatal(err)
	}

	if _, err := loaded.FindClassForCarModel("bmw"); err != smmodel.ErrCarClassUnresolved {
		t.Errorf("expected ErrCarClassUnresolved, got %v", err)
	}
}
