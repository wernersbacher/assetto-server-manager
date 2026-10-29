package servermanager

import (
	"fmt"
)

type PenaltyAction string

const (
	SetPenalty    PenaltyAction = "set"
	RemovePenalty PenaltyAction = "remove"
)

func (cm *ChampionshipManager) ModifyDriverPenalty(championshipID, classID, driverGUID string, action PenaltyAction, penalty int) error {
	championship, err := cm.LoadChampionship(championshipID)

	if err != nil {
		return err
	}

	class, err := championship.ClassByID(classID)

	if err != nil {
		return err
	}

	if class.DriverPenalties == nil {
		class.DriverPenalties = make(map[string]int)
	}

	switch action {
	case SetPenalty:
		class.DriverPenalties[driverGUID] = penalty
	case RemovePenalty:
		delete(class.DriverPenalties, driverGUID)
	default:
		return fmt.Errorf("servermanager: invalid penalty action specified: '%s'", action)
	}

	return cm.UpsertChampionship(championship)
}

func (cm *ChampionshipManager) ModifyTeamPenalty(championshipID, classID, team string, action PenaltyAction, penalty int) error {
	championship, err := cm.LoadChampionship(championshipID)

	if err != nil {
		return err
	}

	class, err := championship.ClassByID(classID)

	if err != nil {
		return err
	}

	if class.TeamPenalties == nil {
		class.TeamPenalties = make(map[string]int)
	}

	switch action {
	case SetPenalty:
		class.TeamPenalties[team] = penalty
	case RemovePenalty:
		delete(class.TeamPenalties, team)
	default:
		return fmt.Errorf("servermanager: invalid penalty action specified: '%s'", action)
	}

	return cm.UpsertChampionship(championship)
}
