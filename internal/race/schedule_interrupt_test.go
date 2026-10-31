package race

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	logrustest "github.com/sirupsen/logrus/hooks/test"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/racecontrol"
	"github.com/wernersbacher/ACPitwall/internal/server/servertest"
	"github.com/wernersbacher/ACPitwall/pkg/udp"
)

func TestCanInterruptForSchedule(t *testing.T) {
	for _, tc := range []struct {
		name     string
		session  udp.SessionType
		drivers  int
		practice bool
		want     bool
	}{
		{"practice with drivers", udp.SessionTypePractice, 5, false, true},
		{"practice without drivers", udp.SessionTypePractice, 0, false, true},
		{"practice event in qualifying with drivers", udp.SessionTypeQualifying, 3, true, true},
		{"qualifying with drivers", udp.SessionTypeQualifying, 1, false, false},
		{"qualifying without drivers", udp.SessionTypeQualifying, 0, false, true},
		{"race with drivers", udp.SessionTypeRace, 12, false, false},
		{"race without drivers", udp.SessionTypeRace, 0, false, true},
		{"booking with drivers", udp.SessionTypeBooking, 2, false, false},
		{"booking without drivers", udp.SessionTypeBooking, 0, false, true},
	} {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			got, reason := CanInterruptForSchedule(tc.session, tc.drivers, tc.practice)

			if got != tc.want {
				t.Errorf("CanInterruptForSchedule(%v, %d, %t) = %t, want %t", tc.session, tc.drivers, tc.practice, got, tc.want)
			}

			if reason == "" {
				t.Error("expected a reason")
			}
		})
	}
}

func scheduledStartFixture(t *testing.T, session udp.SessionType, drivers int) (*RaceManager, *servertest.Interruptible, *smmodel.CustomRace, func()) {
	rm, store, cleanup := setupTestRaceManager(t)

	process := &servertest.Interruptible{}
	rm.process = process

	if err := process.Start(&smmodel.CustomRace{Name: "Running Race", HasCustomName: true}, "", 0, "", 0); err != nil {
		t.Fatal(err)
	}

	rm.raceControl.SessionInfo.Type = session

	for i := 0; i < drivers; i++ {
		driver := racecontrol.NewRaceControlDriver(udp.SessionCarInfo{DriverGUID: udp.DriverGUID(uuid.New().String()), CarID: udp.CarID(i)})
		rm.raceControl.ConnectedDrivers.Add(driver.CarInfo.DriverGUID, driver)
	}

	race := &smmodel.CustomRace{
		Name:            "Scheduled Race",
		HasCustomName:   true,
		UUID:            uuid.New(),
		RaceConfig:      serverconfig.ConfigIniDefault().CurrentRaceConfig,
		EntryList:       serverconfig.EntryList{},
		ScheduledEvents: make(map[smmodel.ServerID]*smmodel.ScheduledEventBase),
	}

	race.Scheduled = time.Now()

	if err := store.UpsertCustomRace(race); err != nil {
		t.Fatal(err)
	}

	return rm, process, race, cleanup
}

func TestRaceManager_ScheduledStartSkippedWhileRaceHasDrivers(t *testing.T) {
	rm, process, race, cleanup := scheduledStartFixture(t, udp.SessionTypeRace, 2)
	defer cleanup()

	hook := logrustest.NewGlobal()
	defer hook.Reset()

	if err := rm.StartScheduledRace(race); err != nil {
		t.Fatal(err)
	}

	if len(process.Started) != 1 || process.Stopped != 0 {
		t.Errorf("the running event must not be replaced, started %d, stopped %d", len(process.Started), process.Stopped)
	}

	var warned bool

	for _, entry := range hook.AllEntries() {
		if entry.Level == logrus.WarnLevel && strings.Contains(entry.Message, "Scheduled Race") && strings.Contains(entry.Message, "2 driver(s) online") {
			warned = true
		}
	}

	if !warned {
		t.Error("expected a warning naming the scheduled event and the drivers online")
	}

	stored, err := rm.store.FindCustomRaceByID(race.UUID.String())

	if err != nil {
		t.Fatal(err)
	}

	if !stored.Scheduled.IsZero() {
		t.Error("expected the schedule to be cleared after the skipped start")
	}
}

func TestRaceManager_ScheduledStartInterruptsPractice(t *testing.T) {
	rm, process, race, cleanup := scheduledStartFixture(t, udp.SessionTypePractice, 3)
	defer cleanup()

	hook := logrustest.NewGlobal()
	defer hook.Reset()

	if err := rm.StartScheduledRace(race); err != nil {
		t.Fatal(err)
	}

	if len(process.Started) != 2 {
		t.Fatalf("expected the scheduled race to be started, started %d events", len(process.Started))
	}

	var logged bool

	for _, entry := range hook.AllEntries() {
		if entry.Level == logrus.InfoLevel && strings.Contains(entry.Message, "Scheduled Race") && strings.Contains(entry.Message, "3 driver(s) online") {
			logged = true
		}
	}

	if !logged {
		t.Error("expected an info message about ending the running session")
	}
}
