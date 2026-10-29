package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
)

// TrackSummaryFunc returns the display name of a track and layout.
type TrackSummaryFunc func(track, layout string) string

type RaceEvent interface {
	GetRaceConfig() serverconfig.CurrentRaceConfig
	GetEntryList() serverconfig.EntryList

	IsLooping(serverID ServerID) bool
	IsChampionship() bool
	IsRaceWeekend() bool
	IsPractice() bool
	IsTimeAttack() bool

	OverrideServerPassword() bool
	ReplacementServerPassword() string

	GetForceStopTime() time.Duration
	GetForceStopWithDrivers() bool

	EventName(trackSummary TrackSummaryFunc) string
	EventDescription() string
}

type marshalledRaceEvent struct {
	Name      string
	RaceEvent RaceEvent
}

// nameOf returns the name a RaceEvent is stored under. The names are part of the stored data and are the ones of the
// types when they lived in the servermanager package (for example *servermanager.QuickRace).
func nameOf(s interface{}) string {
	return strings.Replace(fmt.Sprintf("%T", s), "model.", "servermanager.", 1)
}

func getRaceEvent(name string) (RaceEvent, error) {
	var r RaceEvent

	switch name {
	case nameOf(&QuickRace{}):
		r = &QuickRace{}
	case nameOf(&CustomRace{}):
		r = &CustomRace{}
	case nameOf(&ActiveChampionship{}):
		r = &ActiveChampionship{}
	case nameOf(&ActiveRaceWeekend{}):
		r = &ActiveRaceWeekend{}
	default:
		return nil, fmt.Errorf("servermanager: unknown race event tyre specified: %s", name)
	}

	return r, nil
}

func MarshalRaceEvent(r RaceEvent) ([]byte, error) {
	return json.Marshal(marshalledRaceEvent{
		Name:      nameOf(r),
		RaceEvent: r,
	})
}

func UnmarshalRaceEvent(data []byte) (RaceEvent, error) {
	var rawData map[string]json.RawMessage

	if err := json.Unmarshal(data, &rawData); err != nil {
		return nil, err
	}

	nameData, ok := rawData["Name"]

	if !ok {
		return nil, errors.New("servermanager: race event name not found")
	}

	var name string

	if err := json.Unmarshal(nameData, &name); err != nil {
		return nil, err
	}

	raceEventData, ok := rawData["RaceEvent"]

	if !ok {
		return nil, errors.New("servermanager: race event name not found")
	}

	raceEvent, err := getRaceEvent(name)

	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(raceEventData, &raceEvent); err != nil {
		return nil, err
	}

	return raceEvent, err
}
