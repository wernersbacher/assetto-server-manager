package results

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

// twoDirLoader returns a loader over two new temporary directories, and a function which removes them.
func twoDirLoader(t *testing.T) (*Loader, func()) {
	t.Helper()

	root, err := ioutil.TempDir("", "asm-test-results")

	if err != nil {
		t.Fatal(err)
	}

	dirs := []string{filepath.Join(root, "a"), filepath.Join(root, "b")}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}

	return &Loader{Dir: dirs[0], ExtraDirs: dirs[1:]}, func() { _ = os.RemoveAll(root) }
}

func writeResult(t *testing.T, dir, name, track string) {
	t.Helper()

	data := `{"TrackName":"` + track + `","Result":[{"TotalTime":1,"DriverName":"A"}]}`

	if err := ioutil.WriteFile(filepath.Join(dir, name), []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

func TestLoaderListAllMergesDirs(t *testing.T) {
	l, cleanup := twoDirLoader(t)
	defer cleanup()

	writeResult(t, l.Dir, "2020_1_1_10_0_RACE.json", "a")
	writeResult(t, l.ExtraDirs[0], "2020_1_2_10_0_RACE.json", "b")
	writeResult(t, l.Dir, "2020_1_3_10_0_RACE.json", "a")
	writeResult(t, l.ExtraDirs[0], "2020_1_1_10_0_RACE.json", "shadowed")

	results, err := l.ListAll()

	if err != nil {
		t.Fatal(err)
	}

	want := []string{"2020_1_3_10_0_RACE", "2020_1_2_10_0_RACE", "2020_1_1_10_0_RACE"}

	if len(results) != len(want) {
		t.Fatalf("expected %d results, got %d", len(want), len(results))
	}

	for i, name := range want {
		if results[i].SessionFile != name {
			t.Errorf("result %d: expected %s, got %s", i, name, results[i].SessionFile)
		}
	}

	// the primary directory wins for a file name that exists in both
	if results[2].TrackName != "a" {
		t.Errorf("expected the file of the primary dir, got track %q", results[2].TrackName)
	}

	recent, err := l.ListRecent(2, nil)

	if err != nil {
		t.Fatal(err)
	}

	if len(recent) != 2 || recent[0].SessionFile != want[0] || recent[1].SessionFile != want[1] {
		t.Errorf("unexpected recent results: %v", recent)
	}
}

func TestLoaderListPaginatesAcrossDirs(t *testing.T) {
	l, cleanup := twoDirLoader(t)
	defer cleanup()

	// 30 files, day n lives in dir a if n is odd, else in dir b
	for day := 1; day <= 30; day++ {
		dir := l.Dir

		if day%2 == 0 {
			dir = l.ExtraDirs[0]
		}

		writeResult(t, dir, "2020_1_"+itoa(day)+"_10_0_RACE.json", "a")
	}

	page0, pages, err := l.List(0, Filter{})

	if err != nil {
		t.Fatal(err)
	}

	if len(pages) != 2 || len(page0) != PageSize {
		t.Fatalf("expected 2 pages and %d results, got %d pages and %d results", PageSize, len(pages), len(page0))
	}

	if page0[0].SessionFile != "2020_1_30_10_0_RACE" || page0[PageSize-1].SessionFile != "2020_1_6_10_0_RACE" {
		t.Errorf("unexpected first page: %s ... %s", page0[0].SessionFile, page0[PageSize-1].SessionFile)
	}

	page1, _, err := l.List(1, Filter{})

	if err != nil {
		t.Fatal(err)
	}

	if len(page1) != 5 || page1[0].SessionFile != "2020_1_5_10_0_RACE" || page1[4].SessionFile != "2020_1_1_10_0_RACE" {
		t.Errorf("unexpected second page: %v", page1)
	}
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}

	return itoa(i/10) + string(rune('0'+i%10))
}

func TestLoaderLoadSaveRemoveUseOwningDir(t *testing.T) {
	l, cleanup := twoDirLoader(t)
	defer cleanup()

	other := l.ExtraDirs[0]

	writeResult(t, other, "2020_1_1_10_0_RACE.json", "b")

	result, err := l.Load("2020_1_1_10_0_RACE.json")

	if err != nil {
		t.Fatalf("load from the second dir: %v", err)
	}

	// saving an existing file writes it back to the dir which holds it
	result.TrackName = "changed"

	if err := l.Save("2020_1_1_10_0_RACE.json", result); err != nil {
		t.Fatal(err)
	}

	if fileExists(filepath.Join(l.Dir, "2020_1_1_10_0_RACE.json")) {
		t.Error("existing file was saved to the primary dir")
	}

	reloaded, err := l.Load("2020_1_1_10_0_RACE.json")

	if err != nil || reloaded.TrackName != "changed" {
		t.Errorf("expected changed file, got %v, %v", reloaded, err)
	}

	// a new file goes to the primary (write) dir
	if err := l.Save("2020_1_2_10_0_RACE.json", result); err != nil {
		t.Fatal(err)
	}

	if !fileExists(filepath.Join(l.Dir, "2020_1_2_10_0_RACE.json")) {
		t.Error("new file was not saved to the primary dir")
	}

	if err := l.Remove("2020_1_1_10_0_RACE.json", nil); err != nil {
		t.Fatal(err)
	}

	if fileExists(filepath.Join(other, "2020_1_1_10_0_RACE.json")) {
		t.Error("file was not removed from the second dir")
	}

	if _, err := l.Load("2020_1_1_10_0_RACE.json"); err == nil {
		t.Error("expected an error loading a removed file")
	}
}

func TestLoaderIgnoresMissingExtraDir(t *testing.T) {
	l, cleanup := twoDirLoader(t)
	defer cleanup()

	l.ExtraDirs = append(l.ExtraDirs, filepath.Join(l.Dir, "missing"))
	writeResult(t, l.Dir, "2020_1_1_10_0_RACE.json", "a")

	results, err := l.ListAll()

	if err != nil || len(results) != 1 {
		t.Errorf("expected one result, got %d, %v", len(results), err)
	}
}

func TestListRecentSkipsFilesWithoutSessionData(t *testing.T) {
	l, cleanup := twoDirLoader(t)
	defer cleanup()

	writeResult(t, l.Dir, "2020_1_1_10_0_RACE.json", "a")

	if err := ioutil.WriteFile(filepath.Join(l.Dir, "2020_1_2_10_0_RACE.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}

	recent, err := l.ListRecent(5, nil)

	if err != nil {
		t.Fatal(err)
	}

	if len(recent) != 1 || recent[0].SessionFile != "2020_1_1_10_0_RACE" {
		t.Errorf("expected only the real result, got %v", recent)
	}
}

func TestLoaderFiles(t *testing.T) {
	l, cleanup := twoDirLoader(t)
	defer cleanup()

	writeResult(t, l.Dir, "2020_1_1_10_0_RACE.json", "a")
	writeResult(t, l.ExtraDirs[0], "2020_1_1_11_0_RACE.json", "b")
	writeResult(t, l.ExtraDirs[0], "2020_1_1_10_0_RACE.json", "shadowed")

	if err := ioutil.WriteFile(filepath.Join(l.Dir, "notes.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	files, err := l.Files()

	if err != nil {
		t.Fatal(err)
	}

	got := make(map[string]string)

	for _, f := range files {
		got[f.Info.Name()] = f.Dir
	}

	if len(got) != 2 || got["2020_1_1_10_0_RACE.json"] != l.Dir || got["2020_1_1_11_0_RACE.json"] != l.ExtraDirs[0] {
		t.Fatalf("unexpected files: %v", got)
	}
}
