package admin

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cj123/formulate"
	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/serverenv/envtest"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

func TestRobotsUsesSlotOfRequest(t *testing.T) {
	store, cleanup := envtest.TempJSONStore()
	defer cleanup()

	slots := make(map[smmodel.ServerID]*Slot)

	for id, preventCrawlers := range map[smmodel.ServerID]int{"": 0, "b": 1} {
		opts := serverconfig.ConfigIniDefault().GlobalServerConfig
		opts.PreventWebCrawlers = formulate.BoolNumber(preventCrawlers)

		slotStore := smstore.ForServer(store, id)

		if err := slotStore.UpsertServerOptions(&opts); err != nil {
			t.Fatal(err)
		}

		slots[id] = &Slot{Store: slotStore}
	}

	handler := &ServerAdministrationHandler{
		slotFor: func(r *http.Request) (*Slot, error) {
			slot, ok := slots[smweb.RequestedServerID(r)]

			if !ok {
				return nil, errors.New("unknown server")
			}

			return slot, nil
		},
	}

	for requested, disallowAll := range map[smmodel.ServerID]bool{"": false, "b": true} {
		rec := httptest.NewRecorder()
		handler.Robots(rec, smweb.WithRequestedServerID(httptest.NewRequest(http.MethodGet, "/robots.txt", nil), requested))

		if got := strings.Contains(rec.Body.String(), "Disallow: /"); got != disallowAll {
			t.Errorf("server %q: expected disallow all %t, got %q", requested, disallowAll, rec.Body.String())
		}
	}

	rec := httptest.NewRecorder()
	handler.Robots(rec, smweb.WithRequestedServerID(httptest.NewRequest(http.MethodGet, "/robots.txt", nil), "missing"))

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for an unknown server, got %d", rec.Code)
	}
}
