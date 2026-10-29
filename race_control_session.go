package servermanager

import (
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"
	"github.com/wernersbacher/assetto-server-manager/pkg/udp"
)

var emptyCarInfoMutex = sync.Mutex{}

// OnNewSession occurs every new session. If the session is the first in an event and it is not a looped practice,
// then all driver information is cleared.
func (rc *RaceControl) OnNewSession(sessionInfo udp.SessionInfo) error {
	oldSessionInfo := rc.SessionInfo
	rc.SessionInfo = sessionInfo
	rc.SessionStartTime = time.Now()

	emptyCarInfo := true

	rc.driverSwapPenaltiesMutex.Lock()
	rc.driverSwapPenalties = make(map[udp.DriverGUID]*driverSwapPenalty)
	rc.driverSwapPenaltiesMutex.Unlock()

	if (rc.ConnectedDrivers.Len() > 0 || rc.DisconnectedDrivers.Len() > 0) && sessionInfo.Type == udp.SessionTypePractice {
		if oldSessionInfo.Type == sessionInfo.Type && oldSessionInfo.Track == sessionInfo.Track && oldSessionInfo.TrackConfig == sessionInfo.TrackConfig && oldSessionInfo.Name == sessionInfo.Name {
			// this is a looped event, keep the cars
			emptyCarInfo = false
		}
	}

	if emptyCarInfo {
		_ = rc.ConnectedDrivers.Each(func(driverGUID udp.DriverGUID, driver *smmodel.RaceControlDriver) error {
			emptyCarInfoMutex.Lock()
			defer emptyCarInfoMutex.Unlock()

			*driver = *NewRaceControlDriver(driver.CarInfo)

			return nil
		})

		// all disconnected drivers are removed when car info is emptied, otherwise we are just showing empty entries in
		// the disconnected drivers table, which is pointless.
		rc.DisconnectedDrivers = NewDriverMap(DisconnectedDrivers, rc.SortDrivers)
	}

	// clear out last lap completed time each new session
	_ = rc.ConnectedDrivers.Each(func(driverGUID udp.DriverGUID, driver *smmodel.RaceControlDriver) error {
		driver.Lock()
		defer driver.Unlock()

		driver.CurrentCar().LastLapCompletedTime = time.Now()

		return nil
	})

	var err error

	trackInfo, err := rc.trackDataGateway.TrackInfo(sessionInfo.Track, sessionInfo.TrackConfig)

	if err != nil {
		return err
	}

	rc.TrackInfo = *trackInfo

	trackMapData, err := rc.trackDataGateway.TrackMap(sessionInfo.Track, sessionInfo.TrackConfig)

	if err != nil {
		logrus.WithError(err).Errorf("Could not load track map data")
	} else {
		rc.TrackMapData = *trackMapData
	}

	logrus.Debugf("New session detected: %s at %s (%s) [emptyCarInfo: %t]", sessionInfo.Type.String(), sessionInfo.Track, sessionInfo.TrackConfig, emptyCarInfo)

	// look for live timings stored previously
	persistedInfo, err := rc.store.LoadLiveTimingsData()

	if err == nil && persistedInfo != nil {
		if persistedInfo.SessionType == rc.SessionInfo.Type &&
			persistedInfo.Track == rc.SessionInfo.Track &&
			persistedInfo.TrackLayout == rc.SessionInfo.TrackConfig &&
			persistedInfo.SessionName == rc.SessionInfo.Name {

			for guid, driver := range persistedInfo.Drivers {
				_, driverPresentInDisconnectedList := rc.DisconnectedDrivers.Get(guid)
				_, driverPresentInConnectedList := rc.ConnectedDrivers.Get(guid)

				if !driverPresentInConnectedList && !driverPresentInDisconnectedList {
					rc.DisconnectedDrivers.Add(guid, driver)
				}
			}

			logrus.Infof("Loaded previous Live Timings data for %s (%s), num drivers: %d", persistedInfo.Track, persistedInfo.TrackLayout, len(persistedInfo.Drivers))
		}
	} else {
		logrus.WithError(err).Debugf("Could not load persisted live timings practice data")
	}

	_, err = rc.broadcaster.Send(sessionInfo)

	return err
}

// clearAllDrivers removes all known information about connected and disconnected drivers from RaceControl
func (rc *RaceControl) clearAllDrivers() {
	rc.ConnectedDrivers = NewDriverMap(ConnectedDrivers, rc.SortDrivers)
	rc.DisconnectedDrivers = NewDriverMap(DisconnectedDrivers, rc.SortDrivers)
	rc.carIDToGUIDMutex.Lock()
	rc.CarIDToGUID = make(map[udp.CarID]udp.DriverGUID)
	rc.carIDToGUIDMutex.Unlock()
}

var sessionInfoRequestInterval = time.Second * 30

// requestSessionInfo sends a request every sessionInfoRequestInterval to get information about temps, etc in the session.
func (rc *RaceControl) requestSessionInfo() {
	sessionInfoTicker := time.NewTicker(sessionInfoRequestInterval)

	for {
		select {
		case <-sessionInfoTicker.C:
			err := rc.process.SendUDPMessage(udp.GetSessionInfo{})

			if err == ErrNoOpenUDPConnection {
				logrus.WithError(err).Warnf("Couldn't send session info udp request. Breaking loop.")
				sessionInfoTicker.Stop()
				return
			} else if err != nil {
				logrus.WithError(err).Errorf("Couldn't send session info udp request")
			}

		case <-rc.serverProcessStopped:
			logrus.Debugf("Assetto Process completed. Disconnecting all connected drivers. Session done.")
			sessionInfoTicker.Stop()

			var drivers []*smmodel.RaceControlDriver

			rc.persistTimingData()

			// the server has just stopped. send disconnect messages for all connected cars.
			_ = rc.ConnectedDrivers.Each(func(driverGUID udp.DriverGUID, driver *smmodel.RaceControlDriver) error {
				// Each takes a read lock, so we cannot call disconnectDriver (which takes a write lock) from inside it.
				// we must instead append them to a slice and disconnect them outside the Each call.
				drivers = append(drivers, driver)

				return nil
			})

			for _, driver := range drivers {
				// disconnect the driver
				err := rc.disconnectDriver(driver)

				if err != nil {
					logrus.WithError(err).Errorf("Could not disconnect driver: %s (%s)", driver.CarInfo.DriverName, driver.CarInfo.DriverGUID)
					continue
				}
			}

			if _, err := rc.broadcaster.Send(rc); err != nil {
				logrus.WithError(err).Errorf("Couldn't broadcast race control")
			}

			return
		}
	}
}

func (rc *RaceControl) disconnectDriver(driver *smmodel.RaceControlDriver) error {
	driver.Lock()
	carInfo := driver.CarInfo
	carInfo.EventType = udp.EventConnectionClosed
	driver.Unlock()

	return rc.OnClientDisconnect(carInfo)
}

// OnSessionUpdate is called every sessionRequestInterval.
func (rc *RaceControl) OnSessionUpdate(sessionInfo udp.SessionInfo) (bool, error) {
	oldSessionInfo := rc.SessionInfo

	// we can't just copy over the session information, we must copy individual
	// parts of it, as the session type is incorrect.
	rc.SessionInfo.AmbientTemp = sessionInfo.AmbientTemp
	rc.SessionInfo.RoadTemp = sessionInfo.RoadTemp
	rc.SessionInfo.WeatherGraphics = sessionInfo.WeatherGraphics
	rc.SessionInfo.ElapsedMilliseconds = sessionInfo.ElapsedMilliseconds

	sessionHasChanged := oldSessionInfo.AmbientTemp != rc.SessionInfo.AmbientTemp || oldSessionInfo.RoadTemp != rc.SessionInfo.RoadTemp || oldSessionInfo.WeatherGraphics != rc.SessionInfo.WeatherGraphics

	return sessionHasChanged, nil
}

// OnEndSession is called at the end of every session.
func (rc *RaceControl) OnEndSession(sessionFile udp.EndSession) error {
	filename := filepath.Base(string(sessionFile))
	logrus.Infof("End Session, file outputted at: %s", filename)

	config := rc.process.Event().GetRaceConfig()

	if config.DriverSwapEnabled == 1 {
		_ = rc.ConnectedDrivers.Each(func(driverGUID udp.DriverGUID, driver *smmodel.RaceControlDriver) error {
			if driver.HasDriverSwap() {
				logrus.Infof("Cancelling active driver swap for driver: %s. Reason: Session ended", driver.CarInfo.DriverGUID)
				driver.CancelDriverSwap()
			}

			return nil
		})

		_ = rc.DisconnectedDrivers.Each(func(driverGUID udp.DriverGUID, driver *smmodel.RaceControlDriver) error {
			if driver.HasDriverSwap() {
				logrus.Infof("Cancelling active driver swap for driver: %s. Reason: Session ended", driver.CarInfo.DriverGUID)
				driver.CancelDriverSwap()
			}

			return nil
		})

		rc.driverSwapPenaltiesMutex.Lock()
		defer rc.driverSwapPenaltiesMutex.Unlock()

		if config.DriverSwapMinimumNumberOfSwaps > 0 {
			results, err := LoadResult(filename, LoadResultWithoutPluginFire)

			if err != nil {
				logrus.WithError(err).Errorf("Could not load results file to check min driver swaps")
			} else {
				for _, result := range results.Result {
					numSwaps := results.NumberOfDriverSwaps(result.CarID)

					if numSwaps < config.DriverSwapMinimumNumberOfSwaps {
						guid := udp.DriverGUID(result.DriverGUID)
						penaltyTime := time.Duration((config.DriverSwapMinimumNumberOfSwaps-numSwaps)*config.DriverSwapNotEnoughSwapsPenalty) * time.Second

						if _, ok := rc.driverSwapPenalties[guid]; ok {
							rc.driverSwapPenalties[guid].penalty += penaltyTime
						} else {
							rc.driverSwapPenalties[guid] = &driverSwapPenalty{
								carModel: result.CarModel,
								penalty:  penaltyTime,
							}
						}
					}
				}
			}
		}

		for guid, penalty := range rc.driverSwapPenalties {
			err := rc.penaltiesManager.applyPenalty(filename, string(guid), penalty.carModel, penalty.penalty.Seconds(), true)

			if err != nil {
				logrus.WithError(err).Errorf("could not apply driver swap penalty of %s to driver %s", penalty.penalty.String(), guid)
				continue
			}
		}
	}

	if rc.currentTimeAttackEvent != nil {
		filename := filepath.Base(string(sessionFile))

		err := rc.addFileToTimeAttackEvent(filename)

		if err != nil {
			return err
		}

		logrus.Infof("Time Attack Event (%s) Finished, results files have been combined and saved as %s", rc.currentTimeAttackEvent.EventName(trackSummary), filename)
	}

	return nil
}

func (rc *RaceControl) addFileToTimeAttackEvent(file string) error {
	logrus.Info("Time Attack event completed, combining with any previous results")

	results, err := LoadResult(file)

	if err != nil {
		logrus.WithError(err).Errorf("Could not read session results: %s", file)
		return err
	}

	var resultsArray []*smresults.SessionResults

	resultsArray = append(resultsArray, results)

	if rc.currentTimeAttackEvent.TimeAttackCombinedResultFile != "" {
		result, err := LoadResult(rc.currentTimeAttackEvent.TimeAttackCombinedResultFile + ".json")

		if err != nil {
			logrus.WithError(err).Errorf("Could not read session results: %s", file)
			return err
		}

		resultsArray = append(resultsArray, result)
	}

	results = smresults.Combine(resultsArray)

	results.FallBackSort()
	results.ClearKickedGUIDs()
	results.NormaliseCarIDs()

	if !strings.HasSuffix(results.SessionFile, timeAttackSuffix) {
		results.SessionFile = results.SessionFile + timeAttackSuffix
	}

	results.Date = time.Now()

	err = saveResults(results.SessionFile+".json", results)

	if err != nil {
		return err
	}

	rc.currentTimeAttackEvent.TimeAttackCombinedResultFile = results.SessionFile

	return rc.store.UpsertCustomRace(rc.currentTimeAttackEvent)
}
