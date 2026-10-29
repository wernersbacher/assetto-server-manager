package model

import (
	"github.com/google/uuid"
	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
)

// ChampionshipRaceSettings are race settings which are configured once for a whole Championship instead of on every
// Championship Event. They mirror the fields of the same name in CurrentRaceConfig. Championship Events opt in with
// ChampionshipEvent.UseChampionshipRaceSettings; if they do, all of these values replace the Event's own ones.
//
// Only settings that describe the "rules of the series" are part of this set. Track, cars, sessions, weather, time of
// day and max clients are specific to an Event and are never taken from here.
//
// A Championship with a nil RaceSettings does not use this feature at all.
type ChampionshipRaceSettings struct {
	FuelRate                int
	DamageMultiplier        int
	TyreWearRate            int
	AllowedTyresOut         int
	ABSAllowed              serverconfig.FactoryAssist
	TractionControlAllowed  serverconfig.FactoryAssist
	StabilityControlAllowed int
	AutoClutchAllowed       int
	TyreBlanketsAllowed     int
	ForceVirtualMirror      int

	DynamicTrack serverconfig.DynamicTrackConfig
}

// NewChampionshipRaceSettingsFromConfig copies the championship wide fields out of a race config.
func NewChampionshipRaceSettingsFromConfig(config serverconfig.CurrentRaceConfig) *ChampionshipRaceSettings {
	return &ChampionshipRaceSettings{
		FuelRate:                config.FuelRate,
		DamageMultiplier:        config.DamageMultiplier,
		TyreWearRate:            config.TyreWearRate,
		AllowedTyresOut:         config.AllowedTyresOut,
		ABSAllowed:              config.ABSAllowed,
		TractionControlAllowed:  config.TractionControlAllowed,
		StabilityControlAllowed: config.StabilityControlAllowed,
		AutoClutchAllowed:       config.AutoClutchAllowed,
		TyreBlanketsAllowed:     config.TyreBlanketsAllowed,
		ForceVirtualMirror:      config.ForceVirtualMirror,
		DynamicTrack:            config.DynamicTrack,
	}
}

// ApplyTo returns a copy of config in which all fields of the settings are replaced. config itself is not modified.
func (s *ChampionshipRaceSettings) ApplyTo(config serverconfig.CurrentRaceConfig) serverconfig.CurrentRaceConfig {
	if s == nil {
		return config
	}

	config.FuelRate = s.FuelRate
	config.DamageMultiplier = s.DamageMultiplier
	config.TyreWearRate = s.TyreWearRate
	config.AllowedTyresOut = s.AllowedTyresOut
	config.ABSAllowed = s.ABSAllowed
	config.TractionControlAllowed = s.TractionControlAllowed
	config.StabilityControlAllowed = s.StabilityControlAllowed
	config.AutoClutchAllowed = s.AutoClutchAllowed
	config.TyreBlanketsAllowed = s.TyreBlanketsAllowed
	config.ForceVirtualMirror = s.ForceVirtualMirror
	config.DynamicTrack = s.DynamicTrack

	return config
}

// HasRaceSettings reports whether the Championship has championship wide race settings enabled.
func (c *Championship) HasRaceSettings() bool {
	return c != nil && c.RaceSettings != nil
}

// RaceSettingsForForm returns the settings to show in the championship form. If the Championship does not have race
// settings yet they are prefilled from the most recent non Race Weekend event, or from the defaults.
func (c *Championship) RaceSettingsForForm() ChampionshipRaceSettings {
	if c.RaceSettings != nil {
		return *c.RaceSettings
	}

	for i := len(c.Events) - 1; i >= 0; i-- {
		if !c.Events[i].IsRaceWeekend() {
			return *NewChampionshipRaceSettingsFromConfig(c.Events[i].RaceSetup)
		}
	}

	return *NewChampionshipRaceSettingsFromConfig(serverconfig.ConfigIniDefault().CurrentRaceConfig)
}

// UsesChampionshipRaceSettings reports whether the given Event takes its race settings from the Championship.
func (c *Championship) UsesChampionshipRaceSettings(event *ChampionshipEvent) bool {
	return c.HasRaceSettings() && event != nil && event.UseChampionshipRaceSettings
}

// EffectiveRaceSetup returns the race config the Event actually runs with: the Event's own config, with the
// championship wide race settings applied if the Event uses them. The stored Event is never modified.
func (c *Championship) EffectiveRaceSetup(event *ChampionshipEvent) serverconfig.CurrentRaceConfig {
	if !c.UsesChampionshipRaceSettings(event) {
		return event.RaceSetup
	}

	return c.RaceSettings.ApplyTo(event.RaceSetup)
}

// EffectiveRaceWeekendSessionSetup returns the race config of a session of a Race Weekend which is part of this
// Championship. The settings are applied if the Championship Event of the Race Weekend uses them.
func (c *Championship) EffectiveRaceWeekendSessionSetup(raceWeekendID uuid.UUID, config serverconfig.CurrentRaceConfig) serverconfig.CurrentRaceConfig {
	if c == nil {
		return config
	}

	for _, event := range c.Events {
		if event.IsRaceWeekend() && event.RaceWeekendID == raceWeekendID {
			if c.UsesChampionshipRaceSettings(event) {
				return c.RaceSettings.ApplyTo(config)
			}

			break
		}
	}

	return config
}

// RaceWeekendEvent returns the Championship Event which holds the Race Weekend with the given ID, or nil.
func (c *Championship) RaceWeekendEvent(raceWeekendID uuid.UUID) *ChampionshipEvent {
	for _, event := range c.Events {
		if event.IsRaceWeekend() && event.RaceWeekendID == raceWeekendID {
			return event
		}
	}

	return nil
}
