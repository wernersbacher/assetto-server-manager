package championship

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"

	smconfig "github.com/wernersbacher/assetto-server-manager/internal/config"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	smweb "github.com/wernersbacher/assetto-server-manager/internal/web"
	"github.com/wernersbacher/assetto-server-manager/internal/web/webtest"
)

func TestChampionshipsHandler_ModifyEntrantStatus_AcceptIntoFullChampionship(t *testing.T) {
	cm := newTestChampionshipManager()
	handler := NewChampionshipsHandler(&smconfig.Configuration{}, &smweb.BaseHandler{}, func(*http.Request) (*ChampionshipManager, error) { return cm, nil }, nil)

	// a championship whose only slot is taken already
	championship := smmodel.NewChampionship("Full")
	class := smmodel.NewChampionshipClass("Default")
	class.Entrants = serverconfig.EntryList{"CAR_0": {Model: "rss_formula_rss_4"}}
	championship.AddClass(class)
	championship.SignUpForm.Enabled = true
	championship.SignUpForm.HideCarChoice = true

	response := &smmodel.ChampionshipSignUpResponse{GUID: "76561198000000002", Name: "Late Bird", Car: "rss_formula_rss_4", Status: smmodel.ChampionshipEntrantPending}
	championship.SignUpForm.Responses = []*smmodel.ChampionshipSignUpResponse{response}

	for _, entrant := range class.Entrants {
		entrant.GUID = "76561198000000001"
		entrant.Name = "Early Bird"
	}

	if err := cm.UpsertChampionship(championship); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/championship/x/entrant/y?action=accept", nil)
	req = smweb.WithScope(req, webtest.RequestScope())

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("championshipID", championship.ID.String())
	rctx.URLParams.Add("entrantGUID", response.GUID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	handler.ModifyEntrantStatus(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected a redirect with a flash message, got status %d", rec.Code)
	}

	loaded, err := cm.LoadChampionship(championship.ID.String())

	if err != nil {
		t.Fatal(err)
	}

	if status := loaded.SignUpForm.Responses[0].Status; status != smmodel.ChampionshipEntrantPending {
		t.Errorf("entrant must stay pending if there is no slot, got %q", status)
	}
}
