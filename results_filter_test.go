package servermanager

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func writeResultsFixtures(t *testing.T, resultsDir string, names ...string) {
	t.Helper()

	for _, name := range names {
		data, err := ioutil.ReadFile(filepath.Join("fixtures", "results", name))

		if err != nil {
			t.Fatal(err)
		}

		if err := ioutil.WriteFile(filepath.Join(resultsDir, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestListResultsFilter(t *testing.T) {
	oldInstallPath := ServerInstallPath
	defer func() { ServerInstallPath = oldInstallPath }()

	dir, err := ioutil.TempDir("", "results-filter")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(dir)

	resultsDir := filepath.Join(dir, "results")

	if err := os.MkdirAll(resultsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// barbagallo, red bull ring (layout_national), suzuka (suzukagp), suzuka (suzukaeast), nurburgring (layout_sprint_b)
	writeResultsFixtures(t, resultsDir,
		"2019_2_15_21_16_RACE.json",
		"2019_2_15_21_8_QUALIFY.json",
		"2019_2_15_22_10_RACE.json",
		"2019_3_2_14_41_PRACTICE.json",
		"2019_3_2_21_21_QUALIFY.json",
		"2019_3_2_21_36_RACE.json",
		"2019_3_2_22_15_QUALIFY.json",
	)

	ServerInstallPath = dir

	resetResultSummaryCache := func() {
		resultSummaryCache.Lock()
		resultSummaryCache.entries = make(map[string]*resultSummary)
		resultSummaryCache.Unlock()
	}

	resetResultSummaryCache()
	defer resetResultSummaryCache()

	for _, tc := range []struct {
		name   string
		filter ResultsFilter
		want   []string
	}{
		{"no filter", ResultsFilter{}, []string{
			"2019_2_15_21_16_RACE", "2019_2_15_21_8_QUALIFY", "2019_2_15_22_10_RACE", "2019_3_2_14_41_PRACTICE",
			"2019_3_2_21_21_QUALIFY", "2019_3_2_21_36_RACE", "2019_3_2_22_15_QUALIFY",
		}},
		{"type race", ResultsFilter{Type: "race"}, []string{"2019_2_15_21_16_RACE", "2019_2_15_22_10_RACE", "2019_3_2_21_36_RACE"}},
		{"type qualify", ResultsFilter{Type: "qualify"}, []string{"2019_2_15_21_8_QUALIFY", "2019_3_2_21_21_QUALIFY", "2019_3_2_22_15_QUALIFY"}},
		{"type practice, case insensitive", ResultsFilter{Type: "Practice"}, []string{"2019_3_2_14_41_PRACTICE"}},
		{"type booking", ResultsFilter{Type: "booking"}, nil},
		{"track name", ResultsFilter{Track: "barbagallo"}, []string{"2019_2_15_21_16_RACE", "2019_2_15_21_8_QUALIFY"}},
		{"track readable name, case insensitive", ResultsFilter{Track: "RED BULL"}, []string{"2019_2_15_22_10_RACE"}},
		{"track layout", ResultsFilter{Track: "sprint_b"}, []string{"2019_3_2_21_21_QUALIFY", "2019_3_2_21_36_RACE"}},
		{"track name matching several tracks", ResultsFilter{Track: "suzuka"}, []string{"2019_3_2_14_41_PRACTICE", "2019_3_2_22_15_QUALIFY"}},
		{"driver name, case insensitive", ResultsFilter{Driver: "cheezy"}, []string{"2019_3_2_21_36_RACE", "2019_3_2_22_15_QUALIFY"}},
		{"driver guid", ResultsFilter{Driver: "76561198437072463"}, []string{"2019_3_2_21_21_QUALIFY", "2019_3_2_22_15_QUALIFY"}},
		{"driver guid is not a substring match", ResultsFilter{Driver: "7656119843707"}, nil},
		{"type and driver", ResultsFilter{Type: "race", Driver: "cheezy"}, []string{"2019_3_2_21_36_RACE"}},
		{"track and driver", ResultsFilter{Track: "suzuka", Driver: "cheezy"}, []string{"2019_3_2_22_15_QUALIFY"}},
		{"type, track and driver", ResultsFilter{Type: "qualify", Track: "nurburgring", Driver: "76561198437072463"}, []string{"2019_3_2_21_21_QUALIFY"}},
		{"no matches", ResultsFilter{Driver: "nobody by that name"}, nil},
		{"no matches because of a combination", ResultsFilter{Type: "practice", Driver: "cheezy"}, nil},
	} {
		tc := tc

		// run twice, the second run is served from the cache
		for _, run := range []string{"cold cache", "warm cache"} {
			t.Run(tc.name+", "+run, func(t *testing.T) {
				results, pages, err := listResults(0, tc.filter)

				if err != nil {
					t.Fatal(err)
				}

				var got []string

				for _, result := range results {
					got = append(got, result.SessionFile)
				}

				sort.Strings(got)

				if !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("expected %v, got %v", tc.want, got)
				}

				if len(tc.want) > 0 && len(pages) != 1 {
					t.Fatalf("expected 1 page, got %d", len(pages))
				}

				if len(tc.want) == 0 && len(pages) != 0 {
					t.Fatalf("expected no pages, got %d", len(pages))
				}
			})
		}
	}

	t.Run("no matches, page 1 is not found", func(t *testing.T) {
		if _, _, err := listResults(1, ResultsFilter{Driver: "nobody by that name"}); err != ErrResultsPageNotFound {
			t.Fatalf("expected error %v, got %v", ErrResultsPageNotFound, err)
		}
	})

	t.Run("changed file is reloaded", func(t *testing.T) {
		name := "2019_3_2_14_41_PRACTICE.json"

		results, _, err := listResults(0, ResultsFilter{Track: "red bull"})

		if err != nil || len(results) != 1 {
			t.Fatalf("expected the red bull ring race only, got %d results, error %v", len(results), err)
		}

		// replace the practice with a file with another size, that is at the red bull ring
		data, err := ioutil.ReadFile(filepath.Join("fixtures", "results", "2019_3_2_21_49_QUALIFY.json"))

		if err != nil {
			t.Fatal(err)
		}

		if err := ioutil.WriteFile(filepath.Join(resultsDir, name), data, 0644); err != nil {
			t.Fatal(err)
		}

		results, _, err = listResults(0, ResultsFilter{Track: "red bull"})

		if err != nil || len(results) != 2 {
			t.Fatalf("expected 2 results after the file changed, got %d, error %v", len(results), err)
		}
	})
}

func TestListResultsFilterPagination(t *testing.T) {
	oldInstallPath := ServerInstallPath
	defer func() { ServerInstallPath = oldInstallPath }()

	dir, err := ioutil.TempDir("", "results-filter-pagination")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(dir)

	resultsDir := filepath.Join(dir, "results")

	if err := os.MkdirAll(resultsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// barbagallo races (more than a page of them) and a few results at other tracks
	fixture, err := ioutil.ReadFile(filepath.Join("fixtures", "results", "2019_2_15_21_16_RACE.json"))

	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < pageSize+3; i++ {
		name := fmt.Sprintf("2019_1_1_%d_%d_RACE.json", i/60, i%60)

		if err := ioutil.WriteFile(filepath.Join(resultsDir, name), fixture, 0644); err != nil {
			t.Fatal(err)
		}
	}

	writeResultsFixtures(t, resultsDir, "2019_3_2_14_41_PRACTICE.json", "2019_3_2_21_36_RACE.json")

	ServerInstallPath = dir

	resultSummaryCache.Lock()
	resultSummaryCache.entries = make(map[string]*resultSummary)
	resultSummaryCache.Unlock()

	filter := ResultsFilter{Type: "race", Track: "barbagallo"}

	for _, tc := range []struct {
		page    int
		wantErr error
		wantLen int
	}{
		{0, nil, pageSize},
		{1, nil, 3},
		{2, ErrResultsPageNotFound, 0},
	} {
		results, pages, err := listResults(tc.page, filter)

		if err != tc.wantErr {
			t.Fatalf("page %d: expected error %v, got %v", tc.page, tc.wantErr, err)
		}

		if len(results) != tc.wantLen {
			t.Fatalf("page %d: expected %d results, got %d", tc.page, tc.wantLen, len(results))
		}

		if err == nil && len(pages) != 2 {
			t.Fatalf("page %d: expected 2 pages, got %d", tc.page, len(pages))
		}
	}
}
