package race

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smcontent "github.com/wernersbacher/ACPitwall/internal/content"
	"github.com/wernersbacher/ACPitwall/internal/i18n"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/plainname"
)

// maxCustomRaceNameLength limits the name of an imported custom race.
const maxCustomRaceNameLength = 200

// ImportError is returned by ImportCustomRace if the JSON is not a usable custom race. Nothing has been saved then.
type ImportError struct {
	Message i18n.Message
}

func (e ImportError) Error() string {
	return "servermanager: invalid custom race import: " + e.Message.String()
}

// ExportedCustomRace returns the part of a custom race that an export carries: the setup (name, config, entry
// list, password, force stop). What belongs to this server only is dropped: the schedule, loop settings and star, the
// time attack result file and the dates.
func ExportedCustomRace(race *smmodel.CustomRace) *smmodel.CustomRace {
	return &smmodel.CustomRace{
		Name:                 race.Name,
		HasCustomName:        race.HasCustomName,
		OverridePassword:     race.OverridePassword,
		ReplacementPassword:  race.ReplacementPassword,
		ForceStopTime:        race.ForceStopTime,
		ForceStopWithDrivers: race.ForceStopWithDrivers,
		RaceConfig:           race.RaceConfig,
		EntryList:            race.EntryList,
		UUID:                 race.UUID,
	}
}

// ExportCustomRace loads a custom race and returns it for export (see ExportedCustomRace). Without includePassword
// the replacement password is removed.
func (rm *RaceManager) ExportCustomRace(id string, includePassword bool) (*smmodel.CustomRace, error) {
	race, err := rm.store.FindCustomRaceByID(id)

	if err != nil {
		return nil, err
	}

	export := ExportedCustomRace(race)

	if !includePassword {
		export.ReplacementPassword = ""
		export.OverridePassword = false
	}

	return export, nil
}

// ImportCustomRace saves the custom race of an export as a new custom race. Only the setup is taken over: the
// import always gets a new UUID (the ID of the export is ignored, so importing can neither overwrite nor collide with
// an existing race) and is not starred, looped or scheduled. The data is validated like a saved form before anything is
// written. It returns ImportError (or ValidationErrors) for unusable data.
func (rm *RaceManager) ImportCustomRace(data string) (*smmodel.CustomRace, error) {
	var imported *smmodel.CustomRace

	if err := json.Unmarshal([]byte(data), &imported); err != nil {
		return nil, err
	}

	if imported == nil {
		return nil, ImportError{i18n.Msg("validation.race.import_empty", "The import does not contain a custom race.")}
	}

	race := &smmodel.CustomRace{
		Name:                 strings.TrimSpace(imported.Name),
		HasCustomName:        imported.HasCustomName,
		OverridePassword:     imported.OverridePassword,
		ReplacementPassword:  imported.ReplacementPassword,
		ForceStopTime:        imported.ForceStopTime,
		ForceStopWithDrivers: imported.ForceStopWithDrivers,
		RaceConfig:           imported.RaceConfig,
		EntryList:            imported.EntryList,
	}

	if err := checkImportedCustomRace(race); err != nil {
		return nil, err
	}

	if errs := ValidateCustomRace(&race.RaceConfig, race.EntryList, url.Values{}); len(errs) > 0 {
		return nil, errs
	}

	if race.Name == "" {
		race.Name = defaultCustomRaceName(race.RaceConfig, race.EntryList)
		race.HasCustomName = false
	} else {
		race.HasCustomName = true
	}

	now := time.Now()
	race.UUID = uuid.New()
	race.Created = now
	race.Updated = now

	if err := rm.store.UpsertCustomRace(race); err != nil {
		return nil, err
	}

	return race, nil
}

// checkImportedCustomRace rejects what could make later code fail or reach outside the content directories: empty
// session or entrant entries, names that end up in paths, too long names and negative numbers.
func checkImportedCustomRace(race *smmodel.CustomRace) error {
	invalid := func(key, english string, args ...interface{}) error {
		return ImportError{i18n.Msg(key, english, args...)}
	}

	if len(race.Name) > maxCustomRaceNameLength {
		return invalid("validation.race.import_name", "The name of the custom race is too long (at most {max} characters).", "max", maxCustomRaceNameLength)
	}

	if race.ForceStopTime < 0 {
		return invalid("validation.race.import_force_stop", "The force stop time must not be negative.")
	}

	for sessionType, session := range race.RaceConfig.Sessions {
		if session == nil {
			return invalid("validation.race.import_session", "The session {session} is empty.", "session", sessionType.String())
		}
	}

	if len(race.EntryList) > serverconfig.MaxEntryListSize {
		return invalid("validation.race.import_entry_list", "The entry list has more than {max} entries.", "max", serverconfig.MaxEntryListSize)
	}

	for key, entrant := range race.EntryList {
		if entrant == nil {
			return invalid("validation.race.import_entrant", "The entry {entry} is empty.", "entry", key)
		}
	}

	if race.RaceConfig.Track != "" && !plainname.Valid(race.RaceConfig.Track) {
		return invalid("validation.race.import_name_invalid", "{name} is not a valid track, layout, car, skin or setup name.", "name", race.RaceConfig.Track)
	}

	if race.RaceConfig.TrackLayout != "" && !plainname.Valid(race.RaceConfig.TrackLayout) {
		return invalid("validation.race.import_name_invalid", "{name} is not a valid track, layout, car, skin or setup name.", "name", race.RaceConfig.TrackLayout)
	}

	for _, car := range strings.Split(race.RaceConfig.Cars, ";") {
		if car != "" && !plainname.Valid(car) {
			return invalid("validation.race.import_name_invalid", "{name} is not a valid track, layout, car, skin or setup name.", "name", car)
		}
	}

	for _, entrant := range race.EntryList {
		for _, name := range []string{entrant.Model, entrant.Skin} {
			if name != "" && !plainname.Valid(name) {
				return invalid("validation.race.import_name_invalid", "{name} is not a valid track, layout, car, skin or setup name.", "name", name)
			}
		}

		// a fixed setup may be in a sub directory, but must stay in the setups directory
		if setup := entrant.FixedSetup; setup != "" {
			if filepath.IsAbs(setup) || strings.HasPrefix(setup, "/") || strings.HasPrefix(setup, "\\") || strings.Contains(setup, ":") {
				return invalid("validation.race.import_name_invalid", "{name} is not a valid track, layout, car, skin or setup name.", "name", setup)
			}

			for _, part := range strings.FieldsFunc(setup, func(r rune) bool { return r == '/' || r == rune(92) }) {
				if part == ".." {
					return invalid("validation.race.import_name_invalid", "{name} is not a valid track, layout, car, skin or setup name.", "name", setup)
				}
			}
		}
	}

	return nil
}

// defaultCustomRaceName is the generated name of a custom race without a name of its own.
func defaultCustomRaceName(config serverconfig.CurrentRaceConfig, entryList serverconfig.EntryList) string {
	var trackLayout string

	if config.TrackLayout != "" {
		trackLayout = smcontent.PrettifyName(config.TrackLayout, true)
	}

	return fmt.Sprintf("%s (%s) in %s (%d entrants)",
		smcontent.PrettifyName(config.Track, false),
		trackLayout,
		smcontent.CarList(config.Cars),
		len(entryList),
	)
}
