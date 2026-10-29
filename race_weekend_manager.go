package servermanager

import (
	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smcontent "github.com/wernersbacher/assetto-server-manager/internal/content"
	"github.com/wernersbacher/assetto-server-manager/internal/reminder"

	smconfig "github.com/wernersbacher/assetto-server-manager/internal/config"

	"encoding/json"
	"net/http"
	"sync"

	"github.com/go-chi/chi"
	"github.com/google/uuid"
	"github.com/wernersbacher/assetto-server-manager/pkg/when"
)

type RaceWeekendManager struct {
	config              *smconfig.Configuration
	raceManager         *RaceManager
	championshipManager *ChampionshipManager
	notificationManager NotificationDispatcher
	carManager          *smcontent.CarManager
	store               Store
	process             ServerProcess

	activeRaceWeekend *ActiveRaceWeekend
	mutex             sync.Mutex

	scheduledSessionTimers         map[string]*when.Timer
	scheduledSessionReminderTimers reminder.Timers
}

func NewRaceWeekendManager(
	config *smconfig.Configuration,
	raceManager *RaceManager,
	championshipManager *ChampionshipManager,
	store Store,
	process ServerProcess,
	notificationManager NotificationDispatcher,
	carManager *smcontent.CarManager,
) *RaceWeekendManager {
	return &RaceWeekendManager{
		config:              config,
		raceManager:         raceManager,
		championshipManager: championshipManager,
		notificationManager: notificationManager,
		store:               store,
		process:             process,
		carManager:          carManager,

		scheduledSessionTimers:         make(map[string]*when.Timer),
		scheduledSessionReminderTimers: make(reminder.Timers),
	}
}

func (rwm *RaceWeekendManager) ListRaceWeekends() ([]*RaceWeekend, error) {
	return rwm.store.ListRaceWeekends()
}

func (rwm *RaceWeekendManager) LoadRaceWeekend(id string) (*RaceWeekend, error) {
	raceWeekend, err := rwm.store.LoadRaceWeekend(id)

	if err != nil {
		return nil, err
	}

	if raceWeekend.HasLinkedChampionship() {
		raceWeekend.Championship, err = rwm.store.LoadChampionship(raceWeekend.ChampionshipID.String())

		if err != nil {
			return nil, err
		}

		for _, session := range raceWeekend.Sessions {
			// make sure that session points only exist for classes that exist.
			for championshipClassID := range session.Points {
				if _, err := raceWeekend.Championship.ClassByID(championshipClassID.String()); err != nil {
					delete(session.Points, championshipClassID)
				}
			}

			// make sure that each race weekend session has the correct number of point places
			// available to match the number of championship entrants.
			for _, class := range raceWeekend.Championship.Classes {
				raceWeekendSessionPoints, ok := session.Points[class.ID]

				if !ok {
					continue
				}

				if len(class.Points.Places) > len(raceWeekendSessionPoints.Places) {
					var extendedPlaces []int

					if session.SessionType() == serverconfig.SessionTypeRace {
						extendedPlaces = class.Points.Places[len(raceWeekendSessionPoints.Places):]
					} else {
						extendedPlaces = make([]int, len(class.Points.Places)-len(raceWeekendSessionPoints.Places))
					}

					raceWeekendSessionPoints.Places = append(raceWeekendSessionPoints.Places, extendedPlaces...)
				}
			}
		}
	}

	return raceWeekend, nil
}

func (rwm *RaceWeekendManager) BuildRaceWeekendTemplateOpts(r *http.Request) (*RaceTemplateVars, error) {
	opts, err := rwm.raceManager.BuildRaceOpts(r)

	if err != nil {
		return nil, err
	}

	var raceWeekend *RaceWeekend
	var isEditing bool

	if existingID := chi.URLParam(r, "raceWeekendID"); existingID != "" {
		isEditing = true
		currentRaceWeekend, err := rwm.store.LoadRaceWeekend(existingID)

		if err != nil {
			return nil, err
		}

		raceWeekend = currentRaceWeekend
	} else {
		isEditing = false
		raceWeekend = NewRaceWeekend()
	}

	var championshipID string

	if !isEditing {
		championshipID = r.URL.Query().Get("championshipID")
	} else {
		championshipID = raceWeekend.ChampionshipID.String()
	}

	if championshipID != uuid.Nil.String() && championshipID != "" {
		championship, err := rwm.store.LoadChampionship(championshipID)

		if err != nil {
			return nil, err
		}

		raceWeekend.ChampionshipID = championship.ID

		opts.Championship = championship
	}

	opts.RaceWeekend = raceWeekend
	opts.IsEditing = isEditing

	return opts, nil
}

func (rwm *RaceWeekendManager) SaveRaceWeekend(r *http.Request) (raceWeekend *RaceWeekend, edited bool, err error) {
	if err := r.ParseForm(); err != nil {
		return nil, false, err
	}

	if raceWeekendID := r.FormValue("Editing"); raceWeekendID != "" {
		raceWeekend, err = rwm.LoadRaceWeekend(raceWeekendID)

		if err != nil {
			return nil, edited, err
		}

		edited = true
	} else {
		raceWeekend = NewRaceWeekend()
	}

	raceWeekend.Name = r.FormValue("RaceWeekendName")

	if championshipID := r.FormValue("ChampionshipID"); championshipID != "" {
		champ, err := rwm.store.LoadChampionship(championshipID)

		if err != nil {
			return nil, edited, err
		}

		raceWeekend.ChampionshipID = champ.ID

		useChampionshipRaceSettings := r.FormValue("UseChampionshipRaceSettings") == "1"

		if !edited {
			// add a championship event for this race weekend
			event := NewChampionshipEvent()
			event.RaceWeekendID = raceWeekend.ID
			event.UseChampionshipRaceSettings = useChampionshipRaceSettings

			champ.Events = append(champ.Events, event)

			if err := rwm.store.UpsertChampionship(champ); err != nil {
				return nil, edited, err
			}
		} else if champ.HasRaceSettings() {
			// the checkbox is only part of the form if the championship has race settings
			if event := champ.RaceWeekendEvent(raceWeekend.ID); event != nil && event.UseChampionshipRaceSettings != useChampionshipRaceSettings {
				event.UseChampionshipRaceSettings = useChampionshipRaceSettings

				if err := rwm.store.UpsertChampionship(champ); err != nil {
					return nil, edited, err
				}
			}
		}
	} else {
		// entrylist length is -1 to accommodate for spectator car (below)
		entryList, err := rwm.raceManager.BuildEntryList(r, 0, len(r.Form["EntryList.Name"])-1)

		if err != nil {
			return nil, edited, err
		}

		numAvailableSpectatorCars := formValueAsInt(r.FormValue("NumAvailableSpectatorCars"))
		cars := r.Form["Cars"]
		carSelectionRange := len(cars) - numAvailableSpectatorCars

		if len(cars) > 0 && carSelectionRange < len(cars) {
			// look through all cars and if they have 'any car model', split all chosen cars between the entrants
			cars = cars[:carSelectionRange]

			allCars, err := rwm.carManager.ListCars()

			if err != nil {
				return nil, edited, err
			}

			carMap := allCars.AsMap()

			availableCarIndex := 0
			carSkinChoice := make(map[string]int)

			for _, entrant := range entryList {
				if entrant.Model == serverconfig.AnyCarModel {
					entrant.Model = cars[availableCarIndex%len(cars)]

					if skins, ok := carMap[entrant.Model]; ok && len(skins) > 0 {
						entrant.Skin = skins[carSkinChoice[entrant.Model]%len(skins)]

						carSkinChoice[entrant.Model]++
					}

					availableCarIndex++
				}
			}
		}

		raceWeekend.EntryList = entryList

		// persist race weekend entrants in the autofill entry list
		if err := rwm.raceManager.SaveEntrantsForAutoFill(entryList); err != nil {
			return raceWeekend, edited, err
		}

		spectatorEntrants, err := rwm.raceManager.BuildEntryList(r, len(entryList), 1)

		if err != nil {
			return raceWeekend, edited, err
		}

		raceWeekend.SpectatorCar = *(spectatorEntrants.AsSlice()[0])
		raceWeekend.SpectatorCarEnabled = formValueAsInt(r.FormValue("SpectatorCar.Enabled")) == 1
	}

	return raceWeekend, edited, rwm.UpsertRaceWeekend(raceWeekend)
}

func (rwm *RaceWeekendManager) UpsertRaceWeekend(raceWeekend *RaceWeekend) error {
	err := rwm.store.UpsertRaceWeekend(raceWeekend)

	if err != nil {
		return err
	}

	return nil
}

func (rwm *RaceWeekendManager) DeleteRaceWeekend(id string) error {
	return rwm.store.DeleteRaceWeekend(id)
}

func (rwm *RaceWeekendManager) FindSession(raceWeekendID, sessionID string) (*RaceWeekend, *RaceWeekendSession, error) {
	raceWeekend, err := rwm.LoadRaceWeekend(raceWeekendID)

	if err != nil {
		return nil, nil, err
	}

	session, err := raceWeekend.FindSessionByID(sessionID)

	if err != nil {
		return nil, nil, err
	}

	return raceWeekend, session, nil
}

func (rwm *RaceWeekendManager) FindConnectedSessions(raceWeekendID, parentSessionID, childSessionID string) (*RaceWeekend, *RaceWeekendSession, *RaceWeekendSession, error) {
	raceWeekend, err := rwm.LoadRaceWeekend(raceWeekendID)

	if err != nil {
		return nil, nil, nil, err
	}

	parentSession, err := raceWeekend.FindSessionByID(parentSessionID)

	if err != nil {
		return nil, nil, nil, err
	}

	childSession, err := raceWeekend.FindSessionByID(childSessionID)

	if err != nil {
		return nil, nil, nil, err
	}

	return raceWeekend, parentSession, childSession, nil
}

func (rwm *RaceWeekendManager) ImportRaceWeekend(data string) (string, error) {
	var raceWeekend *RaceWeekend

	err := json.Unmarshal([]byte(data), &raceWeekend)

	if err != nil {
		return "", err
	}

	return raceWeekend.ID.String(), rwm.UpsertRaceWeekend(raceWeekend)
}
