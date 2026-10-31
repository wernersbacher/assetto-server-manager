package raceweekend

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
	"github.com/wernersbacher/ACPitwall/internal/web/webtest"
)

func TestHiddenRaceWeekendsAreOnlyVisibleToLoggedInAccounts(t *testing.T) {
	slots, store := newTestSlots(t, "a")
	handler := newSlotTestHandler(slots, "a", nil)

	hidden := smmodel.NewRaceWeekend()
	hidden.Name = "Hidden"
	hidden.Hidden = true

	if err := store.UpsertRaceWeekend(hidden); err != nil {
		t.Fatal(err)
	}

	request := func(account *smmodel.Account) *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		scope := webtest.RequestScope()
		scope.ReadIsOpen = true
		req = smweb.WithScope(req, scope)

		if account != nil {
			req = smweb.WithAccount(req, account)
		}

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("raceWeekendID", hidden.ID.String())

		return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	}

	reader := &smmodel.Account{Name: "reader", Groups: map[smmodel.ServerID]smmodel.Group{"": smmodel.GroupRead}}

	for name, route := range map[string]http.HandlerFunc{
		"View":             handler.View,
		"Export":           handler.Export,
		"ManageFilters":    handler.ManageFilters,
		"ManageEntryList":  handler.ManageEntryList,
		"EntryListPreview": handler.EntryListPreview,
		"GridPreview":      handler.GridPreview,
	} {
		rec := httptest.NewRecorder()
		route(rec, request(nil))

		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: anonymous visitors must get 404 for a hidden race weekend, got %d", name, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	handler.Export(rec, request(reader))

	if rec.Code != http.StatusOK {
		t.Errorf("logged in accounts must be able to export a hidden race weekend, got %d", rec.Code)
	}
}

func TestRaceWeekendOfHiddenChampionshipIsHidden(t *testing.T) {
	slots, store := newTestSlots(t, "a")
	handler := newSlotTestHandler(slots, "a", nil)

	championship := smmodel.NewChampionship("Hidden")
	championship.Hidden = true

	if err := store.UpsertChampionship(championship); err != nil {
		t.Fatal(err)
	}

	linked := smmodel.NewRaceWeekend()
	linked.Name = "Linked"
	linked.ChampionshipID = championship.ID

	if err := store.UpsertRaceWeekend(linked); err != nil {
		t.Fatal(err)
	}

	request := func(account *smmodel.Account) *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		scope := webtest.RequestScope()
		scope.ReadIsOpen = true
		req = smweb.WithScope(req, scope)

		if account != nil {
			req = smweb.WithAccount(req, account)
		}

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("raceWeekendID", linked.ID.String())

		return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	}

	reader := &smmodel.Account{Name: "reader", Groups: map[smmodel.ServerID]smmodel.Group{"": smmodel.GroupRead}}

	rec := httptest.NewRecorder()
	handler.Export(rec, request(nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("anonymous visitors must get 404 for a race weekend of a hidden championship, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	handler.Export(rec, request(reader))

	if rec.Code != http.StatusOK {
		t.Errorf("logged in accounts must see it, got %d", rec.Code)
	}

	championship.Hidden = false

	if err := store.UpsertChampionship(championship); err != nil {
		t.Fatal(err)
	}

	rec = httptest.NewRecorder()
	handler.Export(rec, request(nil))

	if rec.Code != http.StatusOK {
		t.Errorf("a race weekend of a visible championship stays visible, got %d", rec.Code)
	}
}
