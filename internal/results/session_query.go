package results

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
)

func (s *SessionResults) FallBackSort() {
	// sort the results by laps completed then race time
	// this is a fall back for when assetto's sorting is terrible
	// sort results.Result, if disqualified go to back, if time penalty sort by laps completed then lap time

cars:
	for i := range s.Cars {
		for z := range s.Result {
			// the result may carry the joined GUID of a driver swap car
			if carID, ok := s.FindCarIDForGUIDAndModel(s.Result[z].DriverGUID, s.Cars[i].Model); ok && carID == s.Cars[i].CarID {
				continue cars
			}
		}

		bestLap := s.carBestLap(s.Cars[i].Driver.GUID, s.Cars[i].Model)

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
			if s.LapAssociatedWithGUIDAndModel(lap, result.DriverGUID, result.CarModel) {
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

results:
	// is this result car in the car list? If not then add it
	for _, result := range s.Result {
		if _, ok := s.FindCarIDForGUIDAndModel(result.DriverGUID, result.CarModel); ok {
			continue results
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

	for i := range s.Result {
		s.Result[i].TotalTime = 0

		// per car, so that the laps of all drivers of a driver swap car are counted
		for _, lap := range s.carLaps(s.Result[i].DriverGUID, s.Result[i].CarModel) {
			s.Result[i].TotalTime += lap.LapTime
		}

		// TotalTime stays the plain race time (in ms), like in the results written by the AC server. Time penalties are
		// added on top by GetTime(..., penalty=true), which is used for sorting and display.

		if s.Result[i].BestLap == 0 {
			s.Result[i].BestLap = s.carBestLap(s.Result[i].DriverGUID, s.Result[i].CarModel)
		}
	}

	sort.Slice(s.Result, func(i, j int) bool {
		if (!s.Result[i].Disqualified && !s.Result[j].Disqualified) || (s.Result[i].Disqualified && s.Result[j].Disqualified) {

			if s.Type == serverconfig.SessionTypeQualifying || s.Type == serverconfig.SessionTypePractice {

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
			drivers = append(drivers, DriverName(car.DriverName))
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

				if s.Type == serverconfig.SessionTypeRace {
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

	for _, driverGUID := range strings.Split(driverGUID, serverconfig.DriverSwapEntrantSeparator) {
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

// carBestLap returns the time of the fastest clean lap of the driver's car (all drivers of a driver swap car), or 0.
func (s *SessionResults) carBestLap(guid, model string) int {
	if lap := fastestCleanLap(s.carLaps(guid, model), func(*SessionLap) bool { return true }); lap != nil {
		return lap.LapTime
	}

	return 0
}
