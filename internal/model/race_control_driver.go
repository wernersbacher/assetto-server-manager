package model

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/wernersbacher/ACPitwall/pkg/udp"
)

type RaceControlDriver struct {
	CarInfo      udp.SessionCarInfo `json:"CarInfo"`
	TotalNumLaps int                `json:"TotalNumLaps"`

	ConnectedTime time.Time `json:"ConnectedTime" ts:"date"`
	LoadedTime    time.Time `json:"LoadedTime" ts:"date"`

	Position int       `json:"Position"`
	Split    string    `json:"Split"`
	LastSeen time.Time `json:"LastSeen" ts:"date"`
	LastPos  udp.Vec   `json:"LastPos"`

	Collisions []Collision `json:"Collisions"`

	driverSwapContext context.Context
	driverSwapCfn     context.CancelFunc

	// Cars is a map of CarModel to the information for that car.
	Cars map[string]*RaceControlCarLapInfo `json:"Cars"`

	mutex sync.Mutex
}

func (rcd *RaceControlDriver) CurrentCar() *RaceControlCarLapInfo {
	if car, ok := rcd.Cars[rcd.CarInfo.CarModel]; ok {
		return car
	}

	logrus.Warnf("Could not find current car for driver: %s (current car: %s)", rcd.CarInfo.DriverGUID, rcd.CarInfo.CarModel)
	return &RaceControlCarLapInfo{}
}

type RaceControlCarLapInfo struct {
	TopSpeedThisLap      float64       `json:"TopSpeedThisLap"`
	TopSpeedBestLap      float64       `json:"TopSpeedBestLap"`
	BestLap              time.Duration `json:"BestLap"`
	NumLaps              int           `json:"NumLaps"`
	LastLap              time.Duration `json:"LastLap"`
	LastLapCompletedTime time.Time     `json:"LastLapCompletedTime" ts:"date"`
	TotalLapTime         time.Duration `json:"TotalLapTime"`
	CarName              string        `json:"CarName"`
}

// Lock locks the driver while it is being updated. Callers that modify a driver take the lock themselves.
func (rcd *RaceControlDriver) Lock() {
	rcd.mutex.Lock()
}

// Unlock unlocks a driver locked with Lock.
func (rcd *RaceControlDriver) Unlock() {
	rcd.mutex.Unlock()
}

// BeginDriverSwap marks a driver swap as active for the driver. The returned channel is closed when the driver swap is
// cancelled with CancelDriverSwap.
func (rcd *RaceControlDriver) BeginDriverSwap() <-chan struct{} {
	rcd.driverSwapContext, rcd.driverSwapCfn = context.WithCancel(context.Background())

	return rcd.driverSwapContext.Done()
}

// HasDriverSwap is true once a driver swap has been started for the driver.
func (rcd *RaceControlDriver) HasDriverSwap() bool {
	return rcd.driverSwapCfn != nil
}

// CancelDriverSwap cancels the driver swap started with BeginDriverSwap.
func (rcd *RaceControlDriver) CancelDriverSwap() {
	rcd.driverSwapCfn()
}
