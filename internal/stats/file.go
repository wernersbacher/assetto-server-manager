package stats

import (
	"strings"
	"time"

	"github.com/wernersbacher/assetto-server-manager/internal/results"
)

const (
	collisionWithCar = "COLLISION_WITH_CAR"
	collisionWithEnv = "COLLISION_WITH_ENV"
)

// fileStats is what the index keeps of one results file. It is not changed after it was built.
type fileStats struct {
	name    string // file name, with extension
	dir     string
	size    int64
	modTime time.Time

	track       string
	layout      string
	sessionType string
	date        time.Time

	drivers map[string]*fileDriver // by GUID
}

// fileDriver is one driver in one session.
type fileDriver struct {
	guid         string
	names        []string // distinct names used in the session, the last one used last
	nation       string
	team         string
	position     int // finishing position in a race, 0 for disqualified drivers and other sessions
	hasResult    bool
	disqualified bool
	cars         map[string]*carStats // by car model
}

func (d *fileDriver) name() string {
	if len(d.names) == 0 {
		return ""
	}

	return d.names[len(d.names)-1]
}

func (d *fileDriver) addName(name string) {
	if name == "" {
		return
	}

	d.names = moveToEnd(d.names, name)
}

// carStats are the numbers of one driver with one car in one session.
type carStats struct {
	laps, validLaps, cuts int
	collisionsCar         int
	collisionsEnv         int
	best                  *lapRecord // best valid lap
}

// lapRecord is a lap as the index keeps it.
type lapRecord struct {
	time      time.Duration
	sectors   []time.Duration
	tyre      string
	timestamp int
	name      string
}

// validLap tells if the lap counts for records: it has a time and no cuts.
func validLap(lap *results.SessionLap) bool {
	return lap.Cuts == 0 && lap.LapTime > 0
}

func (f *fileStats) driver(guid string) *fileDriver {
	d, ok := f.drivers[guid]

	if !ok {
		d = &fileDriver{guid: guid, cars: make(map[string]*carStats)}
		f.drivers[guid] = d
	}

	return d
}

func (d *fileDriver) car(model string) *carStats {
	c, ok := d.cars[model]

	if !ok {
		c = &carStats{}
		d.cars[model] = c
	}

	return c
}

// buildFileStats derives the statistics of one session.
func buildFileStats(r *results.SessionResults, name, dir string, size int64, modTime time.Time) *fileStats {
	f := &fileStats{
		name:        name,
		dir:         dir,
		size:        size,
		modTime:     modTime,
		track:       r.TrackName,
		layout:      r.TrackConfig,
		sessionType: string(r.Type),
		date:        r.Date,
		drivers:     make(map[string]*fileDriver),
	}

	carByID := make(map[int]*results.SessionCar)

	for _, car := range r.Cars {
		carByID[car.CarID] = car

		guids := car.Driver.GuidsList

		if len(guids) == 0 {
			guids = []string{car.Driver.GUID}
		}

		for _, guid := range guids {
			if guid == "" {
				continue
			}

			d := f.driver(guid)
			d.team = car.Driver.Team

			// the name and nation of a car belong to its first driver, the others are known from their laps
			if guid == car.Driver.GUID || len(guids) == 1 {
				d.addName(car.Driver.Name)
				d.nation = car.Driver.Nation
			}
		}
	}

	for _, lap := range r.Laps {
		if lap.DriverGUID == "" {
			continue
		}

		d := f.driver(lap.DriverGUID)
		d.addName(lap.DriverName)

		c := d.car(lap.CarModel)
		c.laps++
		c.cuts += lap.Cuts

		if !validLap(lap) {
			continue
		}

		c.validLaps++

		lapTime := time.Duration(lap.LapTime) * time.Millisecond

		if c.best == nil || lapTime < c.best.time {
			sectors := make([]time.Duration, len(lap.Sectors))

			for i, s := range lap.Sectors {
				sectors[i] = time.Duration(s) * time.Millisecond
			}

			c.best = &lapRecord{
				time:      lapTime,
				sectors:   sectors,
				tyre:      lap.Tyre,
				timestamp: lap.Timestamp,
				name:      lap.DriverName,
			}
		}
	}

	for _, event := range r.Events {
		if event.Driver == nil || event.Driver.GUID == "" {
			continue
		}

		model := ""

		if car, ok := carByID[event.CarID]; ok {
			model = car.Model
		}

		c := f.driver(event.Driver.GUID).car(model)

		switch {
		case strings.HasPrefix(event.Type, collisionWithCar):
			c.collisionsCar++
		case strings.HasPrefix(event.Type, collisionWithEnv):
			c.collisionsEnv++
		}
	}

	// results are in finishing order; disqualified drivers get no position
	position := 0

	for _, res := range r.Result {
		guids := []string{res.DriverGUID}

		if car, ok := carByID[res.CarID]; ok && len(car.Driver.GuidsList) > 1 {
			guids = car.Driver.GuidsList
		}

		if !res.Disqualified {
			position++
		}

		for _, guid := range guids {
			if guid == "" {
				continue
			}

			d := f.driver(guid)
			d.hasResult = true
			d.disqualified = res.Disqualified

			if isRace(f.sessionType) && !res.Disqualified {
				d.position = position
			}

			if len(guids) == 1 {
				d.addName(res.DriverName)
			}
		}
	}

	return f
}

// moveToEnd appends name to names, removing an earlier occurrence.
func moveToEnd(names []string, name string) []string {
	for i, n := range names {
		if n == name {
			names = append(names[:i:i], names[i+1:]...)
			break
		}
	}

	return append(names, name)
}
