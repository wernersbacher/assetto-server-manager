package championship

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"

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
			return ValidationError(fmt.Sprintf("invalid form: %d classes but %d values for %s", numClasses, len(r.Form[field]), field))
		}
	}

	totalEntrants, totalPoints, totalCars := 0, 0, 0

	// the spectator car is the first entrant in the form
	if len(r.Form["EntryList.Name"]) > 0 {
		totalEntrants++

		numSpectatorCars := smweb.FormValueAsInt(r.FormValue("NumAvailableSpectatorCars"))

		if numSpectatorCars < 0 {
			return ValidationError("invalid form: negative number of spectator cars")
		}

		totalCars += numSpectatorCars
	}

	for i := 0; i < numClasses; i++ {
		if classID := r.Form["ClassID"][i]; classID != "" {
			if _, err := uuid.Parse(classID); err != nil {
				return ValidationError(fmt.Sprintf("invalid form: %q is not a valid class ID", classID))
			}
		}

		numEntrants := smweb.FormValueAsInt(r.Form["EntryList.NumEntrants"][i])
		numPoints := smweb.FormValueAsInt(r.Form["NumPoints"][i])
		numCars := smweb.FormValueAsInt(r.Form["NumCars"][i])

		if numEntrants < 0 || numPoints < 0 || numCars < 0 {
			return ValidationError("invalid form: negative number of entrants, points or cars")
		}

		totalEntrants += numEntrants
		totalPoints += numPoints
		totalCars += numCars
	}

	if len(r.Form["Points.Place"]) < totalPoints {
		return ValidationError(fmt.Sprintf("invalid form: %d points places expected but %d submitted", totalPoints, len(r.Form["Points.Place"])))
	}

	if len(r.Form["Cars"]) < totalCars {
		return ValidationError(fmt.Sprintf("invalid form: %d cars expected but %d submitted", totalCars, len(r.Form["Cars"])))
	}

	if totalEntrants > 0 {
		for _, field := range entryListFormFields {
			if len(r.Form[field]) < totalEntrants {
				return ValidationError(fmt.Sprintf("invalid form: %d entrants expected but %d values for %s", totalEntrants, len(r.Form[field]), field))
			}
		}
	}

	return nil
}
