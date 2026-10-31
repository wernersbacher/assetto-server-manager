package championship

import (
	"net/http"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

// ParseChampionshipRaceSettings reads the "Championship Race Settings" section of the championship form. It returns
// nil if the section is not enabled.
func ParseChampionshipRaceSettings(r *http.Request) *smmodel.ChampionshipRaceSettings {
	if smweb.FormValueAsInt(r.FormValue("ChampionshipRaceSettings.Enabled")) != 1 {
		return nil
	}

	return &smmodel.ChampionshipRaceSettings{
		FuelRate:                smweb.FormValueAsInt(r.FormValue("ChampionshipRaceSettings.FuelRate")),
		DamageMultiplier:        smweb.FormValueAsInt(r.FormValue("ChampionshipRaceSettings.DamageMultiplier")),
		TyreWearRate:            smweb.FormValueAsInt(r.FormValue("ChampionshipRaceSettings.TyreWearRate")),
		AllowedTyresOut:         smweb.FormValueAsInt(r.FormValue("ChampionshipRaceSettings.AllowedTyresOut")),
		ABSAllowed:              serverconfig.FactoryAssist(smweb.FormValueAsInt(r.FormValue("ChampionshipRaceSettings.ABSAllowed"))),
		TractionControlAllowed:  serverconfig.FactoryAssist(smweb.FormValueAsInt(r.FormValue("ChampionshipRaceSettings.TractionControlAllowed"))),
		StabilityControlAllowed: smweb.FormValueAsInt(r.FormValue("ChampionshipRaceSettings.StabilityControlAllowed")),
		AutoClutchAllowed:       smweb.FormValueAsInt(r.FormValue("ChampionshipRaceSettings.AutoClutchAllowed")),
		TyreBlanketsAllowed:     smweb.FormValueAsInt(r.FormValue("ChampionshipRaceSettings.TyreBlanketsAllowed")),
		ForceVirtualMirror:      smweb.FormValueAsInt(r.FormValue("ChampionshipRaceSettings.ForceVirtualMirror")),
		DynamicTrack: serverconfig.DynamicTrackConfig{
			SessionStart:    smweb.FormValueAsInt(r.FormValue("ChampionshipRaceSettings.SessionStart")),
			Randomness:      smweb.FormValueAsInt(r.FormValue("ChampionshipRaceSettings.Randomness")),
			SessionTransfer: smweb.FormValueAsInt(r.FormValue("ChampionshipRaceSettings.SessionTransfer")),
			LapGain:         smweb.FormValueAsInt(r.FormValue("ChampionshipRaceSettings.LapGain")),
		},
	}
}
