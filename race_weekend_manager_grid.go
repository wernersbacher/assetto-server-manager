package servermanager

import (
	"fmt"
)

type RaceWeekendGridPreview struct {
	Results map[int]SessionPreviewEntrant
	Grid    map[int]SessionPreviewEntrant
	Classes map[string]string
}

type SessionPreviewEntrant struct {
	Name       string
	Session    string
	Class      string
	ClassColor string
}

func NewRaceWeekendGridPreview() *RaceWeekendGridPreview {
	return &RaceWeekendGridPreview{
		Results: make(map[int]SessionPreviewEntrant),
		Grid:    make(map[int]SessionPreviewEntrant),
		Classes: make(map[string]string),
	}
}

func (rwm *RaceWeekendManager) PreviewGrid(raceWeekendID, parentSessionID, childSessionID string, filter *RaceWeekendSessionToSessionFilter) (*RaceWeekendGridPreview, error) {
	raceWeekend, parentSession, childSession, err := rwm.FindConnectedSessions(raceWeekendID, parentSessionID, childSessionID)

	if err != nil {
		return nil, err
	}

	preview := NewRaceWeekendGridPreview()

	finishingGrid, err := parentSession.FinishingGrid(raceWeekend)

	if err != nil {
		return nil, err
	}

	for i, result := range finishingGrid {
		class := result.ChampionshipClass(raceWeekend)

		color, ok := preview.Classes[class.Name]

		if !ok {
			color = ChampionshipClassColor(len(preview.Classes))
			preview.Classes[class.Name] = color
		}

		preview.Results[i+1] = SessionPreviewEntrant{
			Name:       result.Car.GetName(),
			Session:    parentSession.Name(),
			Class:      class.Name,
			ClassColor: color,
		}
	}

	entryList, err := childSession.GetRaceWeekendEntryList(raceWeekend, filter, parentSessionID)

	if err != nil {
		return nil, err
	}

	for _, entrant := range entryList.Sorted() {
		sess, err := raceWeekend.FindSessionByID(entrant.SessionID.String())

		if err != nil {
			continue
		}

		class := entrant.ChampionshipClass(raceWeekend)

		color, ok := preview.Classes[class.Name]

		if !ok {
			color = ChampionshipClassColor(len(preview.Classes))
			preview.Classes[class.Name] = color
		}

		preview.Grid[entrant.PitBox+1] = SessionPreviewEntrant{
			Name:       fmt.Sprintf("%s (%s)", entrant.Car.GetName(), sess.Name()),
			Session:    sess.Name(),
			Class:      class.Name,
			ClassColor: color,
		}
	}

	return preview, nil
}

func (rwm *RaceWeekendManager) UpdateGrid(raceWeekendID, parentSessionID, childSessionID string, filter *RaceWeekendSessionToSessionFilter) error {
	raceWeekend, err := rwm.LoadRaceWeekend(raceWeekendID)

	if err != nil {
		return err
	}

	raceWeekend.AddFilter(parentSessionID, childSessionID, filter)

	return rwm.UpsertRaceWeekend(raceWeekend)
}

func (rwm *RaceWeekendManager) PreviewSessionEntryList(raceWeekendID, sessionID, sortType string, reverseGrid int) (*RaceWeekendGridPreview, error) {
	raceWeekend, session, err := rwm.FindSession(raceWeekendID, sessionID)

	if err != nil {
		return nil, err
	}

	session.SortType = sortType
	session.NumEntrantsToReverse = reverseGrid

	entryList, err := session.GetRaceWeekendEntryList(raceWeekend, nil, "")

	if err != nil {
		return nil, err
	}

	preview := NewRaceWeekendGridPreview()

	for i, entrant := range entryList.Sorted() {
		entrantSession, err := raceWeekend.FindSessionByID(entrant.SessionID.String())

		if err != nil {
			continue
		}

		entrantPositionText := "No Time"

		if entrantSession.Completed() {
			for i, result := range entrantSession.Results.Result {
				if result.DriverGUID == entrant.Car.Driver.GUID {
					entrantPositionText = fmt.Sprintf("%d%s", i+1, ordinal(int64(i+1)))
					break
				}
			}
		}

		class := entrant.ChampionshipClass(raceWeekend)

		color, ok := preview.Classes[class.Name]

		if !ok {
			color = ChampionshipClassColor(len(preview.Classes))
			preview.Classes[class.Name] = color
		}

		preview.Grid[i+1] = SessionPreviewEntrant{
			Name:       fmt.Sprintf("%s (%s - %s)", entrant.Car.GetName(), entrantSession.Name(), entrantPositionText),
			Session:    session.Name(),
			Class:      class.Name,
			ClassColor: color,
		}
	}

	return preview, nil
}

func (rwm *RaceWeekendManager) UpdateSessionSorting(raceWeekendID, sessionID string, sortType string, numEntrantsToReverse int) error {
	raceWeekend, session, err := rwm.FindSession(raceWeekendID, sessionID)

	if err != nil {
		return err
	}

	session.SortType = sortType
	session.NumEntrantsToReverse = numEntrantsToReverse

	return rwm.UpsertRaceWeekend(raceWeekend)
}
