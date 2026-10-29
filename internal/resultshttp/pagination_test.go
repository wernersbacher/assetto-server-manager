package resultshttp

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"
)

func TestListResultsPagination(t *testing.T) {

	fixture, err := ioutil.ReadFile(filepath.Join("..", "..", "fixtures", "results", "2019_2_15_21_16_RACE.json"))

	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name    string
		files   int
		page    int
		wantErr error
		wantLen int
	}{
		{"no results, page 0", 0, 0, nil, 0},
		{"no results, page 1", 0, 1, smresults.ErrPageNotFound, 0},
		{"exactly one full page, page 0", smresults.PageSize, 0, nil, smresults.PageSize},
		{"exactly one full page, page 1", smresults.PageSize, 1, smresults.ErrPageNotFound, 0},
		{"one result more than a page, page 1", smresults.PageSize + 1, 1, nil, 1},
		{"one result more than a page, page 2", smresults.PageSize + 1, 2, smresults.ErrPageNotFound, 0},
		{"negative page", 3, -1, smresults.ErrPageNotFound, 0},
	} {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			dir, err := ioutil.TempDir("", "results-pagination")

			if err != nil {
				t.Fatal(err)
			}

			defer os.RemoveAll(dir)

			resultsDir := filepath.Join(dir, "results")

			if err := os.MkdirAll(resultsDir, 0755); err != nil {
				t.Fatal(err)
			}

			for i := 0; i < tc.files; i++ {
				name := fmt.Sprintf("2019_1_1_%d_%d_RACE.json", i/60, i%60)

				if err := ioutil.WriteFile(filepath.Join(resultsDir, name), fixture, 0644); err != nil {
					t.Fatal(err)
				}
			}

			testInstallPath = dir

			results, _, err := testEnv().Results().List(tc.page, smresults.Filter{})

			if err != tc.wantErr {
				t.Fatalf("expected error %v, got %v", tc.wantErr, err)
			}

			if len(results) != tc.wantLen {
				t.Fatalf("expected %d results, got %d", tc.wantLen, len(results))
			}
		})
	}
}
