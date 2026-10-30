package servermanager

import (
	"math/rand"
	"os"
	"os/signal"
	"time"

	"github.com/sirupsen/logrus"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"
	"github.com/wernersbacher/assetto-server-manager/internal/servers"
	smstore "github.com/wernersbacher/assetto-server-manager/internal/store"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func InitWithResolver(resolver *Resolver) error {
	store := resolver.ResolveStore()

	accountManager := resolver.resolveAccountManager()

	if err := accountManager.LoadOptions(); err != nil {
		return err
	}

	id, err := smstore.InitServerID(store)

	if err != nil {
		return err
	}

	// the other slots have the id of their configuration
	resolver.state.SetServerID(id)

	accountManager.Init(id)

	logrus.Infof("Server manager instance identifies as: %s", id)

	registry := resolver.Servers()

	opts, err := store.LoadServerOptionsFor(smmodel.ServerID(resolver.slotConfig(registry.Default()).ID))

	if err != nil && err != smstore.ErrValueNotSet {
		return err
	}

	// these are process-wide, so they follow the options of the default slot
	smresults.UseShortenedDriverNames = opts != nil && opts.UseShortenedDriverNames == 1
	resolver.state.SetFallBackSorting(opts != nil && opts.FallBackResultsSorting == 1)

	notificationManager := resolver.resolveNotificationManager()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	go func() {
		for range c {
			// ^C, handle it
			for _, slot := range registry.All() {
				resolver.stopSlotOnInterrupt(slot)
			}

			if err := notificationManager.Stop(); err != nil {
				logrus.WithError(err).Errorf("Could not stop notification manager")
			}

			for _, slot := range registry.All() {
				slot.RaceControl.PersistTimingData()
			}

			os.Exit(0)
		}
	}()

	for _, slot := range registry.All() {
		go panicCapture(slot.RaceManager.LoopRaces)

		if err := slot.RaceManager.InitScheduledRaces(); err != nil {
			return err
		}

		if err := slot.ChampionshipManager.InitScheduledChampionships(); err != nil {
			return err
		}

		if err := slot.RaceWeekendManager.WatchForScheduledSessions(); err != nil {
			return err
		}
	}

	carManager := resolver.resolveCarManager()

	go func() {
		err := carManager.CreateOrOpenSearchIndex()

		if err != nil {
			logrus.WithError(err).Error("Could not open search index")
		}
	}()

	for _, slot := range registry.All() {
		resolver.restartLastEvent(slot)
	}

	if resolver.config.Store.ScheduledEventCheckLoop > time.Duration(0) {
		logrus.Infof("Experimental Scheduled Event Check loop feature enabled. Checking for new scheduled events every %s", resolver.config.Store.ScheduledEventCheckLoop)

		go func() {
			ticker := time.NewTicker(resolver.config.Store.ScheduledEventCheckLoop)

			for range ticker.C {
				for _, slot := range registry.All() {
					if err := slot.RaceManager.InitScheduledRaces(); err != nil {
						logrus.WithError(err).Errorf("Could not check scheduled events of server %q", slot.ID())
					}
				}
			}
		}()
	}

	return nil
}

// slotStore returns the view of the store of a slot.
func (r *Resolver) slotStore(slot *servers.Slot) smstore.Store {
	return smstore.ForServer(r.store, smmodel.ServerID(r.slotConfig(slot).ID))
}

// stopSlotOnInterrupt stops the event which runs on a slot, and remembers it for the next start if the options of the
// slot ask for it.
func (r *Resolver) stopSlotOnInterrupt(slot *servers.Slot) {
	if !slot.Process.IsRunning() {
		return
	}

	event := slot.Process.Event()
	store := r.slotStore(slot)

	opts, err := store.LoadServerOptions()

	if err == nil && opts.RestartEventOnServerManagerLaunch == 1 {
		// save the event so it can be started again next time server-manager starts
		err := store.UpsertLastRaceEvent(event)

		if err != nil {
			logrus.WithError(err).Error("Could not save last server event")
		}
	}

	if event.IsChampionship() && !event.IsPractice() {
		if err := slot.ChampionshipManager.StopActiveEvent(); err != nil {
			logrus.WithError(err).Errorf("Error stopping Championship event")
		}
	} else if event.IsRaceWeekend() && !event.IsPractice() {
		if err := slot.RaceWeekendManager.StopActiveSession(); err != nil {
			logrus.WithError(err).Errorf("Error stopping Race Weekend session")
		}
	} else {
		if err := slot.Process.Stop(); err != nil {
			logrus.WithError(err).Errorf("Could not stop server")
		}
	}
}

// restartLastEvent starts the event which ran on a slot when the server manager was stopped, if the options of the
// slot ask for it.
func (r *Resolver) restartLastEvent(slot *servers.Slot) {
	store := r.slotStore(slot)

	opts, err := store.LoadServerOptions()

	if err != nil || opts.RestartEventOnServerManagerLaunch != 1 {
		return
	}

	lastEvent, err := store.LoadLastRaceEvent()

	if err != nil || lastEvent == nil {
		return
	}

	switch a := lastEvent.(type) {
	case *smmodel.ActiveChampionship:
		if !a.IsPractice() {
			err = slot.ChampionshipManager.ApplyConfigAndStart(a)
		} else {
			err = slot.RaceManager.ApplyConfigAndStart(a)
		}
	case *smmodel.ActiveRaceWeekend:
		if !a.IsPractice() {
			err = slot.RaceWeekendManager.ApplyConfigAndStart(a)
		} else {
			err = slot.RaceManager.ApplyConfigAndStart(a)
		}
	default:
		err = slot.RaceManager.ApplyConfigAndStart(a)
	}

	if err != nil {
		logrus.WithError(err).Errorf("Could not start last running event")
	}
}
