package racecontrol

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smconfig "github.com/wernersbacher/ACPitwall/internal/config"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/server/servertest"
	"github.com/wernersbacher/ACPitwall/pkg/udp"
)

func TestTeamForDriver(t *testing.T) {
	entryList := serverconfig.EntryList{
		"CAR_0": &serverconfig.Entrant{Name: "A", GUID: "111", Team: "Red Team"},
		"CAR_1": &serverconfig.Entrant{Name: "B", GUID: "222;333", Team: " Blue "},
		"CAR_2": &serverconfig.Entrant{Name: "C", GUID: "", Team: "Ghost"},
	}

	for guid, expected := range map[string]string{"111": "Red Team", "333": "Blue", "222": "Blue", "999": "", "": ""} {
		if got := teamForDriver(entryList, udp.DriverGUID(guid)); got != expected {
			t.Errorf("team of %q: expected %q, got %q", guid, expected, got)
		}
	}
}

func TestRaceNumberForSkin(t *testing.T) {
	dir, err := ioutil.TempDir("", "racenumber")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(dir)

	write := func(skin, content string) {
		path := filepath.Join(dir, "content", "cars", "car", "skins", skin)

		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}

		if err := ioutil.WriteFile(filepath.Join(path, "ui_skin.json"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	write("string", `{"skinname": "x", "number": "7"}`)
	write("num", `{"number": 12}`)
	write("zero", `{"number": "0"}`)
	write("text", `{"number": "GT3!"}`)
	write("none", `{"skinname": "x"}`)
	write("broken", `{`)

	for skin, expected := range map[string]string{"string": "7", "num": "12", "zero": "", "text": "", "none": "", "broken": "", "missing": "", "../x": ""} {
		if got := raceNumberForSkin(dir, "car", skin); got != expected {
			t.Errorf("number of skin %q: expected %q, got %q", skin, expected, got)
		}
	}
}

func TestOnClientConnectSetsIdentity(t *testing.T) {
	process := &servertest.Interruptible{}
	_ = process.Start(&smmodel.CustomRace{EntryList: serverconfig.EntryList{"CAR_0": {Name: "Test 1", GUID: string(drivers[0].DriverGUID), Team: "Red Team"}}}, "", 0, "", 0)

	raceControl := NewRaceControl(&smconfig.Configuration{}, NilBroadcaster{}, NilTrackData{}, process, testStore, NewPenaltiesManager(testStore, testEnv()), testEnv())

	if err := raceControl.OnClientConnect(drivers[0]); err != nil {
		t.Fatal(err)
	}

	driver, ok := raceControl.ConnectedDrivers.Get(drivers[0].DriverGUID)

	if !ok {
		t.Fatal("driver not connected")
	}

	if driver.Team != "Red Team" {
		t.Errorf("expected team Red Team, got %q", driver.Team)
	}

	if driver.RaceNumber != "" {
		t.Errorf("expected no race number, got %q", driver.RaceNumber)
	}
}
