package race

import (
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"4d63.com/tz"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smcontent "github.com/wernersbacher/ACPitwall/internal/content"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

// errInvalidSolDate is returned when the Sol start date of the weather cannot be parsed.
var errInvalidSolDate = errors.New("servermanager: invalid sol date")

func (rm *RaceManager) SetupQuickRace(r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		return err
	}

	// load default config values
	quickRace := serverconfig.ConfigIniDefault().CurrentRaceConfig

	cars := r.Form["Cars"]

	quickRace.Cars = strings.Join(cars, ";")
	quickRace.Track = r.Form.Get("Track")
	quickRace.TrackLayout = r.Form.Get("TrackLayout")

	if quickRace.TrackLayout == smcontent.DefaultLayoutName {
		quickRace.TrackLayout = ""
	}

	tyres, err := smcontent.ListTyres(rm.env.InstallPath())

	if err != nil {
		return err
	}

	quickRaceTyresMap := make(map[string]bool)

	for _, car := range cars {
		if available, ok := tyres[car]; ok {
			for tyre := range available {
				quickRaceTyresMap[tyre] = true
			}
		}
	}

	var quickRaceTyres []string

	for tyre := range quickRaceTyresMap {
		quickRaceTyres = append(quickRaceTyres, tyre)
	}

	quickRace.LegalTyres = strings.Join(quickRaceTyres, ";")

	quickRace.Sessions = make(map[serverconfig.SessionType]*serverconfig.SessionConfig)

	qualifyingTime, qualifyingErr := strconv.ParseInt(r.Form.Get("Qualifying.Time"), 10, 0)
	raceTime, raceTimeErr := strconv.ParseInt(r.Form.Get("Race.Time"), 10, 0)
	raceLaps, raceLapsErr := strconv.ParseInt(r.Form.Get("Race.Laps"), 10, 0)

	if errs := validateQuickRace(cars, qualifyingTime, qualifyingErr, raceTime, raceTimeErr, raceLaps, raceLapsErr); len(errs) > 0 {
		return errs
	}

	quickRace.AddSession(serverconfig.SessionTypeQualifying, &serverconfig.SessionConfig{
		Name:   "Qualify",
		Time:   int(qualifyingTime),
		IsOpen: 1,
	})

	quickRace.AddSession(serverconfig.SessionTypeRace, &serverconfig.SessionConfig{
		Name:     "Race",
		Time:     int(raceTime),
		Laps:     int(raceLaps),
		IsOpen:   1,
		WaitTime: 60,
	})

	entryList := serverconfig.EntryList{}

	var numPitboxes int

	trackInfo, err := smcontent.GetTrackInfo(rm.env.InstallPath(), quickRace.Track, quickRace.TrackLayout)

	if err == nil {
		boxes, err := trackInfo.Pitboxes.Int64()

		if err != nil {
			numPitboxes = quickRace.MaxClients
		} else {
			numPitboxes = int(boxes)
		}

	} else {
		numPitboxes = quickRace.MaxClients
	}

	allCars, err := rm.carManager.ListCars()

	if err != nil {
		return err
	}

	carMap := allCars.AsMap()

	for i := 0; i < numPitboxes; i++ {
		model := cars[i%len(cars)]

		var skin string

		if skins, ok := carMap[model]; ok && len(skins) > 0 {
			skin = carMap[model][rand.Intn(len(carMap[model]))]
		}

		e := serverconfig.NewEntrant()
		e.PitBox = i
		e.Model = model
		e.Skin = skin

		entryList.AddToBackOfGrid(e)
	}

	quickRace.MaxClients = numPitboxes

	return rm.ApplyConfigAndStart(&smmodel.QuickRace{
		RaceConfig: quickRace,
		EntryList:  entryList,
	})
}

func (rm *RaceManager) BuildEntryList(r *http.Request, start, length int) (serverconfig.EntryList, error) {
	entryList := serverconfig.EntryList{}

	allCars, err := rm.carManager.ListCars()

	if err != nil {
		return nil, err
	}

	carMap := allCars.AsMap()

	for i := start; i < start+length; i++ {
		model := r.Form["EntryList.Car"][i]
		skin := r.Form["EntryList.Skin"][i]

		if skin == "random_skin" {
			if skins, ok := carMap[model]; ok && len(skins) > 0 {
				skin = carMap[model][rand.Intn(len(carMap[model]))]
			}
		}

		e := serverconfig.NewEntrant()

		if r.Form["EntryList.InternalUUID"][i] != "" || r.Form["EntryList.InternalUUID"][i] != uuid.Nil.String() {
			internalUUID, err := uuid.Parse(r.Form["EntryList.InternalUUID"][i])

			if err == nil {
				e.InternalUUID = internalUUID
			}
		}

		e.Name = r.Form["EntryList.Name"][i]
		e.Team = r.Form["EntryList.Team"][i]

		e.GUID = serverconfig.NormaliseEntrantGUID(r.Form["EntryList.GUID"][i])
		e.Model = model
		e.Skin = skin
		// Despite having the option for SpectatorMode, the server does not support it, and panics if set to 1
		// SpectatorMode: smweb.FormValueAsInt(r.Form["EntryList.Spectator"][i]),
		e.Ballast = smweb.FormValueAsInt(r.Form["EntryList.Ballast"][i])
		e.Restrictor = smweb.FormValueAsInt(r.Form["EntryList.Restrictor"][i])
		e.FixedSetup = r.Form["EntryList.FixedSetup"][i]

		// The pit box/grid starting position
		if entrantIDs, ok := r.Form["EntryList.EntrantID"]; ok && i < len(entrantIDs) {
			e.PitBox = smweb.FormValueAsInt(entrantIDs[i])
		} else {
			e.PitBox = i
		}

		if r.Form["EntryList.TransferTeamPoints"] != nil && i < len(r.Form["EntryList.TransferTeamPoints"]) && smweb.FormValueAsInt(r.Form["EntryList.TransferTeamPoints"][i]) == 1 {
			e.TransferTeamPoints = true
		}

		if r.Form["EntryList.OverwriteAllEvents"] != nil && i < len(r.Form["EntryList.OverwriteAllEvents"]) && smweb.FormValueAsInt(r.Form["EntryList.OverwriteAllEvents"][i]) == 1 {
			e.OverwriteAllEvents = true
		}

		entryList.AddInPitBox(e, e.PitBox)
	}

	return entryList, nil
}

func (rm *RaceManager) BuildCustomRaceFromForm(r *http.Request) (*serverconfig.CurrentRaceConfig, error) {
	cars := r.Form["Cars"]
	isSol := r.FormValue("Sol.Enabled") == "1"

	gasPenaltyDisabled := smweb.FormValueAsInt(r.FormValue("RaceGasPenaltyDisabled"))
	lockedEntryList := smweb.FormValueAsInt(r.FormValue("LockedEntryList"))
	pickupModeEnabled := smweb.FormValueAsInt(r.FormValue("PickupModeEnabled"))

	if gasPenaltyDisabled == 0 {
		gasPenaltyDisabled = 1
	} else {
		gasPenaltyDisabled = 0
	}

	timeAttack := smweb.FormValueAsInt(r.FormValue("TimeAttack")) == 1
	loopMode := smweb.FormValueAsInt(r.FormValue("LoopMode"))

	if timeAttack {
		loopMode = 1
	}

	trackLayout := r.FormValue("TrackLayout")

	if trackLayout == "<default>" {
		trackLayout = ""
	}

	legalTyres := strings.Join(r.Form["LegalTyres"], ";")
	legalTyresUnescaped, err := url.PathUnescape(legalTyres)

	if err != nil {
		logrus.WithError(err).Errorf("Couldn't unescape legal Tyres list, there may be an issue with the name of a tyre: %s", legalTyres)
	} else {
		legalTyres = legalTyresUnescaped
	}

	raceConfig := &serverconfig.CurrentRaceConfig{
		// general race config
		Cars:        strings.Join(cars, ";"),
		Track:       r.FormValue("Track"),
		TrackLayout: trackLayout,

		// assists
		ABSAllowed:              serverconfig.FactoryAssist(smweb.FormValueAsInt(r.FormValue("ABSAllowed"))),
		TractionControlAllowed:  serverconfig.FactoryAssist(smweb.FormValueAsInt(r.FormValue("TractionControlAllowed"))),
		StabilityControlAllowed: smweb.FormValueAsInt(r.FormValue("StabilityControlAllowed")),
		AutoClutchAllowed:       smweb.FormValueAsInt(r.FormValue("AutoClutchAllowed")),
		TyreBlanketsAllowed:     smweb.FormValueAsInt(r.FormValue("TyreBlanketsAllowed")),

		// weather
		IsSol:    smweb.FormValueAsInt(r.FormValue("Sol.Enabled")),
		SunAngle: smweb.FormValueAsInt(r.FormValue("SunAngle")),

		// realism
		LegalTyres:          legalTyres,
		FuelRate:            smweb.FormValueAsInt(r.FormValue("FuelRate")),
		DamageMultiplier:    smweb.FormValueAsInt(r.FormValue("DamageMultiplier")),
		TyreWearRate:        smweb.FormValueAsInt(r.FormValue("TyreWearRate")),
		ForceVirtualMirror:  smweb.FormValueAsInt(r.FormValue("ForceVirtualMirror")),
		TimeOfDayMultiplier: smweb.FormValueAsInt(r.FormValue("TimeOfDayMultiplier")),

		DynamicTrack: serverconfig.DynamicTrackConfig{
			SessionStart:    smweb.FormValueAsInt(r.FormValue("SessionStart")),
			Randomness:      smweb.FormValueAsInt(r.FormValue("Randomness")),
			SessionTransfer: smweb.FormValueAsInt(r.FormValue("SessionTransfer")),
			LapGain:         smweb.FormValueAsInt(r.FormValue("LapGain")),
		},

		// rules
		PickupModeEnabled:         pickupModeEnabled,
		LockedEntryList:           lockedEntryList,
		RacePitWindowStart:        smweb.FormValueAsInt(r.FormValue("RacePitWindowStart")),
		RacePitWindowEnd:          smweb.FormValueAsInt(r.FormValue("RacePitWindowEnd")),
		ReversedGridRacePositions: smweb.FormValueAsInt(r.FormValue("ReversedGridRacePositions")),
		QualifyMaxWaitPercentage:  smweb.FormValueAsInt(r.FormValue("QualifyMaxWaitPercentage")),
		RaceGasPenaltyDisabled:    gasPenaltyDisabled,
		MaxBallastKilograms:       smweb.FormValueAsInt(r.FormValue("MaxBallastKilograms")),
		AllowedTyresOut:           smweb.FormValueAsInt(r.FormValue("AllowedTyresOut")),
		LoopMode:                  loopMode,
		RaceOverTime:              smweb.FormValueAsInt(r.FormValue("RaceOverTime")),
		StartRule:                 serverconfig.StartRule(smweb.FormValueAsInt(r.FormValue("StartRule"))),
		MaxClients:                smweb.FormValueAsInt(r.FormValue("MaxClients")),
		RaceExtraLap:              smweb.FormValueAsInt(r.FormValue("RaceExtraLap")),
		MaxContactsPerKilometer:   smweb.FormValueAsInt(r.FormValue("MaxContactsPerKilometer")),
		ResultScreenTime:          smweb.FormValueAsInt(r.FormValue("ResultScreenTime")),
		DisableDRSZones:           smweb.FormValueAsInt(r.FormValue("DisableDRSZones")) == 1,

		TimeAttack: timeAttack,

		DriverSwapEnabled:               smweb.FormValueAsInt(r.FormValue("DriverSwapEnabled")),
		DriverSwapMinTime:               smweb.FormValueAsInt(r.FormValue("DriverSwapMinTime")),
		DriverSwapDisqualifyTime:        smweb.FormValueAsInt(r.FormValue("DriverSwapDisqualifyTime")),
		DriverSwapPenaltyTime:           smweb.FormValueAsInt(r.FormValue("DriverSwapPenaltyTime")),
		DriverSwapMinimumNumberOfSwaps:  smweb.FormValueAsInt(r.FormValue("DriverSwapMinimumNumberOfSwaps")),
		DriverSwapNotEnoughSwapsPenalty: smweb.FormValueAsInt(r.FormValue("DriverSwapNotEnoughSwapsPenalty")),
	}

	if isSol {
		raceConfig.SunAngle = 0
		raceConfig.TimeOfDayMultiplier = 0
	}

	for _, session := range serverconfig.AvailableSessions {
		sessName := session.String()

		disabled := r.FormValue(sessName+".Enabled") != "1"

		if timeAttack {
			if sessName != serverconfig.SessionTypePractice.String() {
				continue
			} else {
				disabled = false
			}
		}

		if disabled {
			continue
		}

		raceConfig.AddSession(session, &serverconfig.SessionConfig{
			Name:     r.FormValue(sessName + ".Name"),
			Time:     smweb.FormValueAsInt(r.FormValue(sessName + ".Time")),
			Laps:     smweb.FormValueAsInt(r.FormValue(sessName + ".Laps")),
			IsOpen:   serverconfig.SessionOpenness(smweb.FormValueAsInt(r.FormValue(sessName + ".IsOpen"))),
			WaitTime: smweb.FormValueAsInt(r.FormValue(sessName + ".WaitTime")),
		})
	}

	// weather
	for i := 0; i < len(r.Form["Graphics"]); i++ {
		weatherName := r.Form["Graphics"][i]

		WFXType, err := smcontent.GetWeatherType(rm.env.InstallPath(), weatherName)

		// if WFXType can't be found due to an error, default to non-sol weather.
		if !isSol || err != nil {
			raceConfig.AddWeather(&serverconfig.WeatherConfig{
				Graphics:               weatherName,
				BaseTemperatureAmbient: smweb.FormValueAsInt(r.Form["BaseTemperatureAmbient"][i]),
				BaseTemperatureRoad:    smweb.FormValueAsInt(r.Form["BaseTemperatureRoad"][i]),
				VariationAmbient:       smweb.FormValueAsInt(r.Form["VariationAmbient"][i]),
				VariationRoad:          smweb.FormValueAsInt(r.Form["VariationRoad"][i]),
				WindBaseSpeedMin:       smweb.FormValueAsInt(r.Form["WindBaseSpeedMin"][i]),
				WindBaseSpeedMax:       smweb.FormValueAsInt(r.Form["WindBaseSpeedMax"][i]),
				WindBaseDirection:      smweb.FormValueAsInt(r.Form["WindBaseDirection"][i]),
				WindVariationDirection: smweb.FormValueAsInt(r.Form["WindVariationDirection"][i]),

				ChampionshipPracticeWeather: r.Form["ChampionshipPracticeWeather"][i],
			})
		} else {
			startTime, err := time.ParseInLocation("2006-01-02T15:04", r.Form["DateUnix"][i], time.UTC)

			if err != nil {
				return nil, fmt.Errorf("%w: %v", errInvalidSolDate, err)
			}

			timeMulti := r.Form["TimeMulti"][i]
			timeMultiInt := smweb.FormValueAsInt(timeMulti)

			// This is probably a bit hacky, and may need removing with a future Sol update
			startTimeFinal := startTime.Add(-(time.Duration(timeMultiInt) * 5 * time.Hour))

			unixTime := time.Unix(0, 0)

			if startTimeFinal.Before(unixTime) {
				startTimeFinal = unixTime
			}

			raceConfig.AddWeather(&serverconfig.WeatherConfig{
				Graphics: weatherName + "_type=" + strconv.Itoa(WFXType) + "_time=0_mult=" +
					timeMulti + "_start=" + strconv.Itoa(int(startTimeFinal.Unix())),
				BaseTemperatureAmbient: smweb.FormValueAsInt(r.Form["BaseTemperatureAmbient"][i]),
				BaseTemperatureRoad:    smweb.FormValueAsInt(r.Form["BaseTemperatureRoad"][i]),
				VariationAmbient:       smweb.FormValueAsInt(r.Form["VariationAmbient"][i]),
				VariationRoad:          smweb.FormValueAsInt(r.Form["VariationRoad"][i]),
				WindBaseSpeedMin:       smweb.FormValueAsInt(r.Form["WindBaseSpeedMin"][i]),
				WindBaseSpeedMax:       smweb.FormValueAsInt(r.Form["WindBaseSpeedMax"][i]),
				WindBaseDirection:      smweb.FormValueAsInt(r.Form["WindBaseDirection"][i]),
				WindVariationDirection: smweb.FormValueAsInt(r.Form["WindVariationDirection"][i]),

				ChampionshipPracticeWeather: r.Form["ChampionshipPracticeWeather"][i],

				CMGraphics:          weatherName,
				CMWFXType:           WFXType,
				CMWFXUseCustomTime:  1,
				CMWFXTime:           0,
				CMWFXTimeMulti:      timeMultiInt,
				CMWFXUseCustomDate:  1,
				CMWFXDate:           int(startTimeFinal.Unix()),
				CMWFXDateUnModified: int(startTime.Unix()),
			})
		}
	}

	return raceConfig, nil
}

// buildCustomRaceAndEntryList builds the config and entry list of a submitted custom race form and validates
// them. Problems with the input are returned as ValidationErrors.
func (rm *RaceManager) buildCustomRaceAndEntryList(r *http.Request) (*serverconfig.CurrentRaceConfig, serverconfig.EntryList, error) {
	raceConfig, err := rm.BuildCustomRaceFromForm(r)

	if err != nil {
		if errors.Is(err, errInvalidSolDate) {
			return nil, nil, ValidationErrors{{Field: "DateUnix", Message: "Enter a valid date and time for the Sol weather."}}
		}

		return nil, nil, err
	}

	var entryList serverconfig.EntryList

	if !raceConfig.HasSession(serverconfig.SessionTypeBooking) {
		entryList, err = rm.BuildEntryList(r, 0, len(r.Form["EntryList.Name"]))

		if err != nil {
			return nil, nil, err
		}
	}

	if errs := ValidateCustomRace(raceConfig, entryList, r.Form); len(errs) > 0 {
		return nil, nil, errs
	}

	return raceConfig, entryList, nil
}

// parseScheduleDate parses the date, time and time zone fields of the schedule form. An unknown time zone
// falls back to the local one.
func parseScheduleDate(dateString, timeString, timezone string) (time.Time, error) {
	location, err := tz.LoadLocation(timezone)

	if err != nil {
		logrus.WithError(err).Errorf("could not find location: %s", timezone)
		location = time.Local
	}

	return time.ParseInLocation("2006-01-02-15:04", dateString+"-"+timeString, location)
}

func (rm *RaceManager) SetupCustomRace(r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		return err
	}

	raceConfig, entryList, err := rm.buildCustomRaceAndEntryList(r)

	if err != nil {
		return err
	}

	if err := rm.SaveEntrantsForAutoFill(entryList); err != nil {
		return err
	}

	completeConfig := serverconfig.ConfigIniDefault()
	completeConfig.CurrentRaceConfig = *raceConfig

	overridePassword := r.FormValue("OverridePassword") == "1"
	replacementPassword := r.FormValue("ReplacementPassword")

	forceStopTime := smweb.FormValueAsInt(r.FormValue("ForceStopTime"))
	forceStopWithDrivers := r.FormValue("ForceStopWithDrivers") == "1"

	if customRaceID := r.FormValue("Editing"); customRaceID != "" {
		// we are editing the race. load the previous one and overwrite it with this one
		customRace, err := rm.store.FindCustomRaceByID(customRaceID)

		if err != nil {
			return err
		}

		customRace.OverridePassword = overridePassword
		customRace.ReplacementPassword = replacementPassword

		customRace.ForceStopTime = forceStopTime
		customRace.ForceStopWithDrivers = forceStopWithDrivers

		customRace.Name = r.FormValue("CustomRaceName")
		customRace.EntryList = entryList
		customRace.RaceConfig = *raceConfig

		return rm.store.UpsertCustomRace(customRace)
	}

	saveAsPresetWithoutStartingRace := r.FormValue("action") == "justSave"
	schedule := r.FormValue("action") == "schedule"

	// the date is parsed before anything is saved, so a bad date does not leave a half created race behind
	var scheduleDate time.Time

	if schedule {
		scheduleDate, err = parseScheduleDate(r.FormValue("CustomRaceScheduled"), r.FormValue("CustomRaceScheduledTime"), r.FormValue("CustomRaceScheduledTimezone"))

		if err != nil {
			return ValidationErrors{{Field: "CustomRaceScheduled", Message: "Enter a valid date and time for the scheduled race."}}
		}
	}

	// save the custom race preset
	race, err := rm.SaveCustomRace(r.FormValue("CustomRaceName"), overridePassword, replacementPassword, *raceConfig, entryList, saveAsPresetWithoutStartingRace, forceStopTime, forceStopWithDrivers)

	if err != nil {
		return err
	}

	if schedule {
		err = rm.ScheduleRace(race.UUID.String(), scheduleDate, "add", r.FormValue("event-schedule-recurrence"))

		if err != nil {
			return err
		}

		return nil
	}

	if race.RaceConfig.TimeAttack {
		logrus.Info("Time Attack event started")
		rm.raceControl.SetTimeAttackEvent(race)
	}

	if saveAsPresetWithoutStartingRace {
		return nil
	}

	return rm.ApplyConfigAndStart(race)
}

// ApplyCurrentRaceSetupToOptions takes current values in race which require more detailed configuration
// and applies them to the template opts.
func (rm *RaceManager) ApplyCurrentRaceSetupToOptions(opts *RaceTemplateVars, race serverconfig.CurrentRaceConfig) error {
	tyres, err := smcontent.ListTyres(rm.env.InstallPath())

	if err != nil {
		return err
	}

	deselectedTyres := make(map[string]bool)

	for _, car := range strings.Split(race.Cars, ";") {
		tyresForCar, ok := tyres[car]

		if !ok {
			continue
		}

		for carTyre := range tyresForCar {
			found := false

			for _, t := range strings.Split(race.LegalTyres, ";") {
				if carTyre == t {
					found = true
					break
				}
			}

			if !found {
				deselectedTyres[carTyre] = true
			}
		}
	}

	opts.Tyres = tyres
	opts.DeselectedTyres = deselectedTyres

	return nil
}
