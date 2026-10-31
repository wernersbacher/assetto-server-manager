package raceweekend

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cj123/ini"
	"github.com/sirupsen/logrus"
	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smcontent "github.com/wernersbacher/ACPitwall/internal/content"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smresults "github.com/wernersbacher/ACPitwall/internal/results"
	"github.com/wernersbacher/ACPitwall/internal/serverenv"
)

type FilterError string

func (f FilterError) Error() string {
	return string(f)
}

func reverseEntrants(numToReverse int, entrants []*smmodel.RaceWeekendSessionEntrant) {
	if numToReverse == 0 {
		return
	}

	if numToReverse > len(entrants) {
		numToReverse = len(entrants)
	}

	var toReverse []*smmodel.RaceWeekendSessionEntrant

	if numToReverse > 0 {
		toReverse = entrants[:numToReverse]
	} else {
		toReverse = entrants
	}

	for i := len(toReverse)/2 - 1; i >= 0; i-- {
		opp := len(toReverse) - 1 - i
		toReverse[i], toReverse[opp] = toReverse[opp], toReverse[i]
	}

	for i := 0; i < len(toReverse); i++ {
		entrants[i] = toReverse[i]
	}
}

var ErrRaceWeekendUnknownSplitType = errors.New("servermanager: unknown split type")

// filterRaceWeekendEntryList takes a set of RaceWeekendSessionEntrants formed by the results of the parent session and filters them into a child session entry list.
func filterRaceWeekendEntryList(env *serverenv.Env, f smmodel.RaceWeekendSessionToSessionFilter, raceWeekend *smmodel.RaceWeekend, parentSession, childSession *smmodel.RaceWeekendSession, parentSessionResults []*smmodel.RaceWeekendSessionEntrant, childSessionEntryList *smmodel.RaceWeekendEntryList) error {
	if parentSession.Completed() || childSession.IsBase() {
		sorter := GetRaceWeekendEntryListSort(env, f.SortType)

		parentSession.NumEntrantsToReverse = f.NumEntrantsToReverse

		// race weekend session is completed and has a valid sorter, use it to sort results before filtering.
		if err := sorter.Sort(raceWeekend, parentSession, parentSessionResults, &f); err != nil {
			return err
		}
	}

	entryListStart := f.EntryListStart - 1

	var split []*smmodel.RaceWeekendSessionEntrant

	switch f.SplitType {
	case smmodel.SplitTypeNumeric:
		resultStart, resultEnd := f.ResultStart, f.ResultEnd

		resultStart--

		if resultStart > len(parentSessionResults) {
			return nil
		}

		if resultEnd > len(parentSessionResults) {
			resultEnd = len(parentSessionResults)
		}

		split = parentSessionResults[resultStart:resultEnd]
	case smmodel.SplitTypeManualDriverSelection:
		for _, driverGUID := range f.SelectedDriverGUIDs {
			for _, entrant := range parentSessionResults {
				if entrant.Car.GetGUID() == driverGUID {
					split = append(split, entrant)
					break
				}
			}
		}
	case smmodel.SplitTypeChampionshipClass:
		for _, entrant := range parentSessionResults {
			class := entrant.ChampionshipClass(raceWeekend)

			if _, ok := f.SelectedChampionshipClassIDs[class.ID]; ok {
				split = append(split, entrant)
			}
		}

	default:
		return ErrRaceWeekendUnknownSplitType
	}

	if !parentSession.Completed() {
		reverseEntrants(f.NumEntrantsToReverse, split)
	}

	splitIndex := 0

	for pitBox := entryListStart; pitBox < entryListStart+len(split); pitBox++ {
		entrant := split[splitIndex]
		entrant.SessionID = parentSession.ID

		if !f.IsPreview && parentSession.Completed() && f.ForceUseTyreFromFastestLap {
			// find the tyre from the entrants fastest lap
			fastestLap := entrant.SessionResults.GetDriversFastestLap(entrant.Car.GetGUID(), entrant.Car.GetCar())

			if fastestLap == nil {
				logrus.Warnf("could not find fastest lap for entrant %s (%s). will not lock their tyre choice.", entrant.Car.GetName(), entrant.Car.GetGUID())
			} else {
				err := buildLockedTyreSetup(env, raceWeekend, childSession, entrant, fastestLap)

				if err != nil {
					logrus.WithError(err).Errorf("could not build locked tyre setup for entrant %s (%s)", entrant.Car.GetName(), entrant.Car.GetGUID())
				}
			}
		}

		childSessionEntryList.AddInPitBox(entrant, pitBox)

		splitIndex++
	}

	return nil
}

const lockedTyreSetupFolder = smcontent.LockedTyreSetupFolder

func buildLockedTyreSetup(env *serverenv.Env, rw *smmodel.RaceWeekend, session *smmodel.RaceWeekendSession, entrant *smmodel.RaceWeekendSessionEntrant, fastestLap *smresults.SessionLap) error {
	tyreIndex, err := smcontent.FindTyreIndex(env.InstallPath(), entrant.Car.Model, fastestLap.Tyre, session.RaceConfig)

	if err != nil {
		return err
	}

	entryList := rw.GetEntryList()

	var setup *ini.File

	for _, raceWeekendEntrant := range entryList {
		if raceWeekendEntrant.GUID == entrant.Car.GetGUID() && raceWeekendEntrant.FixedSetup != "" {
			setup, err = ini.Load(filepath.Join(env.InstallPath(), "setups", raceWeekendEntrant.FixedSetup))

			if err != nil {
				return err
			}

			break
		}
	}

	if setup == nil {
		// no fixed setup was specified
		// write out a temp ini setup file for this car + player.
		setup = ini.NewFile([]ini.DataSource{nil}, ini.LoadOptions{
			IgnoreInlineComment: true,
		})

		_, err = setup.NewSection("DEFAULT")

		if err != nil {
			return err
		}

		car, err := setup.NewSection("CAR")

		if err != nil {
			return err
		}

		_, err = car.NewKey("MODEL", entrant.Car.Model)

		if err != nil {
			return err
		}
	}

	tyres, err := setup.NewSection("TYRES")

	if err != nil {
		return err
	}

	_, err = tyres.NewKey("VALUE", fmt.Sprintf("%d", tyreIndex))

	if err != nil {
		return err
	}

	raceWeekendSection, err := setup.NewSection("RACE_WEEKEND")

	if err != nil {
		return err
	}

	_, err = raceWeekendSection.NewKey("ID", rw.ID.String())

	if err != nil {
		return err
	}

	_, err = raceWeekendSection.NewKey("SESSION_ID", entrant.SessionID.String())

	if err != nil {
		return err
	}

	setupFilePath := filepath.Join(entrant.Car.Model, lockedTyreSetupFolder, fmt.Sprintf("race_weekend_session_%s_%s.ini", entrant.Car.GetGUID(), entrant.SessionID.String()))

	fullSaveFilepath := filepath.Join(env.InstallPath(), "setups", setupFilePath)

	if err := os.MkdirAll(filepath.Dir(fullSaveFilepath), 0755); err != nil {
		return err
	}

	if err := setup.SaveTo(fullSaveFilepath); err != nil {
		return err
	}

	entrant.OverrideSetupFile = setupFilePath

	return nil
}

// getRaceWeekendEntryList returns the RaceWeekendEntryList for the given session, built from the parent session(s) results and applied filters.
func getRaceWeekendEntryList(env *serverenv.Env, rws *smmodel.RaceWeekendSession, rw *smmodel.RaceWeekend, overrideFilter *smmodel.RaceWeekendSessionToSessionFilter, overrideFilterSessionID string) (smmodel.RaceWeekendEntryList, error) {
	var entryList smmodel.RaceWeekendEntryList

	if rws.IsBase() {
		entryList = smmodel.EntryListToRaceWeekendEntryList(rw.GetEntryList(), rws.ID)

		if overrideFilter == nil {
			var err error

			overrideFilter, err = rw.GetFilterOrUseDefault(rw.ID.String(), rws.ID.String())

			if err != nil {
				return nil, err
			}
		}

		err := filterRaceWeekendEntryList(env, *overrideFilter, rw, rws, rws, entryList, &entryList)

		if err != nil {
			return nil, err
		}
	} else {
		entryList = make(smmodel.RaceWeekendEntryList, 0)

		for _, parentSessionID := range rws.ParentIDs {
			parentSession, err := rw.FindSessionByID(parentSessionID.String())

			if err != nil {
				return nil, err
			}

			finishingGrid, err := raceWeekendFinishingGrid(env, parentSession, rw)

			if err != nil {
				return nil, err
			}

			if overrideFilter != nil && parentSessionID.String() == overrideFilterSessionID {
				// override filters are provided when users are modifying filters for their race weekend setups
				err = filterRaceWeekendEntryList(env, *overrideFilter, rw, parentSession, rws, finishingGrid, &entryList)

				if err != nil {
					return nil, err
				}
			} else {
				sessionToSessionFilter, err := rw.GetFilterOrUseDefault(parentSessionID.String(), rws.ID.String())

				if err != nil {
					return nil, err
				}

				err = filterRaceWeekendEntryList(env, *sessionToSessionFilter, rw, parentSession, rws, finishingGrid, &entryList)

				if err != nil {
					return nil, err
				}
			}
		}
	}

	// do an initial sort of the entrylist by pitboxes, as sessions may add drivers out of order.
	sort.Slice(entryList, func(i, j int) bool {
		return entryList[i].PitBox < entryList[j].PitBox
	})

	if rw.SessionCanBeRun(rws) {
		// sorting can only be run if a session is ready to be run.
		sorter := GetRaceWeekendEntryListSort(env, rws.SortType)

		if err := sorter.Sort(rw, rws, entryList, nil); err != nil {
			return nil, err
		}

		var finalEntryList smmodel.RaceWeekendEntryList
		existingGUIDs := make(map[string]bool)

		// filter out duplicate entrants, keeping their highest ranking in the entry list
		for _, entrant := range entryList {
			if _, guidAlreadyInEntryList := existingGUIDs[entrant.Car.GetGUID()]; guidAlreadyInEntryList {
				continue
			}

			finalEntryList = append(finalEntryList, entrant)
			existingGUIDs[entrant.Car.GetGUID()] = true
		}

		// amend pitboxes post-sort
		for i, entrant := range finalEntryList {
			entrant.PitBox = i
		}

		entryList = finalEntryList
	}

	return entryList, nil
}

// raceWeekendFinishingGrid returns the finishing grid of the session, if complete. Otherwise, it returns the EntryList of that session
func raceWeekendFinishingGrid(env *serverenv.Env, rws *smmodel.RaceWeekendSession, raceWeekend *smmodel.RaceWeekend) ([]*smmodel.RaceWeekendSessionEntrant, error) {
	var out []*smmodel.RaceWeekendSessionEntrant

	entryList, err := getRaceWeekendEntryList(env, rws, raceWeekend, nil, "")

	if err != nil {
		return nil, err
	}

	if rws.Completed() {
		for _, result := range rws.Results.Result {
			if result.DriverGUID == "" || result.Disqualified || (raceWeekend.HasSpectatorCar() && result.DriverGUID == raceWeekend.GetSpectatorCar().GUID) {
				// filter out invalid results
				continue
			}

			car, err := rws.Results.FindCarByGUIDAndModel(result.DriverGUID, result.CarModel)

			if err != nil {
				continue
			}

			out = append(out, smmodel.NewRaceWeekendSessionEntrant(rws.ID, car, result, rws.Results))
		}

		// look for entrants not in our session results who started the session.
		for _, entrant := range entryList {
			foundEntrant := false

			for _, driver := range out {
				if driver.Car.GetGUID() == entrant.Car.GetGUID() || serverconfig.NormaliseEntrantGUIDs(driver.Car.Driver.GuidsList) == entrant.Car.GetGUID() {
					foundEntrant = true
					break
				}
			}

			if !foundEntrant && entrant.Car.GetGUID() != "" && (rws.IsBase() || !entrant.IsPlaceholder) {
				if raceWeekend.HasLinkedChampionship() {
					// find the class ID for the car
					class, err := raceWeekend.Championship.FindClassForCarModel(entrant.Car.GetCar())

					if err != nil {
						logrus.WithError(err).Warnf("Could not find class for car model: %s for entrant %s", entrant.Car.GetCar(), entrant.Car.GetGUID())
					} else {
						entrant.EntrantResult.ClassID = class.ID
					}
				}

				e := smmodel.NewRaceWeekendSessionEntrant(rws.ID, entrant.Car, entrant.EntrantResult, rws.Results)
				e.IsPlaceholder = entrant.IsPlaceholder

				out = append(out, e)
			}
		}
	} else {
		// if a session is not completed, we work on the assumption that the finishing grid is equal to the entrylist
		out = entryList.Sorted()
	}

	return out, nil
}

// SessionEntryList returns the entry list which a race weekend session will be run with.
func (rwm *RaceWeekendManager) SessionEntryList(session *smmodel.RaceWeekendSession, raceWeekend *smmodel.RaceWeekend) (serverconfig.EntryList, error) {
	rwe, err := getRaceWeekendEntryList(rwm.env, session, raceWeekend, nil, "")

	if err != nil {
		return nil, err
	}

	return rwe.AsEntryList(), nil
}

// TrackOverview describes the tracks of all sessions of a race weekend.
func TrackOverview(env *serverenv.Env, rw *smmodel.RaceWeekend) string {
	trackInfoMap := make(map[string]bool)

	for _, session := range rw.Sessions {
		trackInfo := env.TrackInfo(session.RaceConfig.Track, session.RaceConfig.TrackLayout)

		var trackDescription string

		if trackInfo != nil {
			trackDescription = trackInfo.Name

			if trackInfo.Country != "" {
				trackDescription += " - " + trackInfo.Country
			}
		} else {
			trackDescription = smcontent.PrettifyName(session.RaceConfig.Track, false)

			if session.RaceConfig.TrackLayout != "" {
				trackDescription += " (" + smcontent.PrettifyName(session.RaceConfig.TrackLayout, true) + ")"
			}
		}

		trackInfoMap[trackDescription] = true
	}

	var trackInfo []string

	for info := range trackInfoMap {
		trackInfo = append(trackInfo, info)
	}

	return strings.Join(trackInfo, ", ")
}

var ErrRaceWeekendSessionDependencyIncomplete = errors.New("servermanager: race weekend session dependency incomplete")
