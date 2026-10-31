package racecontrol

import (
	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smconfig "github.com/wernersbacher/ACPitwall/internal/config"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smresults "github.com/wernersbacher/ACPitwall/internal/results"
	"github.com/wernersbacher/ACPitwall/internal/server/servertest"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"

	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wernersbacher/ACPitwall/pkg/udp"
)

type stoppedServerProcess struct {
	servertest.Dummy
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
		slotFor: func(r *http.Request) (*Slot, error) {
			return &Slot{
				Store:       store,
				Process:     process,
				Env:         testEnv(),
				RaceControl: NewRaceControl(&smconfig.Configuration{}, NilBroadcaster{}, NilTrackData{}, process, store, NewPenaltiesManager(store, testEnv()), testEnv()),
			}, nil
		},
	}

	rec := httptest.NewRecorder()
	rch.ServerStatusAPI(rec, httptest.NewRequest(http.MethodGet, "/api/server-status", nil))

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

	raceControl := NewRaceControl(&smconfig.Configuration{}, NilBroadcaster{}, NilTrackData{}, servertest.Dummy{}, testStore, NewPenaltiesManager(testStore, testEnv()), testEnv())

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
		out := NewAPIServerStatus(testEnv(), true, "Configured Name", event, raceControl)

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
		out := NewAPIServerStatus(testEnv(), true, "", smmodel.ActiveRaceWeekend{Name: "Endurance"}, raceControl)

		if !out.IsRaceWeekend || out.RaceWeekendName != "Endurance" || out.IsChampionship {
			t.Errorf("unexpected status: %+v", out)
		}
	})

	t.Run("not running", func(t *testing.T) {
		out := NewAPIServerStatus(testEnv(), false, "Configured Name", event, raceControl)

		if out.Running || out.ServerName != "Configured Name" || out.Session != nil || out.EventName != "" || len(out.ConnectedDrivers) != 0 {
			t.Errorf("unexpected status: %+v", out)
		}
	})
}

func TestRaceControlHandlerUsesSlotOfRequest(t *testing.T) {
	dir, err := ioutil.TempDir("", "server-status-slots")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(dir)

	store := smstore.NewJSONStore(filepath.Join(dir, "store"), filepath.Join(dir, "store-shared"))
	process := stoppedServerProcess{}

	slots := make(map[smmodel.ServerID]*Slot)

	for id, name := range map[smmodel.ServerID]string{"": "Default Server", "b": "Server B"} {
		opts := serverconfig.ConfigIniDefault().GlobalServerConfig
		opts.Name = name

		slotStore := smstore.ForServer(store, id)

		if err := slotStore.UpsertServerOptions(&opts); err != nil {
			t.Fatal(err)
		}

		slots[id] = &Slot{
			Store:       slotStore,
			Process:     process,
			Env:         testEnv(),
			RaceControl: NewRaceControl(&smconfig.Configuration{}, NilBroadcaster{}, NilTrackData{}, process, slotStore, NewPenaltiesManager(slotStore, testEnv()), testEnv()),
		}
	}

	rch := &RaceControlHandler{
		slotFor: func(r *http.Request) (*Slot, error) {
			slot, ok := slots[smweb.RequestedServerID(r)]

			if !ok {
				return nil, errors.New("unknown server")
			}

			return slot, nil
		},
	}

	for _, tc := range []struct {
		requested smmodel.ServerID
		name      string
	}{
		{"", "Default Server"},
		{"b", "Server B"},
	} {
		req := smweb.WithRequestedServerID(httptest.NewRequest(http.MethodGet, "/api/server-status", nil), tc.requested)
		rec := httptest.NewRecorder()
		rch.ServerStatusAPI(rec, req)

		var out APIServerStatus

		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}

		if out.ServerName != tc.name {
			t.Errorf("server %q: expected name %q, got %q", tc.requested, tc.name, out.ServerName)
		}
	}

	rec := httptest.NewRecorder()
	rch.ServerStatusAPI(rec, smweb.WithRequestedServerID(httptest.NewRequest(http.MethodGet, "/api/server-status", nil), "missing"))

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for an unknown server, got %d", rec.Code)
	}
}
