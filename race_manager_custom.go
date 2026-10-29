package servermanager

import (
	"fmt"
	"sort"
	"time"

	"github.com/etcd-io/bbolt"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
)

const maxRecentRaces = 40

func (rm *RaceManager) ListCustomRaces() (recent, starred, looped, scheduled []*CustomRace, err error) {
	recent, err = rm.store.ListCustomRaces()

	if err == bbolt.ErrBucketNotFound {
		return nil, nil, nil, nil, nil
	} else if err != nil {
		return nil, nil, nil, nil, err
	}

	sort.Slice(recent, func(i, j int) bool {
		return recent[i].Updated.After(recent[j].Updated)
	})

	for _, race := range recent {
		if race.IsLooping(serverID) {
			looped = append(looped, race)
		}

		if race.Starred {
			starred = append(starred, race)
		}

		for _, scheduledEvent := range race.ScheduledEvents {
			if scheduledEvent.Scheduled.IsZero() || scheduledEvent.ScheduledServerID != serverID {
				race.Scheduled = time.Time{}
				race.ScheduledInitial = time.Time{}
				race.ScheduledServerID = ""
				race.Recurrence = ""

				continue
			}

			race.ScheduledServerID = scheduledEvent.ScheduledServerID
			race.Scheduled = scheduledEvent.Scheduled
			race.ScheduledInitial = scheduledEvent.Scheduled
			race.Recurrence = scheduledEvent.Recurrence

			break
		}

		if race.Scheduled.After(time.Now()) && race.ScheduledServerID == serverID {
			scheduled = append(scheduled, race)
		}
	}

	if len(recent) > maxRecentRaces {
		recent = recent[:maxRecentRaces]
	}

	return recent, starred, looped, scheduled, nil
}

func (rm *RaceManager) SaveCustomRace(
	name string,
	overridePassword bool,
	replacementPassword string,
	config serverconfig.CurrentRaceConfig,
	entryList serverconfig.EntryList,
	starred bool,
	forceStopTime int,
	forceStopWithDrivers bool,
) (*CustomRace, error) {
	hasCustomRaceName := true

	if name == "" {
		var trackLayout string

		if config.TrackLayout != "" {
			trackLayout = prettifyName(config.TrackLayout, true)
		}

		name = fmt.Sprintf("%s (%s) in %s (%d entrants)",
			prettifyName(config.Track, false),
			trackLayout,
			carList(config.Cars),
			len(entryList),
		)

		hasCustomRaceName = false
	}

	race := &CustomRace{
		Name:                name,
		HasCustomName:       hasCustomRaceName,
		OverridePassword:    overridePassword,
		ReplacementPassword: replacementPassword,
		Created:             time.Now(),
		UUID:                uuid.New(),
		Starred:             starred,

		ForceStopTime:        forceStopTime,
		ForceStopWithDrivers: forceStopWithDrivers,

		RaceConfig: config,
		EntryList:  entryList,
	}

	err := rm.store.UpsertCustomRace(race)

	if err != nil {
		return nil, err
	}

	return race, nil
}

func (rm *RaceManager) StartCustomRace(uuid string, forceRestart bool) (*CustomRace, error) {
	race, err := rm.store.FindCustomRaceByID(uuid)

	if err != nil {
		return nil, err
	}

	if race.RaceConfig.TimeAttack {
		logrus.Info("Time Attack event started")
		rm.raceControl.currentTimeAttackEvent = race
	}

	// Required for our nice auto loop stuff
	if forceRestart {
		race.RaceConfig.LoopMode = 1
	}

	if race.LoopServer != nil {
		race.LoopServer[serverID] = forceRestart
	}

	return race, rm.applyConfigAndStart(race)
}

func (rm *RaceManager) DeleteCustomRace(uuid string) error {
	race, err := rm.store.FindCustomRaceByID(uuid)

	if err != nil {
		return err
	}

	if !race.Scheduled.IsZero() {
		err := rm.ScheduleRace(uuid, time.Time{}, "remove", "")

		if err != nil {
			return err
		}
	}

	return rm.store.DeleteCustomRace(race)
}

func (rm *RaceManager) ToggleStarCustomRace(uuid string) error {
	race, err := rm.store.FindCustomRaceByID(uuid)

	if err != nil {
		return err
	}

	race.Starred = !race.Starred

	return rm.store.UpsertCustomRace(race)
}

func (rm *RaceManager) ToggleLoopCustomRace(uuid string) (bool, error) {
	race, err := rm.store.FindCustomRaceByID(uuid)

	if err != nil {
		return false, err
	}

	if race.LoopServer == nil {
		race.LoopServer = make(map[ServerID]bool)
	}

	race.LoopServer[serverID] = !race.LoopServer[serverID]

	return race.LoopServer[serverID], rm.store.UpsertCustomRace(race)
}

func (rm *RaceManager) SaveServerOptions(newServerOpts *serverconfig.GlobalServerConfig) error {
	oldServerOpts, err := rm.store.LoadServerOptions()

	if err != nil {
		return err
	}

	err = rm.store.UpsertServerOptions(newServerOpts)

	if err != nil {
		return err
	}

	err = rm.notificationManager.SaveServerOptions(oldServerOpts, newServerOpts)

	if err != nil {
		return err
	}

	err = rm.RescheduleNotifications(oldServerOpts, newServerOpts)

	if err != nil {
		return err
	}

	return nil
}

func (rm *RaceManager) LoadServerOptions() (*serverconfig.GlobalServerConfig, error) {
	serverOpts, err := rm.store.LoadServerOptions()

	if err != nil {
		return nil, err
	}

	udpListenPort, udpSendPort := 0, 0

	for udpListenPort == udpSendPort {
		udpListenPort, err = FreeUDPPort()

		if err != nil {
			return nil, err
		}

		udpSendPort, err = FreeUDPPort()

		if err != nil {
			return nil, err
		}
	}

	serverOpts.FreeUDPPluginAddress = fmt.Sprintf("127.0.0.1:%d", udpSendPort)
	serverOpts.FreeUDPPluginLocalPort = udpListenPort

	return serverOpts, nil
}
