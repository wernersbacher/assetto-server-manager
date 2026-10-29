package servermanager

import (
	"errors"
	"io/ioutil"
	"path/filepath"
	"sort"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"
	"github.com/wernersbacher/assetto-server-manager/pkg/udp"
)

func (cm *ChampionshipManager) ChampionshipEventCallback(message udp.Message) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	if !cm.ChampionshipEventIsRunning() {
		return
	}

	championship, err := cm.LoadChampionship(cm.activeChampionship.ChampionshipID.String())

	if err != nil {
		logrus.WithError(err).Errorf("Couldn't load championship with ID: %s", cm.activeChampionship.ChampionshipID.String())
		return
	}

	currentEventIndex := -1

	for index, event := range championship.Events {
		if event.ID == cm.activeChampionship.EventID {
			currentEventIndex = index
			break
		}
	}

	if currentEventIndex < 0 {
		logrus.Errorf("Couldn't load championship event with given id")
		return
	}

	cm.handleSessionChanges(message, championship, currentEventIndex)
}

type sessionEntrantWrapper udp.SessionCarInfo

func (s sessionEntrantWrapper) GetName() string {
	return s.DriverName
}

func (s sessionEntrantWrapper) GetCar() string {
	return s.CarModel
}

func (s sessionEntrantWrapper) GetSkin() string {
	return s.CarSkin
}

func (s sessionEntrantWrapper) GetGUID() string {
	return string(s.DriverGUID)
}

func (s sessionEntrantWrapper) GetTeam() string {
	return ""
}

func (cm *ChampionshipManager) handleSessionChanges(message udp.Message, championship *Championship, currentEventIndex int) {
	if championship.Events[currentEventIndex].Completed() {
		logrus.Infof("Event is complete. Ignoring messages")
		return
	}

	saveChampionship := true

	defer func() {
		if !saveChampionship {
			return
		}

		err := cm.UpsertChampionship(championship)

		if err != nil {
			logrus.WithError(err).Errorf("Could not save session results to championship %s", cm.activeChampionship.ChampionshipID.String())
			return
		}
	}()

	switch a := message.(type) {

	case udp.SessionCarInfo:
		if championship.OpenEntrants && championship.PersistOpenEntrants && a.Event() == udp.EventNewConnection {
			if championship.HasSpectatorCar() && championship.SpectatorCar.GUID == string(a.DriverGUID) {
				// don't try and add the spectator car to the entrylist.
				return
			}

			// a person joined, check to see if they need adding to the championship
			foundSlot, classForCar, err := cm.AddEntrantFromSessionData(championship, sessionEntrantWrapper(a), false, false)

			if err != nil {
				saveChampionship = false
				logrus.WithError(err).WithField("entrant", a).Errorf("could not add entrant to open championship")

				return
			}

			saveChampionship = foundSlot

			if !foundSlot {
				logrus.Errorf("Could not find free entrant slot in class: %s for %s (%s)", classForCar.Name, a.DriverName, a.DriverGUID)
				return
			}
		}

	case udp.SessionInfo:
		if a.Event() == udp.EventNewSession {
			if championship.Events[currentEventIndex].StartedTime.IsZero() {
				championship.Events[currentEventIndex].StartedTime = time.Now()
			}

			if championship.Events[currentEventIndex].Sessions == nil {
				championship.Events[currentEventIndex].Sessions = make(map[serverconfig.SessionType]*ChampionshipSession)
			}

			// new session created
			logrus.Infof("New Session: %s at %s (%s) - %d laps | %d minutes", a.Name, a.Track, a.TrackConfig, a.Laps, a.Time)
			sessionType, err := cm.findSessionWithName(championship.Events[currentEventIndex], a.Name)

			if err != nil {
				logrus.Errorf("Unexpected session: %s. Cannot track championship progress for this session", a.Name)
				return
			}

			_, ok := championship.Events[currentEventIndex].Sessions[sessionType]

			if !ok {
				championship.Events[currentEventIndex].Sessions[sessionType] = &ChampionshipSession{
					StartedTime: time.Now(),
				}
			}

			previousSessionType := cm.activeChampionship.SessionType
			previousSession, hasPreviousSession := championship.Events[currentEventIndex].Sessions[previousSessionType]
			previousSessionNumLaps := cm.activeChampionship.NumLapsCompleted

			defer func() {
				if !cm.ChampionshipEventIsRunning() {
					return
				}

				cm.activeChampionship.NumLapsCompleted = 0
				cm.activeChampionship.SessionType = sessionType
			}()

			if hasPreviousSession && previousSessionType != sessionType && previousSessionNumLaps > 0 && !previousSession.StartedTime.IsZero() && previousSession.CompletedTime.IsZero() {
				resultsFile, err := cm.findLastWrittenSessionFile()

				if err == nil {
					logrus.Infof("Assetto Server didn't give us a results file for the session: %s, but we found it at %s", cm.activeChampionship.SessionType.String(), resultsFile)
					cm.handleSessionChanges(udp.EndSession(resultsFile), championship, currentEventIndex)
					return
				}
			}
		} else {
			saveChampionship = false
		}
	case udp.LapCompleted:
		cm.activeChampionship.NumLapsCompleted++

	case udp.EndSession:
		filename := filepath.Base(string(a))
		logrus.Infof("End Session, file outputted at: %s", filename)

		results, err := LoadResult(filename)

		if err != nil {
			logrus.WithError(err).Errorf("Could not read session results for %s", cm.activeChampionship.SessionType.String())
			return
		}

		// Update the old results json file with more championship information, required for applying penalties properly
		championship.EnhanceResults(results)
		err = saveResults(filename, results)

		if err != nil {
			logrus.WithError(err).Errorf("Could not update session results for %s", cm.activeChampionship.SessionType.String())
			return
		}

		currentSession, ok := championship.Events[currentEventIndex].Sessions[cm.activeChampionship.SessionType]

		if ok {
			currentSession.CompletedTime = time.Now()
			currentSession.Results = results
		} else {
			logrus.Errorf("Received and EndSession with no matching NewSession")
			return
		}

		lastSession := championship.Events[currentEventIndex].LastSession()

		if cm.activeChampionship.SessionType == lastSession {
			logrus.Infof("End of %s Session detected. Marking championship event %s complete", lastSession.String(), cm.activeChampionship.EventID.String())
			championship.Events[currentEventIndex].CompletedTime = time.Now()

			// clear out all current session stuff
			cm.activeChampionship = nil

			// stop the server
			err := cm.process.Stop()

			if err != nil {
				logrus.WithError(err).Errorf("Could not stop Assetto Process")
			}
		}
	default:
		saveChampionship = false
		return
	}
}

var (
	ErrSessionNotFound    = errors.New("servermanager: session not found")
	ErrResultFileNotFound = errors.New("servermanager: results files not found")
)

func (cm *ChampionshipManager) findSessionWithName(event *ChampionshipEvent, name string) (serverconfig.SessionType, error) {
	for t, sess := range event.RaceSetup.Sessions {
		if sess.Name == name {
			savedRace, hasSavedRace := event.Sessions[serverconfig.SessionTypeRace]

			if t == serverconfig.SessionTypeRace && event.RaceSetup.HasMultipleRaces() && hasSavedRace && savedRace.Completed() {
				// this is a second race session
				return serverconfig.SessionTypeSecondRace, nil
			}

			return t, nil
		}
	}

	return "", ErrSessionNotFound
}

func (cm *ChampionshipManager) findLastWrittenSessionFile() (string, error) {
	resultsPath := filepath.Join(ServerInstallPath, "results")
	resultFiles, err := ioutil.ReadDir(resultsPath)

	if err != nil {
		return "", err
	}

	sort.Slice(resultFiles, func(i, j int) bool {
		return resultFiles[i].ModTime().After(resultFiles[j].ModTime())
	})

	if len(resultFiles) == 0 {
		return "", ErrResultFileNotFound
	}

	return resultFiles[0].Name(), nil
}

func (cm *ChampionshipManager) CancelEvent(championshipID string, eventID string) error {
	championship, err := cm.LoadChampionship(championshipID)

	if err != nil {
		return err
	}

	event, _, err := championship.EventByID(eventID)

	if err != nil {
		return err
	}

	event.StartedTime = time.Time{}
	event.CompletedTime = time.Time{}

	event.Sessions = make(map[serverconfig.SessionType]*ChampionshipSession)

	if err := cm.process.Stop(); err != nil {
		return err
	}

	return cm.UpsertChampionship(championship)
}

func (cm *ChampionshipManager) RestartEvent(championshipID string, eventID string) error {
	err := cm.CancelEvent(championshipID, eventID)

	if err != nil {
		return err
	}

	return cm.StartEvent(championshipID, eventID, false)
}

var ErrNoActiveChampionshipEvent = errors.New("servermanager: no active championship event")

func (cm *ChampionshipManager) ChampionshipEventIsRunning() bool {
	return cm.process.Event().IsChampionship() && !cm.process.Event().IsPractice() && cm.activeChampionship != nil
}

func (cm *ChampionshipManager) RestartActiveEvent() error {
	if !cm.ChampionshipEventIsRunning() {
		return ErrNoActiveChampionshipEvent
	}

	return cm.RestartEvent(cm.activeChampionship.ChampionshipID.String(), cm.activeChampionship.EventID.String())
}

func (cm *ChampionshipManager) StopActiveEvent() error {
	if !cm.ChampionshipEventIsRunning() {
		return ErrNoActiveChampionshipEvent
	}

	return cm.CancelEvent(cm.activeChampionship.ChampionshipID.String(), cm.activeChampionship.EventID.String())
}

func (cm *ChampionshipManager) ListAvailableResultsFilesForEvent(championshipID string, eventID string) (*ChampionshipEvent, []smresults.SessionResults, error) {
	championship, err := cm.LoadChampionship(championshipID)

	if err != nil {
		return nil, nil, err
	}

	event, _, err := championship.EventByID(eventID)

	if err != nil {
		return nil, nil, err
	}

	results, err := ListAllResults()

	if err != nil {
		return nil, nil, err
	}

	var filteredResults []smresults.SessionResults

	for _, result := range results {
		if result.TrackName == event.RaceSetup.Track && result.TrackConfig == event.RaceSetup.TrackLayout {
			filteredResults = append(filteredResults, result)
		}
	}

	return event, filteredResults, nil
}
