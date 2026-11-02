package championship

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi"

	smconfig "github.com/wernersbacher/ACPitwall/internal/config"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
	"github.com/wernersbacher/ACPitwall/internal/web/webtest"
)

func TestHiddenChampionshipsAreOnlyVisibleToLoggedInAccounts(t *testing.T) {
	cm := newTestChampionshipManager()
	handler := NewChampionshipsHandler(&smconfig.Configuration{}, &smweb.BaseHandler{}, func(*http.Request) (*ChampionshipManager, error) { return cm, nil }, nil)

	hidden := smmodel.NewChampionship("Hidden")
	hidden.Hidden = true
	hidden.SignUpForm.Enabled = true

	visible := smmodel.NewChampionship("Visible")

	for _, c := range []*smmodel.Championship{hidden, visible} {
		if err := cm.UpsertChampionship(c); err != nil {
			t.Fatal(err)
		}
	}

	request := func(account *smmodel.Account, championship *smmodel.Championship) *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		scope := webtest.RequestScope()
		req = smweb.WithScope(req, scope)

		if account != nil {
			req = smweb.WithAccount(req, account)
		}

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("championshipID", championship.ID.String())

		return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	}

	reader := &smmodel.Account{Name: "reader", Groups: map[smmodel.ServerID]smmodel.Group{"": smmodel.GroupRead}}

	routes := map[string]http.HandlerFunc{
		"ViewAPI":       handler.ViewAPI,
		"StandingsAPI":  handler.StandingsAPI,
		"Export":        handler.Export,
		"ExportResults": handler.ExportResults,
		"IcalFeed":      handler.IcalFeed,
		"SignUpForm":    handler.SignUpForm,
	}

	for name, route := range routes {
		rec := httptest.NewRecorder()
		route(rec, request(nil, hidden))

		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: anonymous visitors must get 404 for a hidden championship, got %d", name, rec.Code)
		}

		if name == "SignUpForm" {
			continue // rendering the form needs the car and track managers
		}

		rec = httptest.NewRecorder()
		route(rec, request(reader, hidden))

		if rec.Code == http.StatusNotFound {
			t.Errorf("%s: logged in accounts must see a hidden championship", name)
		}

		rec = httptest.NewRecorder()
		route(rec, request(nil, visible))

		if name != "SignUpForm" && rec.Code != http.StatusOK {
			t.Errorf("%s: visible championships stay visible, got %d", name, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	handler.ListAPI(rec, request(nil, visible))

	if body := rec.Body.String(); !strings.Contains(body, "Visible") || strings.Contains(body, "Hidden") {
		t.Errorf("anonymous list must only contain visible championships: %s", body)
	}

	rec = httptest.NewRecorder()
	handler.ListAPI(rec, request(reader, visible))

	if body := rec.Body.String(); !strings.Contains(body, "Visible") || !strings.Contains(body, "Hidden") {
		t.Errorf("logged in list must contain hidden championships: %s", body)
	}
}
