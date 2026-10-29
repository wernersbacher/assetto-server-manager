package servermanager

import (
	smconfig "github.com/wernersbacher/assetto-server-manager/internal/config"
	smcontent "github.com/wernersbacher/assetto-server-manager/internal/content"

	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/wernersbacher/assetto-server-manager/pkg/udp"
)

type RaceControl struct {
	config           *smconfig.Configuration
	process          ServerProcess
	store            Store
	penaltiesManager *PenaltiesManager

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

	currentTimeAttackEvent *CustomRace

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

type CollisionType string

const (
	CollisionWithCar         CollisionType = "with other car"
	CollisionWithEnvironment CollisionType = "with environment"
)

type Collision struct {
	ID              string         `json:"ID"`
	Type            CollisionType  `json:"Type"`
	Time            time.Time      `json:"Time" ts:"date"`
	OtherDriverGUID udp.DriverGUID `json:"OtherDriverGUID"`
	OtherDriverName string         `json:"OtherDriverName"`
	Speed           float64        `json:"Speed"`
}

func NewRaceControl(config *smconfig.Configuration, broadcaster Broadcaster, trackDataGateway TrackDataGateway, process ServerProcess, store Store, penaltiesManager *PenaltiesManager) *RaceControl {
	rc := &RaceControl{
		config:               config,
		broadcaster:          broadcaster,
		trackDataGateway:     trackDataGateway,
		process:              process,
		store:                store,
		driverSwapTimers:     make(map[int]*time.Timer),
		penaltiesManager:     penaltiesManager,
		carUpdaters:          make(map[udp.CarID]chan udp.CarUpdate),
		serverProcessStopped: make(chan struct{}),
	}

	process.NotifyDone(rc.serverProcessStopped)

	rc.clearAllDrivers()

	go panicCapture(rc.watchForTimedOutDrivers)

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
		var driver *RaceControlDriver

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
		rc.CurrentRealtimePosInterval = udp.CurrentRealtimePosIntervalMs

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
		var driversToDisconnect []*RaceControlDriver

		_ = rc.ConnectedDrivers.Each(func(driverGUID udp.DriverGUID, driver *RaceControlDriver) error {
			driver.mutex.Lock()
			defer driver.mutex.Unlock()

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

func (rc *RaceControl) SortDrivers(driverGroup RaceControlDriverGroup, driverA, driverB *RaceControlDriver) bool {
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

type LiveTimingsPersistedData struct {
	SessionType udp.SessionType
	Track       string
	TrackLayout string
	SessionName string

	Drivers map[udp.DriverGUID]*RaceControlDriver
}

func (rc *RaceControl) persistTimingData() {
	rc.persistStoreDataMutex.Lock()
	defer rc.persistStoreDataMutex.Unlock()

	data := &LiveTimingsPersistedData{
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

func (rc *RaceControl) AllLapTimes() map[udp.DriverGUID]*RaceControlDriver {
	out := make(map[udp.DriverGUID]*RaceControlDriver)

	_ = rc.DisconnectedDrivers.Each(func(driverGUID udp.DriverGUID, driver *RaceControlDriver) error {
		out[driverGUID] = driver

		return nil
	})

	_ = rc.ConnectedDrivers.Each(func(driverGUID udp.DriverGUID, driver *RaceControlDriver) error {
		out[driverGUID] = driver

		return nil
	})

	return out
}

func lapToDuration(i int) time.Duration {
	d, _ := time.ParseDuration(fmt.Sprintf("%dms", i))

	return d
}
