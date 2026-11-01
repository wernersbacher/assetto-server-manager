package accounts

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

func TestRateLimiter(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	l := newRateLimiter(3, time.Minute, func() time.Time { return now })

	for i := 0; i < 3; i++ {
		if !l.allow("a") {
			t.Fatalf("event %d must be allowed", i)
		}
	}

	if l.allow("a") {
		t.Fatal("the fourth event within the window must be refused")
	}

	if !l.allow("b") {
		t.Error("another key has its own budget")
	}

	// refused events are not counted: after the window the budget is back
	now = now.Add(time.Minute + time.Second)

	for i := 0; i < 3; i++ {
		if !l.allow("a") {
			t.Fatalf("after the window event %d must be allowed", i)
		}
	}

	// old keys are forgotten
	now = now.Add(time.Hour)
	l.allow("c")

	if len(l.events) != 1 {
		t.Errorf("expected only the current key to be kept, got %d keys", len(l.events))
	}
}

func TestSteamLogin_RateLimitPerIP(t *testing.T) {
	ah, _, cleanup := newTestHandler(t)
	defer cleanup()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ah.accountManager.now = func() time.Time { return now }

	h := ah.RequestScopeMiddleware(ah.SteamLogin())

	request := func(addr string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/login/steam", nil)
		req.RemoteAddr = addr
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		return rec
	}

	for i := 0; i < steamLoginsPerIP; i++ {
		// different ports, same IP
		if rec := request(fmt.Sprintf("203.0.113.7:%d", 1000+i)); rec.Code != http.StatusFound {
			t.Fatalf("request %d: %d", i, rec.Code)
		}
	}

	if rec := request("203.0.113.7:9999"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}

	// the RealIP middleware leaves the bare IP in RemoteAddr
	if rec := request("203.0.113.7"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("bare IP: expected 429, got %d", rec.Code)
	}

	if rec := request("203.0.113.8:1"); rec.Code != http.StatusFound {
		t.Fatalf("another IP must not be limited, got %d", rec.Code)
	}

	now = now.Add(steamLoginWindow + time.Second)

	if rec := request("203.0.113.7:1"); rec.Code != http.StatusFound {
		t.Fatalf("after the window: %d", rec.Code)
	}
}

func TestLoginWithSteam_SignupLimit(t *testing.T) {
	ah, _, cleanup := newTestHandler(t)
	defer cleanup()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ah.accountManager.now = func() time.Time { return now }

	id := func(i int) string { return fmt.Sprintf("765611980000%05d", i) }

	for i := 0; i < steamSignupsLimit; i++ {
		if _, created, err := ah.accountManager.loginWithSteam(steamRequest(ah), httptest.NewRecorder(), id(i)); err != nil || !created {
			t.Fatalf("signup %d: created %v err %v", i, created, err)
		}
	}

	req := steamRequest(ah)

	if _, _, err := ah.accountManager.loginWithSteam(req, httptest.NewRecorder(), id(1000)); err != ErrSteamSignupLimit {
		t.Fatalf("expected ErrSteamSignupLimit, got %v", err)
	}

	if _, err := ah.store.FindAccountByGUID(id(1000)); err == nil {
		t.Error("no account may be created over the limit")
	}

	if sessionAccount(t, ah, req) != "" {
		t.Error("no session over the limit")
	}

	// existing accounts still log in
	if _, created, err := ah.accountManager.loginWithSteam(steamRequest(ah), httptest.NewRecorder(), id(0)); err != nil || created {
		t.Errorf("an existing account must log in: created %v err %v", created, err)
	}

	// admin created accounts do not count, and after the window it works again
	now = now.Add(steamSignupWindow + time.Second)

	if _, created, err := ah.accountManager.loginWithSteam(steamRequest(ah), httptest.NewRecorder(), id(1000)); err != nil || !created {
		t.Errorf("after the window: created %v err %v", created, err)
	}
}

func deleteOwnRequest(ah *AccountHandler, account *smmodel.Account) (*http.Request, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(http.MethodPost, "/accounts/delete-own", nil)
	req = smweb.WithScope(req, &smweb.Scope{SessionStore: ah.accountManager.sessionStore})
	req = smweb.WithAccount(req, account)

	if account.ID != uuid.Nil {
		smweb.Session(req).Values[sessionAccountID] = account.ID.String()
	}

	return req, httptest.NewRecorder()
}

func TestDeleteOwnAccount(t *testing.T) {
	ah, _, cleanup := newTestHandler(t)
	defer cleanup()

	account, err := ah.accountManager.createSteamAccount(testSteamID, smmodel.GroupRead, "Max")

	if err != nil {
		t.Fatal(err)
	}

	var cleaned []uuid.UUID

	ah.SetAccountDeletedHook(func(id uuid.UUID) error {
		cleaned = append(cleaned, id)
		return nil
	})

	req, rec := deleteOwnRequest(ah, account)
	ah.DeleteOwnAccount(rec, req)

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
		t.Fatalf("expected a redirect to /, got %d %q", rec.Code, rec.Header().Get("Location"))
	}

	if _, err := ah.store.FindAccountByID(account.ID.String()); err == nil {
		t.Error("the account must be deleted")
	}

	if len(cleaned) != 1 || cleaned[0] != account.ID {
		t.Errorf("the deleted hook must run once for the account, got %v", cleaned)
	}

	if sessionAccount(t, ah, req) != "" {
		t.Error("the session must be ended")
	}
}

func TestDeleteOwnAccount_NotForAdminAndVisitors(t *testing.T) {
	ah, _, cleanup := newTestHandler(t)
	defer cleanup()

	admin := smmodel.NewAccount(testServerID, "test")
	admin.Name = smmodel.AdminUserName
	admin.PasswordHash = "x"

	if err := ah.store.UpsertAccount(admin); err != nil {
		t.Fatal(err)
	}

	called := false
	ah.SetAccountDeletedHook(func(uuid.UUID) error { called = true; return nil })

	for name, account := range map[string]*smmodel.Account{
		"admin":     admin,
		"free":      ah.accountManager.openAccount,
		"anonymous": {},
	} {
		req, rec := deleteOwnRequest(ah, account)
		ah.DeleteOwnAccount(rec, req)

		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/accounts/update" {
			t.Errorf("%s: expected a redirect back, got %d %q", name, rec.Code, rec.Header().Get("Location"))
		}
	}

	if _, err := ah.store.FindAccountByID(admin.ID.String()); err != nil {
		t.Error("the admin account must stay")
	}

	if called {
		t.Error("the deleted hook must not run")
	}
}
