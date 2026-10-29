package servermanager

import (
	"net/http"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
)

// ParseChampionshipRaceSettings reads the "Championship Race Settings" section of the championship form. It returns
// nil if the section is not enabled.
func ParseChampionshipRaceSettings(r *http.Request) *smmodel.ChampionshipRaceSettings {
	if formValueAsInt(r.FormValue("ChampionshipRaceSettings.Enabled")) != 1 {
		return nil
	}

	return &smmodel.ChampionshipRaceSettings{
		FuelRate:                formValueAsInt(r.FormValue("ChampionshipRaceSettings.FuelRate")),
		DamageMultiplier:        formValueAsInt(r.FormValue("ChampionshipRaceSettings.DamageMultiplier")),
		TyreWearRate:            formValueAsInt(r.FormValue("ChampionshipRaceSettings.TyreWearRate")),
		AllowedTyresOut:         formValueAsInt(r.FormValue("ChampionshipRaceSettings.AllowedTyresOut")),
		ABSAllowed:              serverconfig.FactoryAssist(formValueAsInt(r.FormValue("ChampionshipRaceSettings.ABSAllowed"))),
		TractionControlAllowed:  serverconfig.FactoryAssist(formValueAsInt(r.FormValue("ChampionshipRaceSettings.TractionControlAllowed"))),
		StabilityControlAllowed: formValueAsInt(r.FormValue("ChampionshipRaceSettings.StabilityControlAllowed")),
		AutoClutchAllowed:       formValueAsInt(r.FormValue("ChampionshipRaceSettings.AutoClutchAllowed")),
		TyreBlanketsAllowed:     formValueAsInt(r.FormValue("ChampionshipRaceSettings.TyreBlanketsAllowed")),
		ForceVirtualMirror:      formValueAsInt(r.FormValue("ChampionshipRaceSettings.ForceVirtualMirror")),
		DynamicTrack: serverconfig.DynamicTrackConfig{
			SessionStart:    formValueAsInt(r.FormValue("ChampionshipRaceSettings.SessionStart")),
			Randomness:      formValueAsInt(r.FormValue("ChampionshipRaceSettings.Randomness")),
			SessionTransfer: formValueAsInt(r.FormValue("ChampionshipRaceSettings.SessionTransfer")),
			LapGain:         formValueAsInt(r.FormValue("ChampionshipRaceSettings.LapGain")),
		},
	}
}
