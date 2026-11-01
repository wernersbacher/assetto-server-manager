package racecontrol

import (
	"encoding/json"
	"io/ioutil"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/pkg/udp"
)

// maxRaceNumberLength limits the livery number shown in the live timing.
const maxRaceNumberLength = 4

// teamForDriver returns the team of a connected driver from the entry list of the running event. In a championship
// event the entry list carries the team of the championship entrant. Drivers sharing a car (driver swap) are listed as
// "guid1;guid2" in the entry list.
func teamForDriver(entryList serverconfig.EntryList, guid udp.DriverGUID) string {
	if guid == "" {
		return ""
	}

	for _, entrant := range entryList {
		if entrant == nil || entrant.GUID == "" {
			continue
		}

		for _, entrantGUID := range strings.Split(entrant.GUID, serverconfig.DriverSwapEntrantSeparator) {
			if strings.TrimSpace(entrantGUID) == string(guid) {
				return strings.TrimSpace(entrant.Team)
			}
		}
	}

	return ""
}

// raceNumberForSkin reads the livery number of a skin (the "number" of its ui_skin.json). There is no other source of
// race numbers: the entry list and the championship sign-ups do not hold one. Skins without a usable number (empty,
// "0", too long or not a number) give an empty string.
func raceNumberForSkin(installPath, carModel, skin string) string {
	if installPath == "" || carModel == "" || skin == "" || strings.ContainsAny(carModel+skin, `/\`) || strings.Contains(carModel+skin, "..") {
		return ""
	}

	data, err := ioutil.ReadFile(filepath.Join(installPath, "content", "cars", carModel, "skins", skin, "ui_skin.json"))

	if err != nil {
		return ""
	}

	var uiSkin struct {
		Number interface{} `json:"number"`
	}

	if err := json.Unmarshal(data, &uiSkin); err != nil {
		return ""
	}

	var number string

	switch n := uiSkin.Number.(type) {
	case string:
		number = strings.TrimSpace(n)
	case float64:
		number = strconv.Itoa(int(n))
	}

	if number == "" || len(number) > maxRaceNumberLength {
		return ""
	}

	if value, err := strconv.Atoi(number); err != nil || value <= 0 {
		return ""
	}

	return number
}

// setIdentity fills the team and race number of a driver for the car it connected with.
func (rc *RaceControl) setIdentity(driver *smmodel.RaceControlDriver) {
	if event := rc.process.Event(); event != nil {
		driver.Team = teamForDriver(event.GetEntryList(), driver.CarInfo.DriverGUID)
	}

	driver.RaceNumber = ""

	if rc.env != nil && rc.env.InstallPath != nil {
		driver.RaceNumber = raceNumberForSkin(rc.env.InstallPath(), driver.CarInfo.CarModel, driver.CarInfo.CarSkin)
	}
}
