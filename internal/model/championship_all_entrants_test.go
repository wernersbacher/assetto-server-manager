package model

import (
	"testing"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
)

// AllEntrants must not touch the entrants of the classes: placing them on the combined grid used to overwrite their
// pit boxes in the random order of the entrant map, which reordered the entry list of the class on every save.
func TestChampionship_AllEntrants_KeepsClassPitBoxes(t *testing.T) {
	class := NewChampionshipClass("Class")

	for i := 0; i < 20; i++ {
		entrant := serverconfig.NewEntrant()
		entrant.Name = string(rune('A' + i))
		class.Entrants.AddInPitBox(entrant, i+1)
	}

	championship := NewChampionship("Test")
	championship.AddClass(class)

	all := championship.AllEntrants()

	for i, entrant := range all.AsSlice() {
		if want := string(rune('A' + i)); entrant.Name != want {
			t.Fatalf("combined grid position %d: got %s, want %s", i, entrant.Name, want)
		}

		if entrant.PitBox != i {
			t.Errorf("%s: combined grid pit box %d, want %d", entrant.Name, entrant.PitBox, i)
		}
	}

	for i, entrant := range class.Entrants.AsSlice() {
		if entrant.PitBox != i+1 {
			t.Errorf("%s: class pit box changed to %d, want %d", entrant.Name, entrant.PitBox, i+1)
		}
	}
}
