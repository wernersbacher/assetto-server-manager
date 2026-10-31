package api_test

import (
	"testing"
	"time"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	"github.com/wernersbacher/ACPitwall/internal/api"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smresults "github.com/wernersbacher/ACPitwall/internal/results"
)

func TestNewChampionshipStandings(t *testing.T) {
	championship := smmodel.NewChampionship("Standings Test")
	class := smmodel.NewChampionshipClass("GT3")
	class.Points.Places = []int{10, 6, 3}
	championship.AddClass(class)

	type driver struct{ guid, team string }

	order := []driver{{"A", "X"}, {"B", "X"}, {"C", "Y"}}
	results := &smresults.SessionResults{}

	for i, d := range order {
		results.Result = append(results.Result, &smresults.SessionResult{
			CarID: i, DriverGUID: d.guid, DriverName: "Driver " + d.guid, TotalTime: 1000 + i, BestLap: 100 + i, ClassID: class.ID,
		})
		results.Cars = append(results.Cars, &smresults.SessionCar{
			CarID: i, Model: "dev_car_gt3", Driver: smresults.SessionDriver{GUID: d.guid, Name: "Driver " + d.guid, Team: d.team, ClassID: class.ID},
		})
		results.Laps = append(results.Laps, &smresults.SessionLap{CarID: i, DriverGUID: d.guid, ClassID: class.ID, LapTime: 100 + i, Timestamp: 100 + i})
	}

	event := smmodel.NewChampionshipEvent()
	event.StartedTime = time.Now().Add(-time.Hour)
	event.CompletedTime = time.Now()
	event.Sessions[serverconfig.SessionTypeRace] = &smmodel.ChampionshipSession{
		StartedTime:   event.StartedTime,
		CompletedTime: event.CompletedTime,
		Results:       results,
	}
	championship.Events = []*smmodel.ChampionshipEvent{event}

	out := api.NewChampionshipStandings(championship)

	if out.ChampionshipName != "Standings Test" || !out.Completed || len(out.Classes) != 1 {
		t.Fatalf("unexpected standings: %+v", out)
	}

	drivers := out.Classes[0].Drivers

	if len(drivers) != 3 || drivers[0].DriverGUID != "A" || drivers[0].Position != 1 || drivers[0].Points != 10 ||
		drivers[0].Team != "X" || drivers[0].CarName == "" || drivers[2].DriverGUID != "C" || drivers[2].Points != 3 {
		t.Errorf("unexpected driver standings: %+v", drivers)
	}

	teams := out.Classes[0].Teams

	if len(teams) != 2 || teams[0].Team != "X" || teams[0].Points != 16 || teams[0].Position != 1 || teams[1].Team != "Y" || teams[1].Points != 3 {
		t.Errorf("unexpected team standings: %+v", teams)
	}
}
