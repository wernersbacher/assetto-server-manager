package race

import (
	"fmt"
	"net/http"
)

// entryListFormFields are the per-entrant form fields BuildEntryList reads by index.
var entryListFormFields = []string{
	"EntryList.Car", "EntryList.Skin", "EntryList.InternalUUID", "EntryList.Name", "EntryList.Team",
	"EntryList.GUID", "EntryList.Ballast", "EntryList.Restrictor", "EntryList.FixedSetup",
}

// weatherFormFields are the per-weather form fields (next to Graphics) the weather loop reads by index.
var weatherFormFields = []string{
	"BaseTemperatureAmbient", "BaseTemperatureRoad", "VariationAmbient", "VariationRoad", "WindBaseSpeedMin",
	"WindBaseSpeedMax", "WindBaseDirection", "WindVariationDirection", "ChampionshipPracticeWeather", "DateUnix", "TimeMulti",
}

// checkFormArrays makes sure every field has at least n values, so that indexing the form by position can't panic on
// a manipulated or broken form. The error names the first field which is too short.
func checkFormArrays(r *http.Request, n int, what string, fields []string) error {
	for _, field := range fields {
		if len(r.Form[field]) < n {
			return ValidationErrors{{
				Field:   field,
				Message: fmt.Sprintf("Invalid form: %d %s expected but %d values for %s submitted.", n, what, len(r.Form[field]), field),
			}}
		}
	}

	return nil
}
