package servermanager

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestListResultsPagination(t *testing.T) {
	oldInstallPath := ServerInstallPath
	defer func() { ServerInstallPath = oldInstallPath }()

	fixture, err := ioutil.ReadFile(filepath.Join("fixtures", "results", "2019_2_15_21_16_RACE.json"))

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
		{"no results, page 1", 0, 1, ErrResultsPageNotFound, 0},
		{"exactly one full page, page 0", pageSize, 0, nil, pageSize},
		{"exactly one full page, page 1", pageSize, 1, ErrResultsPageNotFound, 0},
		{"one result more than a page, page 1", pageSize + 1, 1, nil, 1},
		{"one result more than a page, page 2", pageSize + 1, 2, ErrResultsPageNotFound, 0},
		{"negative page", 3, -1, ErrResultsPageNotFound, 0},
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

			ServerInstallPath = dir

			results, _, err := listResults(tc.page)

			if err != tc.wantErr {
				t.Fatalf("expected error %v, got %v", tc.wantErr, err)
			}

			if len(results) != tc.wantLen {
				t.Fatalf("expected %d results, got %d", tc.wantLen, len(results))
			}
		})
	}
}
