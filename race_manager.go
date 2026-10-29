package servermanager

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/wernersbacher/assetto-server-manager/pkg/when"
)

var ErrCustomRaceNotFound = errors.New("servermanager: custom race not found")

type RaceManager struct {
	config              *Configuration
	process             ServerProcess
	store               Store
	carManager          *CarManager
	trackManager        *TrackManager
	raceControl         *RaceControl
	notificationManager NotificationDispatcher

	currentRace      *ServerConfig
	currentEntryList EntryList

	mutex sync.RWMutex

	forceStopTimer *when.Timer

	// looped races
	loopedRaceSessionTypes      []SessionType
	loopedRaceWaitForSecondRace bool

	// scheduled races
	customRaceStartTimers    map[string]*when.Timer
	customRaceReminderTimers reminderTimers
}

func NewRaceManager(
	config *Configuration,
	store Store,
	process ServerProcess,
	carManager *CarManager,
	trackManager *TrackManager,
	notificationManager NotificationDispatcher,
	raceControl *RaceControl,
) *RaceManager {
	return &RaceManager{
		config:                   config,
		store:                    store,
		process:                  process,
		carManager:               carManager,
		trackManager:             trackManager,
		notificationManager:      notificationManager,
		raceControl:              raceControl,
		customRaceStartTimers:    make(map[string]*when.Timer),
		customRaceReminderTimers: make(reminderTimers),
	}
}

func (rm *RaceManager) CurrentRace() (*ServerConfig, EntryList) {
	rm.mutex.RLock()
	defer rm.mutex.RUnlock()

	if !rm.process.IsRunning() {
		return nil, nil
	}

	return rm.currentRace, rm.currentEntryList
}

var ErrEntryListTooBig = errors.New("servermanager: EntryList exceeds MaxClients setting")

func (rm *RaceManager) applyConfigAndStart(event RaceEvent) error {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	// Reset the stored session types if this isn't a looped race
	if !event.IsLooping() {
		rm.clearLoopedRaceSessionTypes()
	}

	if !event.IsTimeAttack() {
		logrus.Debug("event is not time attack, clearing time attack event on race control")
		rm.raceControl.currentTimeAttackEvent = nil
	}

	// load server opts
	serverOpts, err := rm.LoadServerOptions()

	if err != nil {
		return err
	}

	if serverOpts.RestartEventOnServerManagerLaunch == 1 {
		if err := rm.store.ClearLastRaceEvent(); err != nil {
			logrus.WithError(err).Errorf("Could not clear last race event")
		}
	}

	raceConfig := event.GetRaceConfig()
	entryList := event.GetEntryList()

	if rm.config.Lua.Enabled {
		err = eventStartPlugin(&raceConfig, serverOpts, &entryList)

		if err != nil {
			logrus.WithError(err).Error("event start plugin script failed")
		}
	}

	// the server won't start if an entrant has a larger ballast than is set as the max, correct if necessary
	greatestBallast := entryList.FindGreatestBallast()

	if greatestBallast > raceConfig.MaxBallastKilograms {
		raceConfig.MaxBallastKilograms = greatestBallast
	}

	// if this is a championship practice and some practice weathers exist replace weather in config with them
	if event.IsChampionship() && event.IsPractice() {
		practiceWeather := make(map[string]*WeatherConfig)

		id := 0

		for _, weather := range raceConfig.Weather {
			if weather.ChampionshipPracticeWeather == weatherPractice || weather.ChampionshipPracticeWeather == weatherAny {
				practiceWeather[fmt.Sprintf("WEATHER_%d", id)] = weather

				id++
			}
		}

		if len(practiceWeather) > 0 {
			raceConfig.Weather = practiceWeather
		}
	} else if event.IsChampionship() {
		nonPracticeWeather := make(map[string]*WeatherConfig)

		id := 0

		for _, weather := range raceConfig.Weather {
			if weather.ChampionshipPracticeWeather == weatherEvent || weather.ChampionshipPracticeWeather == weatherAny {
				nonPracticeWeather[fmt.Sprintf("WEATHER_%d", id)] = weather

				id++
			}
		}

		if len(nonPracticeWeather) > 0 {
			raceConfig.Weather = nonPracticeWeather
		}
	}

	config := ServerConfig{
		CurrentRaceConfig:  raceConfig,
		GlobalServerConfig: *serverOpts,
	}

	forwardingAddress := config.GlobalServerConfig.UDPPluginAddress
	forwardListenPort := config.GlobalServerConfig.UDPPluginLocalPort

	config.GlobalServerConfig.UDPPluginAddress = config.GlobalServerConfig.FreeUDPPluginAddress
	config.GlobalServerConfig.UDPPluginLocalPort = config.GlobalServerConfig.FreeUDPPluginLocalPort

	// filter out "AnyCarModel"
	finalCars := make([]string, 0)

	for _, car := range strings.Split(config.CurrentRaceConfig.Cars, ";") {
		if car == AnyCarModel {
			continue
		}

		finalCars = append(finalCars, car)
	}

	config.CurrentRaceConfig.Cars = strings.Join(finalCars, ";")

	// if password override turn the password off
	if event.OverrideServerPassword() {
		config.GlobalServerConfig.Password = event.ReplacementServerPassword()
	} else {
		config.GlobalServerConfig.Password = serverOpts.Password
	}

	if config.CurrentRaceConfig.HasSession(SessionTypeBooking) {
		config.CurrentRaceConfig.PickupModeEnabled = 0
	}

	sessions, sessionTypes := config.CurrentRaceConfig.Sessions.AsSliceWithSessionTypes()

	if len(sessions) > 0 {
		session, sessionType := sessions[0], sessionTypes[0]

		if session.IsOpen == SessionOpennessNoJoin {
			if sessionType == SessionTypeRace {
				logrus.Warnf("Session openness was set to no join on the first session of this event! Corrected to 'Free Join until 20 seconds to the green light'")
				session.IsOpen = SessionOpennessFreeJoinUntil20SecondsToTheGreenLight
			} else {
				logrus.Warnf("Session openness was set to no join on the first session of this event! Corrected to 'Free Join'")
				session.IsOpen = SessionOpennessFreeJoin
			}
		}
	}

	// drs zones management
	err = ToggleDRSForTrack(config.CurrentRaceConfig.Track, config.CurrentRaceConfig.TrackLayout, !config.CurrentRaceConfig.DisableDRSZones)

	if err != nil {
		return err
	}

	if config.GlobalServerConfig.ShowRaceNameInServerLobby == 1 {
		// append the race name to the server name
		if name := event.EventName(); name != "" {
			config.GlobalServerConfig.Name = buildFinalServerName(config.GlobalServerConfig.ServerNameTemplate, event, config)
		}
	}

	if config.GlobalServerConfig.EnableContentManagerWrapper == 1 && config.GlobalServerConfig.ContentManagerWrapperPort > 0 {
		config.GlobalServerConfig.Name += fmt.Sprintf(" %c%d", contentManagerWrapperSeparator, config.GlobalServerConfig.ContentManagerWrapperPort)
	}

	err = config.Write()

	if err != nil {
		return err
	}

	numEntrantsWithAnyCar := 0

	for _, entrant := range entryList {
		if entrant.Model == AnyCarModel {
			// cars with 'any car model' become random in the entry list.
			entrant.Model = finalCars[numEntrantsWithAnyCar%len(finalCars)]
			entrant.Skin = rm.carManager.RandomSkin(entrant.Model)

			numEntrantsWithAnyCar++
		}
	}

	err = entryList.Write()

	if err != nil {
		return err
	}

	rm.currentRace = &config
	rm.currentEntryList = entryList

	err = rm.process.Start(event, config.GlobalServerConfig.UDPPluginAddress, config.GlobalServerConfig.UDPPluginLocalPort, forwardingAddress, forwardListenPort)

	if err != nil {
		return err
	}

	if !event.IsLooping() {
		_ = rm.notificationManager.SendRaceStartMessage(config, event)
	}

	// existing timer needs to be stopped in all cases
	if rm.forceStopTimer != nil {
		rm.forceStopTimer.Stop()
	}

	if event.GetForceStopTime() != 0 {
		// initiate force stop timer
		var withDrivers string

		if !event.GetForceStopWithDrivers() {
			withDrivers = "(unless there are drivers on the server at that time)"
		} else {
			withDrivers = "(even if there are drivers on the server at that time)"
		}

		logrus.Infof("Force Stop timer initialised, the server will be forcibly stopped after %.2f minutes %s.", event.GetForceStopTime().Minutes(), withDrivers)

		rm.forceStopTimer, err = when.When(time.Now().Add(event.GetForceStopTime()), func() {
			if rm.process.IsRunning() {

				if (event.GetForceStopWithDrivers()) || (rm.raceControl.ConnectedDrivers.Len() == 0) {
					err := rm.process.Stop()

					if err != nil {
						logrus.WithError(err).Error("couldn't forcibly stop the server!")
						return
					}

					logrus.Infof("Force Stop time expired, the server has been successfully stopped.")
				} else {
					logrus.Infof("Force Stop time expired, but %d drivers are on the server! Force stop aborted. "+
						"The server should stop automatically on event completion.", rm.raceControl.ConnectedDrivers.Len())
				}

			}
		})

		if err != nil {
			return err
		}
	}

	return nil
}

func eventStartPlugin(raceConfig *CurrentRaceConfig, serverOpts *GlobalServerConfig, entryList *EntryList) error {
	p := NewLuaPlugin()

	newRaceConfig, newServerOpts, newEntryList := &CurrentRaceConfig{}, &GlobalServerConfig{}, &EntryList{}

	p.Inputs(raceConfig, serverOpts, entryList).Outputs(newRaceConfig, newServerOpts, newEntryList)
	err := p.Call("./plugins/events.lua", "onEventStart")

	if err != nil {
		return err
	}

	*raceConfig, *serverOpts, *entryList = *newRaceConfig, *newServerOpts, *newEntryList

	return nil
}
