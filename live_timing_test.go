package servermanager

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smconfig "github.com/wernersbacher/ACPitwall/internal/config"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/racecontrol"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
	"github.com/wernersbacher/ACPitwall/internal/web/webtest"
)

// runningRace is a race manager with a running event.
type runningRace struct{}

func (runningRace) CurrentRace() (*serverconfig.ServerConfig, serverconfig.EntryList) {
	config := serverconfig.ConfigIniDefault()

	entryList := serverconfig.EntryList{}
	entryList["CAR_0"] = &serverconfig.Entrant{Name: "Alice Anderson", GUID: "76561190000000001", Model: "lotus_evora_gtc", Skin: "red"}
	entryList["CAR_1"] = &serverconfig.Entrant{Name: "Bob Brown", GUID: "76561190000000002", Model: "lotus_evora_gtc", Skin: "blue"}

	return &config, entryList
}

// The live timing page with a running event (the "with .RaceDetails" branch) has to render for every access level.
func TestLiveTimingRendersRunningEvent(t *testing.T) {
	for _, mapEnabled := range []bool{true, false} {
		for _, tc := range []struct {
			name  string
			group smmodel.Group
			admin bool
			write bool
		}{
			{"read", smmodel.GroupRead, false, false},
			{"write", smmodel.GroupWrite, false, true},
			{"admin", smmodel.GroupAdmin, true, true},
		} {
			config := &smconfig.Configuration{}

			if mapEnabled {
				config.LiveMap.IntervalMs = 500
			}

			resolver, _, cleanup := newTestResolver(t, config)

			handler := racecontrol.NewRaceControlHandler(resolver.resolveBaseHandler(), func(r *http.Request) (*racecontrol.Slot, error) {
				slot, err := resolver.raceControlSlotFor(r)

				if err != nil {
					return nil, err
				}

				slot.RaceManager = runningRace{}

				return slot, nil
			})

			scope := webtest.RequestScope()

			req := httptest.NewRequest(http.MethodGet, "/live-timing", nil)
			req = smweb.WithScope(req, scope)
			req = smweb.WithAccount(req, &smmodel.Account{Name: tc.name, Groups: map[smmodel.ServerID]smmodel.Group{scope.ServerID: tc.group}})

			rec := httptest.NewRecorder()
			handler.LiveTiming(rec, req)

			body := rec.Body.String()

			if rec.Code != http.StatusOK {
				t.Errorf("%s (map %t): expected 200, got %d: %s", tc.name, mapEnabled, rec.Code, body)
				cleanup()
				continue
			}

			for _, want := range []string{
				`id="event-title"`,
				`id="live-table"`,
				`class="live-stepper"`,
				`data-session="Practice"`,
				`data-session="Race"`,
				`id="race-progress-bar"`,
				`id="live-table-disconnected"`,
				`<th class="col-secondary">`,
				"Alice A.",
				"Bob B.",
				"const useMPH =  false",
			} {
				if !strings.Contains(body, want) {
					t.Errorf("%s (map %t): expected %q in the page", tc.name, mapEnabled, want)
				}
			}

			if got := strings.Contains(body, `id="admin-drawer"`); got != tc.admin {
				t.Errorf("%s (map %t): admin drawer rendered: %t, want %t", tc.name, mapEnabled, got, tc.admin)
			}

			if got := strings.Contains(body, `id="add-live-frame"`); got != tc.write {
				t.Errorf("%s (map %t): add frame button rendered: %t, want %t", tc.name, mapEnabled, got, tc.write)
			}

			if got := strings.Contains(body, `id="trackMapImage"`); got != mapEnabled {
				t.Errorf("%s (map %t): map rendered: %t", tc.name, mapEnabled, got)
			}

			if strings.Contains(body, "{{") || strings.Contains(body, "live.col.") {
				t.Errorf("%s (map %t): untranslated or unrendered text in the page", tc.name, mapEnabled)
			}

			cleanup()
		}
	}
}

// Without a running event the page explains that there is no session.
func TestLiveTimingWithoutEvent(t *testing.T) {
	resolver, _, cleanup := newTestResolver(t, &smconfig.Configuration{})
	defer cleanup()

	handler := racecontrol.NewRaceControlHandler(resolver.resolveBaseHandler(), resolver.raceControlSlotFor)

	req := smweb.WithScope(httptest.NewRequest(http.MethodGet, "/live-timing", nil), webtest.RequestScope())

	rec := httptest.NewRecorder()
	handler.LiveTiming(rec, req)

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "live-empty") || strings.Contains(rec.Body.String(), `id="live-table"`) {
		t.Errorf("unexpected page: %d %s", rec.Code, rec.Body.String())
	}
}
