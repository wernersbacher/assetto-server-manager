package servermanager

import (
	smlua "github.com/wernersbacher/assetto-server-manager/internal/lua"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	"github.com/wernersbacher/assetto-server-manager/internal/reminder"
	smstore "github.com/wernersbacher/assetto-server-manager/internal/store"

	"errors"
	"fmt"
	"io"
	"time"

	"github.com/cj123/caldav-go/icalendar"
	"github.com/cj123/caldav-go/icalendar/components"
	"github.com/sirupsen/logrus"
	"github.com/wernersbacher/assetto-server-manager/pkg/when"
)

var ErrScheduledTimeIsZero = errors.New("servermanager: can't schedule race for zero time")

func (cm *ChampionshipManager) ScheduleEvent(championshipID string, eventID string, date time.Time, action string, recurrence string) error {
	championship, event, err := cm.GetChampionshipAndEvent(championshipID, eventID)

	if err != nil {
		return err
	}

	event.Scheduled = date
	event.ScheduledServerID = serverID

	// if there is an existing schedule timer for this event stop it
	if timer := cm.championshipEventStartTimers[event.ID.String()]; timer != nil {
		timer.Stop()
	}

	cm.championshipEventReminderTimers.Stop(event.ID.String())

	if action == "add" {
		if date.IsZero() {
			return ErrScheduledTimeIsZero
		}

		// add a scheduled event on date
		if recurrence != "already-set" {
			if recurrence != "" {
				err := event.SetRecurrenceRule(recurrence)

				if err != nil {
					return err
				}

				// only set once when the event is first scheduled
				event.ScheduledInitial = date
			} else {
				event.ClearRecurrenceRule()
			}
		}

		if cm.config.Lua.Enabled {
			err = championshipEventSchedulePlugin(championship, event)

			if err != nil {
				logrus.WithError(err).Error("event schedule plugin script failed")
			}
		}

		cm.championshipEventStartTimers[event.ID.String()], err = when.When(date, func() {
			err := cm.StartScheduledEvent(championship, event)

			if err != nil {
				logrus.WithError(err).Errorf("couldn't start scheduled championship event")
			}
		})

		if err != nil {
			return err
		}

		if cm.notificationManager.HasNotificationReminders() {
			for _, timer := range cm.notificationManager.GetNotificationReminders() {
				thisTimer := timer

				reminder, err := when.When(date.Add(time.Duration(0-timer)*time.Minute), func() {
					err := cm.notificationManager.SendChampionshipReminderMessage(championship, event, thisTimer)

					if err != nil {
						logrus.WithError(err).Errorf("couldn't send championship reminder message")
					}
				})

				if err != nil {
					logrus.WithError(err).Errorf("couldn't send championship reminder message")
				}

				cm.championshipEventReminderTimers.Add(event.ID.String(), reminder)
			}
		}
	} else {
		event.ClearRecurrenceRule()
	}

	return cm.UpsertChampionship(championship)
}

func championshipEventSchedulePlugin(championship *smmodel.Championship, event *smmodel.ChampionshipEvent) error {
	p := smlua.NewPlugin()

	var standings [][]*smmodel.ChampionshipStanding

	for _, class := range championship.Classes {
		standings = append(standings, class.Standings(championship, championship.Events))
	}

	newEvent, newChampionship := smmodel.NewChampionshipEvent(), smmodel.NewChampionship(championship.Name)

	p.Inputs(event, championship, standings).Outputs(newEvent, newChampionship)
	err := p.Call("./plugins/championship.lua", "onChampionshipEventSchedule")

	if err != nil {
		return err
	}

	*event, *championship = *newEvent, *newChampionship

	return nil
}

// StartScheduledEvent is called by a start timer. The championship and event passed in are the copies that were
// captured when the timer was created, so they may be outdated (results, entrants, edits or sign-ups could have been
// saved in the meantime). Only their IDs and scheduled time are used: everything else is reloaded from the store.
// If the championship or event was deleted, or the event's schedule was changed or removed, nothing is started.
func (cm *ChampionshipManager) StartScheduledEvent(championship *smmodel.Championship, event *smmodel.ChampionshipEvent) error {
	championshipID, eventID, scheduled := championship.ID.String(), event.ID.String(), event.Scheduled

	championship, err := cm.store.LoadChampionship(championshipID)

	if err != nil {
		if err == smstore.ErrChampionshipNotFound {
			logrus.Warnf("Scheduled championship event %s not started: championship %s no longer exists", eventID, championshipID)
			return nil
		}

		return err
	}

	if !championship.Deleted.IsZero() {
		logrus.Warnf("Scheduled championship event %s not started: championship %s was deleted", eventID, championshipID)
		return nil
	}

	event, _, err = championship.EventByID(eventID)

	if err != nil {
		logrus.Warnf("Scheduled championship event %s not started: it no longer exists in championship %s", eventID, championshipID)
		return nil
	}

	if event.Scheduled.IsZero() || !event.Scheduled.Equal(scheduled) {
		logrus.Warnf("Scheduled championship event %s not started: its schedule was changed or removed (timer for %s, now %s)", eventID, scheduled, event.Scheduled)
		return nil
	}

	if event.HasRecurrenceRule() {
		// makes a copy of the (fresh) event and schedules it based on the recurrence rule
		err := cm.ScheduleNextEventFromRecurrence(championship, event)

		if err != nil {
			return err
		}
	} else {
		event.Scheduled = time.Time{}

		err = cm.UpsertChampionship(championship)

		if err != nil {
			return err
		}
	}

	return cm.StartEvent(championshipID, eventID, false)
}

func (cm *ChampionshipManager) ScheduleNextEventFromRecurrence(championship *smmodel.Championship, event *smmodel.ChampionshipEvent) error {
	// make sure the championship is on the event
	event.SetChampionship(championship)

	// duplicate the event with new ID and no schedule/completed time
	eventCopy := smmodel.DuplicateChampionshipEvent(event)
	championship.Events = append(championship.Events, eventCopy)

	err := cm.UpsertChampionship(championship)

	if err != nil {
		return err
	}

	return cm.ScheduleEvent(championship.ID.String(), eventCopy.ID.String(), cm.FindNextEventRecurrence(event, event.Scheduled), "add", "already-set")
}

func (cm *ChampionshipManager) FindNextEventRecurrence(event *smmodel.ChampionshipEvent, start time.Time) time.Time {
	rule, err := event.GetRecurrenceRule()

	if err != nil {
		logrus.WithError(err).Errorf("Couldn't get recurrence rule for race: %s, %s", event.ID.String(), event.Recurrence)
		return time.Time{}
	}

	next := rule.After(start, false)

	if next.After(time.Now()) {
		return next
	}

	return cm.FindNextEventRecurrence(event, next)
}

func (cm *ChampionshipManager) BuildICalFeed(championshipID string, w io.Writer) error {
	championship, err := cm.LoadChampionship(championshipID)

	if err != nil {
		return err
	}

	cal := components.NewCalendar()

	for _, event := range championship.Events {
		if event.Scheduled.IsZero() {
			continue
		}

		event.SetChampionship(championship)

		icalEvent := BuildICalEvent(event, cm.config.HTTP.BaseURL)

		cal.Events = append(cal.Events, icalEvent)
	}

	str, err := icalendar.Marshal(cal)

	if err != nil {
		return err
	}

	_, err = fmt.Fprint(w, str)

	return err
}

func (cm *ChampionshipManager) InitScheduledChampionships() error {
	cm.championshipEventStartTimers = make(map[string]*when.Timer)
	cm.championshipEventReminderTimers = make(reminder.Timers)
	championships, err := cm.ListChampionships()

	if err != nil {
		return err
	}

	for _, championship := range championships {
		championship := championship
		championshipChanged := false

		for _, event := range championship.Events {
			event := event

			if event.ScheduledServerID != serverID {
				continue
			}

			if event.Scheduled.After(time.Now()) {
				// add a scheduled event on date
				cm.championshipEventStartTimers[event.ID.String()], err = when.When(event.Scheduled, func() {
					err := cm.StartScheduledEvent(championship, event)

					if err != nil {
						logrus.WithError(err).Errorf("couldn't start scheduled championship event")
					}
				})

				if err != nil {
					logrus.WithError(err).Errorf("Could not schedule event: %s", event.ID.String())
					continue
				}

				if cm.notificationManager.HasNotificationReminders() {
					for _, timer := range cm.notificationManager.GetNotificationReminders() {
						if event.Scheduled.Add(time.Duration(0-timer) * time.Minute).After(time.Now()) {
							thisTimer := timer

							reminder, err := when.When(event.Scheduled.Add(time.Duration(0-timer)*time.Minute), func() {
								err := cm.notificationManager.SendChampionshipReminderMessage(championship, event, thisTimer)

								if err != nil {
									logrus.WithError(err).Errorf("Could not send championship reminder message for event: %s", event.ID.String())
								}
							})

							if err != nil {
								logrus.WithError(err).Errorf("Could not schedule event: %s", event.ID.String())
								continue
							}

							cm.championshipEventReminderTimers.Add(event.ID.String(), reminder)
						}
					}
				}

				// keep going: every scheduled event needs its own timer, not just the first one found
				continue
			}

			zeroTime := time.Time{}

			if event.Scheduled != zeroTime {
				logrus.Infof("Looks like the server was offline whilst a scheduled event was meant to start!"+
					" Start time: %s. The schedule has been cleared. Start the event manually if you wish to run it.", event.Scheduled.String())

				event.Scheduled = zeroTime
				championshipChanged = true
			}
		}

		if championshipChanged {
			if err := cm.UpsertChampionship(championship); err != nil {
				return err
			}
		}
	}

	return nil
}
