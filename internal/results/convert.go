package results

import "github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"

// AssignEntrantFromResult sets the entrant's properties from a session result and car.
func AssignEntrantFromResult(e *serverconfig.Entrant, result *SessionResult, car *SessionCar) {
	e.Name = result.DriverName
	e.Team = car.Driver.Team
	e.GUID = result.DriverGUID
	e.Model = result.CarModel
	e.Skin = car.Skin
	e.Restrictor = car.Restrictor
	e.Ballast = car.BallastKG
}

// EntrantAsSessionCar converts an entrant to a session car.
func EntrantAsSessionCar(e *serverconfig.Entrant) *SessionCar {
	return &SessionCar{
		BallastKG: e.Ballast,
		CarID:     e.PitBox,
		Driver: SessionDriver{
			GUID:      e.GUID,
			GuidsList: []string{e.GUID},
			Name:      e.Name,
			Team:      e.Team,
		},
		Model:      e.Model,
		Restrictor: e.Restrictor,
		Skin:       e.Skin,
	}
}

// EntrantAsSessionResult converts an entrant to a session result.
func EntrantAsSessionResult(e *serverconfig.Entrant) *SessionResult {
	return &SessionResult{
		BallastKG:  e.Ballast,
		CarID:      e.PitBox,
		CarModel:   e.Model,
		DriverGUID: e.GUID,
		DriverName: e.Name,
		Restrictor: e.Restrictor,
	}
}
