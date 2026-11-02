package accounts

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi"
	"github.com/google/uuid"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

func postTeamOptions(ah *AccountHandler, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/accounts/team-options", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "/accounts")
	req = smweb.WithScope(req, &smweb.Scope{SessionStore: ah.accountManager.sessionStore})

	rec := httptest.NewRecorder()
	ah.SetTeamOptions(rec, req)

	return rec
}

func TestTeamOptions_Default(t *testing.T) {
	ah, _, cleanup := newTestHandler(t)
	defer cleanup()

	if disabled, max := ah.TeamOptions(); disabled || max != 0 {
		t.Errorf("by default every account can create teams with the default limit, got disabled=%v max=%d", disabled, max)
	}
}

func TestSetTeamOptions(t *testing.T) {
	ah, _, cleanup := newTestHandler(t)
	defer cleanup()

	for _, c := range []struct {
		creation, max string
		disabled      bool
		limit         int
	}{
		{"disabled", "5", true, 5},
		{"enabled", "0", false, 0},
		{"enabled", "-1", false, -1},
		{"disabled", " 7 ", true, 7},
		{"enabled", "1000", false, 1000},
	} {
		rec := postTeamOptions(ah, url.Values{"creation": {c.creation}, "max": {c.max}})

		if rec.Code != http.StatusFound {
			t.Fatalf("%+v: expected a redirect, got %d: %s", c, rec.Code, rec.Body.String())
		}

		if disabled, max := ah.TeamOptions(); disabled != c.disabled || max != c.limit {
			t.Errorf("%+v: got disabled=%v max=%d", c, disabled, max)
		}
	}
}

func TestSetTeamOptions_RejectsBadInput(t *testing.T) {
	ah, _, cleanup := newTestHandler(t)
	defer cleanup()

	if rec := postTeamOptions(ah, url.Values{"creation": {"disabled"}, "max": {"4"}}); rec.Code != http.StatusFound {
		t.Fatalf("setup failed: %d", rec.Code)
	}

	for name, form := range map[string]url.Values{
		"no values":        {},
		"unknown creation": {"creation": {"maybe"}, "max": {"3"}},
		"missing creation": {"max": {"3"}},
		"missing max":      {"creation": {"enabled"}},
		"text as max":      {"creation": {"enabled"}, "max": {"many"}},
		"below -1":         {"creation": {"enabled"}, "max": {"-2"}},
		"too large":        {"creation": {"enabled"}, "max": {"1001"}},
		"decimal":          {"creation": {"enabled"}, "max": {"2.5"}},
	} {
		if rec := postTeamOptions(ah, form); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d", name, rec.Code)
		}
	}

	// nothing of the bad requests was applied
	if disabled, max := ah.TeamOptions(); !disabled || max != 4 {
		t.Errorf("the previous options must stay, got disabled=%v max=%d", disabled, max)
	}
}

func TestTeamOptionsPersistence(t *testing.T) {
	ah, _, cleanup := newTestHandler(t)
	defer cleanup()

	// options stored before the team options existed read as "everybody can create teams, default limit"
	if err := ah.store.SetMeta(serverAccountOptionsMetaKey, ServerAccountOptions{Visibility: smmodel.Visibility{smmodel.AreaStats: smmodel.LevelLoggedIn}}); err != nil {
		t.Fatal(err)
	}

	if err := ah.accountManager.LoadOptions(); err != nil {
		t.Fatal(err)
	}

	if disabled, max := ah.TeamOptions(); disabled || max != 0 {
		t.Errorf("old options: got disabled=%v max=%d", disabled, max)
	}

	if ah.AreaLevel(smmodel.AreaStats) != smmodel.LevelLoggedIn {
		t.Error("loading must keep the other options")
	}

	// saved through the handler and loaded again
	if rec := postTeamOptions(ah, url.Values{"creation": {"disabled"}, "max": {"2"}}); rec.Code != http.StatusFound {
		t.Fatalf("saving failed: %d", rec.Code)
	}

	reloaded := NewAccountManager(ah.config, ah.store, ah.accountManager.sessionStore, "test")

	if err := reloaded.LoadOptions(); err != nil {
		t.Fatal(err)
	}

	if !reloaded.options.DisableTeamCreation || reloaded.options.MaxOwnedTeams != 2 || reloaded.options.Visibility.Level(smmodel.AreaStats) != smmodel.LevelLoggedIn {
		t.Errorf("options were not persisted: %+v", reloaded.options)
	}
}

func deleteAccountAs(ah *AccountHandler, as *smmodel.Account, id string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/accounts/delete/"+id, nil)
	req.Header.Set("Referer", "/accounts")
	req = smweb.WithScope(req, &smweb.Scope{SessionStore: ah.accountManager.sessionStore})
	req = smweb.WithAccount(req, as)

	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("id", id)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeContext))

	rec := httptest.NewRecorder()
	ah.DeleteAccount(rec, req)

	return rec
}

func TestDeleteAccount_CallsHook(t *testing.T) {
	ah, accounts, cleanup := newTestHandler(t)
	defer cleanup()

	admin, victim := accounts[smmodel.GroupAdmin], accounts[smmodel.GroupRead]

	var deleted []uuid.UUID

	ah.SetAccountDeletedHook(func(id uuid.UUID) error {
		deleted = append(deleted, id)

		return nil
	})

	// nothing happens for an account that does not exist, and an administrator cannot delete themselves
	deleteAccountAs(ah, admin, uuid.New().String())
	deleteAccountAs(ah, admin, "not-a-uuid")
	deleteAccountAs(ah, admin, admin.ID.String())

	if len(deleted) != 0 {
		t.Fatalf("the hook ran for deletions that did not happen: %v", deleted)
	}

	deleteAccountAs(ah, admin, victim.ID.String())

	if len(deleted) != 1 || deleted[0] != victim.ID {
		t.Fatalf("expected the hook for the deleted account, got %v", deleted)
	}

	if _, err := ah.store.FindAccountByID(victim.ID.String()); err == nil {
		t.Error("the account must be deleted")
	}
}

func TestDeleteAccount_HookErrorDoesNotUndelete(t *testing.T) {
	ah, accounts, cleanup := newTestHandler(t)
	defer cleanup()

	victim := accounts[smmodel.GroupWrite]

	ah.SetAccountDeletedHook(func(uuid.UUID) error { return errors.New("boom") })

	rec := deleteAccountAs(ah, accounts[smmodel.GroupAdmin], victim.ID.String())

	if rec.Code != http.StatusFound {
		t.Errorf("expected the usual redirect, got %d", rec.Code)
	}

	if _, err := ah.store.FindAccountByID(victim.ID.String()); err == nil {
		t.Error("a failing hook must not bring the account back")
	}
}

func TestDeleteAccount_WithoutHook(t *testing.T) {
	ah, accounts, cleanup := newTestHandler(t)
	defer cleanup()

	victim := accounts[smmodel.GroupDelete]

	deleteAccountAs(ah, accounts[smmodel.GroupAdmin], victim.ID.String())

	if _, err := ah.store.FindAccountByID(victim.ID.String()); err == nil {
		t.Error("the account must be deleted")
	}
}
