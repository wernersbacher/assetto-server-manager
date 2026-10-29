package servermanager

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
)

type SessionResult struct {
	BallastKG    int           `json:"BallastKG"`
	BestLap      int           `json:"BestLap"`
	CarID        int           `json:"CarId"`
	CarModel     string        `json:"CarModel"`
	DriverGUID   string        `json:"DriverGuid"`
	DriverName   string        `json:"DriverName"`
	Restrictor   int           `json:"Restrictor"`
	TotalTime    int           `json:"TotalTime"`
	HasPenalty   bool          `json:"HasPenalty"`
	PenaltyTime  time.Duration `json:"PenaltyTime"`
	LapPenalty   int           `json:"LapPenalty"`
	Disqualified bool          `json:"Disqualified"`
	ClassID      uuid.UUID     `json:"ClassID"`
}

func (s *SessionResult) BestLapTyre(results *SessionResults) string {
	for _, lap := range results.Laps {
		if lap.CarID == s.CarID && lap.LapTime == s.BestLap {
			return lap.Tyre
		}
	}

	return "?"
}

type SessionLap struct {
	BallastKG  int       `json:"BallastKG"`
	CarID      int       `json:"CarId"`
	CarModel   string    `json:"CarModel"`
	Cuts       int       `json:"Cuts"`
	DriverGUID string    `json:"DriverGuid"`
	DriverName string    `json:"DriverName"`
	LapTime    int       `json:"LapTime"`
	Restrictor int       `json:"Restrictor"`
	Sectors    []int     `json:"Sectors"`
	Timestamp  int       `json:"Timestamp"`
	Tyre       string    `json:"Tyre"`
	ClassID    uuid.UUID `json:"ClassID"`
}

func (sl *SessionLap) GetSector(x int) time.Duration {
	d, _ := time.ParseDuration(fmt.Sprintf("%dms", sl.Sectors[x]))

	return d
}

func (sl *SessionLap) GetLapTime() time.Duration {
	d, _ := time.ParseDuration(fmt.Sprintf("%dms", sl.LapTime))

	return d
}

func (sl *SessionLap) DidCheat(averageTime time.Duration) bool {
	d, _ := time.ParseDuration(fmt.Sprintf("%dms", sl.LapTime))

	return d < averageTime && sl.Cuts > 0
}

type SessionCar struct {
	BallastKG  int           `json:"BallastKG"`
	CarID      int           `json:"CarId"`
	Driver     SessionDriver `json:"Driver"`
	Model      string        `json:"Model"`
	Restrictor int           `json:"Restrictor"`
	Skin       string        `json:"Skin"`
}

func (c *SessionCar) GetName() string {
	return c.Driver.Name
}

func (c *SessionCar) GetCar() string {
	return c.Model
}

func (c *SessionCar) GetSkin() string {
	return c.Skin
}

func (c *SessionCar) GetGUID() string {
	return c.Driver.GUID
}

func (c *SessionCar) GetTeam() string {
	return c.Driver.Team
}

func (c *SessionCar) HasMultipleDrivers() bool {
	return len(c.Driver.GuidsList) > 1 || strings.Count(c.GetGUID(), serverconfig.DriverSwapEntrantSeparator) > 0
}

type SessionEvent struct {
	CarID         int            `json:"CarId"`
	Driver        *SessionDriver `json:"Driver"`
	ImpactSpeed   float64        `json:"ImpactSpeed"`
	OtherCarID    int            `json:"OtherCarId"`
	OtherDriver   *SessionDriver `json:"OtherDriver"`
	RelPosition   *SessionPos    `json:"RelPosition"`
	Type          string         `json:"Type"`
	WorldPosition *SessionPos    `json:"WorldPosition"`
}

func (se *SessionEvent) GetRelPosition() string {
	return fmt.Sprintf("X: %.1f Y: %.1f Z: %.1f", se.RelPosition.X, se.RelPosition.Y, se.RelPosition.Z)
}

func (se *SessionEvent) GetWorldPosition() string {
	return fmt.Sprintf("X: %.1f Y: %.1f Z: %.1f", se.WorldPosition.X, se.WorldPosition.Y, se.WorldPosition.Z)
}

type SessionDriver struct {
	GUID      string    `json:"Guid"`
	GuidsList []string  `json:"GuidsList"`
	Name      string    `json:"Name"`
	Nation    string    `json:"Nation"`
	Team      string    `json:"Team"`
	ClassID   uuid.UUID `json:"ClassID"`
}

func (sd *SessionDriver) AssignEntrant(entrant *serverconfig.Entrant, classID uuid.UUID) {
	if sd.GUID != entrant.GUID {
		return
	}

	sd.Name = entrant.Name
	sd.Team = entrant.Team
	sd.ClassID = classID
}

type SessionPos struct {
	X float64 `json:"X"`
	Y float64 `json:"Y"`
	Z float64 `json:"Z"`
}
