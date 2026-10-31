package model

import (
	"time"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
)

type QuickRace struct {
	OverridePassword    bool
	ReplacementPassword string
	RaceConfig          serverconfig.CurrentRaceConfig
	EntryList           serverconfig.EntryList
}

func (q QuickRace) GetRaceConfig() serverconfig.CurrentRaceConfig {
	return q.RaceConfig
}

func (q QuickRace) GetEntryList() serverconfig.EntryList {
	return q.EntryList
}

func (q QuickRace) IsLooping(serverID ServerID) bool {
	return false
}

func (q QuickRace) IsPractice() bool {
	return false
}

func (q QuickRace) IsChampionship() bool {
	return false
}

func (q QuickRace) IsRaceWeekend() bool {
	return false
}

func (q QuickRace) IsTimeAttack() bool {
	return q.RaceConfig.TimeAttack
}

func (q QuickRace) EventName(trackSummary TrackSummaryFunc) string {
	return trackSummary(q.RaceConfig.Track, q.RaceConfig.TrackLayout)
}

func (q QuickRace) OverrideServerPassword() bool {
	return q.OverridePassword
}

func (q QuickRace) ReplacementServerPassword() string {
	return q.ReplacementPassword
}

func (q QuickRace) EventDescription() string {
	return ""
}

func (q QuickRace) GetURL() string {
	return ""
}

func (q QuickRace) GetForceStopTime() time.Duration {
	return 0
}

func (q QuickRace) GetForceStopWithDrivers() bool {
	return false
}
