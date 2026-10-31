package resultshttp

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestListRecentResults(t *testing.T) {
	defer setupResultsAPITest(t)()

	// files that aren't results are ignored
	if err := os.Mkdir(filepath.Join(testInstallPath, "results", "backup"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := ioutil.WriteFile(filepath.Join(testInstallPath, "results", "readme.txt"), []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}

	t.Run("newest first, limited", func(t *testing.T) {
		results, err := testEnv().Results().ListRecent(2, nil)

		if err != nil {
			t.Fatal(err)
		}

		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}

		if results[0].SessionFile != "2019_3_2_14_41_PRACTICE" || results[1].SessionFile != "2019_2_15_21_16_RACE" {
			t.Errorf("unexpected order: %s, %s", results[0].SessionFile, results[1].SessionFile)
		}
	})

	t.Run("fewer results than requested", func(t *testing.T) {
		results, err := testEnv().Results().ListRecent(10, nil)

		if err != nil {
			t.Fatal(err)
		}

		if len(results) != 3 {
			t.Fatalf("expected 3 results, got %d", len(results))
		}
	})
}
