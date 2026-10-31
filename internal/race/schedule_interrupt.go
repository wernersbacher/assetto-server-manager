package race

import (
	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/wernersbacher/ACPitwall/pkg/udp"
)

// CanInterruptForSchedule decides if a scheduled start may end the event which currently runs on the server. It may
// if the running session is a practice (the live session type is practice, or the event is a practice event), or if
// nobody is connected. Qualifying and race sessions (and everything else) with drivers online are protected. reason
// describes the running session and the drivers, for the log.
func CanInterruptForSchedule(sessionType udp.SessionType, driversOnline int, isPractice bool) (ok bool, reason string) {
	practice := isPractice || sessionType == udp.SessionTypePractice

	session := sessionType.String() + " session"

	if isPractice && sessionType != udp.SessionTypePractice {
		session = "practice event, " + session
	}

	reason = fmt.Sprintf("%s, %d driver(s) online", session, driversOnline)

	return practice || driversOnline == 0, reason
}

// AllowScheduledStart tells if a scheduled start of the event named scheduledName may go ahead on the server of this
// manager. It does if no event runs, or if the running one may be interrupted (see CanInterruptForSchedule); the
// interruption is logged. Otherwise the start must be skipped: the reason is logged as a warning and sent as a
// notification.
func (rm *RaceManager) AllowScheduledStart(scheduledName string) bool {
	if !rm.process.IsRunning() {
		return true
	}

	var (
		sessionType   udp.SessionType
		driversOnline int
	)

	if rm.raceControl != nil {
		sessionType = rm.raceControl.SessionInfo.Type

		if rm.raceControl.ConnectedDrivers != nil {
			driversOnline = rm.raceControl.ConnectedDrivers.Len()
		}
	}

	event := rm.process.Event()
	runningName := event.EventName(rm.env.TrackSummary)

	ok, reason := CanInterruptForSchedule(sessionType, driversOnline, event.IsPractice())

	if ok {
		logrus.Infof("Scheduled event %q starts: ending the running session of %q (%s)", scheduledName, runningName, reason)
		return true
	}

	logrus.Warnf("Scheduled event %q was not started because %q is running (%s)", scheduledName, runningName, reason)

	if rm.notificationManager != nil {
		_ = rm.notificationManager.SendMessage("Scheduled event not started", fmt.Sprintf("The scheduled event %q was not started because %q is running (%s).", scheduledName, runningName, reason))
	}

	return false
}
