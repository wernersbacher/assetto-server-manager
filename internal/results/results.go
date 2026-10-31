package results

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
)

type SessionResults struct {
	Cars           []*SessionCar            `json:"Cars"`
	Events         []*SessionEvent          `json:"Events"`
	Laps           []*SessionLap            `json:"Laps"`
	Result         []*SessionResult         `json:"Result"`
	TrackConfig    string                   `json:"TrackConfig"`
	TrackName      string                   `json:"TrackName"`
	Type           serverconfig.SessionType `json:"Type"`
	Date           time.Time                `json:"Date"`
	SessionFile    string                   `json:"SessionFile"`
	ChampionshipID string                   `json:"ChampionshipID"`
	RaceWeekendID  string                   `json:"RaceWeekendID"`
}

var ErrSessionCarNotFound = errors.New("servermanager: session car not found")

// Clear Kicked GUIDs removes any instances of the kicked guid from the results
func (s *SessionResults) ClearKickedGUIDs() {
	// remove any instances of KickedGUID in result
	for _, result := range s.Result {
		if result.DriverGUID == KickedGUID {
			for _, car := range s.Cars {
				if car.CarID == result.CarID {
					// check if guid isn't KickedGUID, if not try using guidsList
					if car.Driver.GUID != KickedGUID {
						result.DriverGUID = car.Driver.GUID
					} else {
						if len(car.Driver.GuidsList) >= 1 {
							result.DriverGUID = car.Driver.GuidsList[0]
							car.Driver.GUID = car.Driver.GuidsList[0]
						}
					}
				}
			}
		}
	}

	// remove any instances of KickedGUID in cars
	for _, car := range s.Cars {
		if car.Driver.GUID == KickedGUID {
			for _, result := range s.Result {
				if car.CarID == result.CarID {
					// check if guid isn't KickedGUID, if not try using guidsList
					if result.DriverGUID != KickedGUID {
						car.Driver.GUID = result.DriverGUID
					} else {
						if len(car.Driver.GuidsList) >= 1 {
							result.DriverGUID = car.Driver.GuidsList[0]
							car.Driver.GUID = car.Driver.GuidsList[0]
						}
					}
				}
			}
		}
	}
}

// NormaliseCarIDs fixes any instances where CarID may have changed in some laps
func (s *SessionResults) NormaliseCarIDs() {
	var carIDsNeedNormalising bool

resultCarIDCheck:
	for _, result := range s.Result {
		for _, car := range s.Cars {
			if car.Driver.GUID == result.DriverGUID && car.Model == result.CarModel && car.CarID != result.CarID {
				carIDsNeedNormalising = true
				break resultCarIDCheck
			}
		}

		for _, lap := range s.Laps {
			if lap.DriverGUID == result.DriverGUID && lap.CarModel == result.CarModel && lap.CarID != result.CarID {
				carIDsNeedNormalising = true
				break resultCarIDCheck
			}
		}
	}

	if !carIDsNeedNormalising {
		return
	}

	logrus.Infof("CarIDs in event results do not match, attempting automatic fix.")

	for i, result := range s.Result {
		result.CarID = i
	}

	for _, result := range s.Result {
		for _, car := range s.Cars {
			if car.Driver.GUID == result.DriverGUID && car.Model == result.CarModel {
				car.CarID = result.CarID
			}
		}

		// events might get mixed up, not a lot we can do
		for _, event := range s.Events {
			if event.Driver.GUID == result.DriverGUID {
				event.CarID = result.CarID
			}

			if event.OtherDriver.GUID == result.DriverGUID {
				event.OtherCarID = result.CarID
			}
		}

		for _, lap := range s.Laps {
			if lap.DriverGUID == result.DriverGUID && lap.CarModel == result.CarModel {
				lap.CarID = result.CarID
			}
		}
	}
}

func (s *SessionResults) FindCarByGUIDAndModel(guid, model string) (*SessionCar, error) {
	carID, ok := s.FindCarIDForGUIDAndModel(guid, model)

	if !ok {
		return nil, ErrSessionCarNotFound
	}

	for _, car := range s.Cars {
		if car.CarID == carID {
			return car, nil
		}
	}

	return nil, ErrSessionCarNotFound
}

func (s *SessionResults) Anonymize() {
	for _, car := range s.Cars {
		car.Driver.GUID = AnonymiseDriverGUID(car.Driver.GUID)
		car.Driver.Name = ShortenDriverName(car.Driver.Name)

		for index := range car.Driver.GuidsList {
			car.Driver.GuidsList[index] = AnonymiseDriverGUID(car.Driver.GuidsList[index])
		}
	}

	for _, event := range s.Events {
		event.Driver.GUID = AnonymiseDriverGUID(event.Driver.GUID)
		event.OtherDriver.GUID = AnonymiseDriverGUID(event.OtherDriver.GUID)

		event.Driver.Name = ShortenDriverName(event.Driver.Name)
		event.OtherDriver.Name = ShortenDriverName(event.OtherDriver.Name)

		for i, guid := range event.Driver.GuidsList {
			event.Driver.GuidsList[i] = AnonymiseDriverGUID(guid)
		}

		for i, guid := range event.OtherDriver.GuidsList {
			event.OtherDriver.GuidsList[i] = AnonymiseDriverGUID(guid)
		}
	}

	for _, lap := range s.Laps {
		lap.DriverGUID = AnonymiseDriverGUID(lap.DriverGUID)

		lap.DriverName = ShortenDriverName(lap.DriverName)
	}

	for _, result := range s.Result {
		result.DriverGUID = AnonymiseDriverGUID(result.DriverGUID)

		result.DriverName = ShortenDriverName(result.DriverName)
	}
}

func (s *SessionResults) NormaliseDriverSwapGUIDs() {
	carIDToGUID := make(map[int]string)

	for _, car := range s.Cars {
		if len(car.Driver.GuidsList) > 1 {
			// this car has been subjected to a driver swap.
			carIDToGUID[car.CarID] = serverconfig.NormaliseEntrantGUIDs(car.Driver.GuidsList)
		}
	}

	for _, result := range s.Result {
		if overrideGUID, ok := carIDToGUID[result.CarID]; ok {
			result.DriverGUID = overrideGUID
		}
	}
}

func (s *SessionResults) IsTimeAttack() bool {
	return strings.HasSuffix(s.SessionFile, TimeAttackSuffix)
}

func AnonymiseDriverGUID(guid string) string {
	hasher := md5.New()
	_, _ = hasher.Write([]byte(guid))
	return hex.EncodeToString(hasher.Sum(nil))
}

func (s *SessionResults) MaskDriverNames() {
	for _, car := range s.Cars {
		car.Driver.Name = DriverName(car.Driver.Name)
	}

	for _, event := range s.Events {
		event.Driver.Name = DriverName(event.Driver.Name)
		event.OtherDriver.Name = DriverName(event.OtherDriver.Name)
	}

	for _, lap := range s.Laps {
		lap.DriverName = DriverName(lap.DriverName)
	}

	for _, result := range s.Result {
		result.DriverName = DriverName(result.DriverName)
	}
}

func (s *SessionResults) RenameDriver(guid, newName string) {
	for _, car := range s.Cars {
		if car.Driver.GUID == guid {
			car.Driver.Name = newName
		}
	}

	for _, event := range s.Events {
		if event.Driver.GUID == guid {
			event.Driver.Name = newName
		}

		if event.OtherDriver.GUID == guid {
			event.OtherDriver.Name = newName
		}
	}

	for _, lap := range s.Laps {
		if lap.DriverGUID == guid {
			lap.DriverName = newName
		}
	}

	for _, result := range s.Result {
		if result.DriverGUID == guid {
			result.DriverName = newName
		}
	}
}

func (s *SessionResults) DriversHaveTeams() bool {
	teams := make(map[string]string)

	for _, car := range s.Cars {
		teams[car.Driver.GUID] = car.Driver.Team
	}

	for _, driver := range s.Result {
		if driver.TotalTime > 0 {
			if team, ok := teams[driver.DriverGUID]; ok && team != "" {
				return true
			}
		}
	}

	return false
}

func (s *SessionResults) GetURL(baseURL string) string {
	return baseURL + "/results/download/" + s.SessionFile + ".json"
}

func (s *SessionResults) GetCrashes(guid, model string) int {
	carID, ok := s.FindCarIDForGUIDAndModel(guid, model)

	if !ok {
		return 0
	}

	var num int

	for _, event := range s.Events {
		if event.CarID == carID {
			num++
		}
	}

	return num
}

func (s *SessionResults) GetCrashesOfType(guid, model, collisionType string) int {
	carID, ok := s.FindCarIDForGUIDAndModel(guid, model)

	if !ok {
		return 0
	}

	var num int

	for _, event := range s.Events {
		if event.CarID == carID && event.Type == collisionType {
			num++
		}
	}

	return num
}

func (s *SessionResults) GetDriverDescriptionForLap(lap *SessionLap, autoFillEntrantList []*serverconfig.Entrant) string {
	var car *SessionCar

	for _, sessionCar := range s.Cars {
		if sessionCar.CarID == lap.CarID {
			car = sessionCar
			break
		}
	}

	if car == nil {
		return lap.DriverName
	}

	for _, entrant := range autoFillEntrantList {
		if entrant.GUID == lap.DriverGUID {
			return entrant.Name
		}
	}

	driverIndex := -1

	for index, guid := range car.Driver.GuidsList {
		if guid == lap.DriverGUID {
			driverIndex = index
			break
		}
	}

	if driverIndex >= 0 {
		return fmt.Sprintf("Driver #%d", driverIndex+1)
	}

	return lap.DriverName
}

// FindCarIDForGUIDAndModel returns the ID of the car that the driver (or, for driver swaps, any of the drivers
// or the joined GUID of all drivers) drove with the given model. ok is false if there is no such car, carID is
// then -1. Note that 0 is a valid car ID.
func (s *SessionResults) FindCarIDForGUIDAndModel(guid, model string) (carID int, ok bool) {
	for _, car := range s.Cars {
		if car.Model != model {
			continue
		}

		if car.Driver.GUID == guid {
			return car.CarID, true
		}

		if serverconfig.NormaliseEntrantGUIDs(car.Driver.GuidsList) == guid {
			return car.CarID, true
		}

		for _, carGUID := range car.Driver.GuidsList {
			if guid == carGUID {
				return car.CarID, true
			}
		}
	}

	return -1, false
}

func (s *SessionResults) GetAverageLapTime(guid, model string) time.Duration {
	carID, ok := s.FindCarIDForGUIDAndModel(guid, model)

	if !ok {
		return 0
	}

	var laps []*SessionLap

	for _, lap := range s.Laps {
		if lap.CarID == carID {
			laps = append(laps, lap)
		}
	}

	average := averageLapTimeMS(laps)

	if average == 0 {
		return 0
	}

	return s.GetTime(average, guid, model, false)
}

// averageLapTimeMS returns the average lap time (in ms) of the given laps, leaving out the first lap of each car
// (out lap / standing start), laps with cuts and laps that are slower than 107% of the average of the laps
// counted so far. It returns 0 if no laps are left to average.
func averageLapTimeMS(laps []*SessionLap) int {
	lapsPerCar := make(map[int]int)

	var lapsForAverage, totalTimeForAverage int

	for _, lap := range laps {
		lapsPerCar[lap.CarID]++

		if lapsPerCar[lap.CarID] == 1 || lap.Cuts != 0 {
			continue
		}

		if lapsForAverage > 0 && float64(lap.LapTime) >= float64(totalTimeForAverage)/float64(lapsForAverage)*1.07 {
			continue
		}

		totalTimeForAverage += lap.LapTime
		lapsForAverage++
	}

	if lapsForAverage == 0 {
		return 0
	}

	return totalTimeForAverage / lapsForAverage
}

func (s *SessionResults) GetOverallAverageLapTime() time.Duration {
	return time.Duration(averageLapTimeMS(s.Laps)) * time.Millisecond
}

// carLaps returns all laps of the car the driver drove with the given model. Like GetNumLaps, it works per car: for
// a driver swap car, the laps of all drivers are returned, whether guid is one of the drivers or the joined GUID.
func (s *SessionResults) carLaps(guid, model string) []*SessionLap {
	carID, ok := s.FindCarIDForGUIDAndModel(guid, model)

	if !ok {
		return nil
	}

	var laps []*SessionLap

	for _, lap := range s.Laps {
		if lap.CarID == carID {
			laps = append(laps, lap)
		}
	}

	return laps
}

// GetConsistency compares the best clean lap with the average lap time (both of the whole car) in percent.
func (s *SessionResults) GetConsistency(guid, model string) float64 {
	var bestLap int

	for _, lap := range s.carLaps(guid, model) {
		if lap.Cuts == 0 && (bestLap == 0 || lap.LapTime < bestLap) {
			bestLap = lap.LapTime
		}
	}

	var percentage float64

	average := s.GetAverageLapTime(guid, model)
	best := s.GetTime(bestLap, guid, model, false)

	if average != 0 && best != 0 {
		consistency := average.Seconds() - best.Seconds()

		percentage = 100 - ((consistency / best.Seconds()) * 100)
	} else {
		percentage = 0
	}

	return math.Round(percentage*100) / 100
}

// lapNum is the drivers current lap
func (s *SessionResults) GetPosForLap(guid, model string, lapNum int64) int {
	var pos int

	driverLap := make(map[int]int)

	for _, lap := range s.Laps {
		driverLap[lap.CarID]++

		if driverLap[lap.CarID] == int(lapNum) && s.LapAssociatedWithGUIDAndModel(lap, guid, model) {
			return pos + 1
		} else if driverLap[lap.CarID] == int(lapNum) {
			pos++
		}
	}

	return 0
}

func (s *SessionResults) IsFastestLap(time, cuts int) bool {
	if cuts != 0 {
		return false
	}

	fastest := true

	for _, lap := range s.Laps {
		if lap.LapTime < time && lap.Cuts == 0 {
			fastest = false
			break
		}
	}

	return fastest
}

func (s *SessionResults) IsDriversFastestLap(guid, model string, time, cuts int) bool {
	if cuts != 0 {
		return false
	}

	fastest := true

	for _, lap := range s.Laps {
		if lap.LapTime < time && lap.DriverGUID == guid && lap.CarModel == model && lap.Cuts == 0 {
			fastest = false
			break
		}
	}

	return fastest
}

func (s *SessionResults) GetDriversFastestLap(guid, model string) *SessionLap {
	var fastest *SessionLap

	for _, lap := range s.Laps {
		if lap.DriverGUID == guid && lap.CarModel == model && (fastest == nil || lap.LapTime < fastest.LapTime) && lap.Cuts == 0 {
			fastest = lap
		}
	}

	return fastest
}

func (s *SessionResults) IsFastestSector(sector, time, cuts int) bool {
	if cuts != 0 {
		return false
	}

	fastest := true

	for _, lap := range s.Laps {
		if sector >= len(lap.Sectors) {
			continue
		}

		if lap.Sectors[sector] < time && lap.Cuts == 0 {
			fastest = false
			break
		}
	}

	return fastest
}

func (s *SessionResults) IsDriversFastestSector(guid, model string, sector, time, cuts int) bool {
	if cuts != 0 {
		return false
	}

	fastest := true

	for _, lap := range s.Laps {
		if sector >= len(lap.Sectors) {
			continue
		}

		if lap.Sectors[sector] < time && lap.DriverGUID == guid && lap.CarModel == model && lap.Cuts == 0 {
			fastest = false
			break
		}
	}

	return fastest
}

func (s *SessionResults) UpdateDate(date time.Time) {
	s.Date = date
	s.SessionFile = fmt.Sprintf("%d_%d_%d_%d_%d_%s.json", date.Year(), date.Month(), date.Day(), date.Hour(), date.Minute(), s.Type.OriginalString())
}

// KickedGUID is the placeholder GUID the AC server reports for kicked drivers.
const KickedGUID = "$#@@!$kicked"

// UseShortenedDriverNames is a display option: when true, driver names are shortened ("John Smith" becomes
// "John S.") wherever results are rendered or exposed. It is set from the server options by the application.
var UseShortenedDriverNames = true

// ShortenDriverName turns "John Smith" into "John S.".
func ShortenDriverName(name string) string {
	nameParts := strings.Split(name, " ")

	if len(nameParts) > 1 && len(nameParts[len(nameParts)-1]) > 1 {
		nameParts[len(nameParts)-1] = nameParts[len(nameParts)-1][:1] + "."
	}

	return strings.Join(nameParts, " ")
}

// DriverName returns the name as it should be displayed, honouring UseShortenedDriverNames.
func DriverName(name string) string {
	if UseShortenedDriverNames {
		return ShortenDriverName(name)
	}

	return name
}

// DriverInitials returns the initials of a driver: the first letters of all parts of the name if
// UseShortenedDriverNames is set, otherwise the first three letters of the last name.
func DriverInitials(name string) string {
	name = strings.TrimSpace(name)

	if UseShortenedDriverNames {
		nameParts := strings.Split(name, " ")

		if len(nameParts) == 1 {
			return name
		}

		for i := range nameParts {
			if len(nameParts[i]) > 0 {
				nameParts[i] = nameParts[i][:1]
			}
		}

		return strings.ToUpper(strings.Join(nameParts, ""))
	}

	nameParts := strings.Split(name, " ")

	if len(nameParts) > 0 && len(nameParts[len(nameParts)-1]) >= 3 {
		return strings.ToUpper(nameParts[len(nameParts)-1][:3])
	}

	return strings.ToUpper(name)
}

// IsEmpty reports whether the results contain no session data at all (no track, laps, results or cars), which is the
// case for JSON files that are not session results.
func (s *SessionResults) IsEmpty() bool {
	return s.TrackName == "" && s.Type == "" && len(s.Result) == 0 && len(s.Laps) == 0 && len(s.Cars) == 0
}
