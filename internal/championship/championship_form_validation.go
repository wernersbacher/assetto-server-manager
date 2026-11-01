package championship

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/wernersbacher/ACPitwall/internal/i18n"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

// entryListFormFields are the per-entrant form fields that smrace.RaceManager.BuildEntryList indexes unconditionally.
var entryListFormFields = []string{
	"EntryList.Car", "EntryList.Skin", "EntryList.InternalUUID", "EntryList.Name", "EntryList.Team",
	"EntryList.GUID", "EntryList.Ballast", "EntryList.Restrictor", "EntryList.FixedSetup",
}

// classCarClassesField is the per-class form field with the global car classes of the class: the IDs, separated by
// commas, empty for none. It is one value per class on purpose (unlike Cars) so that it cannot be misassigned.
const classCarClassesField = "ClassCarClasses"

// parseClassCarClasses reads the value of classCarClassesField. Every ID must be a valid UUID; whether the car classes
// exist and are listed once is checked against the car classes in the store (ChampionshipManager.checkCarClasses).
func parseClassCarClasses(value string) ([]uuid.UUID, error) {
	var ids []uuid.UUID

	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)

		if part == "" {
			continue
		}

		id, err := uuid.Parse(part)

		if err != nil || id == uuid.Nil {
			return nil, ValidationError{i18n.Msg("validation.championship.form_car_class_id", "Invalid form: \"{id}\" is not a valid car class ID.", "id", part)}
		}

		ids = append(ids, id)
	}

	return ids, nil
}

// classFormFields are the per-class form fields of the championship form, one value per class.
var classFormFields = []string{
	"ClassID", classCarClassesField, "EntryList.NumEntrants", "NumPoints", "NumCars", "Points.PolePosition", "Points.BestLap",
	"Points.SecondRaceMultiplier", "Points.CollisionWithDriver", "Points.CollisionWithEnv", "Points.CutTrack",
}

func classNameAt(r *http.Request, i int) string {
	if i < len(r.Form["ClassName"]) && r.Form["ClassName"][i] != "" {
		return r.Form["ClassName"][i]
	}

	return fmt.Sprintf("#%d", i+1)
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

		carClasses, err := parseClassCarClasses(r.Form[classCarClassesField][i])

		if err != nil {
			return err
		}

		// a class needs cars: single cars or cars of global car classes (entrants name their car as well, so a class
		// with entrants is fine, like before car classes existed)
		if numCars == 0 && len(carClasses) == 0 && numEntrants == 0 {
			return ValidationError{i18n.Msg("validation.championship.form_class_no_cars", "Class \"{class}\" needs at least one car or car class.", "class", classNameAt(r, i))}
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
