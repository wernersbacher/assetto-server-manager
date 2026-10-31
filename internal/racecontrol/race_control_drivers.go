package racecontrol

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mitchellh/go-wordwrap"
	"github.com/sirupsen/logrus"
	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smcontent "github.com/wernersbacher/ACPitwall/internal/content"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smresults "github.com/wernersbacher/ACPitwall/internal/results"
	"github.com/wernersbacher/ACPitwall/pkg/udp"
)

// OnVersion occurs when the Assetto Corsa Server starts up for the first time.
func (rc *RaceControl) OnVersion(version udp.Version) error {
	go rc.env.PanicCapture(rc.requestSessionInfo)

	// clear chat messages on new server start
	rc.ChatMessagesMutex.Lock()
	rc.ChatMessages = []udp.Chat{}
	rc.ChatMessagesMutex.Unlock()

	_, err := rc.broadcaster.Send(version)

	return err
}

// OnCarUpdate occurs every udp.RealTimePosInterval and returns car position, speed, etc.
// drivers top speeds are recorded per lap, as well as their last seen updated.
func (rc *RaceControl) OnCarUpdate(update udp.CarUpdate) error {
	if ch, ok := rc.carUpdaters[update.CarID]; !ok || ch == nil {
		rc.carUpdaters[update.CarID] = make(chan udp.CarUpdate, 1000)

		go rc.env.PanicCapture(func() {
			for update := range rc.carUpdaters[update.CarID] {
				err := rc.handleCarUpdate(update)

				if err != nil {
					logrus.WithError(err).Error("Could not handle car update")
				}
			}
		})
	}

	rc.carUpdaters[update.CarID] <- update

	return nil
}

func (rc *RaceControl) handleCarUpdate(update udp.CarUpdate) error {
	driver, err := rc.findConnectedDriverByCarID(update.CarID)

	if err != nil {
		return err
	}

	driver.Lock()
	defer driver.Unlock()

	speed := metersPerSecondToKilometersPerHour(
		math.Sqrt(math.Pow(float64(update.Velocity.X), 2) + math.Pow(float64(update.Velocity.Z), 2)),
	)

	if speed > driver.CurrentCar().TopSpeedThisLap {
		driver.CurrentCar().TopSpeedThisLap = speed
	}

	driver.LastSeen = time.Now()
	driver.LastPos = update.Pos

	_, err = rc.broadcaster.Send(update)

	return err
}

// OnClientConnect stores CarID -> DriverGUID mappings. if a driver is known to have previously been in this event,
// they will be moved from DisconnectedDrivers to ConnectedDrivers.
func (rc *RaceControl) OnClientConnect(client udp.SessionCarInfo) error {
	rc.carIDToGUIDMutex.Lock()
	rc.CarIDToGUID[client.CarID] = client.DriverGUID
	rc.carIDToGUIDMutex.Unlock()

	client.DriverInitials = smresults.DriverInitials(client.DriverName)
	client.DriverName = smresults.DriverName(client.DriverName)
	client.CarName = smcontent.PrettifyName(client.CarModel, true)

	var driver *smmodel.RaceControlDriver

	if disconnectedDriver, ok := rc.DisconnectedDrivers.Get(client.DriverGUID); ok {
		driver = disconnectedDriver
		logrus.Debugf("Driver %s (%s) reconnected in %s (car id: %d)", driver.CarInfo.DriverName, driver.CarInfo.DriverGUID, driver.CarInfo.CarModel, client.CarID)
		rc.DisconnectedDrivers.Del(client.DriverGUID)
	} else {
		if connectedDriver, ok := rc.ConnectedDrivers.Get(client.DriverGUID); ok {
			driver = connectedDriver
			logrus.Debugf("Driver %s (%s) reconnected (but was already connected...) in %s (car id: %d)", driver.CarInfo.DriverName, driver.CarInfo.DriverGUID, driver.CarInfo.CarModel, client.CarID)
		} else {
			driver = NewRaceControlDriver(client)
			logrus.Debugf("Driver %s (%s) connected in %s (car id: %d)", driver.CarInfo.DriverName, driver.CarInfo.DriverGUID, driver.CarInfo.CarModel, client.CarID)
		}
	}

	driver.Lock()
	defer driver.Unlock()
	driver.CarInfo = client

	if _, ok := driver.Cars[driver.CarInfo.CarModel]; !ok {
		driver.Cars[driver.CarInfo.CarModel] = NewRaceControlCarLapInfo(driver.CarInfo.CarModel)
	}

	driver.ConnectedTime = time.Now()
	driver.LastSeen = time.Time{}
	driver.CurrentCar().LastLapCompletedTime = time.Now()

	rc.ConnectedDrivers.Add(driver.CarInfo.DriverGUID, driver)

	_, err := rc.broadcaster.Send(client)

	return err
}

// OnClientDisconnect moves a client from ConnectedDrivers to DisconnectedDrivers.
func (rc *RaceControl) OnClientDisconnect(client udp.SessionCarInfo) error {
	if ch, ok := rc.carUpdaters[client.CarID]; ok && ch != nil {
		delete(rc.carUpdaters, client.CarID)
	}

	driver, ok := rc.ConnectedDrivers.Get(client.DriverGUID)

	if !ok {
		return fmt.Errorf("racecontrol: client disconnected without ever being connected: %s (%s)", client.DriverName, client.DriverGUID)
	}

	driver.Lock()
	defer driver.Unlock()

	logrus.Debugf("Driver %s (%s) disconnected", driver.CarInfo.DriverName, driver.CarInfo.DriverGUID)

	driver.LoadedTime = time.Time{}

	rc.ConnectedDrivers.Del(driver.CarInfo.DriverGUID)

	if driver.TotalNumLaps > 0 {
		rc.DisconnectedDrivers.Add(driver.CarInfo.DriverGUID, driver)
	}

	config := rc.process.Event().GetRaceConfig()

	// if this race has driver swaps enabled we should initialise one now
	if config.DriverSwapEnabled == 1 && rc.SessionInfo.Type.String() == serverconfig.SessionTypeRace.String() {
		ticker := time.NewTicker(time.Second)

		go rc.handleDriverSwap(ticker, config, client, driver)
	}

	_, err := rc.broadcaster.Send(client)

	return err
}

// findConnectedDriverByCarID looks for a driver in ConnectedDrivers by their CarID. This is the only place CarID
// is used for a look-up, and it uses the CarIDToGUID map to perform the lookup.
func (rc *RaceControl) findConnectedDriverByCarID(carID udp.CarID) (*smmodel.RaceControlDriver, error) {
	rc.carIDToGUIDMutex.RLock()
	driverGUID, ok := rc.CarIDToGUID[carID]
	rc.carIDToGUIDMutex.RUnlock()

	if !ok {
		return nil, fmt.Errorf("racecontrol: could not find DriverGUID for CarID: %d", carID)
	}

	driver, ok := rc.ConnectedDrivers.Get(driverGUID)

	if !ok {
		return nil, fmt.Errorf("racecontrol: could not find connected driver for DriverGUID: %s", driverGUID)
	}

	return driver, nil
}

// OnClientLoaded marks a connected client as having loaded in.
func (rc *RaceControl) OnClientLoaded(loadedCar udp.ClientLoaded) error {
	driver, err := rc.findConnectedDriverByCarID(udp.CarID(loadedCar))

	if err != nil {
		return err
	}

	serverConfig, err := rc.store.LoadServerOptions()

	if err != nil {
		return err
	}

	solWarning := ""
	liveLink := ""

	if rc.process.Event().GetRaceConfig().IsSol == 1 {
		solWarning = "This server is running Sol. For the best experience please install Sol, and remember the other drivers may be driving in night conditions."
	}

	if rc.config != nil && rc.config.HTTP.BaseURL != "" {
		liveLink = fmt.Sprintf("You can view live timings for this event at %s", rc.config.HTTP.BaseURL+"/live-timing")
	}

	wrapped := strings.Split(wordwrap.WrapString(
		fmt.Sprintf(
			"Hi, %s! Welcome to the %s server! %s %s Make this race count! %s\n",
			driver.CarInfo.DriverName,
			serverConfig.GetName(),
			serverConfig.ServerJoinMessage,
			solWarning,
			liveLink,
		),
		60,
	), "\n")

	for _, msg := range wrapped {
		welcomeMessage, err := udp.NewSendChat(driver.CarInfo.CarID, msg)

		if err == nil {
			err := rc.process.SendUDPMessage(welcomeMessage)

			if err != nil {
				logrus.WithError(err).Errorf("Unable to send welcome message to: %s", driver.CarInfo.DriverName)
			}
		} else {
			logrus.WithError(err).Errorf("Unable to build welcome message to: %s", driver.CarInfo.DriverName)
		}
	}

	if err := rc.sendChampionshipPlayerSummaryMessage(driver); err != nil {
		logrus.WithError(err).Errorf("Couldn't send championship welcome message to driver: %s", driver.CarInfo.DriverName)
	}

	logrus.Debugf("Driver: %s (%s) loaded", driver.CarInfo.DriverName, driver.CarInfo.DriverGUID)

	driver.LoadedTime = time.Now()

	_, err = rc.broadcaster.Send(loadedCar)

	return err
}

func (rc *RaceControl) sendChampionshipPlayerSummaryMessage(driver *smmodel.RaceControlDriver) error {
	var championshipID uuid.UUID

	if championship, ok := rc.process.Event().(*smmodel.ActiveChampionship); ok {
		championshipID = championship.ChampionshipID
	} else if raceWeekend, ok := rc.process.Event().(*smmodel.ActiveRaceWeekend); ok {
		championshipID = raceWeekend.ChampionshipID
	} else {
		return nil
	}

	if championshipID == uuid.Nil {
		return nil
	}

	championship, err := rc.store.LoadChampionship(championshipID.String())

	if err != nil {
		return err
	}

	championshipText := " Championship"

	if strings.HasSuffix(strings.ToLower(championship.Name), "championship") {
		championshipText = ""
	}

	visitServer := ""

	if rc.config != nil && rc.config.HTTP.BaseURL != "" {
		visitServer = fmt.Sprintf(" You can check out the results of this championship in detail at %s.", rc.config.HTTP.BaseURL+"/championship/"+championship.ID.String())
	}

	wrapped := strings.Split(wordwrap.WrapString(
		fmt.Sprintf(
			"This event is part of the %s%s! %s%s\n",
			championship.Name,
			championshipText,
			championship.GetPlayerSummary(string(driver.CarInfo.DriverGUID)),
			visitServer,
		),
		60,
	), "\n")

	for _, msg := range wrapped {
		welcomeMessage, err := udp.NewSendChat(driver.CarInfo.CarID, msg)

		if err == nil {
			err := rc.process.SendUDPMessage(welcomeMessage)

			if err != nil {
				logrus.WithError(err).Errorf("Unable to send welcome message to: %s", driver.CarInfo.DriverName)
			}
		} else {
			logrus.WithError(err).Errorf("Unable to build welcome message to: %s", driver.CarInfo.DriverName)
		}
	}

	return nil
}

// OnLapCompleted occurs every time a driver crosses the line. Lap information is collected for the driver
// and best lap time and top speed are calculated. OnLapCompleted also remembers the car the lap was completed in
// a PreviousCars map on the driver. This is so that lap times between different cars can be compared.
func (rc *RaceControl) OnLapCompleted(lap udp.LapCompleted) error {
	driver, err := rc.findConnectedDriverByCarID(lap.CarID)

	if err != nil {
		return err
	}

	driver.Lock()
	defer driver.Unlock()

	lapDuration := lapToDuration(int(lap.LapTime))

	logrus.Debugf("Lap completed by driver: %s (%s), %s", driver.CarInfo.DriverName, driver.CarInfo.DriverGUID, lapDuration)

	driver.TotalNumLaps++
	currentCar := driver.CurrentCar()

	currentCar.TotalLapTime += lapDuration
	currentCar.LastLap = lapDuration
	currentCar.NumLaps++
	currentCar.LastLapCompletedTime = time.Now()

	if lap.Cuts == 0 && (lapDuration < currentCar.BestLap || currentCar.BestLap == 0) {
		currentCar.BestLap = lapDuration
		currentCar.TopSpeedBestLap = currentCar.TopSpeedThisLap
	}

	currentCar.TopSpeedThisLap = 0

	rc.ConnectedDrivers.sort()

	if rc.SessionInfo.Type == udp.SessionTypeRace {
		// calculate split
		if driver.Position == 1 {
			driver.Split = time.Duration(0).String()
		} else {
			_ = rc.ConnectedDrivers.Each(func(otherDriverGUID udp.DriverGUID, otherDriver *smmodel.RaceControlDriver) error {
				if otherDriver.Position == driver.Position-1 {
					driverCar := driver.CurrentCar()
					otherDriverCar := otherDriver.CurrentCar()

					lapDifference := otherDriverCar.NumLaps - driverCar.NumLaps

					if lapDifference <= 0 {
						driver.Split = (driverCar.TotalLapTime - otherDriverCar.TotalLapTime).Round(time.Millisecond).String()
					} else if lapDifference == 1 {
						driver.Split = "1 lap"
					} else {
						driver.Split = fmt.Sprintf("%d laps", lapDifference)
					}
				}

				return nil
			})
		}
	} else {
		var previousCar *smmodel.RaceControlCarLapInfo

		// gaps are calculated vs best lap
		_ = rc.ConnectedDrivers.Each(func(driverGUID udp.DriverGUID, driver *smmodel.RaceControlDriver) error {
			if previousCar == nil {
				driver.Split = "0s"
			} else {
				car := driver.CurrentCar()

				if car.BestLap >= previousCar.BestLap && car.BestLap != 0 {
					driver.Split = (car.BestLap - previousCar.BestLap).String()
				} else {
					driver.Split = ""
				}
			}

			previousCar = driver.CurrentCar()

			return nil
		})
	}

	rc.PersistTimingData()

	return nil
}

// OnCollisionWithCar registers a driver's collision with another car.
func (rc *RaceControl) OnCollisionWithCar(collision udp.CollisionWithCar) error {
	driver, err := rc.findConnectedDriverByCarID(collision.CarID)

	if err != nil {
		return err
	}

	c := smmodel.Collision{
		ID:    uuid.New().String(),
		Type:  smmodel.CollisionWithCar,
		Time:  time.Now(),
		Speed: metersPerSecondToKilometersPerHour(float64(collision.ImpactSpeed)),
	}

	driver.Lock()
	defer driver.Unlock()

	otherDriver, err := rc.findConnectedDriverByCarID(collision.OtherCarID)

	if err == nil {
		c.OtherDriverGUID = otherDriver.CarInfo.DriverGUID
		c.OtherDriverName = otherDriver.CarInfo.DriverName
	}

	driver.Collisions = append(driver.Collisions, c)

	_, err = rc.broadcaster.Send(collision)

	return err
}

// OnCollisionWithEnvironment registers a driver's collision with the environment.
func (rc *RaceControl) OnCollisionWithEnvironment(collision udp.CollisionWithEnvironment) error {
	driver, err := rc.findConnectedDriverByCarID(collision.CarID)

	if err != nil {
		return err
	}

	driver.Lock()
	defer driver.Unlock()

	driver.Collisions = append(driver.Collisions, smmodel.Collision{
		ID:    uuid.New().String(),
		Type:  smmodel.CollisionWithEnvironment,
		Time:  time.Now(),
		Speed: metersPerSecondToKilometersPerHour(float64(collision.ImpactSpeed)),
	})

	_, err = rc.broadcaster.Send(collision)

	return err
}
