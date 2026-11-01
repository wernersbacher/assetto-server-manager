package championship

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/wernersbacher/ACPitwall/internal/i18n"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

// entryListFormFields are the per-entrant form fields that smrace.RaceManager.BuildEntryList indexes unconditionally.
var entryListFormFields = []string{
	"EntryList.Car", "EntryList.Skin", "EntryList.InternalUUID", "EntryList.Name", "EntryList.Team",
	"EntryList.GUID", "EntryList.Ballast", "EntryList.Restrictor", "EntryList.FixedSetup",
}

// classFormFields are the per-class form fields of the championship form, one value per class.
var classFormFields = []string{
	"ClassID", "EntryList.NumEntrants", "NumPoints", "NumCars", "Points.PolePosition", "Points.BestLap",
	"Points.SecondRaceMultiplier", "Points.CollisionWithDriver", "Points.CollisionWithEnv", "Points.CutTrack",
}

// validateChampionshipForm checks that the (repeated) fields of the championship form are consistent: that every
// class has a value for each of the per-class fields and that the per-class counts (entrants, points, cars) don't
// reach beyond the submitted entrants, points and cars. HandleCreateChampionship indexes these fields by position,
// so a form which is not consistent (e.g. one that was not built by the page's JavaScript) would panic.
func validateChampionshipForm(r *http.Request) error {
	numClasses := len(r.Form["ClassName"])

	for _, field := range classFormFields {
		if len(r.Form[field]) < numClasses {
			return ValidationError{i18n.Msg("validation.championship.form_class_values", "Invalid form: {classes} classes but {count} values for {field}.", "classes", numClasses, "count", len(r.Form[field]), "field", field)}
		}
	}

	totalEntrants, totalPoints, totalCars := 0, 0, 0

	// the spectator car is the first entrant in the form
	if len(r.Form["EntryList.Name"]) > 0 {
		totalEntrants++

		numSpectatorCars := smweb.FormValueAsInt(r.FormValue("NumAvailableSpectatorCars"))

		if numSpectatorCars < 0 {
			return ValidationError{i18n.Msg("validation.championship.form_spectator_cars", "Invalid form: negative number of spectator cars.")}
		}

		totalCars += numSpectatorCars
	}

	for i := 0; i < numClasses; i++ {
		if classID := r.Form["ClassID"][i]; classID != "" {
			if _, err := uuid.Parse(classID); err != nil {
				return ValidationError{i18n.Msg("validation.championship.form_class_id", "Invalid form: \"{id}\" is not a valid class ID.", "id", classID)}
			}
		}

		numEntrants := smweb.FormValueAsInt(r.Form["EntryList.NumEntrants"][i])
		numPoints := smweb.FormValueAsInt(r.Form["NumPoints"][i])
		numCars := smweb.FormValueAsInt(r.Form["NumCars"][i])

		if numEntrants < 0 || numPoints < 0 || numCars < 0 {
			return ValidationError{i18n.Msg("validation.championship.form_negative", "Invalid form: negative number of entrants, points or cars.")}
		}

		totalEntrants += numEntrants
		totalPoints += numPoints
		totalCars += numCars
	}

	if len(r.Form["Points.Place"]) < totalPoints {
		return ValidationError{i18n.Msg("validation.championship.form_points", "Invalid form: {expected} points places expected but {count} submitted.", "expected", totalPoints, "count", len(r.Form["Points.Place"]))}
	}

	if len(r.Form["Cars"]) < totalCars {
		return ValidationError{i18n.Msg("validation.championship.form_cars", "Invalid form: {expected} cars expected but {count} submitted.", "expected", totalCars, "count", len(r.Form["Cars"]))}
	}

	if totalEntrants > 0 {
		for _, field := range entryListFormFields {
			if len(r.Form[field]) < totalEntrants {
				return ValidationError{i18n.Msg("validation.championship.form_entrants", "Invalid form: {expected} entrants expected but {count} values for {field}.", "expected", totalEntrants, "count", len(r.Form[field]), "field", field)}
			}
		}
	}

	return nil
}
