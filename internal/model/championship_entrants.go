package model

import (
	"errors"
	"sync"

	"github.com/sirupsen/logrus"
	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smresults "github.com/wernersbacher/ACPitwall/internal/results"
)

func (c *Championship) ClearEntrant(entrantGUID string) {
	for _, class := range c.Classes {
		for _, classEntrant := range class.Entrants {
			if entrantGUID == classEntrant.GUID {
				// remove the entrant from the championship.
				classEntrant.Name = ""
				classEntrant.GUID = ""
				classEntrant.Team = ""
				return
			}
		}
	}
}

// NumEntrants is the number of entrants across all Classes in a Championship.
func (c *Championship) NumEntrants() int {
	entrants := 0

	if c.SignUpForm.Enabled && !c.OpenEntrants {
		// closed sign up form championships report only the taken slots as their num entrants
		for _, class := range c.Classes {
			for _, entrant := range class.Entrants {
				if entrant.GUID != "" && !entrant.IsPlaceHolder {
					entrants++
				}
			}
		}
	} else {
		for _, class := range c.Classes {
			entrants += len(class.Entrants)
		}
	}

	return entrants
}

// AllEntrants returns the list of all entrants in the championship (across ALL classes), the classes in their order
// and the entrants of a class in the order of their pit boxes. The entrants are copies with the pit boxes of the
// combined grid: placing the entrants of the classes themselves would overwrite their pit boxes (in the random order of
// the class entrant map), which changes the order of the entry list of the class.
func (c *Championship) AllEntrants() serverconfig.EntryList {
	e := make(serverconfig.EntryList)

	for _, class := range c.Classes {
		for _, entrant := range class.Entrants.AsSlice() {
			entrantCopy := *entrant
			e.AddToBackOfGrid(&entrantCopy)
		}
	}

	return e
}

func (c *Championship) EntrantAttendance(guid string) int {
	i := 0

	for _, event := range ExtractRaceWeekendSessionsIntoIndividualEvents(c.Events) {
		if event.Completed() {
			for _, class := range c.Classes {
				standings := class.StandingsForEvent(c, event)

				for _, standing := range standings {
					if standing.Car.GetGUID() == guid {
						i++
					}
				}
			}
		}
	}

	return i
}

func (c *Championship) NumPendingSignUps() int {
	num := 0

	for _, response := range c.SignUpForm.Responses {
		if response.Status == ChampionshipEntrantPending {
			num++
		}
	}

	return num
}

type PotentialChampionshipEntrant interface {
	GetName() string
	GetTeam() string
	GetCar() string
	GetSkin() string
	GetGUID() string
}

var ErrEntryListFull = errors.New("servermanager: entry list is full")

var ErrEntrantHasNoGUID = errors.New("servermanager: entrant has no GUID")

var entryListMutex = sync.Mutex{}

func (c *Championship) AddEntrantInFirstFreeSlot(potentialEntrant PotentialChampionshipEntrant) (foundFreeEntrantSlot bool, entrant *serverconfig.Entrant, entrantClass *ChampionshipClass, err error) {
	if potentialEntrant.GetGUID() == "" {
		// an empty GUID would match every free slot as an existing entrant (and couldn't take a slot anyway)
		return false, nil, nil, ErrEntrantHasNoGUID
	}

	entryListMutex.Lock()
	defer entryListMutex.Unlock()

	for _, class := range c.Classes {
		for _, entrant := range class.Entrants {
			if entrant.GUID == potentialEntrant.GetGUID() {
				// update the entrant details
				entrant.Name = potentialEntrant.GetName()

				// #386: don't replace a team with no team.
				if potentialEntrant.GetTeam() != "" {
					entrant.Team = potentialEntrant.GetTeam()
				}

				return true, entrant, class, nil
			}
		}
	}

	for _, class := range c.Classes {
		// take the free slots in grid order, class.Entrants is a map.
		for _, entrant := range class.Entrants.AsSlice() {
			if entrant.Name == "" && entrant.GUID == "" {
				// take the slot
				entrant.Name = potentialEntrant.GetName()
				entrant.GUID = potentialEntrant.GetGUID()
				entrant.Team = potentialEntrant.GetTeam()

				return true, entrant, class, nil
			}
		}
	}

	return false, nil, nil, ErrEntryListFull
}

func (c *Championship) AddEntrantFromSession(potentialEntrant PotentialChampionshipEntrant) (foundFreeEntrantSlot bool, entrant *serverconfig.Entrant, entrantClass *ChampionshipClass, err error) {
	entryListMutex.Lock()
	defer entryListMutex.Unlock()

	classForCar, err := c.FindClassForCarModel(potentialEntrant.GetCar())

	if err != nil {
		logrus.Errorf("Could not find class for car: %s in championship", potentialEntrant.GetCar())

		return false, nil, nil, err
	}

	var oldEntrant *serverconfig.Entrant
	var oldEntrantClass *ChampionshipClass

	for _, class := range c.Classes {
		for _, entrant := range class.Entrants {
			if entrant.GUID == potentialEntrant.GetGUID() {
				// we found the entrant, but there's a possibility that they changed cars
				// keep the old entrant so we can swap its properties with the one that is being written to
				oldEntrant = entrant
				oldEntrantClass = class

				entrant.GUID = ""
				entrant.Name = ""
			}
		}
	}

	// now look for empty Entrants in the Entrylist with a matching car
	for carNum, entrant := range classForCar.Entrants {
		if entrant.Name == "" && entrant.GUID == "" && entrant.Model == potentialEntrant.GetCar() {
			if oldEntrant != nil {
				// swap the old entrant properties
				oldEntrant.SwapProperties(entrant, oldEntrantClass == classForCar)
			}

			classForCar.AssignToFreeEntrantSlot(entrant, potentialEntrant)
			logrus.Infof("Championship entrant: %s (%s) has been assigned to %s in %s (matching car)", entrant.Name, entrant.GUID, carNum, c.Name)

			return true, entrant, classForCar, nil
		}
	}

	// now look for empty Entrants in the Entrylist with an 'any free car' slot
	for carNum, entrant := range classForCar.Entrants {
		if entrant.Name == "" && entrant.GUID == "" && entrant.Model == serverconfig.AnyCarModel {
			if oldEntrant != nil {
				// swap the old entrant properties
				oldEntrant.SwapProperties(entrant, oldEntrantClass == classForCar)
			}

			classForCar.AssignToFreeEntrantSlot(entrant, potentialEntrant)
			logrus.Infof("Championship entrant: %s (%s) has been assigned an to %s in %s (any car slot)", entrant.Name, entrant.GUID, carNum, c.Name)

			return true, entrant, classForCar, nil
		}
	}

	return false, nil, classForCar, nil
}

const kickedGUID = smresults.KickedGUID

// EnhanceResults takes a set of SessionResults and attaches Championship information to them.
func (c *Championship) EnhanceResults(results *smresults.SessionResults) {
	if results == nil {
		return
	}

	results.ChampionshipID = c.ID.String()

	c.AttachClassIDToResults(results)
	results.NormaliseDriverSwapGUIDs()

	// update names / teams to the values we know to be correct due to championship setup
	for _, class := range c.Classes {
		for _, entrant := range class.Entrants {
			class.AttachEntrantToResult(entrant, results)
		}
	}

	results.ClearKickedGUIDs()
}
