package race

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
)

// FieldError is a validation problem of a single form field. Field is the name of the form input, so the
// form can mark it and link to its section.
type FieldError struct {
	Field   string
	Message string
}

// ValidationErrors is returned by the race setup when the submitted form is not valid. Nothing has been saved
// or started in that case, so the handler can render the form again with the user's input.
type ValidationErrors []FieldError

func (v ValidationErrors) Error() string {
	messages := make([]string, 0, len(v))

	for _, e := range v {
		messages = append(messages, e.Message)
	}

	return "servermanager: invalid race form: " + strings.Join(messages, "; ")
}

// Messages returns the plain error messages.
func (v ValidationErrors) Messages() []string {
	messages := make([]string, 0, len(v))

	for _, e := range v {
		messages = append(messages, e.Message)
	}

	return messages
}

// ValidateCustomRace checks a custom race built from a form. form carries the fields that are not part of the
// race config (name, action).
func ValidateCustomRace(config *serverconfig.CurrentRaceConfig, entryList serverconfig.EntryList, form url.Values) ValidationErrors {
	var errs ValidationErrors

	add := func(field, format string, args ...interface{}) {
		errs = append(errs, FieldError{Field: field, Message: fmt.Sprintf(format, args...)})
	}

	if config.Track == "" {
		add("Track", "Choose a track.")
	}

	if config.Cars == "" {
		add("Cars", "Choose at least one car.")
	}

	if len(config.Sessions) == 0 {
		add("Practice.Enabled", "Enable at least one session.")
	}

	for _, sessionType := range serverconfig.AvailableSessions {
		session, ok := config.Sessions[sessionType]

		if !ok {
			continue
		}

		name := sessionType.String()

		switch {
		case session.Time < 0:
			add(name+".Time", "%s: the time must not be negative.", name)
		case session.Laps < 0:
			add(name+".Laps", "%s: the number of laps must not be negative.", name)
		case sessionType == serverconfig.SessionTypeBooking && session.Time == 0:
			add(name+".Time", "%s: set the booking time.", name)
		case sessionType != serverconfig.SessionTypeBooking && session.Time == 0 && session.Laps == 0:
			add(name+".Time", "%s: set a time or a number of laps.", name)
		}
	}

	if config.MaxClients < 1 {
		add("MaxClients", "The maximum number of clients must be at least 1.")
	}

	if config.RacePitWindowEnd != 0 && config.RacePitWindowEnd < config.RacePitWindowStart {
		add("RacePitWindowEnd", "The pit window must not end before it starts.")
	}

	for _, weather := range config.Weather {
		if weather.WindBaseSpeedMax < weather.WindBaseSpeedMin {
			add("WindBaseSpeedMax", "Weather %s: the maximum wind speed must not be below the minimum.", weather.Graphics)
		}
	}

	entrants := make([]*serverconfig.Entrant, 0, len(entryList))

	for _, entrant := range entryList {
		entrants = append(entrants, entrant)
	}

	sort.Slice(entrants, func(i, j int) bool { return entrants[i].PitBox < entrants[j].PitBox })

	for _, entrant := range entrants {
		if entrant.Model == "" {
			add("EntryList.Car", "Entry %d: choose a car.", entrant.PitBox+1)
		}
	}

	action := form.Get("action")

	if form.Get("CustomRaceName") == "" && (action == "justSave" || action == "schedule" || form.Get("Editing") != "") {
		add("CustomRaceName", "Give the custom race a name.")
	}

	return errs
}

// validateQuickRace checks the fields of the quick race form. The parse errors belong to the numbers next to them.
func validateQuickRace(cars []string, qualifyingTime int64, qualifyingErr error, raceTime int64, raceTimeErr error, raceLaps int64, raceLapsErr error) ValidationErrors {
	var errs ValidationErrors

	if len(cars) == 0 {
		errs = append(errs, FieldError{Field: "Cars", Message: "You must choose at least one car!"})
	}

	if qualifyingErr != nil || qualifyingTime < 0 {
		errs = append(errs, FieldError{Field: "Qualifying.Time", Message: "Enter the qualifying time in minutes (0 or more)."})
	}

	if raceTimeErr != nil || raceTime < 0 {
		errs = append(errs, FieldError{Field: "Race.Time", Message: "Enter the race time in minutes (0 or more)."})
	}

	if raceLapsErr != nil || raceLaps < 0 {
		errs = append(errs, FieldError{Field: "Race.Laps", Message: "Enter the number of race laps (0 or more)."})
	}

	return errs
}
