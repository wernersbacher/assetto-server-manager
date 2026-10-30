package admin

import (
	"testing"

	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	"github.com/wernersbacher/assetto-server-manager/internal/serverenv/envtest"
	smstore "github.com/wernersbacher/assetto-server-manager/internal/store"
)

func TestServerDetailsListsPortsPluginsAndWarnings(t *testing.T) {
	store, cleanup := envtest.TempJSONStore()
	defer cleanup()

	var slots []NamedSlot

	for _, id := range []smmodel.ServerID{"a", "b"} {
		slot := newNamedSlot(t, string(id), "Server "+string(id), nil, 0)
		slot.Store = smstore.ForServer(store, id)
		slots = append(slots, slot)
	}

	// plugin options of a: stracker is enabled
	strackerOpts, err := slots[0].Store.LoadStrackerOptions()

	if err != nil {
		t.Fatal(err)
	}

	strackerOpts.EnableStracker = true

	if err := slots[0].Store.UpsertStrackerOptions(strackerOpts); err != nil {
		t.Fatal(err)
	}

	handler := &ServerAdministrationHandler{
		slotsFunc: func() []NamedSlot { return slots },
		portWarnings: func(id smmodel.ServerID) []string {
			if id == "b" {
				return []string{"Port 9600 (udp) is used by the acServer UDP port and by the acServer UDP port of server \"a\". Change one of them."}
			}

			return nil
		},
	}

	a, b := handler.newServerDetails(slots[0]), handler.newServerDetails(slots[1])

	if a.ID != "a" || a.Name != "Server a" || a.Running {
		t.Errorf("unexpected summary %+v", a.ServerSummary)
	}

	if len(a.Ports) == 0 || a.Ports[0].What != "acServer UDP port" || a.Ports[0].Port != 9600 {
		t.Errorf("expected the acServer ports with the default UDP port first, got %+v", a.Ports)
	}

	if len(a.Plugins) != 3 || a.Plugins[0].Name != "sTracker" || !a.Plugins[0].Enabled || len(a.Plugins[0].Ports) == 0 {
		t.Errorf("expected stracker to be enabled with ports, got %+v", a.Plugins)
	}

	if a.Plugins[1].Enabled || len(a.Plugins[1].Ports) != 0 || a.Plugins[2].Enabled {
		t.Errorf("the other plugins are disabled and use no ports, got %+v", a.Plugins)
	}

	if len(a.Warnings) != 0 || len(b.Warnings) != 1 {
		t.Errorf("expected the warning for b only, got %v and %v", a.Warnings, b.Warnings)
	}

	if b.Plugins[0].Enabled {
		t.Error("the plugin options are per server, stracker of b is not enabled")
	}
}
