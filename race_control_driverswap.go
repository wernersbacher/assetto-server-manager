package servermanager

import (
	"fmt"
	"math"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	"github.com/wernersbacher/assetto-server-manager/pkg/udp"
)

type driverSwapPenalty struct {
	penalty  time.Duration
	carModel string
}

func (rc *RaceControl) handleDriverSwap(ticker *time.Ticker, config serverconfig.CurrentRaceConfig, client udp.SessionCarInfo, driver *smmodel.RaceControlDriver) {
	var (
		totalTime           time.Duration
		newDriverConnected  bool
		firstPositionUpdate bool
		resumeSwap          bool
	)

	completeTime := time.Second * time.Duration(config.DriverSwapMinTime)
	initialGUID := client.DriverGUID
	currentDriver := driver
	position := currentDriver.LastPos

	logrus.Infof(
		"Driver: %s has initiated a driver swap, disconnected in position: %.2f, %.2f, %.2f. Next driver is expected to connect in the same position for a driver swap!",
		currentDriver.CarInfo.DriverGUID,
		currentDriver.LastPos.X,
		currentDriver.LastPos.Y,
		currentDriver.LastPos.Z,
	)

	driverSwapDone := driver.BeginDriverSwap()

	for {
		select {
		case <-driverSwapDone:
			return
		case <-ticker.C:
			totalTime += time.Second

			countdown := completeTime - totalTime

			if !newDriverConnected {
				reconnect := false

				_ = rc.ConnectedDrivers.Each(func(driverGUID udp.DriverGUID, driver *smmodel.RaceControlDriver) error {
					if driver.CarInfo.CarID == currentDriver.CarInfo.CarID {
						if driver.CarInfo.DriverGUID != currentDriver.CarInfo.DriverGUID {
							if !driver.LoadedTime.IsZero() {
								// new driver has connected in the same car
								currentDriver = driver
								newDriverConnected = true

								logrus.Infof("Driver: %d (%s) has connected", currentDriver.CarInfo.CarID, currentDriver.CarInfo.DriverGUID)
							}
						} else {
							// same driver reconnected
							if resumeSwap {
								logrus.Infof("Driver: %s has reconnected, driver swap still in progress", driver.CarInfo.DriverGUID)

								currentDriver = driver
								newDriverConnected = true
								resumeSwap = false
							} else {
								logrus.Infof("Driver: %s has reconnected, driver swap aborted", initialGUID)
								reconnect = true
							}
						}
					}

					return nil
				})

				if reconnect {
					ticker.Stop()
					return
				}
			} else {
				if totalTime.Seconds() >= completeTime.Seconds() {
					sendChat, err := udp.NewSendChat(currentDriver.CarInfo.CarID, "You are clear to leave the pits, go go go!")

					if err == nil {
						err := rc.process.SendUDPMessage(sendChat)

						if err != nil {
							logrus.WithError(err).Errorf("Unable to send driver swap clear to leave message to: %s", currentDriver.CarInfo.DriverName)
						}
					} else {
						logrus.WithError(err).Errorf("Unable to build driver swap clear to leave message to: %s", currentDriver.CarInfo.DriverName)
					}

					logrus.Infof("Driver: %d has successfully completed their driver swap and is free to leave the pits", currentDriver.CarInfo.CarID)

					ticker.Stop()
					return
				}

				if !firstPositionUpdate {
					nilVec := udp.Vec{X: 0, Y: 0, Z: 0}

					if currentDriver.LastPos != nilVec {
						sendChat, err := udp.NewSendChat(
							currentDriver.CarInfo.CarID,
							fmt.Sprintf(
								"Hi! You are mid way through a driver swap, please wait %s before leaving the pits",
								countdown.String(),
							),
						)

						if err == nil {
							err := rc.process.SendUDPMessage(sendChat)

							if err != nil {
								logrus.WithError(err).Errorf("Unable to send driver swap welcome message to: %s", currentDriver.CarInfo.DriverName)
							}
						} else {
							logrus.WithError(err).Errorf("Unable to build driver swap welcome message to: %s", currentDriver.CarInfo.DriverName)
						}

						firstPositionUpdate = true
					}
				}

				// if driver has moved
				if rc.positionHasChanged(position, currentDriver.LastPos) && firstPositionUpdate {
					// if the time is within the disqualify window
					if countdown >= (time.Second * time.Duration(config.DriverSwapDisqualifyTime)) {
						sendChat, err := udp.NewSendChat(
							currentDriver.CarInfo.CarID,
							fmt.Sprintf(
								"You have been kicked from the session for leaving the pits %s early during a driver swap",
								countdown.String(),
							),
						)

						if err == nil {
							err := rc.process.SendUDPMessage(sendChat)

							if err != nil {
								logrus.WithError(err).Errorf("Unable to send driver swap kicked message to: %s", currentDriver.CarInfo.DriverName)
							}
						} else {
							logrus.WithError(err).Errorf("Unable to build driver swap kicked message to: %s", currentDriver.CarInfo.DriverName)
						}

						time.Sleep(5 * time.Second)

						err = rc.process.SendUDPMessage(udp.NewKickUser(uint8(currentDriver.CarInfo.CarID)))

						if err != nil {
							logrus.WithError(err).Errorf("Unable to send kick command (driver swaps)")
						} else {
							logrus.Infof("Driver: %d has been kicked for leaving the pits %s early during a driver swap", currentDriver.CarInfo.CarID, countdown.String())
						}

						// don't stop the ticker, when the driver reconnects they should still have to wait
						firstPositionUpdate = false
						newDriverConnected = false
						resumeSwap = true
						currentDriver.LastPos = udp.Vec{X: 0, Y: 0, Z: 0}
					} else if countdown >= (time.Second * time.Duration(config.DriverSwapPenaltyTime)) {

						rc.driverSwapPenaltiesMutex.Lock()
						{
							if _, ok := rc.driverSwapPenalties[currentDriver.CarInfo.DriverGUID]; ok {
								rc.driverSwapPenalties[currentDriver.CarInfo.DriverGUID].penalty += countdown + (time.Second * 5)
							} else {
								rc.driverSwapPenalties[currentDriver.CarInfo.DriverGUID] = &driverSwapPenalty{
									penalty:  countdown + (time.Second * 5),
									carModel: currentDriver.CarInfo.CarModel,
								}
							}
						}
						rc.driverSwapPenaltiesMutex.Unlock()

						sendChat, err := udp.NewSendChat(
							currentDriver.CarInfo.CarID,
							fmt.Sprintf(
								"You have been given a %s second penalty for leaving the pits %s early during a driver swap",
								(countdown+(time.Second*5)).String(),
								countdown.String(),
							),
						)

						if err == nil {
							err := rc.process.SendUDPMessage(sendChat)

							if err != nil {
								logrus.WithError(err).Errorf("Unable to send driver swap penalty message to: %s", currentDriver.CarInfo.DriverName)
							}
						} else {
							logrus.WithError(err).Errorf("Unable to build driver swap penalty message to: %s", currentDriver.CarInfo.DriverName)
						}

						logrus.Infof(
							"Driver: %d has been given a %s second penalty for leaving the pits %s early during a driver swap",
							currentDriver.CarInfo.CarID,
							(countdown + (time.Second * 5)).String(),
							countdown.String(),
						)

						ticker.Stop()
						return
					}
				}

				// send countdown messages
				if firstPositionUpdate {
					sendChat, err := udp.NewSendChat(currentDriver.CarInfo.CarID, fmt.Sprintf("Free to leave pits in %s", countdown.String()))

					if err == nil {
						err := rc.process.SendUDPMessage(sendChat)

						if err != nil {
							logrus.WithError(err).Errorf("Unable to send driver swap countdown message to: %s", currentDriver.CarInfo.DriverName)
						}
					} else {
						logrus.WithError(err).Errorf("Unable to build driver swap countdown message to: %s", currentDriver.CarInfo.DriverName)
					}
				}
			}
		}
	}
}

const allowedDriverSwapPositionDifference = 10.0

func (rc *RaceControl) positionHasChanged(initialPosition, currentPosition udp.Vec) bool {
	logrus.Debugf("initial position: %.2f, %.2f, %.2f", initialPosition.X, initialPosition.Y, initialPosition.Z)
	logrus.Debugf("current position: %.2f, %.2f, %.2f", currentPosition.X, currentPosition.Y, currentPosition.Z)

	return math.Abs(float64(initialPosition.X-currentPosition.X)) >= allowedDriverSwapPositionDifference ||
		math.Abs(float64(initialPosition.Y-currentPosition.Y)) >= allowedDriverSwapPositionDifference ||
		math.Abs(float64(initialPosition.Z-currentPosition.Z)) >= allowedDriverSwapPositionDifference
}
