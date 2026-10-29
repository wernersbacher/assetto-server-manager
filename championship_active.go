package servermanager

import (
	"time"

	"github.com/google/uuid"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
)

type ActiveChampionship struct {
	Name                    string
	ChampionshipID, EventID uuid.UUID
	SessionType             serverconfig.SessionType
	OverridePassword        bool
	ReplacementPassword     string
	Description             string
	IsPracticeSession       bool
	RaceConfig              serverconfig.CurrentRaceConfig
	EntryList               serverconfig.EntryList

	NumLapsCompleted   int `json:"-"`
	NumRaceStartEvents int `json:"-"`
}

func (a *ActiveChampionship) GetRaceConfig() serverconfig.CurrentRaceConfig {
	return a.RaceConfig
}

func (a *ActiveChampionship) GetEntryList() serverconfig.EntryList {
	return a.EntryList
}

func (a *ActiveChampionship) IsLooping(serverID smmodel.ServerID) bool {
	return false
}

func (a *ActiveChampionship) IsPractice() bool {
	return a.IsPracticeSession
}

func (a *ActiveChampionship) GetURL(baseURL string) string {
	if baseURL != "" {
		return baseURL + "/championship/" + a.ChampionshipID.String()
	}

	return ""
}

func (a *ActiveChampionship) IsChampionship() bool {
	return true
}

func (a *ActiveChampionship) IsRaceWeekend() bool {
	return false
}

func (a *ActiveChampionship) IsTimeAttack() bool {
	return false
}

func (a *ActiveChampionship) OverrideServerPassword() bool {
	return a.OverridePassword
}

func (a *ActiveChampionship) ReplacementServerPassword() string {
	return a.ReplacementPassword
}

func (a *ActiveChampionship) EventName(trackSummary TrackSummaryFunc) string {
	if a.IsPracticeSession {
		return a.Name + " - Looping Practice"
	}

	return a.Name
}

func (a *ActiveChampionship) EventDescription() string {
	return a.Description
}

func (a *ActiveChampionship) GetForceStopTime() time.Duration {
	return 0
}

func (a *ActiveChampionship) GetForceStopWithDrivers() bool {
	return false
}
