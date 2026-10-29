package servermanager

import (
	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smconfig "github.com/wernersbacher/assetto-server-manager/internal/config"
	smcontent "github.com/wernersbacher/assetto-server-manager/internal/content"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	"github.com/wernersbacher/assetto-server-manager/internal/notification/notificationtest"
	smrace "github.com/wernersbacher/assetto-server-manager/internal/race"
	racecontrol "github.com/wernersbacher/assetto-server-manager/internal/racecontrol"
	"github.com/wernersbacher/assetto-server-manager/internal/server/servertest"
	smstore "github.com/wernersbacher/assetto-server-manager/internal/store"

	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wernersbacher/assetto-server-manager/pkg/udp"
	"github.com/wernersbacher/assetto-server-manager/pkg/udp/replay"

	"github.com/etcd-io/bbolt"
)

var TestEntryList = serverconfig.EntryList{
	"CAR_0": {
		Name:  "",
		GUID:  "",
		Team:  "Team 1",
		Model: "rss_formula_rss_4",
		Skin:  "",
	},
	"CAR_1": {
		Name:  "",
		GUID:  "",
		Team:  "Team 1",
		Model: "rss_formula_rss_4",
		Skin:  "",
	},
	"CAR_2": {
		Name:  "",
		GUID:  "",
		Team:  "Team 2",
		Model: "rss_formula_rss_4",
		Skin:  "",
	},
	"CAR_3": {
		Name:  "",
		GUID:  "",
		Team:  "Team 2",
		Model: "rss_formula_rss_4",
		Skin:  "",
	},
	"CAR_4": {
		Name:  "",
		GUID:  "",
		Team:  "Team 2",
		Model: "rss_formula_rss_4",
		Skin:  "",
	},
	"CAR_5": {
		Name:  "",
		GUID:  "",
		Team:  "Team 2",
		Model: "rss_formula_rss_4",
		Skin:  "",
	},
}

var championshipEventFixtures = []string{
	"barbagello.db",
	"red-bull-ring.db",
	// @TODO fix me
	// "barbagello-no-end-sessions.db",
}

func newTestChampionshipManager() *ChampionshipManager {
	config := &smconfig.Configuration{}
	store := smstore.NewJSONStore(filepath.Join(os.TempDir(), "asm-race-store"), filepath.Join(os.TempDir(), "asm-race-store-shared"))
	process := servertest.Dummy{}
	notifications := &notificationtest.Dummy{}

	return NewChampionshipManager(
		smrace.NewRaceManager(
			config,
			store,
			process,
			smcontent.NewCarManager(ServerInstallPath, false, false),
			smcontent.NewTrackManager(ServerInstallPath),
			notifications,
			racecontrol.NewRaceControl(&smconfig.Configuration{}, racecontrol.NilBroadcaster{}, racecontrol.NilTrackData{}, servertest.Dummy{}, testStore, racecontrol.NewPenaltiesManager(testStore, globalsEnv()), globalsEnv()),
			globalsEnv(),
		),
		config,
		store,
		process,
		globalsEnv(),
		notifications,
	)
}

func doReplay(filename string, multiplier int, callbackFunc udp.CallbackFunc, waitTime time.Duration) error {
	db, err := bbolt.Open(filename, 0644, nil)

	if err != nil {
		return err
	}

	defer db.Close()

	return replay.UDPMessages(db, multiplier, callbackFunc, waitTime)
}

func TestChampionshipManager_ChampionshipEventCallback(t *testing.T) {
	ServerInstallPath = filepath.Join("cmd", "server-manager", "assetto")

	championshipManager := newTestChampionshipManager()

	t.Run("Basic championship flow, closed entrylist", func(t *testing.T) {
		// make a championship
		champ := smmodel.NewChampionship("Test Championship")
		champ.OpenEntrants = false
		cl := smmodel.NewChampionshipClass("Default")
		cl.Entrants = TestEntryList
		champ.AddClass(cl)

		for range championshipEventFixtures {
			e := smmodel.NewChampionshipEvent()
			e.RaceSetup = serverconfig.ConfigIniDefault().CurrentRaceConfig

			champ.Events = append(champ.Events, e)
		}

		if err := championshipManager.UpsertChampionship(champ); err != nil {
			t.Error(err)
			return
		}

		for i, sessionFile := range championshipEventFixtures {
			t.Run(sessionFile, func(t *testing.T) {
				if err := championshipManager.StartEvent(champ.ID.String(), champ.Events[i].ID.String(), false); err != nil {
					t.Error(err)
					return
				}

				err := doReplay(filepath.Join("fixtures", sessionFile), 1000,
					championshipManager.ChampionshipEventCallback, time.Second)

				if err != nil {
					t.Error(err)
					return
				}

				checkChampionshipEventCompletion(t, championshipManager, champ.ID.String(), champ.Events[i].ID.String())
			})
		}

		checkAutoPopulatedEntryList(t, championshipManager, champ.ID.String(), 0)
	})

	t.Run("Basic championship flow, open entrylist", func(t *testing.T) {
		// make a championship
		champ := smmodel.NewChampionship("Test Championship")
		champ.OpenEntrants = true
		cl := smmodel.NewChampionshipClass("Default")
		cl.Entrants = TestEntryList
		champ.AddClass(cl)

		for range championshipEventFixtures {
			e := smmodel.NewChampionshipEvent()
			e.RaceSetup = serverconfig.ConfigIniDefault().CurrentRaceConfig

			champ.Events = append(champ.Events, e)
		}

		if err := championshipManager.UpsertChampionship(champ); err != nil {
			t.Error(err)
			return
		}

		for i, sessionFile := range championshipEventFixtures {
			t.Run(sessionFile, func(t *testing.T) {
				if err := championshipManager.StartEvent(champ.ID.String(), champ.Events[i].ID.String(), false); err != nil {
					t.Error(err)
					return
				}

				err := doReplay(filepath.Join("fixtures", sessionFile), 1000,
					championshipManager.ChampionshipEventCallback, time.Second)

				if err != nil {
					t.Error(err)
					return
				}

				checkChampionshipEventCompletion(t, championshipManager, champ.ID.String(), champ.Events[i].ID.String())
			})
		}

		checkAutoPopulatedEntryList(t, championshipManager, champ.ID.String(), 2)
	})

	t.Run("Basic championship flow, open entrylist with only one free slot", func(t *testing.T) {
		// make a championship
		champ := smmodel.NewChampionship("Test Championship")
		champ.OpenEntrants = true
		cl := smmodel.NewChampionshipClass("Default")
		cl.Entrants = serverconfig.EntryList{
			"CAR_0": {
				Name:  "",
				GUID:  "",
				Team:  "Team 1",
				Model: "rss_formula_rss_4",
				Skin:  "",
			},
		}
		champ.AddClass(cl)

		for range championshipEventFixtures {
			e := smmodel.NewChampionshipEvent()
			e.RaceSetup = serverconfig.ConfigIniDefault().CurrentRaceConfig

			champ.Events = append(champ.Events, e)
		}

		if err := championshipManager.UpsertChampionship(champ); err != nil {
			t.Error(err)
			return
		}

		for i, sessionFile := range championshipEventFixtures {
			t.Run(sessionFile, func(t *testing.T) {
				if err := championshipManager.StartEvent(champ.ID.String(), champ.Events[i].ID.String(), false); err != nil {
					t.Error(err)
					return
				}

				err := doReplay(filepath.Join("fixtures", sessionFile), 1000,
					championshipManager.ChampionshipEventCallback, time.Second)

				if err != nil {
					t.Error(err)
					return
				}

				checkChampionshipEventCompletion(t, championshipManager, champ.ID.String(), champ.Events[i].ID.String())
			})
		}

		checkAutoPopulatedEntryList(t, championshipManager, champ.ID.String(), 1)
	})

	t.Run("Basic championship flow, open entrylist with one free slot for a non-matching car", func(t *testing.T) {
		// make a championship
		champ := smmodel.NewChampionship("Test Championship")
		champ.OpenEntrants = true
		cl := smmodel.NewChampionshipClass("Default")
		cl.Entrants = serverconfig.EntryList{
			"CAR_0": {
				Name:  "",
				GUID:  "",
				Team:  "Team 1",
				Model: "bmw_m3",
				Skin:  "",
			},
		}
		champ.AddClass(cl)

		for range championshipEventFixtures {
			e := smmodel.NewChampionshipEvent()
			e.RaceSetup = serverconfig.ConfigIniDefault().CurrentRaceConfig

			champ.Events = append(champ.Events, e)
		}

		if err := championshipManager.UpsertChampionship(champ); err != nil {
			t.Error(err)
			return
		}

		for i, sessionFile := range championshipEventFixtures {
			t.Run(sessionFile, func(t *testing.T) {
				if err := championshipManager.StartEvent(champ.ID.String(), champ.Events[i].ID.String(), false); err != nil {
					t.Error(err)
					return
				}

				err := doReplay(filepath.Join("fixtures", sessionFile), 1000,
					championshipManager.ChampionshipEventCallback, time.Second)

				if err != nil {
					t.Error(err)
					return
				}

				checkChampionshipEventCompletion(t, championshipManager, champ.ID.String(), champ.Events[i].ID.String())
			})
		}

		checkAutoPopulatedEntryList(t, championshipManager, champ.ID.String(), 0)
	})
}

func checkAutoPopulatedEntryList(t *testing.T, championshipManager *ChampionshipManager, championshipID string, expected int) {
	loadedChampionship, err := championshipManager.LoadChampionship(championshipID)

	if err != nil {
		t.Error(err)
		return
	}

	numPopulatedEntrants := 0

	for _, entrant := range loadedChampionship.AllEntrants() {
		if entrant.Name != "" && entrant.GUID != "" {
			numPopulatedEntrants++
		}
	}

	if numPopulatedEntrants != expected {
		t.Fail()
	}
}

func checkChampionshipEventCompletion(t *testing.T, championshipManager *ChampionshipManager, championshipID string, eventID string) {
	// now look at the championship event and see if it has a start/end time
	loadedChampionship, err := championshipManager.LoadChampionship(championshipID)

	if err != nil {
		t.Error(err)
		return
	}

	event, _, err := loadedChampionship.EventByID(eventID)

	if err != nil {
		t.Error(err)
		return
	}

	if event.StartedTime.IsZero() {
		t.Logf("Invalid championship event start time (zero)")
		t.Fail()
		return
	}

	if event.CompletedTime.IsZero() {
		t.Logf("Invalid championship event completed time (zero)")
		t.Fail()
		return
	}

	for _, sess := range event.Sessions {
		if sess.StartedTime.IsZero() {
			t.Logf("Invalid session start time (zero)")
			t.Fail()
			return
		}

		if sess.CompletedTime.IsZero() {
			t.Logf("Invalid session end time (zero)")
			t.Fail()
			return
		}
	}
}

func TestChampionshipManager_ChampionshipEventCallbackOpenChampionshipExample(t *testing.T) {
	championshipManager := newTestChampionshipManager()

	var champ *smmodel.Championship

	f, err := os.Open(filepath.Join("fixtures", "open-championship", "championship-setup.json"))

	if err != nil {
		t.Error(err)
		return
	}

	defer f.Close()

	err = json.NewDecoder(f).Decode(&champ)

	if err != nil {
		t.Error(err)
		return
	}

	err = championshipManager.UpsertChampionship(champ)

	if err != nil {
		t.Error(err)
		return
	}

	eventIDToJSON := map[string]string{
		"006e6edd-7e77-4d7e-a2ce-d757adb65d95": filepath.Join("fixtures", "open-championship", "nurburgring_sprint_b.db"),
		"c412e271-b3c0-4a71-abbe-954c540260de": filepath.Join("fixtures", "open-championship", "rbr_national.db"),
		"09bddc04-45ed-40f7-bc94-73a3fe42f3fb": filepath.Join("fixtures", "open-championship", "suzuka_east.db"),
	}

	eventNum := 1

	for eventID, file := range eventIDToJSON {
		err = championshipManager.StartEvent(champ.ID.String(), eventID, false)

		if err != nil {
			t.Error(err)
			return
		}

		err := doReplay(file, 10000, championshipManager.ChampionshipEventCallback, time.Second)

		if err != nil {
			t.Error(err)
			return
		}

		checkChampionshipEventCompletion(t, championshipManager, champ.ID.String(), eventID)
		eventNum++
	}
}

var testStore = smstore.NewJSONStore(filepath.Join(os.TempDir(), "asm-race-store"), filepath.Join(os.TempDir(), "asm-race-store-shared"))
