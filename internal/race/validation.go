package race

import (
	"net/url"
	"sort"
	"strings"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	"github.com/wernersbacher/ACPitwall/internal/i18n"
)

// FieldError is a validation problem of a single form field. Field is the name of the form input, so the
// form can mark it and link to its section. Message is shown in the language of the request.
type FieldError struct {
	Field   string
	Message i18n.Message
}

// ValidationErrors is returned by the race setup when the submitted form is not valid. Nothing has been saved
// or started in that case, so the handler can render the form again with the user's input.
type ValidationErrors []FieldError

func (v ValidationErrors) Error() string {
	messages := make([]string, 0, len(v))

	for _, e := range v {
		messages = append(messages, e.Message.String())
	}

	return "servermanager: invalid race form: " + strings.Join(messages, "; ")
}

// Messages returns the error messages.
func (v ValidationErrors) Messages() []i18n.Message {
	messages := make([]i18n.Message, 0, len(v))

	for _, e := range v {
		messages = append(messages, e.Message)
	}

	return messages
}

// sessionName is the name of a session type as a message (translated with the session_type catalog keys).
func sessionName(sessionType serverconfig.SessionType) i18n.Message {
	return i18n.Message{Key: "session_type." + strings.ToLower(string(sessionType)), English: sessionType.String()}
}

// ValidateCustomRace checks a custom race built from a form. form carries the fields that are not part of the
// race config (name, action).
func ValidateCustomRace(config *serverconfig.CurrentRaceConfig, entryList serverconfig.EntryList, form url.Values) ValidationErrors {
	var errs ValidationErrors

	add := func(field string, message i18n.Message) {
		errs = append(errs, FieldError{Field: field, Message: message})
	}

	if config.Track == "" {
		add("Track", i18n.Msg("validation.race.track", "Choose a track."))
	}

	if config.Cars == "" {
		add("Cars", i18n.Msg("validation.race.cars", "Choose at least one car."))
	}

	if len(config.Sessions) == 0 {
		add("Practice.Enabled", i18n.Msg("validation.race.sessions", "Enable at least one session."))
	}

	for _, sessionType := range serverconfig.AvailableSessions {
		session, ok := config.Sessions[sessionType]

		if !ok {
			continue
		}

		name := sessionType.String()
		label := sessionName(sessionType)

		switch {
		case session.Time < 0:
			add(name+".Time", i18n.Msg("validation.race.session_time_negative", "{session}: the time must not be negative.", "session", label))
		case session.Laps < 0:
			add(name+".Laps", i18n.Msg("validation.race.session_laps_negative", "{session}: the number of laps must not be negative.", "session", label))
		case sessionType == serverconfig.SessionTypeBooking && session.Time == 0:
			add(name+".Time", i18n.Msg("validation.race.booking_time", "{session}: set the booking time.", "session", label))
		case sessionType != serverconfig.SessionTypeBooking && session.Time == 0 && session.Laps == 0:
			add(name+".Time", i18n.Msg("validation.race.session_length", "{session}: set a time or a number of laps.", "session", label))
		}
	}

	if config.MaxClients < 1 {
		add("MaxClients", i18n.Msg("validation.race.max_clients", "The maximum number of clients must be at least 1."))
	}

	if config.RacePitWindowEnd != 0 && config.RacePitWindowEnd < config.RacePitWindowStart {
		add("RacePitWindowEnd", i18n.Msg("validation.race.pit_window", "The pit window must not end before it starts."))
	}

	for _, weather := range config.Weather {
		if weather.WindBaseSpeedMax < weather.WindBaseSpeedMin {
			add("WindBaseSpeedMax", i18n.Msg("validation.race.wind_speed", "Weather {weather}: the maximum wind speed must not be below the minimum.", "weather", weather.Graphics))
		}
	}

	entrants := make([]*serverconfig.Entrant, 0, len(entryList))

	for _, entrant := range entryList {
		entrants = append(entrants, entrant)
	}

	sort.Slice(entrants, func(i, j int) bool { return entrants[i].PitBox < entrants[j].PitBox })

	for _, entrant := range entrants {
		if entrant.Model == "" {
			add("EntryList.Car", i18n.Msg("validation.race.entry_car", "Entry {entry}: choose a car.", "entry", entrant.PitBox+1))
		}
	}

	action := form.Get("action")

	if form.Get("CustomRaceName") == "" && (action == "justSave" || action == "schedule" || form.Get("Editing") != "") {
		add("CustomRaceName", i18n.Msg("validation.race.name", "Give the custom race a name."))
	}

	return errs
}

// ValidateEventSetup checks the race config of a championship event or race weekend session. The cars come from
// the championship or race weekend and the entry list and name are not part of the form, so only the
// track, sessions and server settings are checked. The maximum number of clients is not: the race weekend session form
// has no such field and both kinds of events set it from the entry list when they start.
func ValidateEventSetup(config *serverconfig.CurrentRaceConfig) ValidationErrors {
	var errs ValidationErrors

	for _, e := range ValidateCustomRace(config, nil, url.Values{}) {
		if e.Field != "Cars" && e.Field != "MaxClients" {
			errs = append(errs, e)
		}
	}

	return errs
}

// validateQuickRace checks the fields of the quick race form. The parse errors belong to the numbers next to them.
func validateQuickRace(cars []string, qualifyingTime int64, qualifyingErr error, raceTime int64, raceTimeErr error, raceLaps int64, raceLapsErr error) ValidationErrors {
	var errs ValidationErrors

	if len(cars) == 0 {
		errs = append(errs, FieldError{Field: "Cars", Message: i18n.Msg("validation.race.quick_cars", "You must choose at least one car!")})
	}

	if qualifyingErr != nil || qualifyingTime < 0 {
		errs = append(errs, FieldError{Field: "Qualifying.Time", Message: i18n.Msg("validation.race.quick_qualifying_time", "Enter the qualifying time in minutes (0 or more).")})
	}

	if raceTimeErr != nil || raceTime < 0 {
		errs = append(errs, FieldError{Field: "Race.Time", Message: i18n.Msg("validation.race.quick_race_time", "Enter the race time in minutes (0 or more).")})
	}

	if raceLapsErr != nil || raceLaps < 0 {
		errs = append(errs, FieldError{Field: "Race.Laps", Message: i18n.Msg("validation.race.quick_race_laps", "Enter the number of race laps (0 or more).")})
	}

	return errs
}
