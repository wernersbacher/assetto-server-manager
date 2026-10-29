package model

import (
	"time"

	"github.com/google/uuid"
	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
)

type CustomRace struct {
	ScheduledEventBase

	Name                            string
	HasCustomName, OverridePassword bool
	ReplacementPassword             string

	Created time.Time
	Updated time.Time
	Deleted time.Time
	UUID    uuid.UUID
	Starred bool
	// Deprecated: Replaced by LoopServer
	Loop       bool
	LoopServer map[ServerID]bool

	TimeAttackCombinedResultFile string

	ForceStopTime        int
	ForceStopWithDrivers bool

	RaceConfig serverconfig.CurrentRaceConfig
	EntryList  serverconfig.EntryList

	ScheduledEvents map[ServerID]*ScheduledEventBase
}

func (cr *CustomRace) GetRaceConfig() serverconfig.CurrentRaceConfig {
	return cr.RaceConfig
}

func (cr *CustomRace) GetEntryList() serverconfig.EntryList {
	return cr.EntryList
}

func (cr *CustomRace) IsLooping(serverID ServerID) bool {
	if cr.LoopServer == nil {
		return false
	}

	return cr.LoopServer[serverID]
}

func (cr *CustomRace) EventName(trackSummary TrackSummaryFunc) string {
	if cr.HasCustomName {
		return cr.Name
	}

	return trackSummary(cr.RaceConfig.Track, cr.RaceConfig.TrackLayout)
}

func (cr *CustomRace) OverrideServerPassword() bool {
	return cr.OverridePassword
}

func (cr *CustomRace) ReplacementServerPassword() string {
	return cr.ReplacementPassword
}

func (cr *CustomRace) IsChampionship() bool {
	return false
}

func (cr *CustomRace) IsPractice() bool {
	return false
}

func (cr *CustomRace) IsRaceWeekend() bool {
	return false
}

func (cr *CustomRace) IsTimeAttack() bool {
	return cr.RaceConfig.TimeAttack
}

func (cr *CustomRace) HasSignUpForm() bool {
	return false
}

func (cr *CustomRace) GetID() uuid.UUID {
	return cr.UUID
}

func (cr *CustomRace) GetScheduledServerID() ServerID {
	return cr.ScheduledServerID
}

func (cr *CustomRace) GetRaceSetup() serverconfig.CurrentRaceConfig {
	return cr.RaceConfig
}

func (cr *CustomRace) GetSummary() string {
	return ""
}

func (cr *CustomRace) GetURL() string {
	return ""
}

func (cr *CustomRace) EventDescription() string {
	return ""
}

func (cr *CustomRace) ReadOnlyEntryList() serverconfig.EntryList {
	return cr.EntryList
}

func (cr *CustomRace) GetForceStopTime() time.Duration {
	return time.Minute * time.Duration(cr.ForceStopTime)
}

func (cr *CustomRace) GetForceStopWithDrivers() bool {
	return cr.ForceStopWithDrivers
}
