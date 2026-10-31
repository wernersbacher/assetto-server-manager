package racecontrol

import (
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	smconfig "github.com/wernersbacher/ACPitwall/internal/config"
	smcontent "github.com/wernersbacher/ACPitwall/internal/content"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smserver "github.com/wernersbacher/ACPitwall/internal/server"
	"github.com/wernersbacher/ACPitwall/internal/serverenv"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
	"github.com/wernersbacher/ACPitwall/pkg/udp"
)

type RaceControl struct {
	config           *smconfig.Configuration
	process          smserver.ServerProcess
	store            smstore.Store
	penaltiesManager *PenaltiesManager
	env              *serverenv.Env

	SessionInfo                udp.SessionInfo        `json:"SessionInfo"`
	TrackMapData               smcontent.TrackMapData `json:"TrackMapData"`
	TrackInfo                  smcontent.TrackInfo    `json:"TrackInfo"`
	SessionStartTime           time.Time              `json:"SessionStartTime"`
	CurrentRealtimePosInterval int                    `json:"CurrentRealtimePosInterval"`

	ChatMessages      []udp.Chat
	ChatMessagesMutex sync.Mutex

	ConnectedDrivers    *DriverMap `json:"ConnectedDrivers"`
	DisconnectedDrivers *DriverMap `json:"DisconnectedDrivers"`

	CarIDToGUID      map[udp.CarID]udp.DriverGUID `json:"CarIDToGUID"`
	carIDToGUIDMutex sync.RWMutex

	carUpdaters          map[udp.CarID]chan udp.CarUpdate
	serverProcessStopped chan struct{}

	broadcaster      Broadcaster
	trackDataGateway TrackDataGateway

	currentTimeAttackEvent *smmodel.CustomRace

	lastUpdateMessage      []byte
	lastUpdateMessageMutex sync.Mutex

	persistStoreDataMutex sync.Mutex

	// driver swap
	driverSwapTimers         map[int]*time.Timer
	driverSwapPenaltiesMutex sync.Mutex
	driverSwapPenalties      map[udp.DriverGUID]*driverSwapPenalty
}

// RaceControl piggyback's on the udp.Message interface so that the entire data can be sent to newly connected clients.
func (rc *RaceControl) Event() udp.Event {
	return 200
}

// NewRaceControl creates the race control, which follows the state of the running server from its UDP messages.
func NewRaceControl(config *smconfig.Configuration, broadcaster Broadcaster, trackDataGateway TrackDataGateway, process smserver.ServerProcess, store smstore.Store, penaltiesManager *PenaltiesManager, env *serverenv.Env) *RaceControl {
	rc := &RaceControl{
		config:               config,
		broadcaster:          broadcaster,
		trackDataGateway:     trackDataGateway,
		process:              process,
		store:                store,
		driverSwapTimers:     make(map[int]*time.Timer),
		penaltiesManager:     penaltiesManager,
		env:                  env,
		carUpdaters:          make(map[udp.CarID]chan udp.CarUpdate),
		serverProcessStopped: make(chan struct{}),
	}

	process.NotifyDone(rc.serverProcessStopped)

	rc.clearAllDrivers()

	go env.PanicCapture(rc.watchForTimedOutDrivers)

	return rc
}

func (rc *RaceControl) UDPCallback(message udp.Message) {
	var err error

	sendUpdatedRaceControlStatus := false

	switch m := message.(type) {
	case udp.Version:
		err = rc.OnVersion(m)
	case udp.SessionInfo:
		if m.Event() == udp.EventNewSession {
			err = rc.OnNewSession(m)
			sendUpdatedRaceControlStatus = true
		} else {
			sendUpdatedRaceControlStatus, err = rc.OnSessionUpdate(m)
		}

	case udp.EndSession:
		err = rc.OnEndSession(m)

		sendUpdatedRaceControlStatus = true
	case udp.CarUpdate:
		err = rc.OnCarUpdate(m)
	case udp.SessionCarInfo:
		if m.Event() == udp.EventNewConnection {
			err = rc.OnClientConnect(m)
		} else if m.Event() == udp.EventConnectionClosed {
			err = rc.OnClientDisconnect(m)
		}

		sendUpdatedRaceControlStatus = true
	case udp.ClientLoaded:
		err = rc.OnClientLoaded(m)

		sendUpdatedRaceControlStatus = true
	case udp.CollisionWithCar:
		err = rc.OnCollisionWithCar(m)
		sendUpdatedRaceControlStatus = true
	case udp.CollisionWithEnvironment:
		err = rc.OnCollisionWithEnvironment(m)
		sendUpdatedRaceControlStatus = true
	case udp.LapCompleted:
		err = rc.OnLapCompleted(m)

		sendUpdatedRaceControlStatus = true
	case udp.Chat:
		// received a chat message
		var driver *smmodel.RaceControlDriver

		driver, err = rc.findConnectedDriverByCarID(m.CarID)

		if err == nil {
			m.DriverGUID = driver.CarInfo.DriverGUID
			m.DriverName = driver.CarInfo.DriverName
		} else if m.DriverGUID == "" && m.DriverName == "" {
			m.DriverGUID = "0"
			m.DriverName = "Server"
		}

		m.Time = time.Now()

		err = rc.OnChatMessage(m)
	default:
		return
	}

	if err != nil {
		logrus.WithError(err).Errorf("Unable to handle event: %d", message.Event())
		return
	}

	if sendUpdatedRaceControlStatus {
		// update the current refresh rate
		rc.CurrentRealtimePosInterval = rc.currentRealtimePosInterval()

		lastUpdateMessage, err := rc.broadcaster.Send(rc)

		if err != nil {
			logrus.WithError(err).Error("Unable to broadcast race control message")
			return
		}

		rc.lastUpdateMessageMutex.Lock()
		rc.lastUpdateMessage = lastUpdateMessage
		rc.lastUpdateMessageMutex.Unlock()
	}
}

var driverTimeout = time.Minute * 5

func (rc *RaceControl) watchForTimedOutDrivers() {
	if udp.RealtimePosIntervalMs <= 0 {
		// with no real time pos interval, we have no driver positions, so no last update time.
		return
	}

	ticker := time.NewTicker(time.Minute)

	for range ticker.C {
		var driversToDisconnect []*smmodel.RaceControlDriver

		_ = rc.ConnectedDrivers.Each(func(driverGUID udp.DriverGUID, driver *smmodel.RaceControlDriver) error {
			driver.Lock()
			defer driver.Unlock()

			if !driver.LastSeen.IsZero() && time.Since(driver.LastSeen) > driverTimeout || driver.LastSeen.IsZero() && time.Since(driver.ConnectedTime) > driverTimeout {
				driversToDisconnect = append(driversToDisconnect, driver)
			}

			return nil
		})

		for _, driver := range driversToDisconnect {
			logrus.Debugf("Driver: %s (%s) has not been seen in 5 minutes, disconnecting", driver.CarInfo.DriverName, driver.CarInfo.DriverGUID)
			err := rc.disconnectDriver(driver)

			if err != nil {
				logrus.WithError(err).Errorf("Could not disconnect driver: %s (%s)", driver.CarInfo.DriverName, driver.CarInfo.DriverGUID)
				continue
			}
		}

		if len(driversToDisconnect) > 0 {
			_, err := rc.broadcaster.Send(rc)

			if err != nil {
				logrus.WithError(err).Error("Could not broadcast driver disconnect message")
			}
		}
	}
}

func (rc *RaceControl) SortDrivers(driverGroup RaceControlDriverGroup, driverA, driverB *smmodel.RaceControlDriver) bool {
	driverACar := driverA.CurrentCar()
	driverBCar := driverB.CurrentCar()

	if rc.SessionInfo.Type == udp.SessionTypeRace {
		if driverGroup == ConnectedDrivers {
			if driverACar.NumLaps == driverBCar.NumLaps {
				return driverACar.TotalLapTime < driverBCar.TotalLapTime
			}

			return driverACar.NumLaps > driverBCar.NumLaps
		} else if driverGroup == DisconnectedDrivers {
			return driverACar.LastLapCompletedTime.After(driverBCar.LastLapCompletedTime)
		} else {
			panic("unknown driver group")
		}
	} else {
		if driverACar.BestLap == 0 && driverBCar.BestLap == 0 {
			if driverACar.NumLaps == driverBCar.NumLaps {
				return driverACar.LastLapCompletedTime.Before(driverBCar.LastLapCompletedTime)
			}

			return driverACar.NumLaps > driverBCar.NumLaps
		}

		if driverACar.BestLap == 0 {
			return false
		} else if driverBCar.BestLap == 0 {
			return true
		}

		return driverACar.BestLap < driverBCar.BestLap
	}
}

func metersPerSecondToKilometersPerHour(mps float64) float64 {
	return mps * 3.6
}

// PersistTimingData writes the live timing data to the store.
func (rc *RaceControl) PersistTimingData() {
	rc.persistStoreDataMutex.Lock()
	defer rc.persistStoreDataMutex.Unlock()

	data := &smmodel.LiveTimingsPersistedData{
		SessionType: rc.SessionInfo.Type,
		Track:       rc.SessionInfo.Track,
		TrackLayout: rc.SessionInfo.TrackConfig,
		SessionName: rc.SessionInfo.Name,

		Drivers: rc.AllLapTimes(),
	}

	err := rc.store.UpsertLiveTimingsData(data)

	if err != nil {
		logrus.WithError(err).Errorf("Could not save live timings data")
	}

	logrus.Debug("successfully persisted live timing data")
}

func (rc *RaceControl) AllLapTimes() map[udp.DriverGUID]*smmodel.RaceControlDriver {
	out := make(map[udp.DriverGUID]*smmodel.RaceControlDriver)

	_ = rc.DisconnectedDrivers.Each(func(driverGUID udp.DriverGUID, driver *smmodel.RaceControlDriver) error {
		out[driverGUID] = driver

		return nil
	})

	_ = rc.ConnectedDrivers.Each(func(driverGUID udp.DriverGUID, driver *smmodel.RaceControlDriver) error {
		out[driverGUID] = driver

		return nil
	})

	return out
}

func lapToDuration(i int) time.Duration {
	d, _ := time.ParseDuration(fmt.Sprintf("%dms", i))

	return d
}

// TrackDataGateway gives the race control access to the details of the tracks.
type TrackDataGateway interface {
	TrackInfo(name, layout string) (*smcontent.TrackInfo, error)
	TrackMap(name, layout string) (*smcontent.TrackMapData, error)
}

type filesystemTrackData struct {
	env *serverenv.Env
}

// NewFilesystemTrackData returns a TrackDataGateway which reads the track data from the Assetto Corsa installation.
func NewFilesystemTrackData(env *serverenv.Env) TrackDataGateway {
	return filesystemTrackData{env: env}
}

func (f filesystemTrackData) TrackMap(name, layout string) (*smcontent.TrackMapData, error) {
	return smcontent.LoadTrackMapData(f.env.InstallPath(), name, layout)
}

func (f filesystemTrackData) TrackInfo(name, layout string) (*smcontent.TrackInfo, error) {
	trackInfo, err := smcontent.GetTrackInfo(f.env.InstallPath(), name, layout)

	if err != nil {
		logrus.WithError(err).Errorf("Could not load track info")

		return &smcontent.TrackInfo{
			Name:    f.env.TrackSummary(name, layout),
			City:    "Unknown",
			Country: "Unknown",
		}, nil
	}

	return trackInfo, err
}

// NilTrackData is a TrackDataGateway which knows no tracks.
type NilTrackData struct{}

func (NilTrackData) TrackInfo(name, layout string) (*smcontent.TrackInfo, error) {
	return &smcontent.TrackInfo{}, nil
}

func (NilTrackData) TrackMap(name, layout string) (*smcontent.TrackMapData, error) {
	return &smcontent.TrackMapData{}, nil
}

// SetTimeAttackEvent sets the time attack race which is currently running, or clears it if race is nil.
func (rc *RaceControl) SetTimeAttackEvent(race *smmodel.CustomRace) {
	rc.currentTimeAttackEvent = race
}

// currentRealtimePosInterval is the real time pos interval of the server of this race control, which each server
// process adjusts on its own. A process which does not report it (the tests' dummies) runs at the configured interval.
func (rc *RaceControl) currentRealtimePosInterval() int {
	if p, ok := rc.process.(interface{ CurrentRealtimePosInterval() int }); ok {
		return p.CurrentRealtimePosInterval()
	}

	return udp.RealtimePosIntervalMs
}
