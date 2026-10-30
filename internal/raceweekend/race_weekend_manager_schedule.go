package raceweekend

import (
	"time"

	"github.com/sirupsen/logrus"
	smlua "github.com/wernersbacher/assetto-server-manager/internal/lua"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	"github.com/wernersbacher/assetto-server-manager/pkg/when"
)

func (rwm *RaceWeekendManager) WatchForScheduledSessions() error {
	raceWeekends, err := rwm.ListRaceWeekends()

	if err != nil {
		return err
	}

	for _, raceWeekend := range raceWeekends {
		raceWeekend := raceWeekend

		for _, session := range raceWeekend.Sessions {
			session := session

			if session.ScheduledServerID != rwm.env.ServerID() {
				continue
			}

			if session.ScheduledTime.After(time.Now()) {
				err := rwm.setupScheduledSessionTimer(raceWeekend, session)

				if err != nil {
					return err
				}
			} else if !session.ScheduledTime.IsZero() {
				logrus.Infof("The %s Session in the %s Race Weekend was scheduled to run, but the server was offline. Please start the session manually.", session.Name(), raceWeekend.Name)
			}
		}
	}

	return nil
}

func (rwm *RaceWeekendManager) clearScheduledSessionTimer(session *smmodel.RaceWeekendSession) {
	if timer := rwm.scheduledSessionTimers[session.ID.String()]; timer != nil {
		timer.Stop()
	}

	rwm.scheduledSessionReminderTimers.Stop(session.ID.String())
}

func (rwm *RaceWeekendManager) setupScheduledSessionTimer(raceWeekend *smmodel.RaceWeekend, session *smmodel.RaceWeekendSession) error {
	rwm.clearScheduledSessionTimer(session)

	var err error

	scheduledTime := session.ScheduledTime

	rwm.scheduledSessionTimers[session.ID.String()], err = when.When(session.ScheduledTime, func() {
		if !rwm.scheduleStillValid(raceWeekend.ID.String(), session.ID.String(), scheduledTime) {
			return
		}

		if rwm.raceManager.AllowScheduledStart(raceWeekend.Name + ": " + session.Name()) {
			err := rwm.StartSession(raceWeekend.ID.String(), session.ID.String(), false)

			if err != nil {
				logrus.WithError(err).Errorf("Could not start scheduled race weekend session")
			}
		}

		// the scheduled time is cleared in any case, so that a skipped session is not fired again

		raceWeekend, session, err := rwm.FindSession(raceWeekend.ID.String(), session.ID.String())

		if err != nil {
			logrus.WithError(err).Error("Could not clear scheduled time on started Race Weekend Session")
			return
		}

		session.ScheduledTime = time.Time{}

		if err := rwm.UpsertRaceWeekend(raceWeekend); err != nil {
			logrus.WithError(err).Error("Could not update race weekend with cleared scheduled time")
		}
	})

	if err != nil {
		return err
	}

	if rwm.notificationManager.HasNotificationReminders() {
		for _, timer := range rwm.notificationManager.GetNotificationReminders() {
			reminderTime := session.ScheduledTime.Add(time.Duration(-timer) * time.Minute)

			if reminderTime.After(time.Now()) {
				// add reminder
				thisTimer := timer

				reminder, err := when.When(reminderTime, func() {
					err := rwm.notificationManager.SendRaceWeekendReminderMessage(raceWeekend, session, thisTimer)

					if err != nil {
						logrus.WithError(err).Errorf("Could not send race weekend reminder message")
					}
				})

				if err != nil {
					logrus.WithError(err).Error("Could not set up race weekend reminder timer")
				}

				rwm.scheduledSessionReminderTimers.Add(session.ID.String(), reminder)
			}
		}
	}

	return nil
}

func (rwm *RaceWeekendManager) ScheduleSession(raceWeekendID, sessionID string, date time.Time, startWhenParentFinishes bool) error {
	raceWeekend, session, err := rwm.FindSession(raceWeekendID, sessionID)

	if err != nil {
		return err
	}

	session.ScheduledTime = date
	session.StartWhenParentHasFinished = startWhenParentFinishes
	session.ScheduledServerID = rwm.env.ServerID()

	if rwm.config.Lua.Enabled {
		err = raceWeekendEventSchedulePlugin(raceWeekend, session)

		if err != nil {
			logrus.WithError(err).Error("race weekend session schedule plugin script failed")
		}
	}

	if !session.ScheduledTime.IsZero() {
		err = rwm.setupScheduledSessionTimer(raceWeekend, session)

		if err != nil {
			return err
		}
	}

	return rwm.UpsertRaceWeekend(raceWeekend)
}

func raceWeekendEventSchedulePlugin(raceWeekend *smmodel.RaceWeekend, raceWeekendSession *smmodel.RaceWeekendSession) error {
	p := smlua.NewPlugin()

	newRaceWeekendSession, newRaceWeekend := smmodel.NewRaceWeekendSession(), smmodel.NewRaceWeekend()

	p.Inputs(raceWeekendSession, raceWeekend).Outputs(newRaceWeekendSession, newRaceWeekend)
	err := p.Call("./plugins/events.lua", "onRaceWeekendEventSchedule")

	if err != nil {
		return err
	}

	*raceWeekendSession, *raceWeekend = *newRaceWeekendSession, *newRaceWeekend

	return nil
}

func (rwm *RaceWeekendManager) DeScheduleSession(raceWeekendID, sessionID string) error {
	raceWeekend, session, err := rwm.FindSession(raceWeekendID, sessionID)

	if err != nil {
		return err
	}

	session.ScheduledTime = time.Time{}
	session.StartWhenParentHasFinished = false

	rwm.clearScheduledSessionTimer(session)

	return rwm.UpsertRaceWeekend(raceWeekend)
}

// scheduleStillValid tells if a session is still scheduled on this server for the time a timer was set for. The timers
// of other servers or older schedules are not stopped when a session is rescheduled or its schedule is removed on
// another server, so a timer checks the stored session when it fires.
func (rwm *RaceWeekendManager) scheduleStillValid(raceWeekendID, sessionID string, scheduledTime time.Time) bool {
	_, session, err := rwm.FindSession(raceWeekendID, sessionID)

	if err != nil {
		logrus.WithError(err).Warnf("Scheduled race weekend session %s not started: it could not be loaded", sessionID)
		return false
	}

	if session.ScheduledServerID != rwm.env.ServerID() || session.ScheduledTime.IsZero() || !session.ScheduledTime.Equal(scheduledTime) {
		logrus.Warnf("Scheduled race weekend session %s not started: its schedule was changed or removed", sessionID)
		return false
	}

	return true
}
