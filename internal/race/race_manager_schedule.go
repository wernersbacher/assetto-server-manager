package race

import (
	"errors"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smlua "github.com/wernersbacher/assetto-server-manager/internal/lua"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	"github.com/wernersbacher/assetto-server-manager/pkg/when"
)

func (rm *RaceManager) ScheduleRace(uuid string, date time.Time, action string, recurrence string) error {
	race, err := rm.store.FindCustomRaceByID(uuid)

	if err != nil {
		return err
	}

	originalDate := race.Scheduled
	race.Scheduled = date
	race.ScheduledServerID = rm.env.ServerID()

	if race.ScheduledEvents == nil {
		race.ScheduledEvents = make(map[smmodel.ServerID]*smmodel.ScheduledEventBase)
	}

	race.ScheduledEvents[rm.env.ServerID()] = &smmodel.ScheduledEventBase{
		Scheduled:         race.Scheduled,
		ScheduledInitial:  race.ScheduledInitial,
		Recurrence:        race.Recurrence,
		ScheduledServerID: race.ScheduledServerID,
	}

	// if there is an existing schedule timer for this event stop it
	if timer := rm.customRaceStartTimers[race.UUID.String()]; timer != nil {
		timer.Stop()
	}

	rm.customRaceReminderTimers.Stop(race.UUID.String())

	if action == "add" {
		if race.Scheduled.IsZero() {
			return ErrScheduledTimeIsZero
		}

		// add a scheduled event on date
		if recurrence != "already-set" {
			if recurrence != "" {
				err := race.SetRecurrenceRule(recurrence)

				if err != nil {
					return err
				}

				// only set once when the event is first scheduled
				race.ScheduledInitial = race.Scheduled
			} else {
				race.ClearRecurrenceRule()
			}
		}

		if rm.config.Lua.Enabled {
			err = eventSchedulePlugin(race)

			if err != nil {
				logrus.WithError(err).Error("event schedule plugin script failed")
			}
		}

		rm.customRaceStartTimers[race.UUID.String()], err = when.When(race.Scheduled, func() {
			err := rm.StartScheduledRace(race)

			if err != nil {
				logrus.WithError(err).Errorf("Couldn't start scheduled race: %s, %s.", race.Name, race.UUID.String())
			}
		})

		if err != nil {
			return err
		}

		if rm.notificationManager.HasNotificationReminders() {
			_ = rm.notificationManager.SendRaceScheduledMessage(race, race.Scheduled)

			for _, timer := range rm.notificationManager.GetNotificationReminders() {
				thisTimer := timer

				reminder, err := when.When(race.Scheduled.Add(time.Duration(0-timer)*time.Minute), func() {
					_ = rm.notificationManager.SendRaceReminderMessage(race, thisTimer)
				})

				if err != nil {
					logrus.WithError(err).Error("Could not set up race reminder timer")
				}

				rm.customRaceReminderTimers.Add(race.UUID.String(), reminder)
			}
		}

	} else {
		_ = rm.notificationManager.SendRaceCancelledMessage(race, originalDate)
		race.ClearRecurrenceRule()
	}

	return rm.store.UpsertCustomRace(race)
}

func eventSchedulePlugin(race *smmodel.CustomRace) error {
	p := smlua.NewPlugin()

	newRace := &smmodel.CustomRace{}

	p.Inputs(race).Outputs(newRace)
	err := p.Call("./plugins/events.lua", "onEventSchedule")

	if err != nil {
		return err
	}

	*race = *newRace

	return nil
}

func (rm *RaceManager) StartScheduledRace(race *smmodel.CustomRace) error {
	startedRace, err := rm.store.FindCustomRaceByID(race.UUID.String())

	if err != nil {
		return err
	}

	// a skipped start still goes through the schedule bookkeeping below (next recurrence or cleared schedule)
	if rm.AllowScheduledStart(startedRace.EventName(rm.env.TrackSummary)) {
		startedRace, err = rm.StartCustomRace(race.UUID.String(), false)

		if err != nil {
			return err
		}
	}

	if startedRace.HasRecurrenceRule() {
		// this function carries out a save
		return rm.ScheduleNextFromRecurrence(startedRace)
	}

	startedRace.Scheduled = time.Time{}

	return rm.store.UpsertCustomRace(startedRace)
}

func (rm *RaceManager) ScheduleNextFromRecurrence(race *smmodel.CustomRace) error {
	// set the scheduled time to the next iteration of the recurrence rule
	nextRecurrence := rm.FindNextRecurrence(race, race.Scheduled)

	if nextRecurrence.IsZero() {
		// no recurrence was found (likely the recurrence had an UNTIL date)
		race.ClearRecurrenceRule()
		return rm.store.UpsertCustomRace(race)
	}

	return rm.ScheduleRace(race.UUID.String(), nextRecurrence, "add", "already-set")
}

func (rm *RaceManager) FindNextRecurrence(race *smmodel.CustomRace, start time.Time) time.Time {
	rule, err := race.GetRecurrenceRule()

	if err != nil {
		logrus.WithError(err).Errorf("Couldn't get recurrence rule for race: %s, %s", race.Name, race.Recurrence)
		return time.Time{}
	}

	next := rule.After(start, false)

	if next.After(time.Now()) {
		return next
	} else if next.IsZero() {
		return next
	} else {
		return rm.FindNextRecurrence(race, next)
	}
}

func (rm *RaceManager) InitScheduledRaces() error {
	races, err := rm.store.ListCustomRaces()

	if err != nil {
		return err
	}

	for _, race := range races {
		race := race

		for _, scheduledEvent := range race.ScheduledEvents {
			if scheduledEvent.Scheduled.IsZero() || scheduledEvent.ScheduledServerID != rm.env.ServerID() {
				continue
			}

			race.ScheduledServerID = scheduledEvent.ScheduledServerID
			race.Scheduled = scheduledEvent.Scheduled
			race.ScheduledInitial = scheduledEvent.Scheduled
			race.Recurrence = scheduledEvent.Recurrence

			break
		}

		if race.ScheduledServerID != rm.env.ServerID() {
			continue
		}

		newScheduledEvent := false

		if timer, ok := rm.customRaceStartTimers[race.UUID.String()]; ok {
			timer.Stop()
			delete(rm.customRaceStartTimers, race.UUID.String())
		} else {
			newScheduledEvent = true
		}

		rm.customRaceReminderTimers.Stop(race.UUID.String())

		if race.Scheduled.After(time.Now()) {
			if newScheduledEvent {
				logrus.Infof("Adding new event (%s) to scheduled start timers, starts at %s", race.Name, race.Scheduled.String())
			}

			// add a scheduled event on date
			rm.customRaceStartTimers[race.UUID.String()], err = when.When(race.Scheduled, func() {
				err := rm.StartScheduledRace(race)

				if err != nil {
					logrus.WithError(err).Errorf("Couldn't start scheduled race: %s, %s", race.Name, race.UUID.String())
				}
			})

			if err != nil {
				logrus.WithError(err).Error("Could not set up scheduled race timer")
			}

			if rm.notificationManager.HasNotificationReminders() {
				for _, timer := range rm.notificationManager.GetNotificationReminders() {
					if race.Scheduled.Add(time.Duration(0-timer) * time.Minute).After(time.Now()) {
						// add reminder
						thisTimer := timer

						reminder, err := when.When(race.Scheduled.Add(time.Duration(0-timer)*time.Minute), func() {
							_ = rm.notificationManager.SendRaceReminderMessage(race, thisTimer)
						})

						if err != nil {
							logrus.WithError(err).Error("Could not set up scheduled race reminder timer")
						}

						rm.customRaceReminderTimers.Add(race.UUID.String(), reminder)
					}
				}
			}
		} else {
			if race.HasRecurrenceRule() {
				emptyTime := time.Time{}
				if race.Scheduled != emptyTime {
					logrus.Infof("Looks like the server was offline whilst a recurring scheduled event was meant to start!"+
						" Start time: %s. The schedule has been cleared, and the next recurrence time has been set."+
						" Start the event manually if you wish to run it.", race.Scheduled.String())
				}

				err := rm.ScheduleNextFromRecurrence(race)

				if err != nil {
					logrus.WithError(err).Errorf("Couldn't schedule next recurring race: %s, %s, %s", race.Name, race.UUID.String(), race.Recurrence)
				}
			} else {
				emptyTime := time.Time{}
				if race.Scheduled != emptyTime {
					logrus.Infof("Looks like the server was offline whilst a scheduled event was meant to start!"+
						" Start time: %s. The schedule has been cleared. Start the event manually if you wish to run it.", race.Scheduled.String())

					race.Scheduled = emptyTime
					race.ScheduledEvents = make(map[smmodel.ServerID]*smmodel.ScheduledEventBase)

					err := rm.store.UpsertCustomRace(race)

					if err != nil {
						return err
					}
				}
			}
		}
	}

	return nil
}

// reschedule notifications if notification timer changed
func (rm *RaceManager) RescheduleNotifications(oldServerOpts *serverconfig.GlobalServerConfig, newServerOpts *serverconfig.GlobalServerConfig) error {
	if newServerOpts.NotificationReminderTimers == oldServerOpts.NotificationReminderTimers {
		return nil
	}

	// stop all existing timers, then rebuild them
	rm.customRaceReminderTimers.StopAll()

	if rm.notificationManager.HasNotificationReminders() {
		races, err := rm.store.ListCustomRaces()

		if err != nil {
			return err
		}

		for _, timer := range rm.notificationManager.GetNotificationReminders() {
			for _, race := range races {
				race := race

				if race.Scheduled.After(time.Now()) && race.Scheduled.Add(time.Duration(0-timer)*time.Minute).After(time.Now()) {
					// add reminder
					thisTimer := timer

					reminder, err := when.When(race.Scheduled.Add(time.Duration(0-timer)*time.Minute), func() {
						_ = rm.notificationManager.SendRaceReminderMessage(race, thisTimer)
					})

					if err != nil {
						logrus.WithError(err).Error("Could not set up scheduled race reminder timer")
					}

					rm.customRaceReminderTimers.Add(race.UUID.String(), reminder)
				}
			}
		}
	}

	return nil
}

// ErrScheduledTimeIsZero is returned when an event is scheduled for the zero time.
var ErrScheduledTimeIsZero = errors.New("servermanager: can't schedule race for zero time")
