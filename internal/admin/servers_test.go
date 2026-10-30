package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	"github.com/wernersbacher/assetto-server-manager/internal/racecontrol"
	"github.com/wernersbacher/assetto-server-manager/internal/server/servertest"
	"github.com/wernersbacher/assetto-server-manager/internal/serverenv/envtest"
	smweb "github.com/wernersbacher/assetto-server-manager/internal/web"
	"github.com/wernersbacher/assetto-server-manager/pkg/udp"
)

func newNamedSlot(t *testing.T, id, name string, event smmodel.RaceEvent, drivers int) NamedSlot {
	t.Helper()

	process := &servertest.Interruptible{}

	if event != nil {
		if err := process.Start(event, "", 0, "", 0); err != nil {
			t.Fatal(err)
		}
	}

	raceControl := &racecontrol.RaceControl{
		ConnectedDrivers: racecontrol.NewDriverMap(racecontrol.ConnectedDrivers, func(_ racecontrol.RaceControlDriverGroup, a, b *smmodel.RaceControlDriver) bool {
			return a.CarInfo.DriverGUID < b.CarInfo.DriverGUID
		}),
	}

	for i := 0; i < drivers; i++ {
		guid := udp.DriverGUID(string(rune('a' + i)))
		raceControl.ConnectedDrivers.Add(guid, racecontrol.NewRaceControlDriver(udp.SessionCarInfo{DriverGUID: guid}))
	}

	return NamedSlot{
		ServerRef: smweb.ServerRef{ID: smmodel.ServerID(id), Name: name},
		Slot: &Slot{
			Env:         envtest.New("install"),
			Process:     process,
			RaceControl: raceControl,
		},
	}
}

func TestNewServerSummary(t *testing.T) {
	idle := NewServerSummary(newNamedSlot(t, "a", "Server A", nil, 2))

	if idle.Running || idle.EventName != "" || idle.DriversOnline != 0 || idle.Name != "Server A" || idle.ID != "a" {
		t.Errorf("an idle server only has id and name, got %+v", idle)
	}

	race := smmodel.QuickRace{RaceConfig: serverconfig.CurrentRaceConfig{Track: "monza"}}
	running := NewServerSummary(newNamedSlot(t, "b", "Server B", race, 2))

	if !running.Running || running.EventType != "Running" || running.EventName == "" || running.DriversOnline != 2 {
		t.Errorf("unexpected summary of a running server: %+v", running)
	}
}

func TestEventType(t *testing.T) {
	for expected, args := range map[string][5]bool{
		"Championship":          {true, false, false, false, false},
		"Championship practice": {true, false, true, false, false},
		"Race Weekend":          {false, true, false, false, false},
		"Looping event":         {false, false, false, false, true},
		"Time attack":           {false, false, false, true, false},
		"Running":               {},
	} {
		if got := eventType(args[0], args[1], args[2], args[3], args[4]); got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	}
}

func TestServersAPIListsAllServers(t *testing.T) {
	race := smmodel.QuickRace{RaceConfig: serverconfig.CurrentRaceConfig{Track: "monza"}}
	slots := []NamedSlot{newNamedSlot(t, "a", "Server A", nil, 0), newNamedSlot(t, "b", "Server B", race, 1)}

	handler := &ServerAdministrationHandler{slotsFunc: func() []NamedSlot { return slots }}

	// the listing does not depend on the server the request selects
	rec := httptest.NewRecorder()
	handler.ServersAPI(rec, smweb.WithRequestedServerID(httptest.NewRequest(http.MethodGet, "/api/servers", nil), "b"))

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d", rec.Code)
	}

	var got []ServerSummary

	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 || got[0].ID != "a" || got[0].Running || got[1].ID != "b" || !got[1].Running || got[1].DriversOnline != 1 {
		t.Errorf("unexpected servers: %+v", got)
	}
}
