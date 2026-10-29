package model

import (
	"time"

	"github.com/google/uuid"
	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
)

// ActiveRaceWeekend indicates which RaceWeekend and RaceWeekendSession are currently running on the server.
type ActiveRaceWeekend struct {
	Name                                     string
	RaceWeekendID, SessionID, ChampionshipID uuid.UUID
	OverridePassword                         bool
	ReplacementPassword                      string
	Description                              string
	IsPracticeSession                        bool
	RaceConfig                               serverconfig.CurrentRaceConfig
	EntryList                                serverconfig.EntryList
}

func (a ActiveRaceWeekend) GetRaceConfig() serverconfig.CurrentRaceConfig {
	return a.RaceConfig
}

func (a ActiveRaceWeekend) GetEntryList() serverconfig.EntryList {
	return a.EntryList
}

func (a ActiveRaceWeekend) IsLooping(serverID ServerID) bool {
	return false
}

func (a ActiveRaceWeekend) IsChampionship() bool {
	return false
}

func (a ActiveRaceWeekend) IsRaceWeekend() bool {
	return true
}

func (a ActiveRaceWeekend) IsPractice() bool {
	return a.IsPracticeSession
}

func (a ActiveRaceWeekend) IsTimeAttack() bool {
	return false
}

func (a ActiveRaceWeekend) OverrideServerPassword() bool {
	return a.OverridePassword
}

func (a ActiveRaceWeekend) ReplacementServerPassword() string {
	return a.ReplacementPassword
}

func (a ActiveRaceWeekend) EventName(trackSummary TrackSummaryFunc) string {
	name := "Race Weekend: " + a.Name

	if a.IsPracticeSession {
		name += " - Practice Session"
	}

	return name
}

func (a ActiveRaceWeekend) EventDescription() string {
	return a.Description
}

func (a ActiveRaceWeekend) GetURL(baseURL string) string {
	if baseURL != "" {
		if a.ChampionshipID != uuid.Nil {
			return baseURL + "/championship/" + a.ChampionshipID.String()
		}

		return baseURL + "/race-weekend/" + a.RaceWeekendID.String()
	}

	return ""
}

func (a ActiveRaceWeekend) GetForceStopTime() time.Duration {
	return 0
}

func (a ActiveRaceWeekend) GetForceStopWithDrivers() bool {
	return false
}
