package championship

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
)

func (cm *ChampionshipManager) ImportChampionship(jsonData string) (string, error) {
	var championship *smmodel.Championship

	err := json.Unmarshal([]byte(jsonData), &championship)

	if err != nil {
		return "", err
	}

	for _, event := range championship.Events {
		if event.IsRaceWeekend() {
			err := cm.store.UpsertRaceWeekend(event.RaceWeekend)

			if err != nil {
				return "", err
			}
		}
	}

	return championship.ID.String(), cm.UpsertChampionship(championship)
}

func (cm *ChampionshipManager) ImportEventSetup(championshipID string, eventID string) error {
	race, err := cm.store.FindCustomRaceByID(eventID)

	if err != nil {
		return err
	}

	championship, err := cm.LoadChampionship(championshipID)

	if err != nil {
		return err
	}

	_, err = championship.ImportEvent(race)

	if err != nil {
		return err
	}

	return cm.UpsertChampionship(championship)
}

func (cm *ChampionshipManager) ImportRaceWeekendSetup(championshipID string, eventID string) error {
	weekend, err := cm.store.LoadRaceWeekend(eventID)

	if err != nil {
		return err
	}

	championship, err := cm.LoadChampionship(championshipID)

	if err != nil {
		return err
	}

	_, err = championship.ImportEvent(weekend)

	if err != nil {
		return err
	}

	err = cm.store.UpsertRaceWeekend(weekend)

	if err != nil {
		return err
	}

	return cm.UpsertChampionship(championship)
}

func (cm *ChampionshipManager) ImportEvent(championshipID string, eventID string, r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		return err
	}

	championship, err := cm.LoadChampionship(championshipID)

	if err != nil {
		return err
	}

	event, _, err := championship.EventByID(eventID)

	if err != nil {
		return err
	}

	event.Sessions = make(map[serverconfig.SessionType]*smmodel.ChampionshipSession)

	sessions := map[serverconfig.SessionType]string{
		serverconfig.SessionTypePractice:   r.FormValue("PracticeResult"),
		serverconfig.SessionTypeQualifying: r.FormValue("QualifyingResult"),
		serverconfig.SessionTypeRace:       r.FormValue("RaceResult"),
		serverconfig.SessionTypeSecondRace: r.FormValue("SecondRaceResult"),
	}

	sessionsOrdered := []serverconfig.SessionType{
		serverconfig.SessionTypePractice,
		serverconfig.SessionTypeQualifying,
		serverconfig.SessionTypeRace,
		serverconfig.SessionTypeSecondRace,
	}

	for _, sessionType := range sessionsOrdered {
		sessionFile, ok := sessions[sessionType]

		if !ok || sessionFile == "" {
			continue
		}

		results, err := cm.env.Results().Load(sessionFile + ".json")

		if err != nil {
			return err
		}

		if championship.OpenEntrants && championship.PersistOpenEntrants {
			// if the championship is open, we might have entrants in this session file who have not
			// raced in this championship before. add them to the championship as they would be added
			// if they joined during a race.
			for _, car := range results.Cars {
				if car.GetGUID() == "" {
					continue
				}

				foundFreeSlot, _, err := cm.AddEntrantFromSessionData(championship, car, false, false)

				if err != nil {
					return err
				}

				if !foundFreeSlot {
					logrus.WithField("car", car).Warn("Could not add entrant to championship. No free slot found")
				}
			}
		}

		championship.EnhanceResults(results)

		if err := cm.env.Results().Save(sessionFile+".json", results); err != nil {
			return err
		}

		event.Sessions[sessionType] = &smmodel.ChampionshipSession{
			StartedTime:   results.Date.Add(-time.Minute * 30),
			CompletedTime: results.Date,
			Results:       results,
		}

		event.CompletedTime = results.Date
	}

	return cm.UpsertChampionship(championship)
}

func (cm *ChampionshipManager) AddEntrantFromSessionData(championship *smmodel.Championship, potentialEntrant smmodel.PotentialChampionshipEntrant, overwriteSkinForAllEvents bool, takeFirstFreeSlot bool) (foundFreeEntrantSlot bool, entrantClass *smmodel.ChampionshipClass, err error) {
	var entrant *serverconfig.Entrant

	if takeFirstFreeSlot {
		foundFreeEntrantSlot, entrant, entrantClass, err = championship.AddEntrantInFirstFreeSlot(potentialEntrant)
	} else {
		foundFreeEntrantSlot, entrant, entrantClass, err = championship.AddEntrantFromSession(potentialEntrant)
	}

	if err == smmodel.ErrEntryListFull {
		// not an error for the callers: like when no slot for the car is free, they tell the user there is no slot
		return false, nil, nil
	} else if err != nil {
		return foundFreeEntrantSlot, entrantClass, err
	}

	if foundFreeEntrantSlot {
		if overwriteSkinForAllEvents {
			// the user's skin setup should be applied to all event settings
			for _, event := range championship.Events {
				eventEntrant := event.EntryList.FindEntrantByInternalUUID(entrant.InternalUUID)

				eventEntrant.Skin = potentialEntrant.GetSkin()
			}
		}

		newEntrant := serverconfig.NewEntrant()

		newEntrant.GUID = potentialEntrant.GetGUID()
		newEntrant.Name = potentialEntrant.GetName()
		newEntrant.Team = potentialEntrant.GetTeam()

		e := make(serverconfig.EntryList)

		e.AddToBackOfGrid(newEntrant)

		err := cm.SaveEntrantsForAutoFill(e)

		if err != nil {
			logrus.Errorf("Couldn't add entrant (GUID: %s, Name: %s) to autofill list", newEntrant.GUID, newEntrant.Name)
		}
	}

	return foundFreeEntrantSlot, entrantClass, nil
}

func (cm *ChampionshipManager) DuplicateChampionship(championshipID string) (*smmodel.Championship, error) {
	championship, err := cm.LoadChampionship(championshipID)

	if err != nil {
		return nil, err
	}

	var events []smmodel.ChampionshipEvent

	for _, event := range championship.Events {
		events = append(events, *event)
	}

	championship.Events = nil

	duplicateChampionship := championship

	duplicateChampionship.ID = uuid.New()
	duplicateChampionship.Created = time.Now()
	duplicateChampionship.Updated = time.Now()
	duplicateChampionship.Name = championship.Name + " Duplicate"

	for _, event := range events {
		if event.IsRaceWeekend() {
			newEvent, err := duplicateChampionship.ImportEvent(event.RaceWeekend)

			if err != nil {
				return nil, err
			}

			if err := cm.store.UpsertRaceWeekend(newEvent.RaceWeekend); err != nil {
				return nil, err
			}
		} else {
			_, err = duplicateChampionship.ImportEvent(&event)

			if err != nil {
				return nil, err
			}
		}
	}

	// clear sign up form responses
	duplicateChampionship.SignUpForm.Responses = nil

	logrus.Infof("New Championship: %s, %s. Duplicate of %s", duplicateChampionship.Name, duplicateChampionship.ID.String(), championshipID)

	return duplicateChampionship, cm.UpsertChampionship(duplicateChampionship)
}

func (cm *ChampionshipManager) DuplicateEventInChampionship(championshipID, eventID string) (*smmodel.ChampionshipEvent, error) {
	championship, err := cm.LoadChampionship(championshipID)

	if err != nil {
		return nil, err
	}

	event, _, err := championship.EventByID(eventID)

	if err != nil {
		return nil, err
	}

	newEvent, err := cm.DuplicateEvent(event, championship)

	if err != nil {
		return nil, err
	}

	if err := cm.UpsertChampionship(championship); err != nil {
		return nil, err
	}

	return newEvent, nil
}

func (cm *ChampionshipManager) DuplicateEvent(event *smmodel.ChampionshipEvent, championship *smmodel.Championship) (*smmodel.ChampionshipEvent, error) {
	var newEvent *smmodel.ChampionshipEvent
	var err error

	if !event.IsRaceWeekend() {
		newEvent, err = championship.ImportEvent(event)

		if err != nil {
			return nil, err
		}
	} else {
		duplicateRaceWeekend, err := event.RaceWeekend.Duplicate()

		if err != nil {
			return nil, err
		}

		newEvent, err = championship.ImportEvent(duplicateRaceWeekend)

		if err != nil {
			return nil, err
		}

		newEvent.RaceWeekend.Name = "Duplicate: " + newEvent.RaceWeekend.Name

		if err := cm.store.UpsertRaceWeekend(newEvent.RaceWeekend); err != nil {
			return nil, err
		}
	}

	return newEvent, nil
}
