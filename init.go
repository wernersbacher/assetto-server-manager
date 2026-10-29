package servermanager

import (
	"math/rand"
	"os"
	"os/signal"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

var (
	serverID        smmodel.ServerID
	serverIDMetaKey = "server_id"
)

func initServerID(store Store) error {
	err := store.GetMeta(serverIDMetaKey, &serverID)

	if err == ErrValueNotSet {
		serverID = smmodel.ServerID(uuid.New().String())
		err = store.SetMeta(serverIDMetaKey, serverID)

		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	return nil
}

func InitWithResolver(resolver *Resolver) error {
	store := resolver.ResolveStore()

	accountManager := resolver.resolveAccountManager()

	if err := accountManager.LoadOptions(); err != nil {
		return err
	}

	if err := initServerID(store); err != nil {
		return err
	}

	accountManager.InitOpenAccount()

	logrus.Infof("Server manager instance identifies as: %s", serverID)

	opts, err := store.LoadServerOptions()

	if err != nil && err != ErrValueNotSet {
		return err
	}

	smresults.UseShortenedDriverNames = opts != nil && opts.UseShortenedDriverNames == 1
	UseFallBackSorting = opts != nil && opts.FallBackResultsSorting == 1

	process := resolver.resolveServerProcess()
	championshipManager := resolver.resolveChampionshipManager()
	raceWeekendManager := resolver.resolveRaceWeekendManager()
	notificationManager := resolver.resolveNotificationManager()
	raceControl := resolver.ResolveRaceControl()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	go func() {
		for range c {
			// ^C, handle it
			if process.IsRunning() {
				event := process.Event()

				opts, err := store.LoadServerOptions()

				if err == nil && opts.RestartEventOnServerManagerLaunch == 1 {
					// save the event so it can be started again next time server-manager starts
					err := store.UpsertLastRaceEvent(event)

					if err != nil {
						logrus.WithError(err).Error("Could not save last server event")
					}
				}

				if event.IsChampionship() && !event.IsPractice() {
					if err := championshipManager.StopActiveEvent(); err != nil {
						logrus.WithError(err).Errorf("Error stopping Championship event")
					}
				} else if event.IsRaceWeekend() && !event.IsPractice() {
					if err := raceWeekendManager.StopActiveSession(); err != nil {
						logrus.WithError(err).Errorf("Error stopping Race Weekend session")
					}
				} else {
					if err := process.Stop(); err != nil {
						logrus.WithError(err).Errorf("Could not stop server")
					}
				}
			}

			if err := notificationManager.Stop(); err != nil {
				logrus.WithError(err).Errorf("Could not stop notification manager")
			}

			raceControl.persistTimingData()

			os.Exit(0)
		}
	}()

	raceManager := resolver.resolveRaceManager()
	go panicCapture(raceManager.LoopRaces)

	err = raceManager.InitScheduledRaces()

	if err != nil {
		return err
	}

	err = championshipManager.InitScheduledChampionships()

	if err != nil {
		return err
	}

	err = raceWeekendManager.WatchForScheduledSessions()

	if err != nil {
		return err
	}

	carManager := resolver.resolveCarManager()

	go func() {
		err = carManager.CreateOrOpenSearchIndex()

		if err != nil {
			logrus.WithError(err).Error("Could not open search index")
		}
	}()

	if opts.RestartEventOnServerManagerLaunch == 1 {
		if lastEvent, err := store.LoadLastRaceEvent(); err == nil && lastEvent != nil {
			var err error

			switch a := lastEvent.(type) {
			case *smmodel.ActiveChampionship:
				if !a.IsPractice() {
					err = championshipManager.applyConfigAndStart(a)
				} else {
					err = raceManager.applyConfigAndStart(a)
				}
			case *smmodel.ActiveRaceWeekend:
				if !a.IsPractice() {
					err = raceWeekendManager.applyConfigAndStart(a)
				} else {
					err = raceManager.applyConfigAndStart(a)
				}
			default:
				err = raceManager.applyConfigAndStart(a)
			}

			if err != nil {
				logrus.WithError(err).Errorf("Could not start last running event")
			}
		}
	}

	if resolver.config.Store.ScheduledEventCheckLoop > time.Duration(0) {
		logrus.Infof("Experimental Scheduled Event Check loop feature enabled. Checking for new scheduled events every %s", resolver.config.Store.ScheduledEventCheckLoop)

		go func() {
			ticker := time.NewTicker(resolver.config.Store.ScheduledEventCheckLoop)

			for range ticker.C {
				err = raceManager.InitScheduledRaces()
			}
		}()
	}

	return nil
}
