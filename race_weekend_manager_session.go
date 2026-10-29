package servermanager

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cj123/ini"
	"github.com/go-chi/chi"
	"github.com/google/uuid"
	"github.com/mattn/go-zglob"
	"github.com/sirupsen/logrus"
	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"
	"github.com/wernersbacher/assetto-server-manager/pkg/udp"
)

func (rwm *RaceWeekendManager) BuildRaceWeekendSessionOpts(r *http.Request) (*RaceTemplateVars, error) {
	opts, err := rwm.raceManager.BuildRaceOpts(r)

	if err != nil {
		return nil, err
	}

	// here we customise the opts to tell the template that this is a race weekend session.
	raceWeekend, err := rwm.LoadRaceWeekend(chi.URLParam(r, "raceWeekendID"))

	if err != nil {
		return nil, err
	}

	var raceWeekendSession *RaceWeekendSession

	if editSessionID := chi.URLParam(r, "sessionID"); editSessionID != "" {
		// editing a race weekend session
		raceWeekendSession, err = raceWeekend.FindSessionByID(editSessionID)

		if err != nil {
			return nil, err
		}

		opts.Current = raceWeekendSession.RaceConfig
		opts.IsEditing = true
		opts.EditingID = editSessionID
		entryList, err := getRaceWeekendEntryList(raceWeekendSession, raceWeekend, nil, "")

		if err == ErrRaceWeekendSessionDependencyIncomplete {
			opts.CurrentEntrants = raceWeekend.GetEntryList()
		} else if err != nil {
			return nil, err
		} else {
			opts.CurrentEntrants = entryList.AsEntryList()
		}
	} else {
		// creating a new race weekend session
		opts.IsEditing = false
		opts.CurrentEntrants = raceWeekend.GetEntryList()

		// override Current race config if there is a previous race weekend race configured
		if len(raceWeekend.Sessions) > 0 {
			opts.Current = raceWeekend.Sessions[len(raceWeekend.Sessions)-1].RaceConfig

			opts.RaceWeekendHasAtLeastOneSession = true
		} else {
			current := serverconfig.ConfigIniDefault().CurrentRaceConfig
			delete(current.Sessions, serverconfig.SessionTypeBooking)
			delete(current.Sessions, serverconfig.SessionTypeQualifying)
			delete(current.Sessions, serverconfig.SessionTypeRace)

			opts.Current = current
			opts.RaceWeekendHasAtLeastOneSession = false
		}

		raceWeekendSession = NewRaceWeekendSession()
	}

	if raceWeekend.HasLinkedChampionship() {
		opts.Championship = raceWeekend.Championship

		if !opts.IsEditing {
			for _, class := range opts.Championship.Classes {
				raceWeekendSession.Points[class.ID] = &class.Points
			}
		}
	}

	opts.RaceWeekendSession = raceWeekendSession
	opts.IsRaceWeekend = true
	opts.RaceWeekend = raceWeekend

	opts.AvailableSessions = serverconfig.AvailableSessionsNoBooking
	opts.ShowOverridePasswordCard = !raceWeekend.HasLinkedChampionship()
	opts.OverridePassword = raceWeekendSession.OverridePassword
	opts.ReplacementPassword = raceWeekendSession.ReplacementPassword

	err = rwm.raceManager.applyCurrentRaceSetupToOptions(opts, opts.Current)

	if err != nil {
		return nil, err
	}

	return opts, nil
}

func (rwm *RaceWeekendManager) SaveRaceWeekendSession(r *http.Request) (raceWeekend *RaceWeekend, session *RaceWeekendSession, edited bool, err error) {
	if err := r.ParseForm(); err != nil {
		return nil, nil, edited, err
	}

	raceWeekend, err = rwm.LoadRaceWeekend(chi.URLParam(r, "raceWeekendID"))

	if err != nil {
		return nil, nil, edited, err
	}

	raceConfig, err := rwm.raceManager.BuildCustomRaceFromForm(r)

	if err != nil {
		return nil, nil, edited, err
	}

	raceConfig.Cars = strings.Join(raceWeekend.GetEntryList().CarIDs(), ";")

	// remove all but the active session from the setup.
	activeSession := r.FormValue("SessionType")

	for session := range raceConfig.Sessions {
		if session.String() != activeSession {
			delete(raceConfig.Sessions, session)
		}
	}

	if sessionID := r.FormValue("Editing"); sessionID != "" {
		edited = true

		session, err = raceWeekend.FindSessionByID(sessionID)

		if err != nil {
			return nil, nil, edited, err
		}

		// we're editing an existing session
		session.RaceConfig = *raceConfig
	} else {
		// creating a new event
		session = NewRaceWeekendSession()
		session.RaceConfig = *raceConfig

		raceWeekend.AddSession(session, nil)
	}

	// empty out previous parent IDs
	session.ParentIDs = []uuid.UUID{}

	// assign parents
	for _, parentID := range r.Form["ParentSessions"] {
		if parentID == "no_parent" {
			// empty out any existing ones
			session.ParentIDs = []uuid.UUID{}
			break
		}

		id, err := uuid.Parse(parentID)

		if err != nil {
			return nil, nil, edited, err
		}

		session.ParentIDs = append(session.ParentIDs, id)
	}

	if len(session.ParentIDs) == 0 {
		session.ParentIDs = append(session.ParentIDs, raceWeekend.ID)
	}

	session.OverridePassword = r.FormValue("OverridePassword") == "1"
	session.ReplacementPassword = r.FormValue("ReplacementPassword")

	if raceWeekend.HasLinkedChampionship() {
		// points
		previousNumPoints := 0

		for i := 0; i < len(r.Form["ClassID"]); i++ {
			classID, err := uuid.Parse(r.Form["ClassID"][i])

			if err != nil {
				return nil, nil, edited, err
			}

			numPointsForClass := formValueAsInt(r.Form["NumPointsForClass"][i])

			pts := &ChampionshipPoints{}

			pts.Places = make([]int, 0)

			for i := previousNumPoints; i < previousNumPoints+numPointsForClass; i++ {
				pts.Places = append(pts.Places, formValueAsInt(r.Form["Points.Place"][i]))
			}

			pts.BestLap = formValueAsInt(r.Form["Points.BestLap"][i])
			pts.CollisionWithDriver = formValueAsInt(r.Form["Points.CollisionWithDriver"][i])
			pts.CollisionWithEnv = formValueAsInt(r.Form["Points.CollisionWithEnv"][i])
			pts.CutTrack = formValueAsInt(r.Form["Points.CutTrack"][i])

			previousNumPoints += numPointsForClass
			session.Points[classID] = pts
		}
	}

	return raceWeekend, session, edited, rwm.UpsertRaceWeekend(raceWeekend)
}

func (rwm *RaceWeekendManager) applyConfigAndStart(raceWeekend *ActiveRaceWeekend) error {
	rwm.activeRaceWeekend = raceWeekend

	err := rwm.raceManager.applyConfigAndStart(raceWeekend)

	if err != nil {
		return err
	}

	return nil
}

func (rwm *RaceWeekendManager) StartPracticeSession(raceWeekendID string, raceWeekendSessionID string) error {
	return rwm.StartSession(raceWeekendID, raceWeekendSessionID, true)
}

func (rwm *RaceWeekendManager) StartSession(raceWeekendID string, raceWeekendSessionID string, isPracticeSession bool) error {
	raceWeekend, err := rwm.LoadRaceWeekend(raceWeekendID)

	if err != nil {
		return err
	}

	session, err := raceWeekend.FindSessionByID(raceWeekendSessionID)

	if err != nil {
		return err
	}

	if !isPracticeSession {
		if !raceWeekend.SessionCanBeRun(session) {
			return ErrRaceWeekendSessionDependencyIncomplete
		}

		session.StartedTime = time.Now()

		if err := rwm.UpsertRaceWeekend(raceWeekend); err != nil {
			return err
		}
	}

	raceWeekendEntryList, err := getRaceWeekendEntryList(session, raceWeekend, nil, "")

	if err != nil {
		return err
	}

	entryList := raceWeekendEntryList.AsEntryList()

	if isPracticeSession && !raceWeekend.SessionCanBeRun(session) {
		// practice sessions run with the whole race weekend entry list if they are not yet available
		entryList = raceWeekend.GetEntryList()
	}

	for k, entrant := range entryList {
		if entrant.IsPlaceHolder {
			// placeholder entrants should not be added to our final entry list
			delete(entryList, k)
			continue
		}

		// look through the user configured entry list and apply any of the options that they set to this entrant.
		for _, raceWeekendEntrant := range raceWeekend.GetEntryList() {
			if raceWeekendEntrant.GUID == entrant.GUID {
				entrant.Model = raceWeekendEntrant.Model
				entrant.Ballast = raceWeekendEntrant.Ballast
				entrant.Restrictor = raceWeekendEntrant.Restrictor
				if entrant.FixedSetup == "" {
					entrant.FixedSetup = raceWeekendEntrant.FixedSetup
				}
				entrant.Skin = raceWeekendEntrant.Skin
				break
			}
		}
	}

	if raceWeekend.HasSpectatorCar() {
		car := raceWeekend.GetSpectatorCar()

		entryList.AddInPitBox(&car, serverconfig.MaxEntryListSize+1)
	}

	if raceWeekend.HasLinkedChampionship() {
		// championship wide race settings only live in this (not persisted) copy of the session.
		session.RaceConfig = raceWeekend.Championship.EffectiveRaceWeekendSessionSetup(raceWeekend.ID, session.RaceConfig)
	}

	session.RaceConfig.MaxClients = len(entryList)
	session.RaceConfig.Cars = strings.Join(entryList.CarIDs(), ";")
	session.RaceConfig.LockedEntryList = 1
	session.RaceConfig.PickupModeEnabled = 1

	// all race weekend sessions must be open so players can join
	for _, acSession := range session.RaceConfig.Sessions {
		acSession.IsOpen = 1
	}

	overridePassword := session.OverridePassword
	replacementPassword := session.ReplacementPassword
	description := fmt.Sprintf("This is a session in the '%s' Race Weekend. Please note that the server will restart between each Race Weekend session.", raceWeekend.Name)

	var championshipID uuid.UUID

	if raceWeekend.HasLinkedChampionship() {
		overridePassword = raceWeekend.Championship.OverridePassword
		replacementPassword = raceWeekend.Championship.ReplacementPassword
		description += string(raceWeekend.Championship.Info)
		championshipID = raceWeekend.ChampionshipID
	}

	raceWeekendRaceEvent := &ActiveRaceWeekend{
		Name:                raceWeekend.Name,
		RaceWeekendID:       raceWeekend.ID,
		SessionID:           session.ID,
		OverridePassword:    overridePassword,
		ReplacementPassword: replacementPassword,
		Description:         description,
		RaceConfig:          session.RaceConfig,
		EntryList:           entryList,
		ChampionshipID:      championshipID,
	}

	if isPracticeSession {
		delete(session.RaceConfig.Sessions, serverconfig.SessionTypePractice)
		delete(session.RaceConfig.Sessions, serverconfig.SessionTypeQualifying)
		delete(session.RaceConfig.Sessions, serverconfig.SessionTypeRace)

		session.RaceConfig.Sessions[serverconfig.SessionTypePractice] = &serverconfig.SessionConfig{
			Name:   "Practice",
			Time:   120,
			IsOpen: 1,
		}

		session.RaceConfig.LoopMode = 1
		session.RaceConfig.ResultScreenTime = 30

		raceWeekendRaceEvent.IsPracticeSession = true
		raceWeekendRaceEvent.RaceConfig = session.RaceConfig
		raceWeekendRaceEvent.EntryList = entryList

		return rwm.raceManager.applyConfigAndStart(raceWeekendRaceEvent)
	}

	return rwm.applyConfigAndStart(raceWeekendRaceEvent)
}

func (rwm *RaceWeekendManager) UDPCallback(message udp.Message) {
	rwm.mutex.Lock()
	defer rwm.mutex.Unlock()

	if !rwm.RaceWeekendSessionIsRunning() {
		return
	}

	if m, ok := message.(udp.EndSession); ok {
		filename := filepath.Base(string(m))
		logrus.Infof("Race Weekend: End session found, result file: %s", filename)

		results, err := LoadResult(filename)

		if err != nil {
			logrus.WithError(err).Errorf("Could not read session results for race weekend: %s, session: %s", rwm.activeRaceWeekend.RaceWeekendID.String(), rwm.activeRaceWeekend.SessionID.String())
			return
		}

		raceWeekend, err := rwm.LoadRaceWeekend(rwm.activeRaceWeekend.RaceWeekendID.String())

		if err != nil {
			logrus.WithError(err).Errorf("Could not load active race weekend")
			return
		}

		session, err := raceWeekend.FindSessionByID(rwm.activeRaceWeekend.SessionID.String())

		if err != nil {
			logrus.WithError(err).Errorf("Could not load active race weekend session")
			return
		}

		session.CompletedTime = time.Now()

		raceWeekend.EnhanceResults(results)

		err = saveResults(filename, results)

		if err != nil {
			logrus.WithError(err).Errorf("Could not update session results %s", filename)
			return
		}

		session.Results = results

		if err := rwm.UpsertRaceWeekend(raceWeekend); err != nil {
			logrus.WithError(err).Errorf("Could not persist race weekend: %s", raceWeekend.ID.String())
			return
		}

		if err := rwm.process.Stop(); err != nil {
			logrus.WithError(err).Error("Could not stop assetto server process")
		}

		if err := rwm.ClearLockedTyreSetups(raceWeekend, session); err != nil {
			logrus.WithError(err).Error("Could not clear previous locked tyres")
		}

		// first, look at siblings of this session and see if they were due to be started
		for _, parent := range session.ParentIDs {
			siblings := raceWeekend.FindChildren(parent.String())

			for _, sibling := range siblings {
				if !sibling.Completed() && sibling.StartWhenParentHasFinished {
					err := rwm.StartSession(raceWeekend.ID.String(), sibling.ID.String(), false)

					if err != nil {
						logrus.WithError(err).Error("Could not start child session")
					}

					return
				}
			}
		}

		// now we can look and see if any child sessions of this session should be started when it finishes
		children := raceWeekend.FindChildren(session.ID.String())

		for _, child := range children {
			if !child.Completed() && child.StartWhenParentHasFinished {
				err := rwm.StartSession(raceWeekend.ID.String(), child.ID.String(), false)

				if err != nil {
					logrus.WithError(err).Error("Could not start child session")
				}

				return
			}
		}
	}
}

func (rwm *RaceWeekendManager) ClearLockedTyreSetups(raceWeekend *RaceWeekend, session *RaceWeekendSession) error {
	matches, err := zglob.Glob(filepath.Join(ServerInstallPath, "setups", "**", lockedTyreSetupFolder, "race_weekend_session_*.ini"))

	if err != nil {
		return err
	}

	for _, match := range matches {
		i, err := ini.Load(match)

		if err != nil {
			return err
		}

		section, err := i.GetSection("RACE_WEEKEND")

		if err != nil {
			return err
		}

		if raceWeekendID, err := section.GetKey("ID"); err == nil && raceWeekendID.String() == raceWeekend.ID.String() {
			if sessionID, err := section.GetKey("SESSION_ID"); err == nil && sessionID.String() == session.ID.String() {
				// this file was from the session that just finished. delete it
				err := os.Remove(match)

				if err != nil {
					return err
				}
			} else if err != nil {
				logrus.WithError(err).Warn("Could not read SessionID from setup file")
				continue
			}
		} else if err != nil {
			logrus.WithError(err).Warn("Could not read RaceWeekendID from setup file")
			continue
		}
	}

	return nil
}

func (rwm *RaceWeekendManager) RestartSession(raceWeekendID string, raceWeekendSessionID string) error {
	err := rwm.CancelSession(raceWeekendID, raceWeekendSessionID)

	if err != nil {
		return err
	}

	return rwm.StartSession(raceWeekendID, raceWeekendSessionID, false)
}

func (rwm *RaceWeekendManager) CancelSession(raceWeekendID string, raceWeekendSessionID string) error {
	raceWeekend, err := rwm.LoadRaceWeekend(raceWeekendID)

	if err != nil {
		return err
	}

	session, err := raceWeekend.FindSessionByID(raceWeekendSessionID)

	if err != nil {
		return err
	}

	session.StartedTime = time.Time{}
	session.CompletedTime = time.Time{}
	session.Results = nil

	if err := rwm.process.Stop(); err != nil {
		return err
	}

	return rwm.UpsertRaceWeekend(raceWeekend)
}

func (rwm *RaceWeekendManager) DeleteSession(raceWeekendID string, raceWeekendSessionID string) error {
	raceWeekend, err := rwm.LoadRaceWeekend(raceWeekendID)

	if err != nil {
		return err
	}

	raceWeekend.DelSession(raceWeekendSessionID)

	return rwm.UpsertRaceWeekend(raceWeekend)
}

var ErrNoActiveRaceWeekendSession = errors.New("servermanager: no active race weekend session")

func (rwm *RaceWeekendManager) RaceWeekendSessionIsRunning() bool {
	return rwm.process.Event().IsRaceWeekend() && !rwm.process.Event().IsPractice() && rwm.activeRaceWeekend != nil
}

func (rwm *RaceWeekendManager) StopActiveSession() error {
	if !rwm.RaceWeekendSessionIsRunning() {
		return ErrNoActiveRaceWeekendSession
	}

	return rwm.CancelSession(rwm.activeRaceWeekend.RaceWeekendID.String(), rwm.activeRaceWeekend.SessionID.String())
}

func (rwm *RaceWeekendManager) RestartActiveSession() error {
	if !rwm.RaceWeekendSessionIsRunning() {
		return ErrNoActiveRaceWeekendSession
	}

	return rwm.RestartSession(rwm.activeRaceWeekend.RaceWeekendID.String(), rwm.activeRaceWeekend.SessionID.String())
}

func (rwm *RaceWeekendManager) ImportSession(raceWeekendID string, raceWeekendSessionID string, r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		return err
	}

	raceWeekend, err := rwm.LoadRaceWeekend(raceWeekendID)

	if err != nil {
		return err
	}

	session, err := raceWeekend.FindSessionByID(raceWeekendSessionID)

	if err != nil {
		return err
	}

	filename := r.FormValue("ResultFile") + ".json"

	session.Results, err = LoadResult(filename)

	if err != nil {
		return err
	}

	raceWeekend.EnhanceResults(session.Results)

	err = saveResults(filename, session.Results)

	if err != nil {
		return err
	}

	session.CompletedTime = session.Results.Date

	return rwm.UpsertRaceWeekend(raceWeekend)
}

func (rwm *RaceWeekendManager) ListAvailableResultsFilesForSorting(raceWeekend *RaceWeekend, session *RaceWeekendSession) ([]smresults.SessionResults, error) {
	results, err := ListAllResults()

	if err != nil {
		return nil, err
	}

	var filteredResults []smresults.SessionResults

	for _, result := range results {

		found := false

	carCheck:
		for _, car := range result.Cars {
			for _, entryListCar := range raceWeekend.GetEntryList().CarIDs() {
				if car.Model == entryListCar {
					// result car found in entry list
					found = true
					break carCheck
				}
			}
		}

		if result.TrackName == session.RaceConfig.Track && result.TrackConfig == session.RaceConfig.TrackLayout && found {
			filteredResults = append(filteredResults, result)
		}
	}

	return filteredResults, nil
}

func (rwm *RaceWeekendManager) ListAvailableResultsFilesForSession(raceWeekendID string, raceWeekendSessionID string) (*RaceWeekendSession, []smresults.SessionResults, error) {
	raceWeekend, err := rwm.LoadRaceWeekend(raceWeekendID)

	if err != nil {
		return nil, nil, err
	}

	session, err := raceWeekend.FindSessionByID(raceWeekendSessionID)

	if err != nil {
		return nil, nil, err
	}

	results, err := ListAllResults()

	if err != nil {
		return nil, nil, err
	}

	var filteredResults []smresults.SessionResults

	for _, result := range results {
		if result.TrackName == session.RaceConfig.Track && result.TrackConfig == session.RaceConfig.TrackLayout {
			filteredResults = append(filteredResults, result)
		}
	}

	return session, filteredResults, nil
}
