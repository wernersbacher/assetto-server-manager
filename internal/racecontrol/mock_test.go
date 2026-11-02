package racecontrol

import (
	"testing"

	smconfig "github.com/wernersbacher/ACPitwall/internal/config"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/server/servertest"
	"github.com/wernersbacher/ACPitwall/pkg/udp"
)

func TestReplayMockSession(t *testing.T) {
	for _, sessionType := range []udp.SessionType{udp.SessionTypePractice, udp.SessionTypeQualifying, udp.SessionTypeRace} {
		t.Run(sessionType.String(), func(t *testing.T) {
			rc := NewRaceControl(&smconfig.Configuration{}, NilBroadcaster{}, NilTrackData{}, servertest.Dummy{}, testStore, NewPenaltiesManager(testStore, testEnv()), testEnv())

			rc.ReplayMockSession(sessionType)

			if rc.SessionInfo.Type != sessionType {
				t.Errorf("session type = %v, want %v", rc.SessionInfo.Type, sessionType)
			}

			if got := rc.ConnectedDrivers.Len(); got != len(mockDrivers) {
				t.Errorf("connected drivers = %d, want %d", got, len(mockDrivers))
			}

			if got := rc.DisconnectedDrivers.Len(); got != 1 {
				t.Errorf("disconnected drivers = %d, want 1", got)
			}

			if len(rc.ChatMessages) == 0 {
				t.Error("no chat messages")
			}

			positions := make(map[int]bool)

			_ = rc.ConnectedDrivers.Each(func(guid udp.DriverGUID, d *smmodel.RaceControlDriver) error {
				car := d.CurrentCar()

				if car.NumLaps == 0 || car.LastLap == 0 {
					t.Errorf("%s has no laps", d.CarInfo.DriverName)
				}

				if d.LastPos == (udp.Vec{}) {
					t.Errorf("%s has no position on the map", d.CarInfo.DriverName)
				}

				if positions[d.Position] {
					t.Errorf("position %d is taken twice", d.Position)
				}

				positions[d.Position] = true

				return nil
			})
		})
	}
}
