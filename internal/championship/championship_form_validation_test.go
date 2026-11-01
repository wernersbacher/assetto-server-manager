package championship

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func newChampionshipFormRequest(values url.Values) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/championships/new/submit", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	return req
}

// validChampionshipForm is a form with the spectator car and one class with one entrant, two points places and one car.
func validChampionshipForm() url.Values {
	return url.Values{
		"ChampionshipName":          {"Form Test"},
		"ClassName":                 {"Default"},
		"ClassID":                   {""},
		"ClassCarClasses":           {""},
		"EntryList.NumEntrants":     {"1"},
		"NumPoints":                 {"2"},
		"NumCars":                   {"1"},
		"NumAvailableSpectatorCars": {"0"},
		"Cars":                      {"rss_formula_rss_4"},
		"Points.Place":              {"25", "18"},

		"Points.PolePosition":         {"1"},
		"Points.BestLap":              {"1"},
		"Points.SecondRaceMultiplier": {"0.5"},
		"Points.CollisionWithDriver":  {"0"},
		"Points.CollisionWithEnv":     {"0"},
		"Points.CutTrack":             {"0"},

		"EntryList.Car":          {"rss_formula_rss_4", "rss_formula_rss_4"},
		"EntryList.Skin":         {"default", "default"},
		"EntryList.InternalUUID": {"", ""},
		"EntryList.Name":         {"Spectator", "Driver"},
		"EntryList.Team":         {"", ""},
		"EntryList.GUID":         {"", "76561198000000001"},
		"EntryList.Ballast":      {"0", "0"},
		"EntryList.Restrictor":   {"0", "0"},
		"EntryList.FixedSetup":   {"", ""},
	}
}

func TestChampionshipManager_HandleCreateChampionship_Form(t *testing.T) {
	cm := newTestChampionshipManager()

	form := validChampionshipForm()

	championship, _, err := cm.HandleCreateChampionship(newChampionshipFormRequest(form))

	if err != nil {
		t.Fatalf("valid form: %v", err)
	}

	if len(championship.Classes) != 1 || len(championship.Classes[0].Points.Places) != 2 {
		t.Errorf("unexpected classes: %+v", championship.Classes)
	}
}

func TestChampionshipManager_HandleCreateChampionship_InvalidForm(t *testing.T) {
	cm := newTestChampionshipManager()

	tests := map[string]func(url.Values){
		"missing points places": func(v url.Values) { v.Del("Points.Place") },
		"too few points places": func(v url.Values) { v["Points.Place"] = []string{"25"} },
		"missing cars":          func(v url.Values) { v.Del("Cars") },
		"missing class values":  func(v url.Values) { v.Del("Points.CutTrack") },
		"missing class ids":     func(v url.Values) { v.Del("ClassID") },
		"bad class id":          func(v url.Values) { v["ClassID"] = []string{"nope"} },
		"missing entrant names": func(v url.Values) { v["EntryList.Name"] = []string{"Spectator"} },
		"missing entrant cars":  func(v url.Values) { v.Del("EntryList.Car") },
		"negative points":       func(v url.Values) { v["NumPoints"] = []string{"-1"} },
		"negative cars":         func(v url.Values) { v["NumCars"] = []string{"-1"} },
		"more entrants":         func(v url.Values) { v["EntryList.NumEntrants"] = []string{"5"} },
		"more classes":          func(v url.Values) { v["ClassName"] = []string{"A", "B"} },
		"missing car classes":   func(v url.Values) { v.Del("ClassCarClasses") },
		"bad car class id":      func(v url.Values) { v["ClassCarClasses"] = []string{"nope"} },
		"unknown car class":     func(v url.Values) { v["ClassCarClasses"] = []string{uuid.New().String()} },
		"car class twice":       func(v url.Values) { id := uuid.New().String(); v["ClassCarClasses"] = []string{id + "," + id} },
		"class without cars": func(v url.Values) {
			v["NumCars"] = []string{"0"}
			v["Cars"] = nil
			v["EntryList.NumEntrants"] = []string{"0"}
		},
	}

	for name, modify := range tests {
		name, modify := name, modify

		t.Run(name, func(t *testing.T) {
			form := validChampionshipForm()
			modify(form)

			_, _, err := cm.HandleCreateChampionship(newChampionshipFormRequest(form))

			if _, ok := err.(ValidationError); !ok {
				t.Fatalf("expected a ValidationError, got %v", err)
			}
		})
	}
}
