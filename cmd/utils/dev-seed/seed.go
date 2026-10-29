package main

import (
	"fmt"
	"html/template"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	servermanager "github.com/wernersbacher/assetto-server-manager"
	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
)

const (
	manifestMetaKey = "dev_seed_manifest"
	manifestVersion = 1

	devCupName    = "Dev Cup"
	devSprintName = "Dev Sprint Series"
	weekendName   = "Dev Weekend"

	randomSeed = 20260101
)

var seedNamespace = uuid.NewSHA1(uuid.NameSpaceDNS, []byte("dev-seed.assetto-server-manager.local"))

// seedID returns a stable UUID for a seeded object, so that seeding twice always touches the same objects.
func seedID(name string) uuid.UUID {
	return uuid.NewSHA1(seedNamespace, []byte(name))
}

var (
	devCupID       = seedID("championship/" + devCupName)
	devSprintID    = seedID("championship/" + devSprintName)
	weekendID      = seedID("raceweekend/" + weekendName)
	customRaceName = []string{"Dev GT3 Sprint", "Dev MX5 Night Race", "Dev Weekly MX5 Meetup"}
)

// seedManifest is stored in the meta bucket and marks the store as seeded.
type seedManifest struct {
	Version     int
	Seeded      time.Time
	ResultFiles []string
}

// seedSummary counts what has been created.
type seedSummary struct {
	Drivers, ResultFiles, Championships, ChampionshipEvents, CustomRaces, RaceWeekends int
}

func (s seedSummary) String() string {
	return fmt.Sprintf("%d drivers, %d result files, %d championships (%d events), %d custom races, %d race weekends",
		s.Drivers, s.ResultFiles, s.Championships, s.ChampionshipEvents, s.CustomRaces, s.RaceWeekends)
}

// at returns a local time daysFromNow days from now at the given time of day.
func at(now time.Time, daysFromNow, hour, minute int) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day()+daysFromNow, hour, minute, 0, 0, time.Local)
}

func seedEntrant(d *seedDriver) *serverconfig.Entrant {
	return &serverconfig.Entrant{
		InternalUUID: seedID("entrant/" + d.GUID),
		Name:         d.Name,
		Team:         d.Team,
		GUID:         d.GUID,
		Model:        d.Model,
		Skin:         d.Skin,
	}
}

// asModel returns a copy of the driver driving another car.
func asModel(d *seedDriver, model, skin string) *seedDriver {
	c := *d
	c.Model, c.Skin = model, skin

	return &c
}

func entryListOf(drivers []*seedDriver) serverconfig.EntryList {
	entryList := make(serverconfig.EntryList)

	for _, d := range drivers {
		entryList.AddToBackOfGrid(seedEntrant(d))
	}

	return entryList
}

func without(drivers []*seedDriver, missing ...string) []*seedDriver {
	var out []*seedDriver

next:
	for _, d := range drivers {
		for _, m := range missing {
			if d.Name == driverByName(m).Name {
				continue next
			}
		}

		out = append(out, d)
	}

	return out
}

func penalties(secs map[string]int) map[string]time.Duration {
	out := make(map[string]time.Duration)

	for name, s := range secs {
		out[driverByName(name).GUID] = time.Duration(s) * time.Second
	}

	return out
}

func eventSetup(cars []string, track, layout string, raceLaps int) serverconfig.CurrentRaceConfig {
	setup := serverconfig.ConfigIniDefault().CurrentRaceConfig

	setup.Cars = joinCars(cars)
	setup.Track = track
	setup.TrackLayout = layout
	setup.LegalTyres = "SM"
	setup.Sessions = serverconfig.Sessions{
		serverconfig.SessionTypeQualifying: {Name: "Qualify", Time: 10, IsOpen: 1},
		serverconfig.SessionTypeRace:       {Name: "Race", Laps: raceLaps, IsOpen: 1, WaitTime: 60},
	}

	return setup
}

func joinCars(cars []string) string {
	out := ""

	for i, c := range cars {
		if i > 0 {
			out += ";"
		}

		out += c
	}

	return out
}

// seedAll creates everything. The caller has already checked that the store was not seeded yet.
func seedAll(store servermanager.Store, serverID smmodel.ServerID, now time.Time) (*seedManifest, *seedSummary, error) {
	rng := rand.New(rand.NewSource(randomSeed))
	manifest := &seedManifest{Version: manifestVersion, Seeded: now}
	summary := &seedSummary{}

	// 1. drivers
	for _, d := range seedDrivers {
		if err := store.UpsertEntrant(*seedEntrant(d)); err != nil {
			return nil, nil, err
		}

		summary.Drivers++
	}

	// 2. plain results
	if err := seedPlainResults(rng, now, manifest); err != nil {
		return nil, nil, err
	}

	// 3. + 4. championships
	devCup, err := seedDevCup(rng, now, serverID, manifest)

	if err != nil {
		return nil, nil, err
	}

	devSprint, err := seedDevSprint(rng, now, serverID, manifest)

	if err != nil {
		return nil, nil, err
	}

	for _, c := range []*smmodel.Championship{devCup, devSprint} {
		if err := store.UpsertChampionship(c); err != nil {
			return nil, nil, err
		}

		summary.Championships++
		summary.ChampionshipEvents += len(c.Events)
	}

	// 5. custom races
	races, err := seedCustomRaces(now, serverID)

	if err != nil {
		return nil, nil, err
	}

	for _, race := range races {
		if err := store.UpsertCustomRace(race); err != nil {
			return nil, nil, err
		}

		summary.CustomRaces++
	}

	// 6. race weekend
	rw, err := seedRaceWeekend(rng, now, manifest)

	if err != nil {
		return nil, nil, err
	}

	if err := store.UpsertRaceWeekend(rw); err != nil {
		return nil, nil, err
	}

	summary.RaceWeekends++
	summary.ResultFiles = len(manifest.ResultFiles)

	return manifest, summary, nil
}

func seedPlainResults(rng *rand.Rand, now time.Time, manifest *seedManifest) error {
	all := seedDrivers

	specs := []sessionSpec{
		{Track: trackRing, Layout: layoutGP, Type: serverconfig.SessionTypePractice, Start: at(now, -24, 19, 30), Entries: all, Laps: 9, CutChance: 0.08, Collisions: 2},
		{Track: trackRing, Layout: layoutGP, Type: serverconfig.SessionTypeQualifying, Start: at(now, -24, 20, 10), Entries: all, Laps: 6, CutChance: 0.06, Collisions: 1},
		{
			Track: trackRing, Layout: layoutGP, Type: serverconfig.SessionTypeRace, Start: at(now, -24, 20, 35), Entries: all, Laps: 5,
			CutChance: 0.05, Collisions: 5,
			Penalties:    penalties(map[string]int{"Marco": 5, "Jack": 10}),
			Disqualified: map[string]bool{driverByName("Tomasz").GUID: true},
			LapsDown:     map[string]int{driverByName("Sam").GUID: 1},
		},
		{Track: trackRing, Layout: layoutShort, Type: serverconfig.SessionTypePractice, Start: at(now, -17, 18, 5), Entries: all[3:9], Laps: 8, CutChance: 0.1, Collisions: 1},
		{Track: trackOther, Layout: "", Type: serverconfig.SessionTypePractice, Start: at(now, -10, 21, 10), Entries: all, Laps: 10, CutChance: 0.07, Collisions: 3},
		{Track: trackOther, Layout: "", Type: serverconfig.SessionTypeQualifying, Start: at(now, -10, 21, 35), Entries: all, Laps: 6, CutChance: 0.05},
		{
			Track: trackRing, Layout: layoutShort, Type: serverconfig.SessionTypeRace, Start: at(now, -3, 17, 50), Entries: all[:8], Laps: 8,
			CutChance: 0.04, Collisions: 3,
			Penalties: penalties(map[string]int{"Pierre": 10}),
		},
	}

	for _, spec := range specs {
		if _, err := emit(rng, spec, &manifest.ResultFiles, nil); err != nil {
			return err
		}
	}

	return nil
}

type cupEvent struct {
	track, layout string
	daysFromNow   int
	useSettings   bool
	missing       []string
	penalties     map[string]int
	lapsDown      map[string]int
	collisions    int
}

// completedEvent builds a completed championship event with a qualifying and a race session, which are written to
// the results folder and attached the way ChampionshipManager does at the end of a session.
func completedEvent(rng *rand.Rand, now time.Time, c *smmodel.Championship, entries []*seedDriver, cars []string, spec cupEvent, manifest *seedManifest) (*smmodel.ChampionshipEvent, error) {
	event := smmodel.NewChampionshipEvent()
	event.ID = seedID(fmt.Sprintf("event/%s/%s/%d", c.Name, spec.track+spec.layout, spec.daysFromNow))
	event.RaceSetup = eventSetup(cars, spec.track, spec.layout, 6)
	event.UseChampionshipRaceSettings = spec.useSettings

	qualyStart := at(now, spec.daysFromNow, 20, 0)
	raceStart := qualyStart.Add(25 * time.Minute)

	if spec.daysFromNow == -9 {
		qualyStart = at(now, spec.daysFromNow, 19, 0)
		raceStart = qualyStart.Add(25 * time.Minute)
	}

	lapsDown := make(map[string]int)

	for name, n := range spec.lapsDown {
		lapsDown[driverByName(name).GUID] = n
	}

	qualy, err := emit(rng, sessionSpec{
		Track: spec.track, Layout: spec.layout, Type: serverconfig.SessionTypeQualifying, Start: qualyStart,
		Entries: entries, Laps: 6, CutChance: 0.05,
	}, &manifest.ResultFiles, c.EnhanceResults)

	if err != nil {
		return nil, err
	}

	race, err := emit(rng, sessionSpec{
		Track: spec.track, Layout: spec.layout, Type: serverconfig.SessionTypeRace, Start: raceStart,
		Entries: entries, Laps: 6, CutChance: 0.04, Collisions: spec.collisions,
		Penalties: penalties(spec.penalties), LapsDown: lapsDown,
	}, &manifest.ResultFiles, c.EnhanceResults)

	if err != nil {
		return nil, err
	}

	event.StartedTime = qualyStart.Add(-2 * time.Minute)
	event.CompletedTime = raceStart.Add(15 * time.Minute)
	event.Sessions[serverconfig.SessionTypeQualifying] = &smmodel.ChampionshipSession{
		StartedTime: event.StartedTime, CompletedTime: qualyStart.Add(10 * time.Minute), Results: qualy,
	}
	event.Sessions[serverconfig.SessionTypeRace] = &smmodel.ChampionshipSession{
		StartedTime: raceStart.Add(-time.Minute), CompletedTime: event.CompletedTime, Results: race,
	}

	return event, nil
}

func scheduledEvent(c *smmodel.Championship, serverID smmodel.ServerID, cars []string, track, layout string, start time.Time, useSettings bool, rule string) (*smmodel.ChampionshipEvent, error) {
	event := smmodel.NewChampionshipEvent()
	event.ID = seedID(fmt.Sprintf("event/%s/%s/%d", c.Name, track+layout, start.Unix()/86400))
	event.RaceSetup = eventSetup(cars, track, layout, 6)
	event.UseChampionshipRaceSettings = useSettings
	event.Scheduled = start
	event.ScheduledInitial = start
	event.ScheduledServerID = serverID

	if rule != "" {
		if err := event.SetRecurrenceRule(rule); err != nil {
			return nil, err
		}
	}

	return event, nil
}

func newClass(name string, drivers []*seedDriver, cars []string, points smmodel.ChampionshipPoints) *smmodel.ChampionshipClass {
	class := smmodel.NewChampionshipClass(name)
	class.ID = seedID("class/" + name)
	class.Points = points
	class.AvailableCars = cars
	class.DriverPenalties = make(map[string]int)
	class.TeamPenalties = make(map[string]int)

	for _, d := range drivers {
		class.Entrants.AddToBackOfGrid(seedEntrant(d))
	}

	return class
}

func seedDevCup(rng *rand.Rand, now time.Time, serverID smmodel.ServerID, manifest *seedManifest) (*smmodel.Championship, error) {
	c := smmodel.NewChampionship(devCupName)
	c.ID = devCupID
	c.Created = now.AddDate(0, 0, -30)
	c.Info = template.HTML("Seeded by <code>cmd/utils/dev-seed</code>: two classes, drop worst result, championship wide race settings.")
	c.DropWorstResults = 1
	c.RaceSettings = &smmodel.ChampionshipRaceSettings{
		FuelRate:                150,
		DamageMultiplier:        50,
		TyreWearRate:            200,
		AllowedTyresOut:         2,
		ABSAllowed:              1,
		TractionControlAllowed:  1,
		StabilityControlAllowed: 0,
		AutoClutchAllowed:       0,
		TyreBlanketsAllowed:     1,
		ForceVirtualMirror:      1,
		DynamicTrack:            serverconfig.DynamicTrackConfig{SessionStart: 95, Randomness: 2, SessionTransfer: 80, LapGain: 12},
	}

	points := smmodel.DefaultChampionshipPoints
	points.BestLap = 1
	points.PolePosition = 1

	gt3 := driversOfClass("GT3")
	mx5 := driversOfClass("MX5")

	c.AddClass(newClass("GT3", gt3, []string{carGT3}, points))
	c.AddClass(newClass("MX5", mx5, []string{carMX5}, points))

	all := append(append([]*seedDriver{}, gt3...), mx5...)
	cars := []string{carGT3, carMX5}

	completed := []cupEvent{
		{track: trackRing, layout: layoutGP, daysFromNow: -28, useSettings: true, collisions: 4},
		{track: trackRing, layout: layoutShort, daysFromNow: -21, useSettings: false, missing: []string{"Pierre"}, collisions: 3, penalties: map[string]int{"Sofia": 5}},
		{track: trackOther, layout: "", daysFromNow: -14, useSettings: true, missing: []string{"Tomasz"}, collisions: 5, lapsDown: map[string]int{"Sam": 1}},
		{track: trackRing, layout: layoutGP, daysFromNow: -7, useSettings: true, missing: []string{"Sam"}, collisions: 4, penalties: map[string]int{"Jack": 10}},
	}

	for _, spec := range completed {
		event, err := completedEvent(rng, now, c, without(all, spec.missing...), cars, spec, manifest)

		if err != nil {
			return nil, err
		}

		c.Events = append(c.Events, event)
	}

	upcoming, err := scheduledEvent(c, serverID, cars, trackOther, "", at(now, 3, 20, 0), false, "")

	if err != nil {
		return nil, err
	}

	c.Events = append(c.Events, upcoming)

	return c, nil
}

func seedDevSprint(rng *rand.Rand, now time.Time, serverID smmodel.ServerID, manifest *seedManifest) (*smmodel.Championship, error) {
	c := smmodel.NewChampionship(devSprintName)
	c.ID = devSprintID
	c.Created = now.AddDate(0, 0, -12)
	c.Info = template.HTML("Seeded by <code>cmd/utils/dev-seed</code>: single class, no drops, no race settings.")

	var drivers []*seedDriver

	for _, name := range []string{"Tomasz", "Yuki", "Emma", "Pierre", "Carlos", "Sam"} {
		drivers = append(drivers, asModel(driverByName(name), carMX5, "00_white"))
	}

	cars := []string{carMX5}

	c.AddClass(newClass("Sprint", drivers, cars, smmodel.DefaultChampionshipPoints))

	first, err := completedEvent(rng, now, c, drivers, cars, cupEvent{track: trackRing, layout: layoutShort, daysFromNow: -9, collisions: 3}, manifest)

	if err != nil {
		return nil, err
	}

	c.Events = append(c.Events, first)

	weekly, err := scheduledEvent(c, serverID, cars, trackOther, "", at(now, 5, 19, 0), false, "FREQ=WEEKLY")

	if err != nil {
		return nil, err
	}

	later, err := scheduledEvent(c, serverID, cars, trackRing, layoutGP, at(now, 12, 19, 0), false, "")

	if err != nil {
		return nil, err
	}

	c.Events = append(c.Events, weekly, later)

	return c, nil
}

func seedCustomRaces(now time.Time, serverID smmodel.ServerID) ([]*smmodel.CustomRace, error) {
	gt3 := driversOfClass("GT3")
	mx5 := driversOfClass("MX5")

	newRace := func(name string, cars []string, track, layout string, drivers []*seedDriver) *smmodel.CustomRace {
		setup := serverconfig.ConfigIniDefault().CurrentRaceConfig
		setup.Cars = joinCars(cars)
		setup.Track = track
		setup.TrackLayout = layout
		setup.LegalTyres = "SM"

		return &smmodel.CustomRace{
			Name:          name,
			HasCustomName: true,
			Created:       now.AddDate(0, 0, -5),
			Updated:       now.AddDate(0, 0, -1),
			UUID:          seedID("customrace/" + name),
			RaceConfig:    setup,
			EntryList:     entryListOf(drivers),
		}
	}

	starred := newRace(customRaceName[0], []string{carGT3}, trackRing, layoutGP, gt3)
	starred.Starred = true

	tomorrow := newRace(customRaceName[1], []string{carMX5}, trackOther, "", mx5)
	weekly := newRace(customRaceName[2], []string{carMX5}, trackRing, layoutShort, mx5)

	for _, s := range []struct {
		race *smmodel.CustomRace
		when time.Time
		rule string
	}{
		{tomorrow, at(now, 1, 19, 0), ""},
		{weekly, at(now, 2, 20, 30), "FREQ=WEEKLY"},
	} {
		s.race.Scheduled = s.when
		s.race.ScheduledInitial = s.when
		s.race.ScheduledServerID = serverID

		if s.rule != "" {
			if err := s.race.SetRecurrenceRule(s.rule); err != nil {
				return nil, err
			}
		}

		s.race.ScheduledEvents = map[smmodel.ServerID]*smmodel.ScheduledEventBase{
			serverID: {
				Scheduled:         s.race.Scheduled,
				ScheduledInitial:  s.race.ScheduledInitial,
				Recurrence:        s.race.Recurrence,
				ScheduledServerID: serverID,
			},
		}
	}

	return []*smmodel.CustomRace{starred, tomorrow, weekly}, nil
}

func seedRaceWeekend(rng *rand.Rand, now time.Time, manifest *seedManifest) (*smmodel.RaceWeekend, error) {
	rw := smmodel.NewRaceWeekend()
	rw.ID = weekendID
	rw.Name = weekendName
	rw.Created = now.AddDate(0, 0, -2)
	rw.EntryList = entryListOf(seedDrivers)

	cars := []string{carGT3, carMX5}

	newSession := func(name string, sessionType serverconfig.SessionType, laps, minutes int, parent uuid.UUID, sortType string) *smmodel.RaceWeekendSession {
		s := smmodel.NewRaceWeekendSession()
		s.ID = seedID("raceweekend/session/" + name)
		s.Created = rw.Created
		s.ParentIDs = []uuid.UUID{parent}
		s.SortType = sortType

		s.RaceConfig = eventSetup(cars, trackRing, layoutGP, laps)
		s.RaceConfig.Sessions = serverconfig.Sessions{
			sessionType: {Name: name, Time: minutes, Laps: laps, IsOpen: 1},
		}

		return s
	}

	practice := newSession("Practice", serverconfig.SessionTypePractice, 0, 20, rw.ID, "")
	qualy := newSession("Qualify", serverconfig.SessionTypeQualifying, 0, 10, practice.ID, "fastest_lap")
	race := newSession("Race", serverconfig.SessionTypeRace, 6, 0, qualy.ID, "fastest_lap")

	start := at(now, -1, 19, 0)

	results, err := emit(rng, sessionSpec{
		Track: trackRing, Layout: layoutGP, Type: serverconfig.SessionTypePractice, Start: start,
		Entries: seedDrivers, Laps: 8, CutChance: 0.06, Collisions: 2,
	}, &manifest.ResultFiles, rw.EnhanceResults)

	if err != nil {
		return nil, err
	}

	practice.StartedTime = start.Add(-time.Minute)
	practice.CompletedTime = start.Add(20 * time.Minute)
	practice.Results = results

	rw.AddSession(practice, nil)
	rw.AddSession(qualy, nil)
	rw.AddSession(race, nil)

	// AddSession(nil) does not set parents, they have been configured above.
	return rw, nil
}

// removeResultFiles deletes the result files of a previous seed run.
func removeResultFiles(files []string) int {
	removed := 0

	for _, f := range files {
		if err := os.Remove(filepath.Join(servermanager.ServerInstallPath, "results", f)); err == nil {
			removed++
		}
	}

	return removed
}
