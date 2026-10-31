package racecontrol

import (
	"sort"
	"sync"
	"time"

	smcontent "github.com/wernersbacher/ACPitwall/internal/content"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/pkg/udp"
)

func NewRaceControlDriver(carInfo udp.SessionCarInfo) *smmodel.RaceControlDriver {
	driver := &smmodel.RaceControlDriver{
		CarInfo:  carInfo,
		Cars:     make(map[string]*smmodel.RaceControlCarLapInfo),
		LastSeen: time.Now(),
	}

	driver.Cars[carInfo.CarModel] = NewRaceControlCarLapInfo(carInfo.CarModel)

	return driver
}

func NewRaceControlCarLapInfo(carModel string) *smmodel.RaceControlCarLapInfo {
	return &smmodel.RaceControlCarLapInfo{
		CarName: smcontent.PrettifyName(carModel, true),
	}
}

type DriverMap struct {
	Drivers                map[udp.DriverGUID]*smmodel.RaceControlDriver `json:"Drivers"`
	GUIDsInPositionalOrder []udp.DriverGUID                              `json:"GUIDsInPositionalOrder"`

	driverSortLessFunc driverSortLessFunc
	driverGroup        RaceControlDriverGroup

	rwMutex sync.RWMutex
}

type RaceControlDriverGroup int

const (
	ConnectedDrivers    RaceControlDriverGroup = 0
	DisconnectedDrivers RaceControlDriverGroup = 1
)

type driverSortLessFunc func(group RaceControlDriverGroup, driverA, driverB *smmodel.RaceControlDriver) bool

func NewDriverMap(driverGroup RaceControlDriverGroup, driverSortLessFunc driverSortLessFunc) *DriverMap {
	return &DriverMap{
		Drivers:            make(map[udp.DriverGUID]*smmodel.RaceControlDriver),
		driverSortLessFunc: driverSortLessFunc,
		driverGroup:        driverGroup,
	}
}

func (d *DriverMap) Each(fn func(driverGUID udp.DriverGUID, driver *smmodel.RaceControlDriver) error) error {
	d.rwMutex.RLock()
	defer d.rwMutex.RUnlock()

	for _, guid := range d.GUIDsInPositionalOrder {
		driver, ok := d.Drivers[guid]

		if !ok {
			continue
		}

		err := fn(guid, driver)

		if err != nil {
			return err
		}
	}

	return nil
}

func (d *DriverMap) Get(driverGUID udp.DriverGUID) (*smmodel.RaceControlDriver, bool) {
	d.rwMutex.RLock()
	defer d.rwMutex.RUnlock()

	driver, ok := d.Drivers[driverGUID]

	return driver, ok
}

func (d *DriverMap) Add(driverGUID udp.DriverGUID, driver *smmodel.RaceControlDriver) {
	d.rwMutex.Lock()
	defer d.rwMutex.Unlock()
	defer d.sort()

	d.Drivers[driverGUID] = driver

	for _, guid := range d.GUIDsInPositionalOrder {
		if guid == driverGUID {
			return
		}
	}

	d.GUIDsInPositionalOrder = append(d.GUIDsInPositionalOrder, driverGUID)
}

func (d *DriverMap) sort() {
	sort.Slice(d.GUIDsInPositionalOrder, func(i, j int) bool {
		driverA, ok := d.Drivers[d.GUIDsInPositionalOrder[i]]

		if !ok {
			return false
		}

		driverB, ok := d.Drivers[d.GUIDsInPositionalOrder[j]]

		if !ok {
			return false
		}

		return d.driverSortLessFunc(d.driverGroup, driverA, driverB)
	})

	// correct positions
	for pos, guid := range d.GUIDsInPositionalOrder {
		driver, ok := d.Drivers[guid]

		if !ok {
			continue
		}

		driver.Position = pos + 1
	}
}

func (d *DriverMap) Del(driverGUID udp.DriverGUID) {
	d.rwMutex.Lock()
	defer d.rwMutex.Unlock()

	delete(d.Drivers, driverGUID)

	for index, guid := range d.GUIDsInPositionalOrder {
		if guid == driverGUID {
			d.GUIDsInPositionalOrder = append(d.GUIDsInPositionalOrder[:index], d.GUIDsInPositionalOrder[index+1:]...)
			break
		}
	}

	d.sort()
}

func (d *DriverMap) Len() int {
	d.rwMutex.RLock()
	defer d.rwMutex.RUnlock()

	return len(d.Drivers)
}
