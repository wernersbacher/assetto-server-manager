package resultshttp

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	smresults "github.com/wernersbacher/ACPitwall/internal/results"
)

func writeResultsFixtures(t *testing.T, resultsDir string, names ...string) {
	t.Helper()

	for _, name := range names {
		data, err := ioutil.ReadFile(filepath.Join("..", "..", "fixtures", "results", name))

		if err != nil {
			t.Fatal(err)
		}

		if err := ioutil.WriteFile(filepath.Join(resultsDir, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestListResultsFilter(t *testing.T) {

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

	testInstallPath = dir

	resetResultSummaryCache := func() {
		smresults.ClearSummaryCache()
	}

	resetResultSummaryCache()
	defer resetResultSummaryCache()

	for _, tc := range []struct {
		name   string
		filter smresults.Filter
		want   []string
	}{
		{"no filter", smresults.Filter{}, []string{
			"2019_2_15_21_16_RACE", "2019_2_15_21_8_QUALIFY", "2019_2_15_22_10_RACE", "2019_3_2_14_41_PRACTICE",
			"2019_3_2_21_21_QUALIFY", "2019_3_2_21_36_RACE", "2019_3_2_22_15_QUALIFY",
		}},
		{"type race", smresults.Filter{Type: "race"}, []string{"2019_2_15_21_16_RACE", "2019_2_15_22_10_RACE", "2019_3_2_21_36_RACE"}},
		{"type qualify", smresults.Filter{Type: "qualify"}, []string{"2019_2_15_21_8_QUALIFY", "2019_3_2_21_21_QUALIFY", "2019_3_2_22_15_QUALIFY"}},
		{"type practice, case insensitive", smresults.Filter{Type: "Practice"}, []string{"2019_3_2_14_41_PRACTICE"}},
		{"type booking", smresults.Filter{Type: "booking"}, nil},
		{"track name", smresults.Filter{Track: "barbagallo"}, []string{"2019_2_15_21_16_RACE", "2019_2_15_21_8_QUALIFY"}},
		{"track readable name, case insensitive", smresults.Filter{Track: "RED BULL"}, []string{"2019_2_15_22_10_RACE"}},
		{"track layout", smresults.Filter{Track: "sprint_b"}, []string{"2019_3_2_21_21_QUALIFY", "2019_3_2_21_36_RACE"}},
		{"track name matching several tracks", smresults.Filter{Track: "suzuka"}, []string{"2019_3_2_14_41_PRACTICE", "2019_3_2_22_15_QUALIFY"}},
		{"driver name, case insensitive", smresults.Filter{Driver: "cheezy"}, []string{"2019_3_2_21_36_RACE", "2019_3_2_22_15_QUALIFY"}},
		{"driver guid", smresults.Filter{Driver: "76561198437072463"}, []string{"2019_3_2_21_21_QUALIFY", "2019_3_2_22_15_QUALIFY"}},
		{"driver guid is not a substring match", smresults.Filter{Driver: "7656119843707"}, nil},
		{"type and driver", smresults.Filter{Type: "race", Driver: "cheezy"}, []string{"2019_3_2_21_36_RACE"}},
		{"track and driver", smresults.Filter{Track: "suzuka", Driver: "cheezy"}, []string{"2019_3_2_22_15_QUALIFY"}},
		{"type, track and driver", smresults.Filter{Type: "qualify", Track: "nurburgring", Driver: "76561198437072463"}, []string{"2019_3_2_21_21_QUALIFY"}},
		{"no matches", smresults.Filter{Driver: "nobody by that name"}, nil},
		{"no matches because of a combination", smresults.Filter{Type: "practice", Driver: "cheezy"}, nil},
	} {
		tc := tc

		// run twice, the second run is served from the cache
		for _, run := range []string{"cold cache", "warm cache"} {
			t.Run(tc.name+", "+run, func(t *testing.T) {
				results, pages, err := testEnv().Results().List(0, tc.filter)

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
		if _, _, err := testEnv().Results().List(1, smresults.Filter{Driver: "nobody by that name"}); err != smresults.ErrPageNotFound {
			t.Fatalf("expected error %v, got %v", smresults.ErrPageNotFound, err)
		}
	})

	t.Run("changed file is reloaded", func(t *testing.T) {
		name := "2019_3_2_14_41_PRACTICE.json"

		results, _, err := testEnv().Results().List(0, smresults.Filter{Track: "red bull"})

		if err != nil || len(results) != 1 {
			t.Fatalf("expected the red bull ring race only, got %d results, error %v", len(results), err)
		}

		// replace the practice with a file with another size, that is at the red bull ring
		data, err := ioutil.ReadFile(filepath.Join("..", "..", "fixtures", "results", "2019_3_2_21_49_QUALIFY.json"))

		if err != nil {
			t.Fatal(err)
		}

		if err := ioutil.WriteFile(filepath.Join(resultsDir, name), data, 0644); err != nil {
			t.Fatal(err)
		}

		results, _, err = testEnv().Results().List(0, smresults.Filter{Track: "red bull"})

		if err != nil || len(results) != 2 {
			t.Fatalf("expected 2 results after the file changed, got %d, error %v", len(results), err)
		}
	})
}

func TestListResultsFilterPagination(t *testing.T) {

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
	fixture, err := ioutil.ReadFile(filepath.Join("..", "..", "fixtures", "results", "2019_2_15_21_16_RACE.json"))

	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < smresults.PageSize+3; i++ {
		name := fmt.Sprintf("2019_1_1_%d_%d_RACE.json", i/60, i%60)

		if err := ioutil.WriteFile(filepath.Join(resultsDir, name), fixture, 0644); err != nil {
			t.Fatal(err)
		}
	}

	writeResultsFixtures(t, resultsDir, "2019_3_2_14_41_PRACTICE.json", "2019_3_2_21_36_RACE.json")

	testInstallPath = dir

	smresults.ClearSummaryCache()

	filter := smresults.Filter{Type: "race", Track: "barbagallo"}

	for _, tc := range []struct {
		page    int
		wantErr error
		wantLen int
	}{
		{0, nil, smresults.PageSize},
		{1, nil, 3},
		{2, smresults.ErrPageNotFound, 0},
	} {
		results, pages, err := testEnv().Results().List(tc.page, filter)

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
