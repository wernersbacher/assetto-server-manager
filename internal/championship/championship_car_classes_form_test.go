package championship

import (
	"encoding/json"
	"net/url"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/server/servertest"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
)

// twoClassForm is a form with two classes (one entrant and two points places each, plus the spectator car): class A with the
// single cars carsA and the car classes refsA, class B with the single cars carsB and the car classes refsB. The
// entrant of a class drives the given car (the model of entrants counts as a car of the class).
func twoClassForm(entrantA, entrantB string, carsA []string, refsA string, carsB []string, refsB string) url.Values {
	cars := append(append([]string(nil), carsA...), carsB...)

	return url.Values{
		"ChampionshipName":          {"Car Classes"},
		"ClassName":                 {"A", "B"},
		"ClassID":                   {"", ""},
		"ClassCarClasses":           {refsA, refsB},
		"EntryList.NumEntrants":     {"1", "1"},
		"NumPoints":                 {"2", "2"},
		"NumCars":                   {strconv.Itoa(len(carsA)), strconv.Itoa(len(carsB))},
		"NumAvailableSpectatorCars": {"0"},
		"Cars":                      cars,
		"Points.Place":              {"25", "18", "25", "18"},

		"Points.PolePosition":         {"1", "1"},
		"Points.BestLap":              {"1", "1"},
		"Points.SecondRaceMultiplier": {"0.5", "0.5"},
		"Points.CollisionWithDriver":  {"0", "0"},
		"Points.CollisionWithEnv":     {"0", "0"},
		"Points.CutTrack":             {"0", "0"},

		"EntryList.Car":          {"rss_formula_rss_4", entrantA, entrantB},
		"EntryList.Skin":         {"default", "default", "default"},
		"EntryList.InternalUUID": {"", "", ""},
		"EntryList.Name":         {"Spectator", "Driver A", "Driver B"},
		"EntryList.Team":         {"", "", ""},
		"EntryList.GUID":         {"", "76561198000000001", "76561198000000002"},
		"EntryList.Ballast":      {"0", "0", "0"},
		"EntryList.Restrictor":   {"0", "0", "0"},
		"EntryList.FixedSetup":   {"", "", ""},
	}
}

func storeCarClasses(t *testing.T, store smstore.Store, classes map[string][]string) map[string]*smmodel.CarClass {
	t.Helper()

	out := make(map[string]*smmodel.CarClass)

	for name, cars := range classes {
		class, err := smmodel.NewCarClass(name, cars, time.Now())

		if err != nil {
			t.Fatal(err)
		}

		if err := store.UpsertCarClass(class); err != nil {
			t.Fatal(err)
		}

		out[name] = class
	}

	return out
}

func TestHandleCreateChampionship_CarClassReferences(t *testing.T) {
	cm, store, cleanup := setupScheduledEventTest(t, &servertest.Dummy{})
	defer cleanup()

	carClasses := storeCarClasses(t, store, map[string][]string{"GT3": {"bmw", "audi"}, "GT4": {"mini", "ford"}})
	gt3, gt4 := carClasses["GT3"], carClasses["GT4"]

	t.Run("mixed classes keep their offsets", func(t *testing.T) {
		// A: references only (no single cars), B: two single cars, so the Cars of B start at offset 0
		form := twoClassForm("bmw", "x1", nil, gt3.ID.String(), []string{"x1", "x2"}, "")

		championship, _, err := cm.HandleCreateChampionship(newChampionshipFormRequest(form))

		if err != nil {
			t.Fatal(err)
		}

		a, b := championship.Classes[0], championship.Classes[1]

		if len(a.AvailableCars) != 0 || !reflect.DeepEqual(a.CarClassIDs, []uuid.UUID{gt3.ID}) {
			t.Errorf("class A: cars %v, car classes %v", a.AvailableCars, a.CarClassIDs)
		}

		if !reflect.DeepEqual(b.AvailableCars, []string{"x1", "x2"}) || len(b.CarClassIDs) != 0 {
			t.Errorf("class B: cars %v, car classes %v", b.AvailableCars, b.CarClassIDs)
		}

		if got := a.ValidCarIDs(); !reflect.DeepEqual(got, []string{"bmw", "audi"}) {
			t.Errorf("class A valid cars: %v", got)
		}
	})

	t.Run("single cars and several references", func(t *testing.T) {
		form := twoClassForm("x1", "mini", []string{"x1", "x2"}, "", []string{"y1"}, gt4.ID.String()+" , "+gt3.ID.String())

		championship, _, err := cm.HandleCreateChampionship(newChampionshipFormRequest(form))

		if err != nil {
			t.Fatal(err)
		}

		a, b := championship.Classes[0], championship.Classes[1]

		if !reflect.DeepEqual(a.AvailableCars, []string{"x1", "x2"}) || len(a.CarClassIDs) != 0 {
			t.Errorf("class A: cars %v, car classes %v", a.AvailableCars, a.CarClassIDs)
		}

		if !reflect.DeepEqual(b.AvailableCars, []string{"y1"}) || !reflect.DeepEqual(b.CarClassIDs, []uuid.UUID{gt4.ID, gt3.ID}) {
			t.Errorf("class B: cars %v, car classes %v", b.AvailableCars, b.CarClassIDs)
		}

		if got := b.ValidCarIDs(); !reflect.DeepEqual(got, []string{"y1", "mini", "ford", "bmw", "audi"}) {
			t.Errorf("class B valid cars: %v", got)
		}
	})

	t.Run("refused", func(t *testing.T) {
		tests := map[string]url.Values{
			"car of a car class also in another class": twoClassForm("bmw", "audi", nil, gt3.ID.String(), []string{"audi"}, ""),
			"same car class in two classes":            twoClassForm("bmw", "mini", nil, gt3.ID.String(), nil, gt3.ID.String()),
			"unknown car class":                        twoClassForm("x1", "y1", []string{"x1"}, uuid.New().String(), []string{"y1"}, ""),
			"invalid car class id":                     twoClassForm("x1", "y1", []string{"x1"}, "nope", []string{"y1"}, ""),
			"nil car class id":                         twoClassForm("x1", "y1", []string{"x1"}, uuid.Nil.String(), []string{"y1"}, ""),
		}

		for name, form := range tests {
			name, form := name, form

			t.Run(name, func(t *testing.T) {
				_, _, err := cm.HandleCreateChampionship(newChampionshipFormRequest(form))

				if _, ok := err.(ValidationError); !ok {
					t.Fatalf("expected a ValidationError, got %T %v", err, err)
				}
			})
		}
	})
}

// Changing a global car class applies to a championship that refers to it, and the form offers only the own cars of a
// class as single cars (saving the form must not copy the cars of a car class).
func TestCarClassReferenceIsLive(t *testing.T) {
	cm, store, cleanup := setupScheduledEventTest(t, &servertest.Dummy{})
	defer cleanup()

	gt3 := storeCarClasses(t, store, map[string][]string{"GT3": {"bmw", "audi"}})["GT3"]

	championship, _, err := cm.HandleCreateChampionship(newChampionshipFormRequest(twoClassForm("bmw", "x1", nil, gt3.ID.String(), []string{"x1"}, "")))

	if err != nil {
		t.Fatal(err)
	}

	if err := cm.UpsertChampionship(championship); err != nil {
		t.Fatal(err)
	}

	loaded, err := cm.LoadChampionship(championship.ID.String())

	if err != nil {
		t.Fatal(err)
	}

	if got := loaded.Classes[0].ValidCarIDs(); !reflect.DeepEqual(got, []string{"bmw", "audi"}) {
		t.Fatalf("valid cars: %v", got)
	}

	if refs := loaded.Classes[0].ReferencedCarClasses(); len(refs) != 1 || refs[0].Name != "GT3" {
		t.Errorf("referenced car classes: %v", refs)
	}

	if got := loaded.Classes[0].OwnCarIDs(); len(got) != 0 {
		t.Errorf("own cars of a class with a car class: %v", got)
	}

	if got := loaded.Classes[1].OwnCarIDs(); !reflect.DeepEqual(got, []string{"x1"}) {
		t.Errorf("own cars: %v", got)
	}

	updated := *gt3
	updated.Cars = []string{"bmw", "audi", "porsche"}

	if err := store.UpsertCarClass(&updated); err != nil {
		t.Fatal(err)
	}

	loaded, err = cm.LoadChampionship(championship.ID.String())

	if err != nil {
		t.Fatal(err)
	}

	if got := loaded.Classes[0].ValidCarIDs(); !reflect.DeepEqual(got, []string{"bmw", "audi", "porsche"}) {
		t.Errorf("valid cars after the change of the car class: %v", got)
	}
}

// The form lists all car classes, selects the referenced ones and shows a reference that does not exist (anymore).
func TestCarClassOptions(t *testing.T) {
	gt3, _ := smmodel.NewCarClass("GT3", []string{"bmw"}, time.Now())
	gt4, _ := smmodel.NewCarClass("GT4", []string{"mini"}, time.Now())
	gone := uuid.New()

	class := smmodel.NewChampionshipClass("A")
	class.CarClassIDs = []uuid.UUID{gt4.ID, gone}

	options := (&ChampionshipTemplateVars{CarClasses: []*smmodel.CarClass{gt3, gt4}}).CarClassOptions(class)

	if len(options) != 3 {
		t.Fatalf("options: %+v", options)
	}

	if options[0].Selected || !options[1].Selected || options[1].Missing {
		t.Errorf("selection of the existing car classes: %+v", options)
	}

	if !options[2].Missing || !options[2].Selected || options[2].ID != gone.String() {
		t.Errorf("missing reference: %+v", options[2])
	}
}

func TestImportChampionship_CarClasses(t *testing.T) {
	cm, store, cleanup := setupScheduledEventTest(t, &servertest.Dummy{})
	defer cleanup()

	gt3 := storeCarClasses(t, store, map[string][]string{"GT3": {"bmw", "audi"}})["GT3"]

	export := func(id uuid.UUID) string {
		championship := smmodel.NewChampionship("Imported")
		class := smmodel.NewChampionshipClass("A")
		class.CarClassIDs = []uuid.UUID{id}
		championship.AddClass(class)

		data, err := json.Marshal(championship)

		if err != nil {
			t.Fatal(err)
		}

		return string(data)
	}

	if _, err := cm.ImportChampionship(export(gt3.ID)); err != nil {
		t.Fatalf("import with an existing car class: %v", err)
	}

	_, err := cm.ImportChampionship(export(uuid.New()))

	if _, ok := err.(ValidationError); !ok {
		t.Fatalf("import of a missing car class: expected a ValidationError, got %v", err)
	}

	all, err := store.ListChampionships()

	if err != nil {
		t.Fatal(err)
	}

	if len(all) != 1 {
		t.Errorf("expected only the first import to be saved, got %d championships", len(all))
	}
}
