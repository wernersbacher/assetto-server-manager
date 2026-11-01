package accounts

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

const testSteamID = "76561198000000042"

func steamRequest(ah *AccountHandler) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/login/steam", nil)

	return smweb.WithScope(req, &smweb.Scope{SessionStore: ah.accountManager.sessionStore})
}

func sessionAccount(t *testing.T, ah *AccountHandler, req *http.Request) string {
	t.Helper()

	id, _ := smweb.Session(req).Values[sessionAccountID].(string)

	return id
}

func TestLoginWithSteam_ExistingAccount(t *testing.T) {
	ah, _, cleanup := newTestHandler(t)
	defer cleanup()

	existing := smmodel.NewAccount(testServerID, "test")
	existing.Name = "Someone"
	existing.GUID = testSteamID

	if err := ah.store.UpsertAccount(existing); err != nil {
		t.Fatal(err)
	}

	// works without the sign up option
	req, rec := steamRequest(ah), httptest.NewRecorder()
	account, created, err := ah.accountManager.loginWithSteam(req, rec, testSteamID)

	if err != nil || created || account.ID != existing.ID {
		t.Fatalf("account %v created %v err %v", account, created, err)
	}

	if got := sessionAccount(t, ah, req); got != existing.ID.String() {
		t.Errorf("session account %q, want %q", got, existing.ID)
	}

	if rec.Header().Get("Set-Cookie") == "" {
		t.Error("the session cookie is set in the same response")
	}
}

func TestLoginWithSteam_NewAccount(t *testing.T) {
	ah, _, cleanup := newTestHandler(t)
	defer cleanup()

	// sign up off: no account, no session
	req := steamRequest(ah)

	if _, _, err := ah.accountManager.loginWithSteam(req, httptest.NewRecorder(), testSteamID); err != ErrSteamSignupDisabled {
		t.Fatalf("expected ErrSteamSignupDisabled, got %v", err)
	}

	if sessionAccount(t, ah, req) != "" {
		t.Error("no session without sign up")
	}

	if _, err := ah.store.FindAccountByGUID(testSteamID); err == nil {
		t.Fatal("no account may be created")
	}

	// sign up on
	ah.accountManager.options.SteamSignup = true
	req = steamRequest(ah)
	account, created, err := ah.accountManager.loginWithSteam(req, httptest.NewRecorder(), testSteamID)

	if err != nil || !created {
		t.Fatalf("created %v err %v", created, err)
	}

	if account.Name != "steam-"+testSteamID || account.GUID != testSteamID || !account.IsPasswordless() {
		t.Errorf("unexpected account %+v", account)
	}

	if account.Groups[testServerID] != smmodel.GroupRead {
		t.Errorf("group %v, want read", account.Groups[testServerID])
	}

	if sessionAccount(t, ah, req) != account.ID.String() {
		t.Error("session must hold the account id")
	}

	// the second login finds the same account
	again, created, err := ah.accountManager.loginWithSteam(steamRequest(ah), httptest.NewRecorder(), testSteamID)

	if err != nil || created || again.ID != account.ID {
		t.Fatalf("second login: %v %v %v", again, created, err)
	}
}

func TestLoginWithSteam_DeletedAccount(t *testing.T) {
	ah, _, cleanup := newTestHandler(t)
	defer cleanup()

	ah.accountManager.options.SteamSignup = true

	deleted := smmodel.NewAccount(testServerID, "test")
	deleted.Name = "steam-" + testSteamID
	deleted.GUID = testSteamID
	deleted.Deleted = time.Now()

	if err := ah.store.UpsertAccount(deleted); err != nil {
		t.Fatal(err)
	}

	req := steamRequest(ah)

	if _, _, err := ah.accountManager.loginWithSteam(req, httptest.NewRecorder(), testSteamID); err == nil {
		t.Fatal("a deleted account must not log in")
	}

	if sessionAccount(t, ah, req) != "" {
		t.Error("no session for a deleted account")
	}
}

func TestLoginWithSteam_RejectsOddIDs(t *testing.T) {
	ah, _, cleanup := newTestHandler(t)
	defer cleanup()

	ah.accountManager.options.SteamSignup = true

	for _, id := range []string{"", "abc", "123;456", testSteamID + "x"} {
		if _, _, err := ah.accountManager.loginWithSteam(steamRequest(ah), httptest.NewRecorder(), id); err == nil {
			t.Errorf("id %q must be rejected", id)
		}
	}
}

func TestSteamLogin_Redirects(t *testing.T) {
	ah, _, cleanup := newTestHandler(t)
	defer cleanup()

	h := ah.RequestScopeMiddleware(ah.SteamLogin())

	// start: to Steam
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login/steam", nil))

	if rec.Code != http.StatusFound {
		t.Errorf("start: status %d", rec.Code)
	}

	// a steamGUID in the query is ignored, and no session is started without Steam's answer
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login/steam?steamGUID="+testSteamID, nil))

	if rec.Code != http.StatusFound || rec.Header().Get("Set-Cookie") != "" {
		t.Errorf("steamGUID query: status %d cookie %q", rec.Code, rec.Header().Get("Set-Cookie"))
	}

	// cancelled at Steam: back to the login page
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login/steam?openid.mode=cancel", nil))

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/login" {
		t.Errorf("cancel: status %d location %q", rec.Code, rec.Header().Get("Location"))
	}
}
