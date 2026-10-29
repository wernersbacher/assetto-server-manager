package servermanager

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"
)

const sortTestCarModel = "sort_test_car"

// sortTestDriver describes the outcome of a single driver in a hand-built session.
type sortTestDriver struct {
	guid      string
	name      string
	model     string
	bestLap   int
	totalTime int
	laps      int
	cuts      int
	crashes   int
	classID   uuid.UUID

	lapPenalty  int
	penaltyTime time.Duration
}

// buildSortTestResults builds SessionResults for the given drivers. Car IDs start at 1 so that a
// failed car lookup (which yields car ID 0) can never match a real car.
func buildSortTestResults(sessionType serverconfig.SessionType, drivers ...sortTestDriver) *smresults.SessionResults {
	results := &smresults.SessionResults{Type: sessionType}

	for i, d := range drivers {
		carID := i + 1
		model := d.model

		if model == "" {
			model = sortTestCarModel
		}

		name := d.name

		if name == "" {
			name = d.guid
		}

		results.Cars = append(results.Cars, &smresults.SessionCar{
			CarID:  carID,
			Model:  model,
			Driver: smresults.SessionDriver{GUID: d.guid, Name: name, ClassID: d.classID},
		})

		results.Result = append(results.Result, &smresults.SessionResult{
			CarID:       carID,
			CarModel:    model,
			DriverGUID:  d.guid,
			DriverName:  name,
			BestLap:     d.bestLap,
			TotalTime:   d.totalTime,
			ClassID:     d.classID,
			LapPenalty:  d.lapPenalty,
			HasPenalty:  d.penaltyTime > 0 || d.lapPenalty > 0,
			PenaltyTime: d.penaltyTime,
		})

		for lap := 0; lap < d.laps; lap++ {
			cuts := 0

			if lap == 0 {
				cuts = d.cuts
			}

			lapTime := d.bestLap

			if lapTime == 0 {
				lapTime = 100000
			}

			results.Laps = append(results.Laps, &smresults.SessionLap{
				CarID:      carID,
				CarModel:   model,
				DriverGUID: d.guid,
				DriverName: name,
				LapTime:    lapTime,
				Cuts:       cuts,
				Timestamp:  (lap + 1) * 1000,
				ClassID:    d.classID,
			})
		}

		for crash := 0; crash < d.crashes; crash++ {
			results.Events = append(results.Events, &smresults.SessionEvent{
				CarID: carID,
				Type:  "COLLISION_WITH_ENV",
			})
		}
	}

	return results
}

// buildSortTestEntrants builds RaceWeekendSessionEntrants that share one SessionResults, the same
// way a finished parent session feeds its entrants into the next session.
func buildSortTestEntrants(sessionType serverconfig.SessionType, drivers ...sortTestDriver) []*smmodel.RaceWeekendSessionEntrant {
	results := buildSortTestResults(sessionType, drivers...)
	sessionID := uuid.New()

	var entrants []*smmodel.RaceWeekendSessionEntrant

	for i := range drivers {
		entrants = append(entrants, smmodel.NewRaceWeekendSessionEntrant(sessionID, results.Cars[i], results.Result[i], results))
	}

	return entrants
}

func sortTestSession(sessionType serverconfig.SessionType) *smmodel.RaceWeekendSession {
	return &smmodel.RaceWeekendSession{
		ID: uuid.New(),
		RaceConfig: serverconfig.CurrentRaceConfig{
			Sessions: serverconfig.Sessions{sessionType: &serverconfig.SessionConfig{}},
		},
	}
}

func sortTestGUIDs(entrants []*smmodel.RaceWeekendSessionEntrant) []string {
	var out []string

	for _, e := range entrants {
		out = append(out, e.Car.Driver.GUID)
	}

	return out
}

func assertSortTestOrder(t *testing.T, entrants []*smmodel.RaceWeekendSessionEntrant, expected ...string) {
	t.Helper()

	if actual := sortTestGUIDs(entrants); !reflect.DeepEqual(actual, expected) {
		t.Errorf("expected order %v, got %v", expected, actual)
	}
}

type sorterTestCase struct {
	name        string
	sessionType serverconfig.SessionType
	drivers     []sortTestDriver
	expected    []string
	// unorderedTail is a set of GUIDs expected (in any order) after the ordered ones,
	// e.g. multiple DNFs which compare as equal in a non-stable sort.
	unorderedTail []string
}

func runSorterTestCases(t *testing.T, sorter RaceWeekendEntryListSortFunc, cases []sorterTestCase) {
	t.Helper()

	for _, tc := range cases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			sessionType := tc.sessionType

			if sessionType == "" {
				sessionType = serverconfig.SessionTypeRace
			}

			entrants := buildSortTestEntrants(sessionType, tc.drivers...)

			if err := sorter(&smmodel.RaceWeekend{}, sortTestSession(sessionType), entrants, nil); err != nil {
				t.Fatal(err)
			}

			actual := sortTestGUIDs(entrants)

			if len(actual) != len(tc.expected)+len(tc.unorderedTail) {
				t.Fatalf("expected %d entrants, got %d (%v)", len(tc.expected)+len(tc.unorderedTail), len(actual), actual)
			}

			if !reflect.DeepEqual(actual[:len(tc.expected)], tc.expected) {
				t.Errorf("expected order %v (then %v in any order), got %v", tc.expected, tc.unorderedTail, actual)
			}

			if len(tc.unorderedTail) > 0 {
				tail := append([]string(nil), actual[len(tc.expected):]...)
				expectedTail := append([]string(nil), tc.unorderedTail...)
				sort.Strings(tail)
				sort.Strings(expectedTail)

				if !reflect.DeepEqual(tail, expectedTail) {
					t.Errorf("expected tail %v in any order, got %v", tc.unorderedTail, actual)
				}
			}
		})
	}
}

func TestRaceWeekendSort_FastestLap(t *testing.T) {
	runSorterTestCases(t, FastestLapRaceWeekendEntryListSort, []sorterTestCase{
		{
			name: "distinct best laps",
			drivers: []sortTestDriver{
				{guid: "A", bestLap: 100000, laps: 3},
				{guid: "B", bestLap: 90000, laps: 3},
				{guid: "C", bestLap: 95000, laps: 3},
			},
			expected: []string{"B", "C", "A"},
		},
		{
			name: "drivers without a lap go to the back",
			drivers: []sortTestDriver{
				{guid: "DNF1", bestLap: 0},
				{guid: "A", bestLap: 100000, laps: 3},
				{guid: "DNF2", bestLap: 0},
				{guid: "B", bestLap: 90000, laps: 3},
			},
			expected:      []string{"B", "A"},
			unorderedTail: []string{"DNF1", "DNF2"},
		},
		{
			name: "equal best lap is decided by fewer crashes",
			drivers: []sortTestDriver{
				{guid: "A", bestLap: 90000, laps: 2, crashes: 2},
				{guid: "B", bestLap: 90000, laps: 2, crashes: 0},
				{guid: "C", bestLap: 80000, laps: 2, crashes: 5},
			},
			expected: []string{"C", "B", "A"},
		},
		{
			name: "equal best lap and crashes is decided by fewer cuts",
			drivers: []sortTestDriver{
				{guid: "A", bestLap: 90000, laps: 2, crashes: 1, cuts: 3},
				{guid: "B", bestLap: 90000, laps: 2, crashes: 1, cuts: 1},
			},
			expected: []string{"B", "A"},
		},
		{
			name:        "qualifying session sorts the same way",
			sessionType: serverconfig.SessionTypeQualifying,
			drivers: []sortTestDriver{
				{guid: "A", bestLap: 91000, laps: 1},
				{guid: "B", bestLap: 0},
				{guid: "C", bestLap: 90000, laps: 1},
			},
			expected: []string{"C", "A", "B"},
		},
	})
}

func TestRaceWeekendSort_TotalRaceTime(t *testing.T) {
	runSorterTestCases(t, TotalRaceTimeRaceWeekendEntryListSort, []sorterTestCase{
		{
			name: "same laps, sorted by total time",
			drivers: []sortTestDriver{
				{guid: "A", bestLap: 90000, laps: 10, totalTime: 1000000},
				{guid: "B", bestLap: 91000, laps: 10, totalTime: 990000},
				{guid: "C", bestLap: 89000, laps: 10, totalTime: 995000},
			},
			expected: []string{"B", "C", "A"},
		},
		{
			name: "more laps beats a lower total time",
			drivers: []sortTestDriver{
				{guid: "A", laps: 9, bestLap: 90000, totalTime: 900000},
				{guid: "B", laps: 10, bestLap: 90000, totalTime: 1000000},
				{guid: "C", laps: 8, bestLap: 90000, totalTime: 700000},
			},
			expected: []string{"B", "A", "C"},
		},
		{
			name: "drivers without laps go to the back",
			drivers: []sortTestDriver{
				{guid: "DNF", laps: 0, totalTime: 0},
				{guid: "A", laps: 1, bestLap: 90000, totalTime: 90000},
				{guid: "B", laps: 10, bestLap: 90000, totalTime: 1000000},
			},
			expected: []string{"B", "A", "DNF"},
		},
		{
			name: "lap penalties reduce the lap count",
			drivers: []sortTestDriver{
				{guid: "A", laps: 10, bestLap: 90000, totalTime: 900000, lapPenalty: 1},
				{guid: "B", laps: 10, bestLap: 90000, totalTime: 950000},
			},
			expected: []string{"B", "A"},
		},
		{
			name: "time penalties are added to the total time",
			drivers: []sortTestDriver{
				{guid: "A", laps: 10, bestLap: 90000, totalTime: 900000, penaltyTime: 10 * time.Second},
				{guid: "B", laps: 10, bestLap: 90000, totalTime: 905000},
			},
			expected: []string{"B", "A"},
		},
		{
			name: "same laps but no total time goes to the back",
			drivers: []sortTestDriver{
				{guid: "NOTIME", laps: 10, bestLap: 90000, totalTime: 0},
				{guid: "A", laps: 10, bestLap: 90000, totalTime: 1000000},
				{guid: "B", laps: 10, bestLap: 90000, totalTime: 990000},
			},
			expected: []string{"B", "A", "NOTIME"},
		},
		{
			name: "no total time is not rescued by a time penalty",
			drivers: []sortTestDriver{
				{guid: "NOTIME", laps: 10, bestLap: 90000, totalTime: 0, penaltyTime: 5 * time.Second},
				{guid: "A", laps: 10, bestLap: 90000, totalTime: 1000000},
			},
			expected: []string{"A", "NOTIME"},
		},
	})
}

func TestRaceWeekendSort_FewestCollisions(t *testing.T) {
	runSorterTestCases(t, FewestCollisionsRaceWeekendEntryListSort, []sorterTestCase{
		{
			name: "fewest crashes first",
			drivers: []sortTestDriver{
				{guid: "A", laps: 10, bestLap: 90000, totalTime: 900000, crashes: 3},
				{guid: "B", laps: 10, bestLap: 90000, totalTime: 950000, crashes: 0},
				{guid: "C", laps: 10, bestLap: 90000, totalTime: 920000, crashes: 1},
			},
			expected: []string{"B", "C", "A"},
		},
		{
			name:        "race tie-break is total race time",
			sessionType: serverconfig.SessionTypeRace,
			drivers: []sortTestDriver{
				{guid: "A", laps: 10, bestLap: 80000, totalTime: 950000, crashes: 1},
				{guid: "B", laps: 10, bestLap: 90000, totalTime: 900000, crashes: 1},
				{guid: "C", laps: 10, bestLap: 85000, totalTime: 990000, crashes: 0},
			},
			expected: []string{"C", "B", "A"},
		},
		{
			name:        "race tie-break puts no total time last",
			sessionType: serverconfig.SessionTypeRace,
			drivers: []sortTestDriver{
				{guid: "NOTIME", laps: 10, bestLap: 80000, totalTime: 0, crashes: 1},
				{guid: "A", laps: 10, bestLap: 90000, totalTime: 900000, crashes: 1},
			},
			expected: []string{"A", "NOTIME"},
		},
		{
			name:        "qualifying tie-break is best lap",
			sessionType: serverconfig.SessionTypeQualifying,
			drivers: []sortTestDriver{
				{guid: "A", laps: 3, bestLap: 80000, totalTime: 950000, crashes: 1},
				{guid: "B", laps: 3, bestLap: 90000, totalTime: 900000, crashes: 1},
				{guid: "C", laps: 3, bestLap: 0, crashes: 1},
			},
			expected: []string{"A", "B", "C"},
		},
	})
}

func TestRaceWeekendSort_FewestCuts(t *testing.T) {
	runSorterTestCases(t, FewestCutsRaceWeekendEntryListSort, []sorterTestCase{
		{
			name: "fewest cuts first",
			drivers: []sortTestDriver{
				{guid: "A", laps: 10, bestLap: 90000, totalTime: 900000, cuts: 4},
				{guid: "B", laps: 10, bestLap: 90000, totalTime: 950000, cuts: 0},
				{guid: "C", laps: 10, bestLap: 90000, totalTime: 920000, cuts: 2},
			},
			expected: []string{"B", "C", "A"},
		},
		{
			name:        "race tie-break is total race time",
			sessionType: serverconfig.SessionTypeRace,
			drivers: []sortTestDriver{
				{guid: "A", laps: 10, bestLap: 80000, totalTime: 950000, cuts: 2},
				{guid: "B", laps: 10, bestLap: 90000, totalTime: 900000, cuts: 2},
				{guid: "C", laps: 9, bestLap: 85000, totalTime: 800000, cuts: 2},
			},
			expected: []string{"B", "A", "C"},
		},
		{
			name:        "practice tie-break is best lap",
			sessionType: serverconfig.SessionTypePractice,
			drivers: []sortTestDriver{
				{guid: "A", laps: 3, bestLap: 92000, cuts: 1},
				{guid: "B", laps: 3, bestLap: 91000, cuts: 1},
				{guid: "C", laps: 3, bestLap: 99000, cuts: 0},
			},
			expected: []string{"C", "B", "A"},
		},
	})
}

func TestRaceWeekendSort_Safety(t *testing.T) {
	runSorterTestCases(t, SafetyRaceWeekendEntryListSort, []sorterTestCase{
		{
			name: "crashes are more important than cuts",
			drivers: []sortTestDriver{
				{guid: "A", laps: 10, bestLap: 90000, totalTime: 900000, crashes: 1, cuts: 0},
				{guid: "B", laps: 10, bestLap: 90000, totalTime: 900000, crashes: 0, cuts: 5},
				{guid: "C", laps: 10, bestLap: 90000, totalTime: 900000, crashes: 2, cuts: 0},
			},
			expected: []string{"B", "A", "C"},
		},
		{
			name: "equal crashes are decided by cuts",
			drivers: []sortTestDriver{
				{guid: "A", laps: 10, bestLap: 90000, totalTime: 900000, crashes: 1, cuts: 3},
				{guid: "B", laps: 10, bestLap: 90000, totalTime: 990000, crashes: 1, cuts: 1},
			},
			expected: []string{"B", "A"},
		},
		{
			name:        "race tie-break is total race time",
			sessionType: serverconfig.SessionTypeRace,
			drivers: []sortTestDriver{
				{guid: "A", laps: 10, bestLap: 80000, totalTime: 950000, crashes: 1, cuts: 1},
				{guid: "B", laps: 10, bestLap: 90000, totalTime: 900000, crashes: 1, cuts: 1},
			},
			expected: []string{"B", "A"},
		},
		{
			name:        "qualifying tie-break is best lap",
			sessionType: serverconfig.SessionTypeQualifying,
			drivers: []sortTestDriver{
				{guid: "A", laps: 3, bestLap: 80000, totalTime: 950000, crashes: 1, cuts: 1},
				{guid: "B", laps: 3, bestLap: 90000, totalTime: 900000, crashes: 1, cuts: 1},
			},
			expected: []string{"A", "B"},
		},
	})
}

func TestRaceWeekendSort_Alphabetical(t *testing.T) {
	runSorterTestCases(t, AlphabeticalRaceWeekendEntryListSort, []sorterTestCase{
		{
			name: "sorted by driver name, not GUID or result",
			drivers: []sortTestDriver{
				{guid: "1", name: "Charlie", bestLap: 80000, laps: 1, totalTime: 80000},
				{guid: "2", name: "Alice", bestLap: 0},
				{guid: "3", name: "Bob", bestLap: 90000, laps: 1, totalTime: 90000},
			},
			expected: []string{"2", "3", "1"},
		},
	})
}

func TestRaceWeekendSort_Unchanged(t *testing.T) {
	runSorterTestCases(t, UnchangedRaceWeekendEntryListSort, []sorterTestCase{
		{
			name: "order is kept",
			drivers: []sortTestDriver{
				{guid: "C", bestLap: 80000, laps: 1, totalTime: 80000},
				{guid: "A", bestLap: 0},
				{guid: "B", bestLap: 70000, laps: 1, totalTime: 70000},
			},
			expected: []string{"C", "A", "B"},
		},
	})
}

func TestRaceWeekendSort_RandomIsPermutation(t *testing.T) {
	var drivers []sortTestDriver

	for i := 0; i < 10; i++ {
		drivers = append(drivers, sortTestDriver{guid: fmt.Sprintf("D%02d", i), bestLap: 90000 + i, laps: 1, totalTime: 90000 + i})
	}

	original := sortTestGUIDs(buildSortTestEntrants(serverconfig.SessionTypeRace, drivers...))
	changed := false

	// run it a few times; it's very unlikely that 10 entrants keep their order every time.
	for run := 0; run < 5; run++ {
		entrants := buildSortTestEntrants(serverconfig.SessionTypeRace, drivers...)

		if err := RandomRaceWeekendEntryListSort(&smmodel.RaceWeekend{}, sortTestSession(serverconfig.SessionTypeRace), entrants, nil); err != nil {
			t.Fatal(err)
		}

		actual := sortTestGUIDs(entrants)

		if !reflect.DeepEqual(actual, original) {
			changed = true
		}

		sorted := append([]string(nil), actual...)
		sort.Strings(sorted)

		if !reflect.DeepEqual(sorted, original) {
			t.Fatalf("random sort is not a permutation of the input: %v", actual)
		}
	}

	if !changed {
		t.Errorf("random sort never changed the order of 10 entrants in 5 runs")
	}

	// empty and single entry lists must not panic
	if err := RandomRaceWeekendEntryListSort(&smmodel.RaceWeekend{}, sortTestSession(serverconfig.SessionTypeRace), nil, nil); err != nil {
		t.Fatal(err)
	}
}

// writeSortTestResultsFile writes a results file into ServerInstallPath/results and returns the
// name that is used in RaceWeekendSessionToSessionFilter.AvailableResultsForSorting (without .json).
func writeSortTestResultsFile(t *testing.T, name string, results *smresults.SessionResults) string {
	t.Helper()

	data, err := json.Marshal(results)

	if err != nil {
		t.Fatal(err)
	}

	if err := ioutil.WriteFile(filepath.Join(ServerInstallPath, "results", name+".json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	return name
}

// withSortTestResultsDir points ServerInstallPath at a temporary directory with an empty results folder.
func withSortTestResultsDir(t *testing.T) func() {
	t.Helper()

	dir, err := ioutil.TempDir("", "race-weekend-sort")

	if err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "results"), 0755); err != nil {
		t.Fatal(err)
	}

	oldInstallPath := ServerInstallPath
	ServerInstallPath = dir

	return func() {
		ServerInstallPath = oldInstallPath
		_ = os.RemoveAll(dir)
	}
}

func TestRaceWeekendSort_FastestResultsFile(t *testing.T) {
	defer withSortTestResultsDir(t)()

	file1 := writeSortTestResultsFile(t, "2020_1_2_10_00_PRACTICE", buildSortTestResults(serverconfig.SessionTypePractice,
		sortTestDriver{guid: "A", bestLap: 95000, laps: 3, totalTime: 300000},
		sortTestDriver{guid: "B", bestLap: 91000, laps: 3, totalTime: 300000},
		// zero best lap must not override anything
		sortTestDriver{guid: "C", bestLap: 0, laps: 0, totalTime: 1},
	))

	file2 := writeSortTestResultsFile(t, "2020_1_2_11_00_PRACTICE", buildSortTestResults(serverconfig.SessionTypePractice,
		sortTestDriver{guid: "A", bestLap: 89000, laps: 3, totalTime: 300000},
		sortTestDriver{guid: "B", bestLap: 93000, laps: 3, totalTime: 300000},
		sortTestDriver{guid: "C", bestLap: 92000, laps: 3, totalTime: 300000},
	))

	entrants := buildSortTestEntrants(serverconfig.SessionTypeRace,
		sortTestDriver{guid: "NOLAPS"},
		sortTestDriver{guid: "C"},
		sortTestDriver{guid: "B"},
		sortTestDriver{guid: "A"},
	)

	t.Run("nil filter keeps order", func(t *testing.T) {
		if err := FastestResultsFileRaceWeekendEntryListSort(&smmodel.RaceWeekend{}, sortTestSession(serverconfig.SessionTypeRace), entrants, nil); err != nil {
			t.Fatal(err)
		}

		assertSortTestOrder(t, entrants, "NOLAPS", "C", "B", "A")
	})

	t.Run("best lap across all files, drivers without laps last", func(t *testing.T) {
		filter := &smmodel.RaceWeekendSessionToSessionFilter{AvailableResultsForSorting: []string{file1, file2}}

		if err := FastestResultsFileRaceWeekendEntryListSort(&smmodel.RaceWeekend{}, sortTestSession(serverconfig.SessionTypeRace), entrants, filter); err != nil {
			t.Fatal(err)
		}

		// A: 89000 (file2), B: 91000 (file1), C: 92000 (file2), NOLAPS: none
		assertSortTestOrder(t, entrants, "A", "B", "C", "NOLAPS")
	})

	t.Run("single file", func(t *testing.T) {
		filter := &smmodel.RaceWeekendSessionToSessionFilter{AvailableResultsForSorting: []string{file1}}

		if err := FastestResultsFileRaceWeekendEntryListSort(&smmodel.RaceWeekend{}, sortTestSession(serverconfig.SessionTypeRace), entrants, filter); err != nil {
			t.Fatal(err)
		}

		actual := sortTestGUIDs(entrants)

		if !reflect.DeepEqual(actual[:2], []string{"B", "A"}) {
			t.Errorf("expected B, A first, got %v", actual)
		}
	})

	t.Run("missing results file returns an error", func(t *testing.T) {
		filter := &smmodel.RaceWeekendSessionToSessionFilter{AvailableResultsForSorting: []string{"2020_1_2_12_00_RACE"}}

		if err := FastestResultsFileRaceWeekendEntryListSort(&smmodel.RaceWeekend{}, sortTestSession(serverconfig.SessionTypeRace), entrants, filter); err == nil {
			t.Error("expected an error for a missing results file")
		}
	})
}

func TestRaceWeekendSort_NumberResultsFile(t *testing.T) {
	defer withSortTestResultsDir(t)()

	file1 := writeSortTestResultsFile(t, "2020_1_2_10_00_PRACTICE", buildSortTestResults(serverconfig.SessionTypePractice,
		sortTestDriver{guid: "A", bestLap: 90000, laps: 2, totalTime: 200000},
		sortTestDriver{guid: "B", bestLap: 90000, laps: 5, totalTime: 500000},
		sortTestDriver{guid: "C", bestLap: 90000, laps: 1, totalTime: 100000},
	))

	file2 := writeSortTestResultsFile(t, "2020_1_2_11_00_PRACTICE", buildSortTestResults(serverconfig.SessionTypePractice,
		sortTestDriver{guid: "C", bestLap: 90000, laps: 3, totalTime: 300000},
		sortTestDriver{guid: "A", bestLap: 90000, laps: 5, totalTime: 500000},
	))

	entrants := buildSortTestEntrants(serverconfig.SessionTypeRace,
		sortTestDriver{guid: "NOLAPS"},
		sortTestDriver{guid: "C"},
		sortTestDriver{guid: "B"},
		sortTestDriver{guid: "A"},
	)

	t.Run("nil filter keeps order", func(t *testing.T) {
		if err := NumberResultsFileRaceWeekendEntryListSort(&smmodel.RaceWeekend{}, sortTestSession(serverconfig.SessionTypeRace), entrants, nil); err != nil {
			t.Fatal(err)
		}

		assertSortTestOrder(t, entrants, "NOLAPS", "C", "B", "A")
	})

	t.Run("most laps across all files first", func(t *testing.T) {
		filter := &smmodel.RaceWeekendSessionToSessionFilter{AvailableResultsForSorting: []string{file1, file2}}

		if err := NumberResultsFileRaceWeekendEntryListSort(&smmodel.RaceWeekend{}, sortTestSession(serverconfig.SessionTypeRace), entrants, filter); err != nil {
			t.Fatal(err)
		}

		// A: 7, B: 5, C: 4, NOLAPS: 0
		assertSortTestOrder(t, entrants, "A", "B", "C", "NOLAPS")
	})

	t.Run("missing results file returns an error", func(t *testing.T) {
		filter := &smmodel.RaceWeekendSessionToSessionFilter{AvailableResultsForSorting: []string{"2020_1_2_12_00_RACE"}}

		if err := NumberResultsFileRaceWeekendEntryListSort(&smmodel.RaceWeekend{}, sortTestSession(serverconfig.SessionTypeRace), entrants, filter); err == nil {
			t.Error("expected an error for a missing results file")
		}
	})
}

func TestSortDriversWithNoTimeToBackOfGrid(t *testing.T) {
	for _, numEntrants := range []int{4, 25} {
		numEntrants := numEntrants

		t.Run(fmt.Sprintf("%d entrants", numEntrants), func(t *testing.T) {
			var drivers []sortTestDriver
			var expectedTimed, expectedNoTime []string

			for i := 0; i < numEntrants; i++ {
				guid := fmt.Sprintf("D%02d", i)
				totalTime := 0

				// every third driver has no time
				if i%3 != 0 {
					totalTime = 100000 + (numEntrants-i)*1000
					expectedTimed = append(expectedTimed, guid)
				} else {
					expectedNoTime = append(expectedNoTime, guid)
				}

				drivers = append(drivers, sortTestDriver{guid: guid, totalTime: totalTime, laps: 1, bestLap: 90000})
			}

			entrants := buildSortTestEntrants(serverconfig.SessionTypeRace, drivers...)
			sortDriversWithNoTimeToBackOfGrid(entrants)

			// relative order of both groups is kept
			assertSortTestOrder(t, entrants, append(expectedTimed, expectedNoTime...)...)
		})
	}
}

// sortTestChampionship builds a championship with one class (sortTestCarModel) and the given completed
// events. Each event is given as a finishing order of driver GUIDs.
func sortTestChampionship(events ...[]string) (*smmodel.Championship, *smmodel.ChampionshipClass) {
	championship := smmodel.NewChampionship("Sort Test")
	class := smmodel.NewChampionshipClass("Class")
	class.AvailableCars = []string{sortTestCarModel}
	class.Points.Places = []int{10, 6, 3, 1}
	class.Points.BestLap = 0
	championship.AddClass(class)

	for i, finishOrder := range events {
		var drivers []sortTestDriver

		for pos, guid := range finishOrder {
			drivers = append(drivers, sortTestDriver{
				guid:      guid,
				bestLap:   90000 + pos,
				laps:      1,
				totalTime: 90000 + pos,
				classID:   class.ID,
			})
		}

		event := smmodel.NewChampionshipEvent()
		event.StartedTime = time.Now().Add(-time.Duration(len(events)-i+1) * time.Hour)
		event.CompletedTime = event.StartedTime.Add(time.Minute * 30)
		event.Sessions[serverconfig.SessionTypeRace] = &smmodel.ChampionshipSession{
			StartedTime:   event.StartedTime,
			CompletedTime: event.CompletedTime,
			Results:       buildSortTestResults(serverconfig.SessionTypeRace, drivers...),
		}

		championship.Events = append(championship.Events, event)
	}

	return championship, class
}

func sortTestRaceWeekendWithChampionship(championship *smmodel.Championship) *smmodel.RaceWeekend {
	return &smmodel.RaceWeekend{
		ID:             uuid.New(),
		ChampionshipID: championship.ID,
		Championship:   championship,
	}
}

func TestRaceWeekendSort_ChampionshipStandingsOrder(t *testing.T) {
	// standings: B 20, A 12, C 6; D never raced
	championship, _ := sortTestChampionship(
		[]string{"B", "A", "C"},
		[]string{"B", "A", "C"},
	)

	newEntrants := func() []*smmodel.RaceWeekendSessionEntrant {
		return buildSortTestEntrants(serverconfig.SessionTypeQualifying,
			sortTestDriver{guid: "D", bestLap: 80000, laps: 1, totalTime: 80000},
			sortTestDriver{guid: "C", bestLap: 81000, laps: 1, totalTime: 81000},
			sortTestDriver{guid: "A", bestLap: 82000, laps: 1, totalTime: 82000},
			sortTestDriver{guid: "B", bestLap: 83000, laps: 1, totalTime: 83000},
		)
	}

	t.Run("without linked championship order is kept", func(t *testing.T) {
		entrants := newEntrants()

		if err := (&ChampionshipStandingsOrderEntryListSort{}).Sort(&smmodel.RaceWeekend{}, sortTestSession(serverconfig.SessionTypeQualifying), entrants, nil); err != nil {
			t.Fatal(err)
		}

		assertSortTestOrder(t, entrants, "D", "C", "A", "B")
	})

	t.Run("linked championship ID but championship not loaded keeps order", func(t *testing.T) {
		entrants := newEntrants()
		rw := &smmodel.RaceWeekend{ChampionshipID: championship.ID}

		if err := (&ChampionshipStandingsOrderEntryListSort{}).Sort(rw, sortTestSession(serverconfig.SessionTypeQualifying), entrants, nil); err != nil {
			t.Fatal(err)
		}

		assertSortTestOrder(t, entrants, "D", "C", "A", "B")
	})

	t.Run("empty entrant list", func(t *testing.T) {
		if err := (&ChampionshipStandingsOrderEntryListSort{}).Sort(sortTestRaceWeekendWithChampionship(championship), sortTestSession(serverconfig.SessionTypeQualifying), nil, nil); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("sorted by standings, drivers not in standings last", func(t *testing.T) {
		entrants := newEntrants()

		if err := (&ChampionshipStandingsOrderEntryListSort{}).Sort(sortTestRaceWeekendWithChampionship(championship), sortTestSession(serverconfig.SessionTypeQualifying), entrants, nil); err != nil {
			t.Fatal(err)
		}

		assertSortTestOrder(t, entrants, "B", "A", "C", "D")
	})

	t.Run("via GetRaceWeekendEntryListSort, drivers without championship races at the back", func(t *testing.T) {
		entrants := newEntrants()
		session := sortTestSession(serverconfig.SessionTypeQualifying)

		if err := GetRaceWeekendEntryListSort("championship_standings_order").Sort(sortTestRaceWeekendWithChampionship(championship), session, entrants, nil); err != nil {
			t.Fatal(err)
		}

		assertSortTestOrder(t, entrants, "B", "A", "C", "D")
	})

	t.Run("via GetRaceWeekendEntryListSort with reversed grid", func(t *testing.T) {
		entrants := newEntrants()
		session := sortTestSession(serverconfig.SessionTypeQualifying)
		session.NumEntrantsToReverse = -1

		if err := GetRaceWeekendEntryListSort("championship_standings_order").Sort(sortTestRaceWeekendWithChampionship(championship), session, entrants, nil); err != nil {
			t.Fatal(err)
		}

		// reversed: D, C, A, B -> D has not attended any championship race, so goes to the back
		assertSortTestOrder(t, entrants, "C", "A", "B", "D")
	})
}

func TestRaceWeekendSort_ChampionshipClass(t *testing.T) {
	championship := smmodel.NewChampionship("Class Sort")
	classes := []*smmodel.ChampionshipClass{smmodel.NewChampionshipClass("One"), smmodel.NewChampionshipClass("Two")}
	classes[0].AvailableCars = []string{"car_one"}
	classes[1].AvailableCars = []string{"car_two"}

	for _, class := range classes {
		championship.AddClass(class)
	}

	sort.Slice(classes, func(i, j int) bool {
		return classes[i].ID.String() < classes[j].ID.String()
	})

	firstModel, secondModel := classes[0].AvailableCars[0], classes[1].AvailableCars[0]

	newEntrants := func() []*smmodel.RaceWeekendSessionEntrant {
		return buildSortTestEntrants(serverconfig.SessionTypeRace,
			sortTestDriver{guid: "S1", model: secondModel, bestLap: 80000, laps: 1, totalTime: 80000},
			sortTestDriver{guid: "F1", model: firstModel, bestLap: 90000, laps: 1, totalTime: 90000},
			sortTestDriver{guid: "S2", model: secondModel, bestLap: 81000, laps: 1, totalTime: 81000},
			sortTestDriver{guid: "F2", model: firstModel, bestLap: 91000, laps: 1, totalTime: 91000},
		)
	}

	t.Run("without linked championship order is kept", func(t *testing.T) {
		entrants := newEntrants()

		if err := (&ChampionshipClassSort{}).Sort(&smmodel.RaceWeekend{}, sortTestSession(serverconfig.SessionTypeRace), entrants, nil); err != nil {
			t.Fatal(err)
		}

		assertSortTestOrder(t, entrants, "S1", "F1", "S2", "F2")
	})

	t.Run("grouped by class ID", func(t *testing.T) {
		entrants := newEntrants()

		if err := (&ChampionshipClassSort{}).Sort(sortTestRaceWeekendWithChampionship(championship), sortTestSession(serverconfig.SessionTypeRace), entrants, nil); err != nil {
			t.Fatal(err)
		}

		actual := sortTestGUIDs(entrants)
		first := []string{actual[0], actual[1]}
		sort.Strings(first)

		if !reflect.DeepEqual(first, []string{"F1", "F2"}) {
			t.Errorf("expected first class entrants F1, F2 first, got %v", actual)
		}
	})

	t.Run("via GetRaceWeekendEntryListSort it is not split per results class", func(t *testing.T) {
		entrants := newEntrants()

		if err := GetRaceWeekendEntryListSort("championship_class").Sort(sortTestRaceWeekendWithChampionship(championship), sortTestSession(serverconfig.SessionTypeRace), entrants, nil); err != nil {
			t.Fatal(err)
		}

		actual := sortTestGUIDs(entrants)
		first := []string{actual[0], actual[1]}
		sort.Strings(first)

		if !reflect.DeepEqual(first, []string{"F1", "F2"}) {
			t.Errorf("expected first class entrants F1, F2 first, got %v", actual)
		}
	})
}

func TestSortDriversWithNoChampionshipRacesToBackOfGrid(t *testing.T) {
	championship, _ := sortTestChampionship(
		[]string{"A", "B"},
		[]string{"C", "A"},
	)

	t.Run("drivers without attendance go to the back, order kept", func(t *testing.T) {
		entrants := buildSortTestEntrants(serverconfig.SessionTypeRace,
			sortTestDriver{guid: "X"},
			sortTestDriver{guid: "C"},
			sortTestDriver{guid: "Y"},
			sortTestDriver{guid: "A"},
			sortTestDriver{guid: "B"},
		)

		sortDriversWithNoChampionshipRacesToBackOfGrid(championship, entrants)

		assertSortTestOrder(t, entrants, "C", "A", "B", "X", "Y")
	})

	t.Run("incomplete events do not count as attendance", func(t *testing.T) {
		withInProgress, _ := sortTestChampionship([]string{"A"}, []string{"Z", "A"})
		withInProgress.Events[1].CompletedTime = time.Time{}

		entrants := buildSortTestEntrants(serverconfig.SessionTypeRace,
			sortTestDriver{guid: "Z"},
			sortTestDriver{guid: "A"},
		)

		sortDriversWithNoChampionshipRacesToBackOfGrid(withInProgress, entrants)

		assertSortTestOrder(t, entrants, "A", "Z")
	})
}

func TestGetRaceWeekendEntryListSort(t *testing.T) {
	newEntrants := func() []*smmodel.RaceWeekendSessionEntrant {
		return buildSortTestEntrants(serverconfig.SessionTypeQualifying,
			sortTestDriver{guid: "A", bestLap: 92000, laps: 3, totalTime: 300000},
			sortTestDriver{guid: "DNF", bestLap: 0, laps: 0, totalTime: 0},
			sortTestDriver{guid: "B", bestLap: 90000, laps: 3, totalTime: 300000},
			sortTestDriver{guid: "C", bestLap: 91000, laps: 3, totalTime: 300000},
			sortTestDriver{guid: "D", bestLap: 93000, laps: 3, totalTime: 300000},
		)
	}

	cases := []struct {
		name     string
		key      string
		reverse  int
		expected []string
	}{
		{name: "unknown key keeps order but moves drivers without time to the back", key: "does_not_exist", expected: []string{"A", "B", "C", "D", "DNF"}},
		{name: "empty key keeps finishing grid", key: "", expected: []string{"A", "B", "C", "D", "DNF"}},
		{name: "fastest lap", key: "fastest_lap", expected: []string{"B", "C", "A", "D", "DNF"}},
		{name: "fastest lap, all reversed, DNF stays at the back", key: "fastest_lap", reverse: -1, expected: []string{"D", "A", "C", "B", "DNF"}},
		{name: "fastest lap, top 2 reversed", key: "fastest_lap", reverse: 2, expected: []string{"C", "B", "A", "D", "DNF"}},
		{name: "fastest lap, top 3 reversed", key: "fastest_lap", reverse: 3, expected: []string{"A", "C", "B", "D", "DNF"}},
		{name: "fastest lap, reverse more than entrants", key: "fastest_lap", reverse: 50, expected: []string{"D", "A", "C", "B", "DNF"}},
		{name: "alphabetical", key: "alphabetical", expected: []string{"A", "B", "C", "D", "DNF"}},
	}

	for _, tc := range cases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			entrants := newEntrants()
			session := sortTestSession(serverconfig.SessionTypeQualifying)
			session.NumEntrantsToReverse = tc.reverse

			if err := GetRaceWeekendEntryListSort(tc.key).Sort(&smmodel.RaceWeekend{}, session, entrants, nil); err != nil {
				t.Fatal(err)
			}

			assertSortTestOrder(t, entrants, tc.expected...)
		})
	}
}

func TestPerClassSort_MultipleClasses(t *testing.T) {
	slowClass, fastClass := uuid.New(), uuid.New()

	newEntrants := func() []*smmodel.RaceWeekendSessionEntrant {
		return buildSortTestEntrants(serverconfig.SessionTypeQualifying,
			sortTestDriver{guid: "S1", classID: slowClass, bestLap: 100000, laps: 2, totalTime: 200000},
			sortTestDriver{guid: "F1", classID: fastClass, bestLap: 82000, laps: 2, totalTime: 200000},
			sortTestDriver{guid: "S2", classID: slowClass, bestLap: 99000, laps: 2, totalTime: 200000},
			sortTestDriver{guid: "F2", classID: fastClass, bestLap: 81000, laps: 2, totalTime: 200000},
			sortTestDriver{guid: "F3", classID: fastClass, bestLap: 83000, laps: 2, totalTime: 200000},
		)
	}

	t.Run("classes ordered by their fastest lap, sorted within class", func(t *testing.T) {
		entrants := newEntrants()

		if err := GetRaceWeekendEntryListSort("fastest_lap").Sort(&smmodel.RaceWeekend{}, sortTestSession(serverconfig.SessionTypeQualifying), entrants, nil); err != nil {
			t.Fatal(err)
		}

		assertSortTestOrder(t, entrants, "F2", "F1", "F3", "S2", "S1")
	})

	t.Run("reverse grid applies per class", func(t *testing.T) {
		entrants := newEntrants()
		session := sortTestSession(serverconfig.SessionTypeQualifying)
		session.NumEntrantsToReverse = 2

		if err := GetRaceWeekendEntryListSort("fastest_lap").Sort(&smmodel.RaceWeekend{}, session, entrants, nil); err != nil {
			t.Fatal(err)
		}

		assertSortTestOrder(t, entrants, "F1", "F2", "F3", "S1", "S2")
	})

	t.Run("base session without lap data orders classes by class ID", func(t *testing.T) {
		classA, classB := uuid.New(), uuid.New()

		if classA.String() > classB.String() {
			classA, classB = classB, classA
		}

		entrants := buildSortTestEntrants(serverconfig.SessionTypeQualifying,
			sortTestDriver{guid: "B1", classID: classB},
			sortTestDriver{guid: "A1", classID: classA},
			sortTestDriver{guid: "B2", classID: classB},
			sortTestDriver{guid: "A2", classID: classA},
		)

		// the dummy session of a race weekend is the 'base' session
		baseRaceWeekend := smmodel.NewRaceWeekend()
		session, err := baseRaceWeekend.FindSessionByID(baseRaceWeekend.ID.String())

		if err != nil {
			t.Fatal(err)
		}

		session.RaceConfig = sortTestSession(serverconfig.SessionTypeQualifying).RaceConfig

		if err := GetRaceWeekendEntryListSort("").Sort(&smmodel.RaceWeekend{}, session, entrants, nil); err != nil {
			t.Fatal(err)
		}

		assertSortTestOrder(t, entrants, "A1", "A2", "B1", "B2")
	})

	t.Run("classes without a lap time go after classes with one, deterministically", func(t *testing.T) {
		withLap, withoutLap1, withoutLap2 := uuid.New(), uuid.New(), uuid.New()

		if withoutLap1.String() > withoutLap2.String() {
			withoutLap1, withoutLap2 = withoutLap2, withoutLap1
		}

		// the class order used to come from map iteration when a class had no lap time in a
		// non-base session, so run it often enough to catch a random order.
		for run := 0; run < 100; run++ {
			entrants := buildSortTestEntrants(serverconfig.SessionTypeQualifying,
				sortTestDriver{guid: "Z1", classID: withoutLap2, bestLap: 0},
				sortTestDriver{guid: "N1", classID: withoutLap1, bestLap: 0},
				sortTestDriver{guid: "L1", classID: withLap, bestLap: 90000, laps: 1, totalTime: 90000},
			)

			if err := GetRaceWeekendEntryListSort("fastest_lap").Sort(&smmodel.RaceWeekend{}, sortTestSession(serverconfig.SessionTypeQualifying), entrants, nil); err != nil {
				t.Fatal(err)
			}

			if actual := sortTestGUIDs(entrants); !reflect.DeepEqual(actual, []string{"L1", "N1", "Z1"}) {
				t.Fatalf("run %d: expected order [L1 N1 Z1], got %v", run, actual)
			}
		}
	})

	t.Run("classes with equal fastest laps are ordered by class ID", func(t *testing.T) {
		classA, classB := uuid.New(), uuid.New()

		if classA.String() > classB.String() {
			classA, classB = classB, classA
		}

		for run := 0; run < 100; run++ {
			entrants := buildSortTestEntrants(serverconfig.SessionTypeQualifying,
				sortTestDriver{guid: "B1", classID: classB, bestLap: 90000, laps: 1, totalTime: 90000},
				sortTestDriver{guid: "A1", classID: classA, bestLap: 90000, laps: 1, totalTime: 90000},
			)

			if err := GetRaceWeekendEntryListSort("fastest_lap").Sort(&smmodel.RaceWeekend{}, sortTestSession(serverconfig.SessionTypeQualifying), entrants, nil); err != nil {
				t.Fatal(err)
			}

			if actual := sortTestGUIDs(entrants); !reflect.DeepEqual(actual, []string{"A1", "B1"}) {
				t.Fatalf("run %d: expected order [A1 B1], got %v", run, actual)
			}
		}
	})
}
