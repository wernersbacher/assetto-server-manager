package racecontrol

import (
	"math"
	"math/rand"
	"sort"
	"strings"
	"time"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/pkg/udp"
)

// This file builds a frozen live timing session for UI development: it replays a made-up session as UDP messages
// through UDPCallback, so the page shows what a real session looks like without an acServer. The drivers are those of
// cmd/utils/dev-seed, on the fake install in dev/assetto-seed. It is only started when DEV_MOCK_LIVE_TIMING is set.

type mockDriver struct {
	name, guid, team, car, skin, number string
	pace                                float64
	topSpeed                            float64
}

var mockDrivers = []mockDriver{
	{"Lena Hartmann", "76561198100000001", "Apex Racing", "dev_car_gt3", "00_red", "7", 1.000, 281},
	{"Marco Bellini", "76561198100000002", "Apex Racing", "dev_car_gt3", "00_red", "11", 1.006, 279},
	{"Jack Turner", "76561198100000003", "Blue Fox Motorsport", "dev_car_gt3", "01_blue", "23", 1.012, 283},
	{"Sofia Almeida", "76561198100000004", "Blue Fox Motorsport", "dev_car_gt3", "01_blue", "24", 1.018, 276},
	{"Yuki Tanabe", "76561198100000006", "Nordschleife Nomads", "dev_car_gt3", "01_blue", "88", 1.022, 278},
	{"Tomasz Kowal", "76561198100000005", "", "dev_car_gt3", "00_red", "", 1.030, 274},
	{"Emma Lindqvist", "76561198100000007", "Nordschleife Nomads", "dev_car_mx5", "00_white", "5", 1.140, 201},
	{"Pierre Dubois", "76561198100000008", "", "dev_car_mx5", "00_white", "", 1.150, 199},
	{"Carlos Ortega", "76561198100000009", "Blue Fox Motorsport", "dev_car_mx5", "00_white", "19", 1.160, 203},
	{"Sam O'Neill", "76561198100000010", "", "dev_car_mx5", "00_white", "", 1.180, 196},
}

// mockLeftDriver has driven two laps and has left the server again (shown in the table of disconnected drivers).
var mockLeftDriver = mockDriver{"Anna Weber", "76561198100000011", "", "dev_car_gt3", "00_red", "", 1.040, 275}

const (
	mockTrack       = "dev_ring"
	mockTrackLayout = "gp"
	mockBaseLapMs   = 118000
)

// MockSessionType maps the value of DEV_MOCK_LIVE_TIMING ("practice", "qualifying", "race"; anything else, like
// "true", is a race) to a session type.
func MockSessionType(value string) udp.SessionType {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "practice":
		return udp.SessionTypePractice
	case "qualifying", "qualify":
		return udp.SessionTypeQualifying
	default:
		return udp.SessionTypeRace
	}
}

// ReplayMockSession puts a made-up session of the given type into the race control, as if the server had sent it.
// It is meant for development only. The session stays as it is (frozen), only the "last seen" time of the drivers
// is refreshed, so that they are not disconnected after five minutes without position updates.
func (rc *RaceControl) ReplayMockSession(sessionType udp.SessionType) {
	rng := rand.New(rand.NewSource(42))

	name := map[udp.SessionType]string{
		udp.SessionTypePractice:   "Practice",
		udp.SessionTypeQualifying: "Qualify",
		udp.SessionTypeRace:       "Race",
	}[sessionType]

	laps := uint16(0)
	sessionMinutes := uint16(20)

	if sessionType == udp.SessionTypeRace {
		laps = 10
		sessionMinutes = 0
	}

	rc.UDPCallback(udp.SessionInfo{
		Version:             4,
		SessionIndex:        0,
		CurrentSessionIndex: 0,
		SessionCount:        1,
		ServerName:          "ACPitwall dev mock",
		Track:               mockTrack,
		TrackConfig:         mockTrackLayout,
		Name:                name,
		Type:                sessionType,
		Time:                sessionMinutes,
		Laps:                laps,
		AmbientTemp:         24,
		RoadTemp:            31,
		WeatherGraphics:     "03_clear",
		ElapsedMilliseconds: 7*118000 + 41000,
		EventType:           udp.EventNewSession,
	})

	all := append(append([]mockDriver{}, mockDrivers...), mockLeftDriver)

	for i, d := range all {
		rc.UDPCallback(udp.SessionCarInfo{
			CarID:      udp.CarID(i),
			DriverName: d.name,
			DriverGUID: udp.DriverGUID(d.guid),
			CarModel:   d.car,
			CarSkin:    d.skin,
			EventType:  udp.EventNewConnection,
		})

		driver, err := rc.findConnectedDriverByCarID(udp.CarID(i))

		if err != nil {
			continue
		}

		driver.Lock()
		driver.Team = d.team
		driver.RaceNumber = d.number
		driver.LoadedTime = time.Now().Add(-10 * time.Minute)
		driver.ConnectedTime = time.Now().Add(-12 * time.Minute)
		driver.Unlock()
	}

	// every driver completes a different number of laps (the one who left only two)
	lapsOf := func(i int, d mockDriver) int {
		if d.guid == mockLeftDriver.guid {
			return 2
		}

		if sessionType == udp.SessionTypeRace {
			return 7 - (i+3)/5 // 7 laps for the first, 6 for the middle of the field, 5 at the back
		}

		return 4 + (i*3)%5
	}

	type lapEvent struct {
		carID    udp.CarID
		driver   mockDriver
		lapTime  uint32
		cuts     uint8
		lapIndex int
		at       int // cumulative time
	}

	var events []lapEvent

	for i, d := range all {
		total := 0

		for lap := 1; lap <= lapsOf(i, d); lap++ {
			lapTime := float64(mockBaseLapMs) * d.pace * (1 + (rng.Float64()-0.5)*0.03)

			if sessionType == udp.SessionTypeRace && lap == 1 {
				lapTime += 7000 // standing start
			}

			cuts := uint8(0)

			if rng.Intn(9) == 0 {
				cuts = 1
			}

			total += int(lapTime)
			events = append(events, lapEvent{udp.CarID(i), d, uint32(lapTime), cuts, lap, total})
		}
	}

	sort.SliceStable(events, func(a, b int) bool { return events[a].at < events[b].at })

	for _, e := range events {
		if driver, err := rc.findConnectedDriverByCarID(e.carID); err == nil {
			driver.Lock()
			driver.CurrentCar().TopSpeedThisLap = e.driver.topSpeed - float64(rng.Intn(12))
			driver.Unlock()
		}

		rc.UDPCallback(udp.LapCompleted{CarID: e.carID, LapTime: e.lapTime, Cuts: e.cuts, CarsCount: uint8(len(all))})
	}

	// a few collisions: one between two cars, one with a wall
	rc.UDPCallback(udp.CollisionWithCar{CarID: 4, OtherCarID: 5, ImpactSpeed: 32, WorldPos: mockPos(0.31)})
	rc.UDPCallback(udp.CollisionWithEnvironment{CarID: 8, ImpactSpeed: 47, WorldPos: mockPos(0.62)})

	// chat
	for _, c := range []struct {
		carID udp.CarID
		text  string
	}{
		{0, "gl hf everyone"},
		{4, "sorry Tomasz, braked too late in T3"},
		{5, "no worries"},
		{8, "this MX5 is so much fun"},
	} {
		rc.UDPCallback(udp.Chat{CarID: c.carID, Message: c.text})
	}

	// the driver who left
	rc.UDPCallback(udp.SessionCarInfo{
		CarID:      udp.CarID(len(all) - 1),
		DriverName: mockLeftDriver.name,
		DriverGUID: udp.DriverGUID(mockLeftDriver.guid),
		CarModel:   mockLeftDriver.car,
		CarSkin:    mockLeftDriver.skin,
		EventType:  udp.EventConnectionClosed,
	})

	// positions on the track, in the order of the field, as the live map shows them
	connected := rc.ConnectedDrivers.Len()

	for i := range mockDrivers {
		driver, err := rc.findConnectedDriverByCarID(udp.CarID(i))

		if err != nil {
			continue
		}

		driver.Lock()
		driver.LastPos = mockPos(float64(i) / float64(connected))
		driver.LastSeen = time.Now()
		driver.Unlock()
	}

	rc.UDPCallback(udp.SessionInfo{AmbientTemp: 24, RoadTemp: 31, WeatherGraphics: "03_clear", ElapsedMilliseconds: 7*118000 + 41000, EventType: udp.EventSessionInfo})

	// republish the state with the positions (UDPCallback only broadcasts for some events)
	if lastUpdateMessage, err := rc.broadcaster.Send(rc); err == nil {
		rc.lastUpdateMessageMutex.Lock()
		rc.lastUpdateMessage = lastUpdateMessage
		rc.lastUpdateMessageMutex.Unlock()
	}

	// keep the drivers "seen", otherwise watchForTimedOutDrivers disconnects them
	go rc.env.PanicCapture(func() {
		for range time.Tick(time.Minute) {
			_ = rc.ConnectedDrivers.Each(func(_ udp.DriverGUID, driver *smmodel.RaceControlDriver) error {
				driver.Lock()
				driver.LastSeen = time.Now()
				driver.Unlock()

				return nil
			})
		}
	})
}

// mockPos is a point on the ellipse of the fake track map (dev/assetto-seed/content/tracks/dev_ring/*/map.png);
// t goes from 0 to 1 around the lap.
func mockPos(t float64) udp.Vec {
	angle := 2 * math.Pi * t

	return udp.Vec{X: float32(250 * math.Cos(angle)), Z: float32(180 * math.Sin(angle))}
}
