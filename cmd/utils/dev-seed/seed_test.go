package main

import (
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	servermanager "github.com/wernersbacher/assetto-server-manager"
)

const testConfig = `
http:
  session_key: test-session-key
  session_store_type: cookie
store:
  type: json
  path: store
steam:
  install_path: assetto
`

func TestSeedIsIdempotentAndStandingsWork(t *testing.T) {
	dir, err := ioutil.TempDir("", "dev-seed-test")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(dir)

	if err := ioutil.WriteFile(filepath.Join(dir, "config.yml"), []byte(testConfig), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "assetto"), 0755); err != nil {
		t.Fatal(err)
	}

	wd, _ := os.Getwd()

	defer os.Chdir(wd)

	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	conf, err := servermanager.ReadConfig("config.yml")

	if err != nil {
		t.Fatal(err)
	}

	servermanager.SetAssettoInstallPath(conf.Steam.InstallPath)

	store, closeStore, err := openStore(conf)

	if err != nil {
		t.Fatal(err)
	}

	defer closeStore()

	var out bytes.Buffer

	if err := seed(store, false, time.Now(), &out); err != nil {
		t.Fatalf("seed: %v\n%s", err, out.String())
	}

	t.Log(out.String())

	files, _ := ioutil.ReadDir(filepath.Join(dir, "assetto", "results"))
	numFiles := len(files)

	if numFiles < 12 {
		t.Fatalf("expected at least 12 result files, got %d", numFiles)
	}

	championships, races, weekends, entrants := countSeeded(store)

	// second run changes nothing
	out.Reset()

	if err := seed(store, false, time.Now().Add(48*time.Hour), &out); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), "already seeded") {
		t.Fatalf("second run should be a no-op: %s", out.String())
	}

	files, _ = ioutil.ReadDir(filepath.Join(dir, "assetto", "results"))
	championships2, races2, weekends2, entrants2 := countSeeded(store)

	if len(files) != numFiles || championships2 != championships || races2 != races || weekends2 != weekends || entrants2 != entrants {
		t.Fatal("seeding twice duplicated data")
	}

	if championships != 2 || races != 3 || weekends != 1 || entrants != 10 {
		t.Fatalf("unexpected counts: %d championships, %d races, %d weekends, %d entrants", championships, races, weekends, entrants)
	}

	// reset removes everything, seeding again recreates it
	out.Reset()

	if err := seed(store, true, time.Now(), &out); err != nil {
		t.Fatalf("reset: %v", err)
	}

	files, _ = ioutil.ReadDir(filepath.Join(dir, "assetto", "results"))
	championships3, races3, _, _ := countSeeded(store)

	if len(files) != numFiles || championships3 != 2 || races3 != 3 {
		t.Fatalf("reset + seed gave %d files, %d championships, %d races", len(files), championships3, races3)
	}
}

// countSeeded counts the seeded objects in the store. A fresh store contains examples added by migrations.
func countSeeded(store servermanager.Store) (championships, races, weekends, entrants int) {
	cs, _ := store.ListChampionships()

	for _, c := range cs {
		if c.ID == devCupID || c.ID == devSprintID {
			championships++
		}
	}

	rs, _ := store.ListCustomRaces()

	for _, r := range rs {
		for _, name := range customRaceName {
			if r.UUID == seedID("customrace/"+name) {
				races++
			}
		}
	}

	ws, _ := store.ListRaceWeekends()

	for _, w := range ws {
		if w.ID == weekendID {
			weekends++
		}
	}

	es, _ := store.ListEntrants()

	for _, e := range es {
		for _, d := range seedDrivers {
			if e.GUID == d.GUID {
				entrants++
			}
		}
	}

	return
}
