package championship

import (
	"fmt"
	"net/url"
	"testing"
)

// entrantOrderForm is a form with the spectator car and n named entrants in one class, in form order.
func entrantOrderForm(names []string) url.Values {
	form := validChampionshipForm()
	form["EntryList.NumEntrants"] = []string{fmt.Sprint(len(names))}
	form["NumPoints"] = []string{"2"}

	form["EntryList.Name"] = []string{"Spectator"}
	form["EntryList.GUID"] = []string{""}

	for i, name := range names {
		form["EntryList.Name"] = append(form["EntryList.Name"], name)
		form["EntryList.GUID"] = append(form["EntryList.GUID"], fmt.Sprintf("7656119800000%04d", i+1))
	}

	for _, field := range []string{"EntryList.Car", "EntryList.Skin", "EntryList.InternalUUID", "EntryList.Team", "EntryList.Ballast", "EntryList.Restrictor", "EntryList.FixedSetup"} {
		value := ""

		switch field {
		case "EntryList.Car":
			value = "rss_formula_rss_4"
		case "EntryList.Skin":
			value = "default"
		case "EntryList.Ballast", "EntryList.Restrictor":
			value = "0"
		}

		form[field] = make([]string, len(names)+1)

		for i := range form[field] {
			form[field][i] = value
		}
	}

	return form
}

// The saved order of the entrants of a class must be the order of the form, however many entrants were deleted or
// added in the form before (the pit box of a championship entrant is its position in the form).
func TestChampionshipManager_HandleCreateChampionship_KeepsEntrantOrder(t *testing.T) {
	cm := newTestChampionshipManager()

	names := []string{"Zed", "Alice", "Mike", "Bob", "Carl", "Dora", "Eve", "Finn", "Gus", "Hans", "Ida", "Jo"}

	championship, _, err := cm.HandleCreateChampionship(newChampionshipFormRequest(entrantOrderForm(names)))

	if err != nil {
		t.Fatal(err)
	}

	var got []string

	for _, entrant := range championship.Classes[0].Entrants.AsSlice() {
		got = append(got, entrant.Name)
	}

	if fmt.Sprint(got) != fmt.Sprint(names) {
		t.Errorf("entrant order changed:\n got %v\nwant %v", got, names)
	}
}
