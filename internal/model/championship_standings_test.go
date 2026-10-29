package model

import (
	"testing"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
)

func TestChampionshipClass_Standings_ResultsWithoutLaps(t *testing.T) {
	championship := NewChampionship("No Laps")
	class := NewChampionshipClass("Class")
	class.Points.Places = []int{10, 6, 3}
	class.Points.BestLap = 1
	championship.AddClass(class)

	event := dropTestEvent(class, dropDriverA, dropDriverB)
	// e.g. imported or manually edited results, which have finishing times but no laps
	event.Sessions[serverconfig.SessionTypeRace].Results.Laps = nil
	championship.Events = []*ChampionshipEvent{event}

	standings := class.Standings(championship, championship.Events)

	if a, b := standingFor(t, standings, "A"), standingFor(t, standings, "B"); a.Points != 10 || b.Points != 6 {
		t.Errorf("expected points A=10 B=6, got A=%v B=%v", a.Points, b.Points)
	}
}

func TestChampionshipClass_Standings_FastestLapPoints(t *testing.T) {
	championship := NewChampionship("Fastest Lap")
	class := NewChampionshipClass("Class")
	class.Points.Places = []int{10, 6, 3}
	class.Points.BestLap = 1
	championship.AddClass(class)

	event := dropTestEvent(class, dropDriverA, dropDriverB)
	// B sets the fastest lap
	event.Sessions[serverconfig.SessionTypeRace].Results.Laps[1].LapTime = 50
	championship.Events = []*ChampionshipEvent{event}

	standings := class.Standings(championship, championship.Events)

	if a, b := standingFor(t, standings, "A"), standingFor(t, standings, "B"); a.Points != 10 || b.Points != 7 {
		t.Errorf("expected points A=10 B=7, got A=%v B=%v", a.Points, b.Points)
	}
}
