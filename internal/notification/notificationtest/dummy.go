// Package notificationtest provides a stand-in for the notification manager, for the tests of the packages which use it.
package notificationtest

import (
	"net/url"
	"time"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

// Dummy is a notification dispatcher which does nothing and has no reminders configured.
type Dummy struct{}

func (d *Dummy) HasNotificationReminders() bool {
	return false
}

func (d *Dummy) GetNotificationReminders() []int {
	var reminders []int

	return reminders
}

func (d Dummy) SendRaceWeekendReminderMessage(raceWeekend *smmodel.RaceWeekend, session *smmodel.RaceWeekendSession, timer int) error {
	return nil
}

func (d Dummy) SendMessage(title string, msg string) error {
	return nil
}

func (d Dummy) SendMessageWithLink(title string, msg string, linkText string, link *url.URL) error {
	return nil
}

func (d Dummy) SendRaceStartMessage(config serverconfig.ServerConfig, event smmodel.RaceEvent) error {
	return nil
}

func (d Dummy) GetCarList(cars string) string {
	return "nil"
}

func (d Dummy) GetTrackInfo(track string, layout string, download bool) string {
	return "nil"
}

func (d Dummy) SendRaceScheduledMessage(event *smmodel.CustomRace, date time.Time) error {
	return nil
}

func (d Dummy) SendRaceCancelledMessage(event *smmodel.CustomRace, date time.Time) error {
	return nil
}

func (d Dummy) SendRaceReminderMessage(event *smmodel.CustomRace, timer int) error {
	return nil
}

func (d Dummy) SendChampionshipReminderMessage(championship *smmodel.Championship, event *smmodel.ChampionshipEvent, timer int) error {
	return nil
}

func (d Dummy) SaveServerOptions(oldServerOpts *serverconfig.GlobalServerConfig, newServerOpts *serverconfig.GlobalServerConfig) error {
	return nil
}
