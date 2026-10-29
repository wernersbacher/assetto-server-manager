package servermanager

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smcontent "github.com/wernersbacher/assetto-server-manager/internal/content"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
)

func (rm *RaceManager) ListAutoFillEntrants() ([]*serverconfig.Entrant, error) {
	entrants, err := rm.store.ListEntrants()

	if err != nil {
		return nil, err
	}

	sort.Slice(entrants, func(i, j int) bool {
		return entrants[i].Name < entrants[j].Name
	})

	return entrants, nil
}

type TrackOptsGrouped []smcontent.Track

type TracksSplit struct {
	Stock, DLC, Mod []smcontent.Track
}

func (tog TrackOptsGrouped) Split() TracksSplit {
	var split TracksSplit
	for _, t := range tog {
		if t.IsMod() {
			split.Mod = append(split.Mod, t)
		} else if t.IsPaidDLC() {
			split.DLC = append(split.DLC, t)
		} else {
			split.Stock = append(split.Stock, t)
		}
	}

	return split
}

type RaceTemplateVars struct {
	BaseTemplateVars

	CarOpts             smcontent.Cars
	TrackOpts           TrackOptsGrouped
	AvailableSessions   []serverconfig.SessionType
	Weather             smcontent.Weather
	SolIsInstalled      bool
	Current             serverconfig.CurrentRaceConfig
	CurrentEntrants     serverconfig.EntryList
	PossibleEntrants    []*serverconfig.Entrant
	FixedSetups         smcontent.CarSetups
	IsEditing           bool
	EditingID           string
	CustomRaceName      string
	SurfacePresets      []smcontent.TrackSurfacePreset
	OverridePassword    bool
	ReplacementPassword string
	Tyres               smcontent.Tyres
	DeselectedTyres     map[string]bool

	ForceStopTime        int
	ForceStopWithDrivers bool

	IsChampionship                 bool
	Championship                   *smmodel.Championship
	ChampionshipHasAtLeastOnceRace bool
	ChampionshipEventIndex         int

	// UseChampionshipRaceSettings is the state of the "Use championship race settings" checkbox of the event form.
	UseChampionshipRaceSettings bool

	IsRaceWeekend                   bool
	RaceWeekend                     *smmodel.RaceWeekend
	RaceWeekendSession              *smmodel.RaceWeekendSession
	RaceWeekendHasAtLeastOneSession bool

	ShowOverridePasswordCard bool
}

// BuildRaceOpts builds a quick race form
func (rm *RaceManager) BuildRaceOpts(r *http.Request) (*RaceTemplateVars, error) {
	_, cars, err := rm.carManager.Search(r.Context(), "", 0, 100000)

	if err != nil {
		return nil, err
	}

	tracks, err := rm.trackManager.ListTracks()

	if err != nil {
		return nil, err
	}

	weather, err := smcontent.ListWeather(ServerInstallPath)

	if err != nil {
		return nil, err
	}

	race := serverconfig.ConfigIniDefault()

	templateID := r.URL.Query().Get("from")

	var entrants serverconfig.EntryList

	if templateID != "" {
		// load a from a custom race template
		customRace, err := rm.store.FindCustomRaceByID(templateID)

		if err != nil {
			return nil, err
		}

		race.CurrentRaceConfig = customRace.RaceConfig
		entrants = customRace.EntryList
	}

	templateIDForEditing := chi.URLParam(r, "uuid")
	isEditing := templateIDForEditing != ""
	var customRaceName, replacementPassword string
	var overridePassword, forceStopWithDrivers bool
	var forceStopTime int

	if isEditing {
		customRace, err := rm.store.FindCustomRaceByID(templateIDForEditing)

		if err != nil {
			return nil, err
		}

		customRaceName = customRace.Name
		race.CurrentRaceConfig = customRace.RaceConfig
		entrants = customRace.EntryList
		overridePassword = customRace.OverrideServerPassword()
		replacementPassword = customRace.ReplacementServerPassword()

		forceStopTime = customRace.ForceStopTime
		forceStopWithDrivers = customRace.ForceStopWithDrivers
	}

	possibleEntrants, err := rm.ListAutoFillEntrants()

	if err != nil {
		return nil, err
	}

	fixedSetups, err := smcontent.ListAllSetups(ServerInstallPath)

	if err != nil {
		return nil, err
	}

	solIsInstalled := false

	for availableWeather := range weather {
		if strings.HasPrefix(availableWeather, "sol_") {
			solIsInstalled = true
			break
		}
	}

	// default sol time to now
	for _, weather := range race.CurrentRaceConfig.Weather {
		if weather.CMWFXDate <= 0 {
			weather.CMWFXDate = int(time.Now().Unix())
			weather.CMWFXDateUnModified = int(time.Now().Unix())
		}
	}

	opts := &RaceTemplateVars{
		CarOpts:                  cars,
		TrackOpts:                tracks,
		AvailableSessions:        serverconfig.AvailableSessions,
		Weather:                  weather,
		SolIsInstalled:           solIsInstalled,
		Current:                  race.CurrentRaceConfig,
		CurrentEntrants:          entrants,
		PossibleEntrants:         possibleEntrants,
		FixedSetups:              fixedSetups,
		IsChampionship:           false, // this flag is overridden by championship setup
		IsRaceWeekend:            false, // this flag is overridden by race weekend setup
		IsEditing:                isEditing,
		EditingID:                templateIDForEditing,
		CustomRaceName:           customRaceName,
		SurfacePresets:           smcontent.DefaultTrackSurfacePresets,
		OverridePassword:         overridePassword,
		ReplacementPassword:      replacementPassword,
		ShowOverridePasswordCard: true,
		ForceStopTime:            forceStopTime,
		ForceStopWithDrivers:     forceStopWithDrivers,
	}

	err = rm.applyCurrentRaceSetupToOptions(opts, race.CurrentRaceConfig)

	if err != nil {
		return nil, err
	}

	return opts, nil
}

func (rm *RaceManager) SaveEntrantsForAutoFill(entryList serverconfig.EntryList) error {
	for _, entrant := range entryList {
		if entrant.Name == "" || entrant.GUID == "" {
			continue // only save entrants that have a name
		}

		err := rm.store.UpsertEntrant(*entrant)

		if err != nil {
			return err
		}
	}

	return nil
}
