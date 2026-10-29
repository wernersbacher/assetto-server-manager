package model

import (
	"github.com/google/uuid"
)

type RaceWeekendFilterSplitType string

func (fst RaceWeekendFilterSplitType) String() string {
	return string(fst)
}

const (
	SplitTypeNumeric               RaceWeekendFilterSplitType = "Numeric"
	SplitTypeManualDriverSelection RaceWeekendFilterSplitType = "Manual Driver Selection"
	SplitTypeChampionshipClass     RaceWeekendFilterSplitType = "Championship Class"
)

type RaceWeekendSessionToSessionFilter struct {
	// IsPreview indicates that the Filter is for preview only, and will not actually affect a starting grid.
	IsPreview bool

	// ResultStart is the beginning of the split from the previous session's result
	ResultStart int
	// ResultEnd is the end of the split from the previous session's result
	ResultEnd int

	// NumEntrantsToReverse defines how many entrants to reverse. -1 indicates all, 0 indicates none, or N entrants.
	NumEntrantsToReverse int

	// EntryListStart is where to place the entrants in the starting grid of the next session
	EntryListStart int

	// SortType defines how the entrants are sorted
	SortType string

	// ForceUseTyreFromFastestLap forces drivers to start on the same tyre compound as the tyre compound that
	// they achieved their fastest lap on in the previous session
	ForceUseTyreFromFastestLap bool

	// AvailableResultsForSorting are the results files to be used for sorting the entrants.
	AvailableResultsForSorting []string

	// SplitType defines the kind of splitting that we want to do.
	SplitType RaceWeekendFilterSplitType

	// SelectedDriverGUIDs is a list of the currently selected driver GUIDs. This is only populated if SplitType == SplitTypeManualDriverSelection
	SelectedDriverGUIDs []string

	// SelectedChampionshipClassIDs is a list of the currently selected ChampionshipClass IDs. This is only populated if SplitType == SplitTypeChampionshipClass
	SelectedChampionshipClassIDs map[uuid.UUID]bool

	// Deprecated: ManualDriverSelection indicates that drivers are picked manually from the above results file.
	ManualDriverSelection bool
}
