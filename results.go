package servermanager

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io/ioutil"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

type SessionResults struct {
	Cars           []*SessionCar    `json:"Cars"`
	Events         []*SessionEvent  `json:"Events"`
	Laps           []*SessionLap    `json:"Laps"`
	Result         []*SessionResult `json:"Result"`
	TrackConfig    string           `json:"TrackConfig"`
	TrackName      string           `json:"TrackName"`
	Type           SessionType      `json:"Type"`
	Date           time.Time        `json:"Date"`
	SessionFile    string           `json:"SessionFile"`
	ChampionshipID string           `json:"ChampionshipID"`
	RaceWeekendID  string           `json:"RaceWeekendID"`
}

var ErrSessionCarNotFound = errors.New("servermanager: session car not found")

// Clear Kicked GUIDs removes any instances of the kicked guid from the results
func (s *SessionResults) ClearKickedGUIDs() {
	// remove any instances of kickedGUID in result
	for _, result := range s.Result {
		if result.DriverGUID == kickedGUID {
			for _, car := range s.Cars {
				if car.CarID == result.CarID {
					// check if guid isn't kickedGUID, if not try using guidsList
					if car.Driver.GUID != kickedGUID {
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

	// remove any instances of kickedGUID in cars
	for _, car := range s.Cars {
		if car.Driver.GUID == kickedGUID {
			for _, result := range s.Result {
				if car.CarID == result.CarID {
					// check if guid isn't kickedGUID, if not try using guidsList
					if result.DriverGUID != kickedGUID {
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
		car.Driver.Name = shortenDriverName(car.Driver.Name)

		for index := range car.Driver.GuidsList {
			car.Driver.GuidsList[index] = AnonymiseDriverGUID(car.Driver.GuidsList[index])
		}
	}

	for _, event := range s.Events {
		event.Driver.GUID = AnonymiseDriverGUID(event.Driver.GUID)
		event.OtherDriver.GUID = AnonymiseDriverGUID(event.OtherDriver.GUID)

		event.Driver.Name = shortenDriverName(event.Driver.Name)
		event.OtherDriver.Name = shortenDriverName(event.OtherDriver.Name)

		for i, guid := range event.Driver.GuidsList {
			event.Driver.GuidsList[i] = AnonymiseDriverGUID(guid)
		}

		for i, guid := range event.OtherDriver.GuidsList {
			event.OtherDriver.GuidsList[i] = AnonymiseDriverGUID(guid)
		}
	}

	for _, lap := range s.Laps {
		lap.DriverGUID = AnonymiseDriverGUID(lap.DriverGUID)

		lap.DriverName = shortenDriverName(lap.DriverName)
	}

	for _, result := range s.Result {
		result.DriverGUID = AnonymiseDriverGUID(result.DriverGUID)

		result.DriverName = shortenDriverName(result.DriverName)
	}
}

func (s *SessionResults) NormaliseDriverSwapGUIDs() {
	carIDToGUID := make(map[int]string)

	for _, car := range s.Cars {
		if len(car.Driver.GuidsList) > 1 {
			// this car has been subjected to a driver swap.
			carIDToGUID[car.CarID] = NormaliseEntrantGUIDs(car.Driver.GuidsList)
		}
	}

	for _, result := range s.Result {
		if overrideGUID, ok := carIDToGUID[result.CarID]; ok {
			result.DriverGUID = overrideGUID
		}
	}
}

func (s *SessionResults) IsTimeAttack() bool {
	return strings.HasSuffix(s.SessionFile, timeAttackSuffix)
}

func AnonymiseDriverGUID(guid string) string {
	hasher := md5.New()
	_, _ = hasher.Write([]byte(guid))
	return hex.EncodeToString(hasher.Sum(nil))
}

func (s *SessionResults) MaskDriverNames() {
	for _, car := range s.Cars {
		car.Driver.Name = driverName(car.Driver.Name)
	}

	for _, event := range s.Events {
		event.Driver.Name = driverName(event.Driver.Name)
		event.OtherDriver.Name = driverName(event.OtherDriver.Name)
	}

	for _, lap := range s.Laps {
		lap.DriverName = driverName(lap.DriverName)
	}

	for _, result := range s.Result {
		result.DriverName = driverName(result.DriverName)
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

func (s *SessionResults) GetURL() string {
	return config.HTTP.BaseURL + "/results/download/" + s.SessionFile + ".json"
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

func (s *SessionResults) GetDriverDescriptionForLap(lap *SessionLap, autoFillEntrantList []*Entrant) string {
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

		if NormaliseEntrantGUIDs(car.Driver.GuidsList) == guid {
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

func (s *SessionResults) FallBackSort() {
	// sort the results by laps completed then race time
	// this is a fall back for when assetto's sorting is terrible
	// sort results.Result, if disqualified go to back, if time penalty sort by laps completed then lap time

cars:
	for i := range s.Cars {
		for z := range s.Result {
			if s.Cars[i].Driver.GUID == s.Result[z].DriverGUID {
				continue cars
			}
		}

		var bestLap int

		for y := range s.Laps {
			if (s.Cars[i].Driver.GUID == s.Laps[y].DriverGUID) && (s.Cars[i].Model == s.Laps[y].CarModel) && s.IsDriversFastestLap(s.Cars[i].Driver.GUID, s.Cars[i].Model, s.Laps[y].LapTime, s.Laps[y].Cuts) {
				bestLap = s.Laps[y].LapTime
				break
			}
		}

		s.Result = append(s.Result, &SessionResult{
			BallastKG:    s.Cars[i].BallastKG,
			BestLap:      bestLap,
			CarID:        s.Cars[i].CarID,
			CarModel:     s.Cars[i].Model,
			DriverGUID:   s.Cars[i].Driver.GUID,
			DriverName:   s.Cars[i].Driver.Name,
			Restrictor:   s.Cars[i].Restrictor,
			TotalTime:    0,
			HasPenalty:   false,
			PenaltyTime:  time.Duration(0),
			LapPenalty:   0,
			Disqualified: false,
		})
	}

laps:
	// in the crazy case that a driver has laps but is in neither car or results list
	for _, lap := range s.Laps {
		for _, result := range s.Result {
			if result.DriverGUID == lap.DriverGUID {
				continue laps
			}
		}

		bestLap := 0

		for _, findBestLap := range s.Laps {
			if (findBestLap.DriverGUID == lap.DriverGUID) && (findBestLap.CarModel == lap.CarModel) && s.IsDriversFastestLap(findBestLap.DriverGUID, findBestLap.CarModel, findBestLap.LapTime, findBestLap.Cuts) {
				bestLap = findBestLap.LapTime
				break
			}
		}

		s.Result = append(s.Result, &SessionResult{
			BallastKG:    lap.BallastKG,
			BestLap:      bestLap,
			CarID:        lap.CarID,
			CarModel:     lap.CarModel,
			DriverGUID:   lap.DriverGUID,
			DriverName:   lap.DriverName,
			Restrictor:   lap.Restrictor,
			TotalTime:    0,
			HasPenalty:   false,
			PenaltyTime:  0,
			LapPenalty:   0,
			Disqualified: false,
			ClassID:      lap.ClassID,
		})
	}

	for i := range s.Result {
		s.Result[i].TotalTime = 0

		for _, lap := range s.Laps {
			if lap.DriverGUID == s.Result[i].DriverGUID {
				s.Result[i].TotalTime += lap.LapTime
			}
		}

		// TotalTime stays the plain race time (in ms), like in the results written by the AC server. Time penalties are
		// added on top by GetTime(..., penalty=true), which is used for sorting and display.

		if s.Result[i].BestLap == 0 {
			for _, findBestLap := range s.Laps {
				if (findBestLap.DriverGUID == s.Result[i].DriverGUID) && (findBestLap.CarModel == s.Result[i].CarModel) && s.IsDriversFastestLap(findBestLap.DriverGUID, findBestLap.CarModel, findBestLap.LapTime, findBestLap.Cuts) {
					s.Result[i].BestLap = findBestLap.LapTime
					break
				}
			}
		}
	}

results:
	// is this result car in the car list? If not then add it
	for _, result := range s.Result {
		for _, car := range s.Cars {
			if result.DriverGUID == car.Driver.GUID && result.CarModel == car.Model {
				continue results
			}
		}

		guidList := []string{result.DriverGUID}

		driver := SessionDriver{
			GUID:      result.DriverGUID,
			GuidsList: guidList,
			Name:      result.DriverName,
			Nation:    "",
			Team:      "",
			ClassID:   result.ClassID,
		}

		s.Cars = append(s.Cars, &SessionCar{
			BallastKG:  result.BallastKG,
			CarID:      result.CarID,
			Driver:     driver,
			Model:      result.CarModel,
			Restrictor: result.Restrictor,
			Skin:       "",
		})
	}

	sort.Slice(s.Result, func(i, j int) bool {
		if (!s.Result[i].Disqualified && !s.Result[j].Disqualified) || (s.Result[i].Disqualified && s.Result[j].Disqualified) {

			if s.Type == SessionTypeQualifying || s.Type == SessionTypePractice {

				if s.Result[i].BestLap == 0 {
					return false
				}

				if s.Result[j].BestLap == 0 {
					return true
				}

				return s.GetTime(s.Result[i].BestLap, s.Result[i].DriverGUID, s.Result[i].CarModel, true) <
					s.GetTime(s.Result[j].BestLap, s.Result[j].DriverGUID, s.Result[j].CarModel, true)
			}

			// if both drivers aren't/are disqualified
			if s.GetNumLaps(s.Result[i].DriverGUID, s.Result[i].CarModel) == s.GetNumLaps(s.Result[j].DriverGUID, s.Result[j].CarModel) {
				if s.Result[i].HasPenalty || s.Result[j].HasPenalty {
					return s.GetTime(s.Result[i].TotalTime, s.Result[i].DriverGUID, s.Result[i].CarModel, true) <
						s.GetTime(s.Result[j].TotalTime, s.Result[j].DriverGUID, s.Result[j].CarModel, true)
				}

				// if their number of laps are equal, compare last lap pos
				return s.GetLastLapPos(s.Result[i].DriverGUID, s.Result[i].CarModel) < s.GetLastLapPos(s.Result[j].DriverGUID, s.Result[j].CarModel)
			}

			return s.GetNumLaps(s.Result[i].DriverGUID, s.Result[i].CarModel) >= s.GetNumLaps(s.Result[j].DriverGUID, s.Result[j].CarModel)

		}

		// driver i is closer to the front than j if they are not disqualified and j is
		return s.Result[j].Disqualified
	})
}

func (s *SessionResults) GetDate() string {
	return s.Date.Format(time.RFC822)
}

// GetNumSectors returns a slice with one entry per sector (for ranging over in templates). Laps may have a differing
// number of sectors (e.g. incomplete laps), the lap with the most sectors is used.
func (s *SessionResults) GetNumSectors() []int {
	var numSectors int

	for _, lap := range s.Laps {
		if len(lap.Sectors) > numSectors {
			numSectors = len(lap.Sectors)
		}
	}

	num := make([]int, numSectors)

	for i := range num {
		num[i] = 1
	}

	return num
}

func (s *SessionResults) GetDrivers() string {
	var drivers []string

	for _, car := range s.Result {
		if car.DriverName != "" {
			drivers = append(drivers, driverName(car.DriverName))
		}
	}

	return strings.Join(drivers, ", ")
}

func (s *SessionResults) GetTime(timeINT int, driverGUID, model string, penalty bool) time.Duration {
	if i := s.GetNumLaps(driverGUID, model); i == 0 {
		return time.Duration(0)
	}

	carID, ok := s.FindCarIDForGUIDAndModel(driverGUID, model)

	if !ok {
		return time.Duration(0)
	}

	d, _ := time.ParseDuration(fmt.Sprintf("%dms", timeINT))

	if penalty {
		for _, driver := range s.Result {
			if driver.CarID == carID && driver.HasPenalty {
				d += driver.PenaltyTime

				if s.Type == SessionTypeRace {
					d -= time.Duration(driver.LapPenalty) * s.GetLastLapTime(driverGUID, model)
				}
			}
		}
	}

	return d
}

func (s *SessionResults) GetTeamName(driverGUID string) string {
	for _, car := range s.Cars {
		if car.Driver.GUID == driverGUID {
			return car.Driver.Team
		}
	}

	return ""
}

func (s *SessionResults) GetNumLaps(driverGUID, model string) int {
	carID, ok := s.FindCarIDForGUIDAndModel(driverGUID, model)

	if !ok {
		return 0
	}

	var i int

	for _, lap := range s.Laps {
		if lap.CarID == carID {
			i++
		}
	}

	// Apply lap penalty
	for _, driver := range s.Result {
		if driver.CarID == carID {
			i -= driver.LapPenalty
		}
	}

	return i
}

func (s *SessionResults) GetLastLapTime(driverGUID, model string) time.Duration {
	carID, ok := s.FindCarIDForGUIDAndModel(driverGUID, model)

	if !ok {
		return 1
	}

	for i := len(s.Laps) - 1; i >= 0; i-- {
		if s.Laps[i].CarID == carID {
			return s.Laps[i].GetLapTime()
		}
	}

	return 1
}

func (s *SessionResults) HasHandicaps() bool {
	for _, car := range s.Result {
		if car.BallastKG > 0 || car.Restrictor > 0 {
			return true
		}
	}

	return false
}

// GetPotentialLap sums up the best sectors of all clean laps of the driver's car.
func (s *SessionResults) GetPotentialLap(driverGUID, model string) time.Duration {
	var sectors []int

	for _, lap := range s.carLaps(driverGUID, model) {
		if lap.Cuts > 0 {
			continue
		}

		for i, sector := range lap.Sectors {
			if i >= len(sectors) {
				sectors = append(sectors, sector)
			} else if sectors[i] == 0 || sector < sectors[i] {
				sectors[i] = sector
			}
		}
	}

	var totalSectorTime time.Duration

	for _, sector := range sectors {
		totalSectorTime += time.Duration(sector) * time.Millisecond
	}

	return totalSectorTime
}

// GetLastLapPos returns the position of the driver's car when it completed its last lap.
func (s *SessionResults) GetLastLapPos(driverGUID, model string) int {
	return s.GetPosForLap(driverGUID, model, int64(len(s.carLaps(driverGUID, model))))
}

func (s *SessionResults) GetDriverPosition(driverGUID, model string) int {
	for i := range s.Result {
		if s.Result[i].DriverGUID == driverGUID && s.Result[i].CarModel == model {
			return i + 1
		}
	}

	return 0
}

func (s *SessionResults) GetCuts(driverGUID, model string) int {
	carID, ok := s.FindCarIDForGUIDAndModel(driverGUID, model)

	if !ok {
		return 0
	}

	var i int

	for _, lap := range s.Laps {
		if lap.CarID == carID {
			i += lap.Cuts
		}
	}

	return i
}

// FastestLap returns the fastest clean lap of the session, or nil if there is none.
func (s *SessionResults) FastestLap() *SessionLap {
	return fastestCleanLap(s.Laps, func(*SessionLap) bool { return true })
}

// fastestCleanLap returns the fastest lap without cuts that matches filter (the first one on equal times),
// or nil if there is none.
func fastestCleanLap(laps []*SessionLap, filter func(lap *SessionLap) bool) *SessionLap {
	var fastest *SessionLap

	for _, lap := range laps {
		if lap.Cuts != 0 || !filter(lap) {
			continue
		}

		if fastest == nil || lap.LapTime < fastest.LapTime {
			fastest = lap
		}
	}

	return fastest
}

func (s *SessionResults) ResultHasMultipleDrivers(result *SessionResult) bool {
	car, err := s.FindCarByGUIDAndModel(result.DriverGUID, result.CarModel)

	if err != nil {
		return false
	}

	return car.HasMultipleDrivers()
}

func (s *SessionResults) NumberOfDriverSwaps(carID int) int {
	var lastGUID string
	var numSwaps int

	for _, lap := range s.Laps {
		if lap.CarID != carID {
			continue
		}

		if lastGUID == "" {
			lastGUID = lap.DriverGUID
		}

		if lap.DriverGUID != lastGUID {
			numSwaps++
			lastGUID = lap.DriverGUID
		}
	}

	return numSwaps
}

func (s *SessionResults) LapAssociatedWithGUIDAndModel(lap *SessionLap, driverGUID, model string) bool {
	if lap.DriverGUID == driverGUID && lap.CarModel == model {
		return true
	}

	for _, driverGUID := range strings.Split(driverGUID, driverSwapEntrantSeparator) {
		for _, car := range s.Cars {
			if car.Model != model {
				continue
			}

			assoc1 := false
			assoc2 := false

			for _, guid := range car.Driver.GuidsList {
				if guid == lap.DriverGUID {
					assoc1 = true
				}

				if guid == driverGUID {
					assoc2 = true
				}
			}

			if assoc1 && assoc2 {
				return true
			}
		}
	}

	return false
}

// FastestLapInClass returns the fastest clean lap of the given class, or nil if there is none.
func (s *SessionResults) FastestLapInClass(classID uuid.UUID) *SessionLap {
	return fastestCleanLap(s.Laps, func(lap *SessionLap) bool { return lap.ClassID == classID })
}

type SessionResult struct {
	BallastKG    int           `json:"BallastKG"`
	BestLap      int           `json:"BestLap"`
	CarID        int           `json:"CarId"`
	CarModel     string        `json:"CarModel"`
	DriverGUID   string        `json:"DriverGuid"`
	DriverName   string        `json:"DriverName"`
	Restrictor   int           `json:"Restrictor"`
	TotalTime    int           `json:"TotalTime"`
	HasPenalty   bool          `json:"HasPenalty"`
	PenaltyTime  time.Duration `json:"PenaltyTime"`
	LapPenalty   int           `json:"LapPenalty"`
	Disqualified bool          `json:"Disqualified"`
	ClassID      uuid.UUID     `json:"ClassID"`
}

func (s *SessionResult) BestLapTyre(results *SessionResults) string {
	for _, lap := range results.Laps {
		if lap.CarID == s.CarID && lap.LapTime == s.BestLap {
			return lap.Tyre
		}
	}

	return "?"
}

type SessionLap struct {
	BallastKG  int       `json:"BallastKG"`
	CarID      int       `json:"CarId"`
	CarModel   string    `json:"CarModel"`
	Cuts       int       `json:"Cuts"`
	DriverGUID string    `json:"DriverGuid"`
	DriverName string    `json:"DriverName"`
	LapTime    int       `json:"LapTime"`
	Restrictor int       `json:"Restrictor"`
	Sectors    []int     `json:"Sectors"`
	Timestamp  int       `json:"Timestamp"`
	Tyre       string    `json:"Tyre"`
	ClassID    uuid.UUID `json:"ClassID"`
}

func (sl *SessionLap) GetSector(x int) time.Duration {
	d, _ := time.ParseDuration(fmt.Sprintf("%dms", sl.Sectors[x]))

	return d
}

func (sl *SessionLap) GetLapTime() time.Duration {
	d, _ := time.ParseDuration(fmt.Sprintf("%dms", sl.LapTime))

	return d
}

func (sl *SessionLap) DidCheat(averageTime time.Duration) bool {
	d, _ := time.ParseDuration(fmt.Sprintf("%dms", sl.LapTime))

	return d < averageTime && sl.Cuts > 0
}

type SessionCar struct {
	BallastKG  int           `json:"BallastKG"`
	CarID      int           `json:"CarId"`
	Driver     SessionDriver `json:"Driver"`
	Model      string        `json:"Model"`
	Restrictor int           `json:"Restrictor"`
	Skin       string        `json:"Skin"`
}

func (c *SessionCar) GetName() string {
	return c.Driver.Name
}

func (c *SessionCar) GetCar() string {
	return c.Model
}

func (c *SessionCar) GetSkin() string {
	return c.Skin
}

func (c *SessionCar) GetGUID() string {
	return c.Driver.GUID
}

func (c *SessionCar) GetTeam() string {
	return c.Driver.Team
}

func (c *SessionCar) HasMultipleDrivers() bool {
	return len(c.Driver.GuidsList) > 1 || strings.Count(c.GetGUID(), driverSwapEntrantSeparator) > 0
}

type SessionEvent struct {
	CarID         int            `json:"CarId"`
	Driver        *SessionDriver `json:"Driver"`
	ImpactSpeed   float64        `json:"ImpactSpeed"`
	OtherCarID    int            `json:"OtherCarId"`
	OtherDriver   *SessionDriver `json:"OtherDriver"`
	RelPosition   *SessionPos    `json:"RelPosition"`
	Type          string         `json:"Type"`
	WorldPosition *SessionPos    `json:"WorldPosition"`
}

func (se *SessionEvent) GetRelPosition() string {
	return fmt.Sprintf("X: %.1f Y: %.1f Z: %.1f", se.RelPosition.X, se.RelPosition.Y, se.RelPosition.Z)
}

func (se *SessionEvent) GetWorldPosition() string {
	return fmt.Sprintf("X: %.1f Y: %.1f Z: %.1f", se.WorldPosition.X, se.WorldPosition.Y, se.WorldPosition.Z)
}

type SessionDriver struct {
	GUID      string    `json:"Guid"`
	GuidsList []string  `json:"GuidsList"`
	Name      string    `json:"Name"`
	Nation    string    `json:"Nation"`
	Team      string    `json:"Team"`
	ClassID   uuid.UUID `json:"ClassID"`
}

func (sd *SessionDriver) AssignEntrant(entrant *Entrant, classID uuid.UUID) {
	if sd.GUID != entrant.GUID {
		return
	}

	sd.Name = entrant.Name
	sd.Team = entrant.Team
	sd.ClassID = classID
}

type SessionPos struct {
	X float64 `json:"X"`
	Y float64 `json:"Y"`
	Z float64 `json:"Z"`
}

const pageSize = 25

var ErrResultsPageNotFound = errors.New("servermanager: results page not found")

// ResultsFilter narrows down the results list. Empty fields do not filter.
type ResultsFilter struct {
	Type   string // session type as in the file name, e.g. "race". "booking" matches BOOK files.
	Track  string // case-insensitive substring of the track name, readable track name or layout
	Driver string // case-insensitive substring of a driver name, or an exact driver GUID
}

func (f ResultsFilter) IsActive() bool {
	return f.Type != "" || f.Track != "" || f.Driver != ""
}

// needsLoading is true if the filter can only be evaluated by looking into the results files.
func (f ResultsFilter) needsLoading() bool {
	return f.Track != "" || f.Driver != ""
}

// matchesFileName applies the session type filter, which can be decided from the file name alone.
func (f ResultsFilter) matchesFileName(name string) bool {
	if f.Type == "" {
		return true
	}

	name = strings.TrimSuffix(name, ".json")
	name = strings.TrimSuffix(name, timeAttackSuffix)

	sessionType := strings.ToUpper(name[strings.LastIndex(name, "_")+1:])

	want := strings.ToUpper(f.Type)

	if want == "BOOKING" {
		want = string(SessionTypeBooking)
	}

	// RACEx2 counts as a race
	return sessionType == want || (want == string(SessionTypeRace) && strings.HasPrefix(sessionType, want))
}

// resultSummary is the part of a results file that the track and driver filters look at.
type resultSummary struct {
	size    int64
	modTime time.Time

	track   string // lowercased track names and layout, as shown in the list and as stored
	drivers []string
	guids   map[string]bool
}

func (s *resultSummary) matches(f ResultsFilter) bool {
	if f.Track != "" && !strings.Contains(s.track, strings.ToLower(f.Track)) {
		return false
	}

	if f.Driver != "" {
		if s.guids[f.Driver] {
			return true
		}

		driver := strings.ToLower(f.Driver)

		for _, name := range s.drivers {
			if strings.Contains(name, driver) {
				return true
			}
		}

		return false
	}

	return true
}

// resultSummaryCache avoids reparsing every results file for each filtered request. Entries are keyed by file
// name and are only valid while the size and mod time of the file are unchanged.
var resultSummaryCache = struct {
	sync.Mutex
	entries map[string]*resultSummary
}{entries: make(map[string]*resultSummary)}

func getResultSummary(info os.FileInfo) (*resultSummary, error) {
	resultSummaryCache.Lock()
	summary, ok := resultSummaryCache.entries[info.Name()]
	resultSummaryCache.Unlock()

	if ok && summary.size == info.Size() && summary.modTime.Equal(info.ModTime()) {
		return summary, nil
	}

	result, err := LoadResult(info.Name(), LoadResultWithoutPluginFire)

	if err != nil {
		return nil, err
	}

	summary = &resultSummary{
		size:    info.Size(),
		modTime: info.ModTime(),
		track: strings.ToLower(strings.Join([]string{
			result.TrackName, prettifyName(result.TrackName, false), result.TrackConfig, prettifyName(result.TrackConfig, false),
		}, " ")),
		guids: make(map[string]bool),
	}

	for _, car := range result.Cars {
		summary.drivers = append(summary.drivers, strings.ToLower(car.Driver.Name))
		summary.guids[car.Driver.GUID] = true

		for _, guid := range car.Driver.GuidsList {
			summary.guids[guid] = true
		}
	}

	for _, driver := range result.Result {
		summary.drivers = append(summary.drivers, strings.ToLower(driver.DriverName))
		summary.guids[driver.DriverGUID] = true
	}

	resultSummaryCache.Lock()
	resultSummaryCache.entries[info.Name()] = summary
	resultSummaryCache.Unlock()

	return summary, nil
}

func listResults(page int, filter ResultsFilter) ([]SessionResults, []int, error) {
	resultsPath := filepath.Join(ServerInstallPath, "results")
	resultFiles, err := ioutil.ReadDir(resultsPath)

	if err != nil {
		return nil, nil, err
	}

	if filter.Type != "" {
		var typeFiltered []os.FileInfo

		for _, resultFile := range resultFiles {
			if filter.matchesFileName(resultFile.Name()) {
				typeFiltered = append(typeFiltered, resultFile)
			}
		}

		resultFiles = typeFiltered
	}

	sort.Slice(resultFiles, func(i, j int) bool {
		d1, _ := GetResultDate(resultFiles[i].Name())
		d2, _ := GetResultDate(resultFiles[j].Name())

		return d1.After(d2)
	})

	if filter.needsLoading() {
		var matching []os.FileInfo

		for _, resultFile := range resultFiles {
			summary, err := getResultSummary(resultFile)

			if err != nil {
				logrus.WithError(err).Errorf("Could not load results file: %s", resultFile.Name())
				continue
			}

			if summary.matches(filter) {
				matching = append(matching, resultFile)
			}
		}

		resultFiles = matching
	}

	pages := float64(len(resultFiles)) / float64(pageSize)
	pagesRound := math.Ceil(pages)

	// page 0 is always valid, so that an empty results list can still be rendered
	if page < 0 || (page > 0 && page >= int(pagesRound)) {
		return nil, nil, ErrResultsPageNotFound
	}

	var pagesSlice []int

	for x := 0; x < int(pagesRound); x++ {
		pagesSlice = append(pagesSlice, x)
	}

	// get result files for selected page probably
	if len(resultFiles) > page*pageSize+pageSize {
		resultFiles = resultFiles[page*pageSize : page*pageSize+pageSize]
	} else {
		resultFiles = resultFiles[page*pageSize:]
	}

	var results []SessionResults

	for _, resultFile := range resultFiles {
		result, err := LoadResult(resultFile.Name())

		if err != nil {
			logrus.WithError(err).Errorf("Could not load results file: %s", resultFile.Name())
			continue
		}

		results = append(results, *result)
	}

	return results, pagesSlice, nil
}

func ListAllResults() ([]SessionResults, error) {
	resultsPath := filepath.Join(ServerInstallPath, "results")
	resultFiles, err := ioutil.ReadDir(resultsPath)

	if err != nil {
		return nil, err
	}

	sort.Slice(resultFiles, func(i, j int) bool {
		d1, _ := GetResultDate(resultFiles[i].Name())
		d2, _ := GetResultDate(resultFiles[j].Name())

		return d1.After(d2)
	})

	var results []SessionResults

	for _, resultFile := range resultFiles {
		result, err := LoadResult(resultFile.Name(), LoadResultWithoutPluginFire)

		if err != nil {
			logrus.WithError(err).Errorf("Could not load results file: %s", resultFile.Name())
			continue
		}

		results = append(results, *result)
	}

	return results, nil
}

func GetResultDate(name string) (time.Time, error) {
	dateSplit := strings.Split(name, "_")
	dateSplit = dateSplit[0 : len(dateSplit)-1]
	date := strings.Join(dateSplit, "_")

	var year, month, day, hour, minute int

	_, err := fmt.Sscanf(date, "%d_%d_%d_%d_%d", &year, &month, &day, &hour, &minute)

	if err != nil {
		return time.Time{}, err
	}

	return time.Date(year, time.Month(month), day, hour, minute, 0, 0, time.Local), nil
}

var UseFallBackSorting = false

type LoadResultOpts int

const LoadResultWithoutPluginFire LoadResultOpts = 0

func LoadResult(fileName string, opts ...LoadResultOpts) (*SessionResults, error) {
	var result *SessionResults

	resultsPath := filepath.Join(ServerInstallPath, "results")

	data, err := ioutil.ReadFile(filepath.Join(resultsPath, fileName))

	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(data, &result)

	if err != nil {
		return nil, err
	}

	date, err := GetResultDate(fileName)

	if err == nil {
		result.Date = date
	} else {
		logrus.WithError(err).Errorf("Could not parse results date from filename: %s. Using mod time.", fileName)

		f, err := os.Stat(filepath.Join(resultsPath, fileName))

		if err != nil {
			return nil, err
		}

		result.Date = f.ModTime()
	}

	result.SessionFile = strings.Trim(fileName, ".json")

	var validResults []*SessionResult

	// filter out invalid results
	for _, driver := range result.Result {
		if driver.TotalTime > 0 {
			validResults = append(validResults, driver)
		}
	}

	result.Result = validResults

	if UseFallBackSorting {
		result.FallBackSort()
	}

	var skipLua bool

	for _, opt := range opts {
		if opt == LoadResultWithoutPluginFire {
			skipLua = true
		}
	}

	if !skipLua && config.Lua.Enabled && Premium() {
		err = resultsLoadPlugin(result)

		if err != nil {
			logrus.WithError(err).Error("results load plugin script failed")
		}
	}

	return result, nil
}

func resultsLoadPlugin(results *SessionResults) error {
	p := NewLuaPlugin()

	newSessionResults := &SessionResults{}

	p.Inputs(results).Outputs(newSessionResults)
	err := p.Call("./plugins/results.lua", "onResultsLoad")

	if err != nil {
		return err
	}

	*results = *newSessionResults

	return nil
}

type ResultsHandler struct {
	*BaseHandler

	store Store
}

func NewResultsHandler(baseHandler *BaseHandler, store Store) *ResultsHandler {
	return &ResultsHandler{
		BaseHandler: baseHandler,
		store:       store,
	}
}

type resultsListTemplateVars struct {
	BaseTemplateVars

	Results     []SessionResults
	Pages       []int
	CurrentPage int
	Filter      ResultsFilter
}

// resultsListQuery reads the page and filters of a results list request, which is shared by the results list page
// and the results API.
func resultsListQuery(r *http.Request) (int, ResultsFilter) {
	page, err := strconv.Atoi(r.URL.Query().Get("page"))

	if err != nil {
		page = 0
	}

	return page, ResultsFilter{
		Type:   strings.TrimSpace(r.URL.Query().Get("type")),
		Track:  strings.TrimSpace(r.URL.Query().Get("track")),
		Driver: strings.TrimSpace(r.URL.Query().Get("driver")),
	}
}

func (rh *ResultsHandler) list(w http.ResponseWriter, r *http.Request) {
	page, filter := resultsListQuery(r)

	results, pages, err := listResults(page, filter)

	if err == ErrResultsPageNotFound {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	} else if err != nil {
		logrus.WithError(err).Errorf("could not get result list")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	rh.viewRenderer.MustLoadTemplate(w, r, "results/index.html", &resultsListTemplateVars{
		Results:     results,
		Pages:       pages,
		CurrentPage: page,
		Filter:      filter,
	})
}

func (rh *ResultsHandler) uploadHandler(w http.ResponseWriter, r *http.Request) {
	matched, err := rh.upload(r)

	if err != nil {
		logrus.WithError(err).Errorf("could not parse results form")
		AddErrorFlash(w, r, "Sorry, we couldn't parse that results file! Please make sure the format is correct.")
	}

	if !matched {
		AddErrorFlash(w, r, "Your results file content was correct, but the file name is incorrect! Please make sure the file name matches the AC standard then try again.")
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

type combineResultsTemplateVars struct {
	BaseTemplateVars

	Results []SessionResults
}

const combinedSuffix = "-combined"

func (rh *ResultsHandler) combineResults(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			logrus.WithError(err).Error("Combine Results: Couldn't parse form")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		combineResultsSessionFiles := r.Form["CombineResults"]

		var resultsToCombine []*SessionResults

		for _, resultSessionFile := range combineResultsSessionFiles {
			result, err := LoadResult(resultSessionFile + ".json")

			if err != nil {
				logrus.WithError(err).Error("Combine Results: Couldn't load result")
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}

			resultsToCombine = append(resultsToCombine, result)
		}

		combinedResult := combineResults(resultsToCombine)

		combinedResult.FallBackSort()
		combinedResult.UpdateDate(time.Now())

		if !strings.HasSuffix(combinedResult.SessionFile, combinedSuffix) {
			combinedResult.SessionFile = combinedResult.SessionFile + combinedSuffix
		}

		err := saveResults(combinedResult.SessionFile+".json", combinedResult)

		if err != nil {
			logrus.WithError(err).Error("Combine Results: Couldn't save result")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, "/results", http.StatusFound)
		return
	}

	results, err := ListAllResults()

	if err != nil {
		logrus.WithError(err).Error("Combine Results: Couldn't list results")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	rh.viewRenderer.MustLoadTemplate(w, r, "results/combine.html", &combineResultsTemplateVars{
		Results: results,
	})
}

const uploadFileSizeLimit = 5e6

func (rh *ResultsHandler) upload(r *http.Request) (bool, error) {
	err := r.ParseMultipartForm(10 << 20)

	if err != nil {
		return true, err
	}

	file, header, err := r.FormFile("resultsFile")
	if err != nil {
		return true, err
	}
	defer file.Close()

	if header.Size > (uploadFileSizeLimit) {
		return true, fmt.Errorf("servermanager: file size too large, limit is: %d, this file is: %d", int64(uploadFileSizeLimit), header.Size)
	}

	fileBytes, err := ioutil.ReadAll(file)
	if err != nil {
		return true, err
	}

	// make sure what we've been given is actually a results file
	var resultTest *SessionResults

	err = json.Unmarshal(fileBytes, &resultTest)

	if err != nil {
		return true, err
	}

	matched, err := regexp.MatchString(`\d{4}_\d{1,2}_\d{1,2}_\d{1,2}_\d{1,2}_(RACE|QUALIFY|PRACTICE|BOOK)\.json`, header.Filename)

	if err != nil {
		return matched, err
	}

	if !matched || !validResultsFileName(header.Filename) {
		return false, nil
	}

	path := filepath.Join(ServerInstallPath, "results", header.Filename)

	err = ioutil.WriteFile(path, fileBytes, 0644)

	if err != nil {
		return matched, err
	}

	return matched, nil
}

type resultsViewTemplateVars struct {
	BaseTemplateVars

	Result           *SessionResults
	AutoFillEntrants []*Entrant
	Account          *Account
	UseMPH           bool
}

func (rh *ResultsHandler) view(w http.ResponseWriter, r *http.Request) {
	fileName := chi.URLParam(r, "fileName")

	if !validResultsFileName(fileName + ".json") {
		http.NotFound(w, r)
		return
	}

	result, err := LoadResult(fileName + ".json")

	if os.IsNotExist(err) {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	} else if err != nil {
		logrus.WithError(err).Errorf("could not get result")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	serverOpts, err := rh.store.LoadServerOptions()

	if err != nil {
		logrus.WithError(err).Errorf("couldn't load server options")
	}

	autoFillEntrants, err := rh.store.ListEntrants()

	if err != nil {
		logrus.WithError(err).Errorf("couldn't load autofill entrant list")
	}

	result.ClearKickedGUIDs()
	result.NormaliseCarIDs()

	rh.viewRenderer.MustLoadTemplate(w, r, "results/result.html", &resultsViewTemplateVars{
		BaseTemplateVars: BaseTemplateVars{
			WideContainer: true,
		},
		Result:           result,
		AutoFillEntrants: autoFillEntrants,
		Account:          AccountFromRequest(r),
		UseMPH:           serverOpts.UseMPH == 1,
	})
}

func (rh *ResultsHandler) file(w http.ResponseWriter, r *http.Request) {
	fileName := chi.URLParam(r, "fileName")

	if !validResultsFileName(fileName) {
		http.NotFound(w, r)
		return
	}

	result, err := LoadResult(fileName)

	if os.IsNotExist(err) {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	} else if err != nil {
		logrus.WithError(err).Errorf("could not get result")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if UseShortenedDriverNames {
		result.MaskDriverNames()
	}

	w.Header().Add("Content-Type", "application/json")

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(result)
}

func (rh *ResultsHandler) edit(w http.ResponseWriter, r *http.Request) {
	fileName := chi.URLParam(r, "fileName")

	if !validResultsFileName(fileName + ".json") {
		http.NotFound(w, r)
		return
	}

	results, err := LoadResult(fileName + ".json")

	if os.IsNotExist(err) {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	} else if err != nil {
		logrus.WithError(err).Error("could not load results")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	for key, vals := range r.Form {
		if strings.HasPrefix(key, "guid:") {
			guid := strings.TrimPrefix(key, "guid:")
			name := vals[0]

			results.RenameDriver(guid, name)
		}
	}

	err = saveResults(results.SessionFile+".json", results)

	if err != nil {
		logrus.WithError(err).Error("could not load results")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	AddFlash(w, r, "Drivers successfully edited")
	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

var ErrInvalidResultsFileName = errors.New("servermanager: invalid results file name")

// ResultsFileReferencedError is returned when a results file can't be deleted because a championship or race weekend uses it.
type ResultsFileReferencedError struct {
	Reference string
}

func (e ResultsFileReferencedError) Error() string {
	return fmt.Sprintf("servermanager: results file is referenced by %s", e.Reference)
}

// validResultsFileName checks that fileName is a plain base name ending in .json, so that it
// cannot be used to reach files outside of the results directory.
func validResultsFileName(fileName string) bool {
	return fileName != "" &&
		strings.HasSuffix(fileName, ".json") &&
		fileName != ".json" &&
		filepath.Base(fileName) == fileName &&
		!strings.ContainsAny(fileName, `/\`)
}

// findResultsReference returns a description of the championship or race weekend which uses the given results file,
// or an empty string if it is not referenced. Championship events and race weekend sessions embed a copy of the
// results they were built from, so they are matched by the results' SessionFile.
func findResultsReference(store Store, fileName string) (string, error) {
	// LoadResult derives SessionFile from the file name in this way
	sessionFile := strings.Trim(fileName, ".json")

	championships, err := store.ListChampionships()

	if err != nil {
		return "", err
	}

	for _, championship := range championships {
		for _, event := range championship.Events {
			for _, session := range event.Sessions {
				if session.Results != nil && session.Results.SessionFile == sessionFile {
					return fmt.Sprintf("championship %s", championship.Name), nil
				}

				if session.RaceWeekendSession != nil && session.RaceWeekendSession.Results != nil && session.RaceWeekendSession.Results.SessionFile == sessionFile {
					return fmt.Sprintf("championship %s", championship.Name), nil
				}
			}
		}
	}

	raceWeekends, err := store.ListRaceWeekends()

	if err != nil {
		return "", err
	}

	for _, raceWeekend := range raceWeekends {
		for _, session := range raceWeekend.Sessions {
			if session.Results != nil && session.Results.SessionFile == sessionFile {
				return fmt.Sprintf("race weekend %s", raceWeekend.Name), nil
			}
		}
	}

	return "", nil
}

// deleteResultsFile removes a results file from the results directory, unless it is used by a championship or race weekend.
func deleteResultsFile(store Store, fileName string) error {
	if !validResultsFileName(fileName) {
		return ErrInvalidResultsFileName
	}

	path := filepath.Join(ServerInstallPath, "results", fileName)

	info, err := os.Stat(path)

	if err != nil {
		return err
	}

	if !info.Mode().IsRegular() {
		return ErrInvalidResultsFileName
	}

	reference, err := findResultsReference(store, fileName)

	if err != nil {
		return err
	}

	if reference != "" {
		return ResultsFileReferencedError{Reference: reference}
	}

	if err := os.Remove(path); err != nil {
		return err
	}

	resultSummaryCache.Lock()
	delete(resultSummaryCache.entries, fileName)
	resultSummaryCache.Unlock()

	return nil
}

func (rh *ResultsHandler) delete(w http.ResponseWriter, r *http.Request) {
	fileName := chi.URLParam(r, "fileName")

	err := deleteResultsFile(rh.store, fileName)

	var referencedErr ResultsFileReferencedError

	switch {
	case err == ErrInvalidResultsFileName || os.IsNotExist(err):
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	case errors.As(err, &referencedErr):
		logrus.WithError(err).Warnf("refusing to delete results file: %s", fileName)
		AddErrorFlash(w, r, fmt.Sprintf("Results file could not be deleted, it is used by %s", referencedErr.Reference))
	case err != nil:
		logrus.WithError(err).Errorf("could not delete results file: %s", fileName)
		AddErrorFlash(w, r, "Unable to delete the results file")
	default:
		AddFlash(w, r, "Results file successfully deleted")
	}

	http.Redirect(w, r, "/results", http.StatusFound)
}

type vec struct {
	x    float64
	z    float64
	relX float64
	relZ float64

	color color.RGBA
}

func (rh *ResultsHandler) renderCollisions(w http.ResponseWriter, r *http.Request) {
	var collisionsToLoad []int

	fileName := chi.URLParam(r, "fileName")

	if !validResultsFileName(fileName + ".json") {
		http.NotFound(w, r)
		return
	}

	result, err := LoadResult(fileName + ".json")

	if os.IsNotExist(err) {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	} else if err != nil {
		logrus.WithError(err).Errorf("could not get result")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	collisions := r.URL.Query().Get("collisions")

	if collisions == "all" {
		for z := range result.Events {
			collisionsToLoad = append(collisionsToLoad, z)
		}
	} else {
		splitCollisions := strings.Split(collisions, ",")

		for i := range splitCollisions {

			collisionNum, err := strconv.Atoi(splitCollisions[i])

			if err != nil {
				continue
			}

			collisionsToLoad = append(collisionsToLoad, collisionNum)
		}
	}

	trackMapData, err := LoadTrackMapData(result.TrackName, result.TrackConfig)

	if err != nil {
		logrus.WithError(err).Error("could not load track map data")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	trackMapImage, err := LoadTrackMapImage(result.TrackName, result.TrackConfig)

	if err != nil {
		logrus.WithError(err).Error("could not load track map image")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	var collisionVectors []vec

	for i, collision := range result.Events {

		var mainColor color.RGBA

		if collision.Type == "COLLISION_WITH_ENV" {
			mainColor = color.RGBA{R: 255, G: 125, B: 0, A: 0xff}
		} else {
			mainColor = color.RGBA{R: 255, G: 0, B: 0, A: 0xff}
		}

		for z := range collisionsToLoad {
			if collisionsToLoad[z] == i {
				collisionVectors = append(collisionVectors, vec{
					x:     (collision.WorldPosition.X + trackMapData.OffsetX) / trackMapData.ScaleFactor,
					z:     (collision.WorldPosition.Z + trackMapData.OffsetZ) / trackMapData.ScaleFactor,
					relX:  collision.RelPosition.X,
					relZ:  collision.RelPosition.Z,
					color: mainColor,
				})
			}
		}
	}

	img := image.NewRGBA(image.Rectangle{Min: image.Pt(0, 0), Max: image.Pt(trackMapImage.Bounds().Max.X, trackMapImage.Bounds().Max.Y)})
	radius := 4

	for _, collisionVector := range collisionVectors {
		if collisionVector.relX > 0 || collisionVector.relZ > 0 {
			// show the relative collision position
			draw.Draw(img, img.Bounds(), &circle{image.Pt(int(collisionVector.x+collisionVector.relX), int(collisionVector.z+collisionVector.relZ)), radius, color.RGBA{R: 0, G: 0, B: 255, A: 0xff}}, image.Pt(0, 0), draw.Over)
		}

		draw.Draw(img, img.Bounds(), &circle{image.Pt(int(collisionVector.x), int(collisionVector.z)), radius, collisionVector.color}, image.Pt(0, 0), draw.Over)
	}

	err = png.Encode(w, img)

	if err != nil {
		logrus.WithError(err).Error("could not encode image")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
}

type circle struct {
	p     image.Point
	r     int
	color color.RGBA
}

func (c *circle) ColorModel() color.Model {
	return color.AlphaModel
}

func (c *circle) Bounds() image.Rectangle {
	return image.Rect(c.p.X-c.r, c.p.Y-c.r, c.p.X+c.r, c.p.Y+c.r)
}

func (c *circle) At(x, y int) color.Color {
	xx, yy, rr := float64(x-c.p.X)+0.5, float64(y-c.p.Y)+0.5, float64(c.r)
	if xx*xx+yy*yy < rr*rr {
		return c.color
	}
	return color.RGBA{0, 0, 0, 0}
}

// saveResults takes a full json filepath (including the json extension) and saves the results to that file.
func saveResults(jsonFileName string, results *SessionResults) error {
	path := filepath.Join(ServerInstallPath, "results", jsonFileName)

	file, err := os.Create(path)

	if err != nil {
		return err
	}

	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "\t")

	return encoder.Encode(results)
}
