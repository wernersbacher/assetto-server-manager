package servermanager

import (
	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smconfig "github.com/wernersbacher/assetto-server-manager/internal/config"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"
	smstore "github.com/wernersbacher/assetto-server-manager/internal/store"

	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wernersbacher/assetto-server-manager/pkg/udp"
)

type stoppedServerProcess struct {
	dummyServerProcess
}

func (stoppedServerProcess) IsRunning() bool {
	return false
}

func (stoppedServerProcess) Event() smmodel.RaceEvent {
	return smmodel.QuickRace{}
}

func TestRaceControlHandlerServerStatusAPI(t *testing.T) {
	dir, err := ioutil.TempDir("", "server-status")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(dir)

	store := smstore.NewJSONStore(filepath.Join(dir, "store"), filepath.Join(dir, "store-shared"))

	opts := serverconfig.ConfigIniDefault().GlobalServerConfig
	opts.Name = "Test Server"

	if err := store.UpsertServerOptions(&opts); err != nil {
		t.Fatal(err)
	}

	process := stoppedServerProcess{}
	rch := &RaceControlHandler{
		store:         store,
		serverProcess: process,
		raceControl:   NewRaceControl(&smconfig.Configuration{}, NilBroadcaster{}, nilTrackData{}, process, store, NewPenaltiesManager(store)),
	}

	rec := httptest.NewRecorder()
	rch.serverStatusAPI(rec, httptest.NewRequest(http.MethodGet, "/api/server-status", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var out APIServerStatus

	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}

	if out.Running || out.ServerName != "Test Server" || out.Session != nil || out.NumConnectedDrivers != 0 || out.ConnectedDrivers == nil {
		t.Errorf("unexpected status: %+v", out)
	}

	if strings.Contains(rec.Body.String(), "null") {
		t.Errorf("expected no null values in %s", rec.Body.String())
	}
}

func TestNewAPIServerStatus(t *testing.T) {
	oldShortened := smresults.UseShortenedDriverNames
	smresults.UseShortenedDriverNames = false

	defer func() { smresults.UseShortenedDriverNames = oldShortened }()

	raceControl := NewRaceControl(&smconfig.Configuration{}, NilBroadcaster{}, nilTrackData{}, dummyServerProcess{}, testStore, NewPenaltiesManager(testStore))

	if err := raceControl.OnNewSession(udp.SessionInfo{
		ServerName:  "Lobby Name",
		Track:       "api_test_ring",
		TrackConfig: "gp",
		Name:        "Qualify",
		Type:        udp.SessionTypeQualifying,
		Time:        15,
	}); err != nil {
		t.Fatal(err)
	}

	if err := raceControl.OnClientConnect(udp.SessionCarInfo{
		CarID:      1,
		DriverName: "Test Driver",
		DriverGUID: "7827162738272615",
		CarModel:   "ford_gt",
		CarSkin:    "red_01",
		EventType:  udp.EventNewConnection,
	}); err != nil {
		t.Fatal(err)
	}

	event := &smmodel.ActiveChampionship{
		Name:        "Test Championship",
		RaceConfig:  serverconfig.CurrentRaceConfig{Track: "other_track", MaxClients: 18},
		SessionType: serverconfig.SessionTypeQualifying,
	}

	t.Run("championship", func(t *testing.T) {
		out := NewAPIServerStatus(true, "Configured Name", event, raceControl)

		if !out.Running || out.ServerName != "Lobby Name" || !out.IsChampionship || out.ChampionshipName != "Test Championship" || out.IsRaceWeekend {
			t.Errorf("unexpected status: %+v", out)
		}

		if out.Track != "api_test_ring" || out.TrackLayout != "gp" || out.TrackName != "Api Test Ring (GP)" || out.MaxClients != 18 {
			t.Errorf("unexpected track or max clients: %+v", out)
		}

		if out.Session == nil || out.Session.Type != serverconfig.SessionTypeQualifying || out.Session.Name != "Qualify" || out.Session.Time != 15 || out.Session.Laps != 0 || time.Since(out.Session.Started) > time.Minute {
			t.Errorf("unexpected session: %+v", out.Session)
		}

		if out.NumConnectedDrivers != 1 || len(out.ConnectedDrivers) != 1 {
			t.Fatalf("expected 1 connected driver, got %+v", out.ConnectedDrivers)
		}

		if driver := out.ConnectedDrivers[0]; driver.Name != "Test Driver" || driver.CarModel != "ford_gt" || driver.CarSkin != "red_01" || driver.CarName == "" {
			t.Errorf("unexpected driver: %+v", driver)
		}

		// GUIDs must never be exposed
		data, err := json.Marshal(out)

		if err != nil {
			t.Fatal(err)
		}

		if strings.Contains(string(data), "7827162738272615") {
			t.Errorf("driver GUID found in %s", data)
		}
	})

	t.Run("race weekend", func(t *testing.T) {
		out := NewAPIServerStatus(true, "", smmodel.ActiveRaceWeekend{Name: "Endurance"}, raceControl)

		if !out.IsRaceWeekend || out.RaceWeekendName != "Endurance" || out.IsChampionship {
			t.Errorf("unexpected status: %+v", out)
		}
	})

	t.Run("not running", func(t *testing.T) {
		out := NewAPIServerStatus(false, "Configured Name", event, raceControl)

		if out.Running || out.ServerName != "Configured Name" || out.Session != nil || out.EventName != "" || len(out.ConnectedDrivers) != 0 {
			t.Errorf("unexpected status: %+v", out)
		}
	})
}
