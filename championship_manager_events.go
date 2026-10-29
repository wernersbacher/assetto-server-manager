package servermanager

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi"
	"github.com/sirupsen/logrus"
	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smlua "github.com/wernersbacher/assetto-server-manager/internal/lua"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
)

func (cm *ChampionshipManager) BuildChampionshipEventOpts(r *http.Request) (*RaceTemplateVars, error) {
	opts, err := cm.BuildRaceOpts(r)

	if err != nil {
		return nil, err
	}

	// here we customise the opts to tell the template that this is a championship race.
	championship, err := cm.LoadChampionship(chi.URLParam(r, "championshipID"))

	if err != nil {
		return nil, err
	}

	opts.IsChampionship = true
	opts.Championship = championship

	if editEventID := chi.URLParam(r, "eventID"); editEventID != "" {
		// editing a championship event
		event, eventIndex, err := championship.EventByID(editEventID)

		if err != nil {
			return nil, err
		}

		opts.Current = event.RaceSetup
		opts.UseChampionshipRaceSettings = event.UseChampionshipRaceSettings
		opts.IsEditing = true
		opts.EditingID = editEventID
		opts.CurrentEntrants = event.CombineEntryLists(championship)
		opts.ChampionshipEventIndex = eventIndex
	} else {
		// creating a new championship event
		opts.IsEditing = false
		opts.UseChampionshipRaceSettings = championship.HasRaceSettings()
		opts.CurrentEntrants = championship.AllEntrants()

		// override Current race config if there is a previous championship race configured
		if len(championship.Events) > 0 {
			foundEvent := false

			// championship events should only inherit from non-race weekend events
			for i := len(championship.Events) - 1; i >= 0; i-- {
				event := championship.Events[i]

				if !event.IsRaceWeekend() {
					opts.Current = event.RaceSetup
					foundEvent = true
					break
				}
			}

			if !foundEvent {
				defaultConfig := serverconfig.ConfigIniDefault()
				opts.Current = defaultConfig.CurrentRaceConfig
			}

			opts.ChampionshipHasAtLeastOnceRace = true
		} else {
			defaultConfig := serverconfig.ConfigIniDefault()

			opts.Current = defaultConfig.CurrentRaceConfig
			opts.ChampionshipHasAtLeastOnceRace = false
		}
	}

	if !championship.OpenEntrants {
		opts.AvailableSessions = serverconfig.AvailableSessionsNoBooking
	}

	opts.ShowOverridePasswordCard = false

	err = cm.applyCurrentRaceSetupToOptions(opts, opts.Current)

	if err != nil {
		return nil, err
	}

	return opts, nil
}

func (cm *ChampionshipManager) SaveChampionshipEvent(r *http.Request) (championship *smmodel.Championship, event *smmodel.ChampionshipEvent, edited bool, err error) {
	if err := r.ParseForm(); err != nil {
		return nil, nil, false, err
	}

	championship, err = cm.LoadChampionship(chi.URLParam(r, "championshipID"))

	if err != nil {
		return nil, nil, false, err
	}

	raceConfig, err := cm.BuildCustomRaceFromForm(r)

	if err != nil {
		return nil, nil, false, err
	}

	raceConfig.Cars = strings.Join(championship.ValidCarIDs(), ";")

	entryList, err := cm.BuildEntryList(r, 0, len(r.Form["EntryList.Name"]))

	if err != nil {
		return nil, nil, false, err
	}

	if eventID := r.FormValue("Editing"); eventID != "" {
		edited = true

		event, _, err = championship.EventByID(eventID)

		if err != nil {
			return nil, nil, true, err
		}

		// we're editing an existing event
		event.RaceSetup = *raceConfig
		event.EntryList = entryList
		event.UseChampionshipRaceSettings = r.FormValue("UseChampionshipRaceSettings") == "1"
	} else {
		// creating a new event
		event = smmodel.NewChampionshipEvent()
		event.RaceSetup = *raceConfig
		event.EntryList = entryList
		event.UseChampionshipRaceSettings = r.FormValue("UseChampionshipRaceSettings") == "1"

		championship.Events = append(championship.Events, event)
	}

	return championship, event, edited, cm.UpsertChampionship(championship)
}

func (cm *ChampionshipManager) DeleteEvent(championshipID string, eventID string) error {
	championship, err := cm.LoadChampionship(championshipID)

	if err != nil {
		return err
	}

	toDelete := -1

	for i, event := range championship.Events {
		if event.ID.String() == eventID {
			toDelete = i

			if event.IsRaceWeekend() {
				if err := cm.store.DeleteRaceWeekend(event.RaceWeekendID.String()); err != nil {
					return err
				}
			}

			if !event.Scheduled.IsZero() {
				if err := cm.ScheduleEvent(championshipID, eventID, time.Time{}, "", ""); err != nil {
					return err
				}
			}

			break
		}
	}

	if toDelete < 0 {
		return smmodel.ErrInvalidChampionshipEvent
	}

	championship.Events = append(championship.Events[:toDelete], championship.Events[toDelete+1:]...)

	return cm.UpsertChampionship(championship)
}

// Start a 2hr long Practice Event based off the existing championship event with eventID
func (cm *ChampionshipManager) StartPracticeEvent(championshipID string, eventID string) error {
	return cm.StartEvent(championshipID, eventID, true)
}

func (cm *ChampionshipManager) FinalEventConfigurationFiles(championship *smmodel.Championship, event *smmodel.ChampionshipEvent, isPreChampionshipPracticeEvent bool) (serverconfig.CurrentRaceConfig, serverconfig.EntryList) {
	// the event's stored config is never modified: championship wide settings are only applied to this copy.
	raceSetup := championship.EffectiveRaceSetup(event)

	raceSetup.Cars = strings.Join(championship.ValidCarIDs(), ";")

	entryList := event.CombineEntryLists(championship)

	if championship.HasSpectatorCar() {
		entryList.AddInPitBox(&championship.SpectatorCar, serverconfig.MaxEntryListSize+1)
	}

	if championship.SignUpForm.Enabled && !championship.OpenEntrants && !isPreChampionshipPracticeEvent {
		filteredEntryList := make(serverconfig.EntryList)

		// a sign up championship (which is not open) should remove all empty entrants before the event starts
		// here we are building a new filtered entry list so grid positions are not 'missing'.
		for pitBox, entrant := range entryList.AsSlice() {
			if entrant.GUID != "" {
				filteredEntryList.AddInPitBox(entrant, pitBox)
			}
		}

		entryList = filteredEntryList

		raceSetup.PickupModeEnabled = 1
		raceSetup.LockedEntryList = 1
	} else {
		if championship.OpenEntrants {
			raceSetup.PickupModeEnabled = 1
			raceSetup.LockedEntryList = 0
		} else {
			raceSetup.PickupModeEnabled = 1
			raceSetup.LockedEntryList = 1
		}
	}

	raceSetup.LoopMode = 1

	if raceSetup.HasSession(serverconfig.SessionTypeBooking) {
		logrus.Infof("Championship event has a booking session. Disabling PickupMode, clearing EntryList")
		// championship events with booking do not have an entry list. pick up mode is disabled.
		raceSetup.PickupModeEnabled = 0
		entryList = nil
	} else {
		raceSetup.MaxClients = len(entryList)
	}

	return raceSetup, entryList
}

func (cm *ChampionshipManager) StartEvent(championshipID string, eventID string, isPreChampionshipPracticeEvent bool) error {
	championship, event, err := cm.GetChampionshipAndEvent(championshipID, eventID)

	if err != nil {
		return err
	}

	raceSetup, entryList := cm.FinalEventConfigurationFiles(championship, event, isPreChampionshipPracticeEvent)

	if cm.config.Lua.Enabled {
		err := championshipEventStartPlugin(event, championship, &entryList)

		if err != nil {
			logrus.WithError(err).Error("championship event start plugin script failed")
		}
	}

	if !isPreChampionshipPracticeEvent {
		logrus.Infof("Starting Championship Event: %s at %s (%s) with %d entrants", raceSetup.Cars, raceSetup.Track, raceSetup.TrackLayout, raceSetup.MaxClients)

		// track that this is the current event
		return cm.applyConfigAndStart(&smmodel.ActiveChampionship{
			ChampionshipID:      championship.ID,
			EventID:             event.ID,
			Name:                championship.Name,
			OverridePassword:    championship.OverridePassword,
			ReplacementPassword: championship.ReplacementPassword,
			Description:         string(championship.Info),
			RaceConfig:          raceSetup,
			EntryList:           entryList,
		})
	}

	// delete all sessions other than booking (if there is a booking session)
	delete(raceSetup.Sessions, serverconfig.SessionTypePractice)
	delete(raceSetup.Sessions, serverconfig.SessionTypeQualifying)
	delete(raceSetup.Sessions, serverconfig.SessionTypeRace)

	raceSetup.Sessions[serverconfig.SessionTypePractice] = &serverconfig.SessionConfig{
		Name:   "Practice",
		Time:   120,
		IsOpen: 1,
	}

	if !raceSetup.HasSession(serverconfig.SessionTypeBooking) {
		// #271: override pickup mode to ON for practice sessions
		raceSetup.PickupModeEnabled = 1
	}

	raceSetup.ResultScreenTime = 30

	return cm.RaceManager.applyConfigAndStart(&smmodel.ActiveChampionship{
		ChampionshipID:      championship.ID,
		EventID:             event.ID,
		Name:                championship.Name,
		OverridePassword:    championship.OverridePassword,
		ReplacementPassword: championship.ReplacementPassword,
		Description:         string(championship.Info),
		IsPracticeSession:   true,
		RaceConfig:          raceSetup,
		EntryList:           entryList,
	})
}

func championshipEventStartPlugin(event *smmodel.ChampionshipEvent, championship *smmodel.Championship, entryList *serverconfig.EntryList) error {
	var standings [][]*smmodel.ChampionshipStanding

	for _, class := range championship.Classes {
		standings = append(standings, class.Standings(championship, championship.Events))
	}

	p := smlua.NewPlugin()

	newEvent, newChampionship, newEntryList := smmodel.NewChampionshipEvent(), smmodel.NewChampionship(championship.Name), &serverconfig.EntryList{}

	p.Inputs(event, championship, standings, entryList).Outputs(&newEntryList, newEvent, newChampionship)
	err := p.Call("./plugins/championship.lua", "onChampionshipEventStart")

	if err != nil {
		return err
	}

	*event, *championship, *entryList = *newEvent, *newChampionship, *newEntryList

	return nil
}

func (cm *ChampionshipManager) GetChampionshipAndEvent(championshipID string, eventID string) (*smmodel.Championship, *smmodel.ChampionshipEvent, error) {
	championship, err := cm.LoadChampionship(championshipID)

	if err != nil {
		return nil, nil, err
	}

	event, _, err := championship.EventByID(eventID)

	if err != nil {
		return nil, nil, err
	}

	return championship, event, nil
}

func (cm *ChampionshipManager) ReorderChampionshipEvents(championshipID string, championshipEventIDsInOrder []string) error {
	championship, err := cm.LoadChampionship(championshipID)

	if err != nil {
		return err
	}

	var orderedEvents []*smmodel.ChampionshipEvent

	for _, championshipEventID := range championshipEventIDsInOrder {
		event, _, err := championship.EventByID(championshipEventID)

		if err != nil {
			return err
		}

		orderedEvents = append(orderedEvents, event)
	}

	championship.Events = orderedEvents

	return cm.UpsertChampionship(championship)
}
