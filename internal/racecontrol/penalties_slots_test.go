package racecontrol

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"
	"github.com/wernersbacher/assetto-server-manager/internal/serverenv/envtest"
)

// The penalties manager holds the Env of the default slot. A penalty for a result which another slot's acServer wrote
// has to change that file in the other slot's results directory and must not create a copy in the default slot's.
func TestPenaltiesManagerResultOfOtherSlot(t *testing.T) {
	dir, err := ioutil.TempDir("", "penalties-slots")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(dir)

	defaultInstall := filepath.Join(dir, "a")
	otherInstall := filepath.Join(dir, "b")

	for _, install := range []string{defaultInstall, otherInstall} {
		if err := os.MkdirAll(filepath.Join(install, "results"), 0755); err != nil {
			t.Fatal(err)
		}
	}

	fixture, err := ioutil.ReadFile(filepath.Join("..", "..", "fixtures", "results", "2019_2_15_21_16_RACE.json"))

	if err != nil {
		t.Fatal(err)
	}

	const fileName = "2019_2_15_21_16_RACE.json"

	otherFile := filepath.Join(otherInstall, "results", fileName)

	if err := ioutil.WriteFile(otherFile, fixture, 0644); err != nil {
		t.Fatal(err)
	}

	env := envtest.New(defaultInstall)
	env.ResultsDirs = func() []string {
		return []string{filepath.Join(defaultInstall, "results"), filepath.Join(otherInstall, "results")}
	}

	pm := NewPenaltiesManager(nil, env)

	if err := pm.applyPenalty(fileName, "76561198029578060", "rss_formula_rss_4", 10, true); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(defaultInstall, "results", fileName)); !os.IsNotExist(err) {
		t.Fatalf("the penalty created a copy in the results of the default slot (err: %v)", err)
	}

	// read the other slot's file directly
	other := &smresults.Loader{Dir: filepath.Join(otherInstall, "results")}

	result, err := other.Load(fileName)

	if err != nil {
		t.Fatal(err)
	}

	var penalised bool

	for _, entry := range result.Result {
		if entry.DriverGUID == "76561198029578060" && entry.HasPenalty {
			penalised = true
		}
	}

	if !penalised {
		t.Fatal("the result file of the other slot does not hold the penalty")
	}
}
