package model

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	"github.com/wernersbacher/ACPitwall/internal/i18n"
)

func testCarClass(t *testing.T, name string, cars ...string) *CarClass {
	t.Helper()

	class, err := NewCarClass(name, cars, time.Now())

	if err != nil {
		t.Fatal(err)
	}

	return class
}

func testChampionshipClass(name string, cars []string, refs ...*CarClass) *ChampionshipClass {
	class := NewChampionshipClass(name)
	class.AvailableCars = cars

	for _, ref := range refs {
		class.CarClassIDs = append(class.CarClassIDs, ref.ID)
	}

	return class
}

func TestChampionshipClassResolvesCarClasses(t *testing.T) {
	gt3 := testCarClass(t, "GT3", "bmw", "audi")
	gt4 := testCarClass(t, "GT4", "audi", "mini")

	championship := NewChampionship("c")
	championship.SetCarClassLookup(NewCarClassMap([]*CarClass{gt3, gt4}))

	class := testChampionshipClass("A", []string{"ferrari", "bmw"}, gt3, gt4)
	championship.AddClass(class)

	expected := []string{"ferrari", "bmw", "audi", "mini"}

	if got := class.ValidCarIDs(); !reflect.DeepEqual(got, expected) {
		t.Errorf("class: expected %v, got %v", expected, got)
	}

	if got := championship.ValidCarIDs(); !reflect.DeepEqual(got, expected) {
		t.Errorf("championship: expected %v, got %v", expected, got)
	}

	if found, err := championship.FindClassForCarModel("mini"); err != nil || found != class {
		t.Errorf("mini should be found in the class through its reference: %v", err)
	}

	// live: a changed car class applies at once
	gt4.Cars = []string{"audi", "porsche"}

	if _, err := championship.FindClassForCarModel("mini"); err != ErrClassNotFound {
		t.Errorf("mini was removed from the car class, expected ErrClassNotFound, got %v", err)
	}

	if _, err := championship.FindClassForCarModel("porsche"); err != nil {
		t.Errorf("porsche was added to the car class: %v", err)
	}
}

func TestChampionshipClassOnlyReferences(t *testing.T) {
	gt3 := testCarClass(t, "GT3", "bmw")

	championship := NewChampionship("c")
	championship.SetCarClassLookup(NewCarClassMap([]*CarClass{gt3}))

	class := testChampionshipClass("A", nil, gt3)
	// entrant models do not widen a class that has references (they are checked by CheckCarClasses)
	class.Entrants = serverconfig.EntryList{"0": &serverconfig.Entrant{Model: serverconfig.AnyCarModel}}
	championship.AddClass(class)

	if got := class.ValidCarIDs(); !reflect.DeepEqual(got, []string{"bmw"}) {
		t.Errorf("expected only the car of the reference, got %v", got)
	}
}

func TestChampionshipClassWithoutReferencesIsUnchanged(t *testing.T) {
	class := NewChampionshipClass("A")
	class.AvailableCars = []string{"a", "b"}

	if got := class.ValidCarIDs(); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("available cars: %v", got)
	}

	// no available cars: the models of the entrants, no lookup needed
	class = NewChampionshipClass("B")
	class.Entrants = serverconfig.EntryList{
		"0": &serverconfig.Entrant{Model: "z"},
		"1": &serverconfig.Entrant{Model: "y"},
		"2": &serverconfig.Entrant{Model: "z"},
	}

	if got := class.ValidCarIDs(); !reflect.DeepEqual(got, []string{"y", "z"}) {
		t.Errorf("entrant models: %v", got)
	}

	championship := NewChampionship("c")
	championship.AddClass(class)

	if err := championship.CheckCarClasses(nil); err != nil {
		t.Errorf("no references and no lookup must be fine: %v", err)
	}
}

func TestChampionshipValidCarIDsDeduplicatesAndKeepsOrder(t *testing.T) {
	championship := NewChampionship("c")
	championship.AddClass(testChampionshipClass("A", []string{"c", "a", "c"}))
	championship.AddClass(testChampionshipClass("B", []string{"b", "a"}))
	championship.SpectatorCarEnabled = true
	championship.SpectatorCar = serverconfig.Entrant{GUID: "1", Model: "spec"}

	for i := 0; i < 20; i++ {
		if got := championship.ValidCarIDs(); !reflect.DeepEqual(got, []string{"c", "a", "b", "spec"}) {
			t.Fatalf("unstable or duplicated: %v", got)
		}
	}
}

func TestMissingCarClassReference(t *testing.T) {
	gt3 := testCarClass(t, "GT3", "bmw")
	missing := uuid.New()

	for name, lookup := range map[string]CarClassLookup{"no lookup": nil, "unknown id": NewCarClassMap(nil)} {
		t.Run(name, func(t *testing.T) {
			championship := NewChampionship("c")

			if lookup != nil {
				championship.SetCarClassLookup(lookup)
			}

			class := testChampionshipClass("A", []string{"audi"})
			class.CarClassIDs = []uuid.UUID{gt3.ID, missing}
			other := testChampionshipClass("B", []string{"mini"})
			championship.AddClass(class)
			championship.AddClass(other)

			cars, err := class.ResolveCarIDs(nil)

			if err == nil || !strings.Contains(err.Error(), gt3.ID.String()) {
				t.Fatalf("expected an error for the missing reference, got %v", err)
			}

			if !reflect.DeepEqual(cars, []string{"audi"}) && !reflect.DeepEqual(cars, []string{"audi", "bmw"}) {
				t.Errorf("resolved part: %v", cars)
			}

			// a car of the unresolvable class must never end up in another class, nor in "no class" silently
			if _, err := championship.FindClassForCarModel("bmw"); err != ErrCarClassUnresolved {
				t.Errorf("expected ErrCarClassUnresolved, got %v", err)
			}

			if found, err := championship.FindClassForCarModel("mini"); err != nil || found != other {
				t.Errorf("a car of a resolvable class is still found: %v", err)
			}

			if err := championship.CheckCarClasses(nil); err == nil {
				t.Error("the missing reference must be refused")
			} else if msg, ok := err.(i18n.Message); !ok || msg.Key != "validation.championship.car_class_missing" {
				t.Errorf("unexpected error %#v", err)
			}
		})
	}
}

func TestCheckCarClasses(t *testing.T) {
	gt3 := testCarClass(t, "GT3", "bmw", "audi")
	gt4 := testCarClass(t, "GT4", "audi", "mini")
	lookup := NewCarClassMap([]*CarClass{gt3, gt4})

	build := func(classes ...*ChampionshipClass) *Championship {
		championship := NewChampionship("c")
		championship.SetCarClassLookup(lookup)

		for _, class := range classes {
			championship.AddClass(class)
		}

		return championship
	}

	keyOf := func(err error) string {
		if msg, ok := err.(i18n.Message); ok {
			return msg.Key
		}

		return ""
	}

	t.Run("valid", func(t *testing.T) {
		championship := build(testChampionshipClass("A", []string{"ferrari"}, gt3), testChampionshipClass("B", []string{"fiat"}))

		if err := championship.CheckCarClasses(lookup); err != nil {
			t.Error(err)
		}
	})

	t.Run("same class listed twice", func(t *testing.T) {
		class := testChampionshipClass("A", nil, gt3, gt3)

		if err := build(class).CheckCarClasses(lookup); keyOf(err) != "validation.championship.car_class_duplicate" {
			t.Errorf("got %v", err)
		}
	})

	t.Run("car in two classes through references", func(t *testing.T) {
		championship := build(testChampionshipClass("A", nil, gt3), testChampionshipClass("B", nil, gt4))

		err := championship.CheckCarClasses(lookup)

		if keyOf(err) != "validation.championship.car_in_two_classes" || !strings.Contains(err.Error(), "audi") {
			t.Errorf("got %v", err)
		}
	})

	t.Run("car in two classes through available cars and reference", func(t *testing.T) {
		championship := build(testChampionshipClass("A", []string{"mini"}), testChampionshipClass("B", nil, gt4))

		if err := championship.CheckCarClasses(lookup); keyOf(err) != "validation.championship.car_in_two_classes" {
			t.Errorf("got %v", err)
		}
	})

	t.Run("car in two classes through entrant model", func(t *testing.T) {
		a := testChampionshipClass("A", []string{"fiat"})
		b := testChampionshipClass("B", []string{"vw"})
		b.Entrants = serverconfig.EntryList{"0": &serverconfig.Entrant{Model: "fiat"}}

		if err := build(a, b).CheckCarClasses(lookup); keyOf(err) != "validation.championship.car_in_two_classes" {
			t.Errorf("got %v", err)
		}
	})

	t.Run("the same car twice within a class is fine, as is AnyCarModel in several classes", func(t *testing.T) {
		a := testChampionshipClass("A", []string{"fiat", "fiat"})
		a.Entrants = serverconfig.EntryList{"0": &serverconfig.Entrant{Model: serverconfig.AnyCarModel}, "1": &serverconfig.Entrant{Model: "fiat"}}
		b := testChampionshipClass("B", []string{"vw"})
		b.Entrants = serverconfig.EntryList{"0": &serverconfig.Entrant{Model: serverconfig.AnyCarModel}}

		if err := build(a, b).CheckCarClasses(lookup); err != nil {
			t.Error(err)
		}
	})
}

// TestCarClassIDsCompatibility checks that championships stored before CarClassIDs existed still decode, and that the
// runtime lookup never ends up in the persisted data.
func TestCarClassIDsCompatibility(t *testing.T) {
	gt3 := testCarClass(t, "GT3", "bmw")

	championship := NewChampionship("c")
	championship.SetCarClassLookup(NewCarClassMap([]*CarClass{gt3}))
	championship.AddClass(testChampionshipClass("A", []string{"audi"}, gt3))

	data, err := json.Marshal(championship)

	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(strings.ToLower(string(data)), "lookup") || strings.Contains(string(data), "carClasses") {
		t.Errorf("the resolver must not be persisted: %s", data)
	}

	var fromJSON Championship

	if err := json.Unmarshal(data, &fromJSON); err != nil {
		t.Fatal(err)
	}

	if len(fromJSON.Classes) != 1 || len(fromJSON.Classes[0].CarClassIDs) != 1 || fromJSON.Classes[0].CarClassIDs[0] != gt3.ID {
		t.Errorf("references lost in JSON: %+v", fromJSON.Classes)
	}

	buf := new(bytes.Buffer)

	if err := gob.NewEncoder(buf).Encode(championship); err != nil {
		t.Fatal(err)
	}

	var fromGob Championship

	if err := gob.NewDecoder(buf).Decode(&fromGob); err != nil {
		t.Fatal(err)
	}

	if len(fromGob.Classes[0].CarClassIDs) != 1 || fromGob.Classes[0].carClasses != nil {
		t.Errorf("gob round trip: %+v", fromGob.Classes[0])
	}

	// old data: no CarClassIDs
	var legacy Championship

	if err := json.Unmarshal([]byte(`{"Classes":[{"Name":"A","AvailableCars":["x","y"]}]}`), &legacy); err != nil {
		t.Fatal(err)
	}

	if got := legacy.ValidCarIDs(); !reflect.DeepEqual(got, []string{"x", "y"}) {
		t.Errorf("legacy championship: %v", got)
	}
}

func TestFindClassNeverGuessesWithoutLookup(t *testing.T) {
	gt3 := testCarClass(t, "GT3", "bmw")

	championship := NewChampionship("c")
	// no lookup: the reference of class A cannot be resolved
	a := testChampionshipClass("A", nil, gt3)
	b := testChampionshipClass("B", []string{"mini"})
	championship.AddClass(a)
	championship.AddClass(b)

	if _, err := championship.FindClassForCarModel("bmw"); err != ErrCarClassUnresolved {
		t.Fatalf("expected ErrCarClassUnresolved, got %v", err)
	}

	if found, err := championship.FindClassForCarModel("mini"); err != nil || found != b {
		t.Fatalf("mini: %v", err)
	}
}
