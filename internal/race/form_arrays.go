package race

import (
	"net/http"

	"github.com/wernersbacher/ACPitwall/internal/i18n"
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
// a manipulated or broken form. The error names the first field which is too short; what names the items (e.g.
// "entrants").
func checkFormArrays(r *http.Request, n int, what i18n.Message, fields []string) error {
	for _, field := range fields {
		if len(r.Form[field]) < n {
			return ValidationErrors{{
				Field: field,
				Message: i18n.Msg("validation.race.form_arrays", "Invalid form: {expected} {items} expected but {count} values for {field} submitted.",
					"expected", n, "items", what, "count", len(r.Form[field]), "field", field),
			}}
		}
	}

	return nil
}
