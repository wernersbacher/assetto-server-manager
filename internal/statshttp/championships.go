package statshttp

import (
	"strings"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

// guidMatches tells if the GUID (which may be several GUIDs of a driver swap, joined) contains the driver's.
func guidMatches(joined, guid string) bool {
	if joined == guid {
		return true
	}

	if !strings.Contains(joined, serverconfig.DriverSwapEntrantSeparator) {
		return false
	}

	for _, g := range strings.Split(joined, serverconfig.DriverSwapEntrantSeparator) {
		if g == guid {
			return true
		}
	}

	return false
}

// mayContain is a cheap pre-check which keeps the standings of championships the driver cannot be in from being
// calculated: the driver is in the entry list of a class, or the championship has open entrants (which are filled
// when drivers join).
func mayContain(championship *smmodel.Championship, guid string) bool {
	if championship.OpenEntrants {
		return true
	}

	for _, class := range championship.Classes {
		for _, entrant := range class.Entrants {
			if entrant != nil && guidMatches(entrant.GUID, guid) {
				return true
			}
		}
	}

	return false
}

// ChampionshipEntries returns the standing of the driver in every class of the championships they are in, in the order
// of the championships. Deleted championships are skipped.
func ChampionshipEntries(championships []*smmodel.Championship, guid string) []ChampionshipEntry {
	var entries []ChampionshipEntry

	for _, championship := range championships {
		if championship == nil || !championship.Deleted.IsZero() || !mayContain(championship, guid) {
			continue
		}

		for _, class := range championship.Classes {
			standings := class.Standings(championship, championship.Events)

			for pos, standing := range standings {
				if standing.Car == nil || !guidMatches(standing.Car.Driver.GUID, guid) {
					continue
				}

				entry := ChampionshipEntry{
					ID:        championship.ID.String(),
					Name:      championship.Name,
					Position:  pos + 1,
					Entrants:  len(standings),
					Points:    standing.Points,
					Completed: championship.Progress() == 100,
				}

				if championship.IsMultiClass() {
					entry.Class = class.Name
				}

				entries = append(entries, entry)

				break
			}
		}
	}

	return entries
}
