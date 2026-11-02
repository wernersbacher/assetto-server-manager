package accounts

import (
	"net/http"
	"net/http/httptest"
	"testing"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

func TestAreaAccessMiddleware(t *testing.T) {
	ah, accounts, cleanup := newTestHandler(t)
	defer cleanup()

	type principal struct {
		name    string
		account *smmodel.Account
		// allowed for the levels public, login, write, admin
		allowed [4]bool
	}

	principals := []principal{
		{"anonymous", nil, [4]bool{true, false, false, false}},
		{"no_access", accounts[smmodel.GroupNoAccess], [4]bool{true, false, false, false}},
		{"read", accounts[smmodel.GroupRead], [4]bool{true, true, false, false}},
		{"write", accounts[smmodel.GroupWrite], [4]bool{true, true, true, false}},
		{"admin", accounts[smmodel.GroupAdmin], [4]bool{true, true, true, true}},
	}

	for _, area := range smmodel.AllAreas {
		for i, level := range smmodel.AllLevels {
			ah.accountManager.options.Visibility = smmodel.Visibility{area: level}

			for _, p := range principals {
				rec := requestAs(t, ah, p.account, ah.AreaAccessMiddleware(area))

				if p.allowed[i] {
					if rec.Code != http.StatusOK {
						t.Errorf("area %s level %s %s: expected access, got %d", area, level, p.name, rec.Code)
					}

					continue
				}

				if rec.Code != http.StatusFound {
					t.Errorf("area %s level %s %s: expected a redirect, got %d", area, level, p.name, rec.Code)
				}

				// visitors without a session and accounts without access are sent to the login, others home
				wantLocation := "/login"

				if p.account != nil && p.account.Groups[testServerID] != smmodel.GroupNoAccess {
					wantLocation = "/"
				}

				if got := rec.Header().Get("Location"); got != wantLocation {
					t.Errorf("area %s level %s %s: expected a redirect to %s, got %s", area, level, p.name, wantLocation, got)
				}
			}
		}
	}
}

func TestAreaAccessUsesDefaults(t *testing.T) {
	ah, accounts, cleanup := newTestHandler(t)
	defer cleanup()

	// nothing configured: results are public, teams need a login, custom races need write access
	if rec := requestAs(t, ah, nil, ah.AreaAccessMiddleware(smmodel.AreaResults)); rec.Code != http.StatusOK {
		t.Errorf("results are public by default, got %d", rec.Code)
	}

	if rec := requestAs(t, ah, nil, ah.AreaAccessMiddleware(smmodel.AreaTeams)); rec.Code != http.StatusFound {
		t.Errorf("teams need a login by default, got %d", rec.Code)
	}

	if rec := requestAs(t, ah, accounts[smmodel.GroupRead], ah.AreaAccessMiddleware(smmodel.AreaCustomRaces)); rec.Code != http.StatusFound {
		t.Errorf("custom races need write access by default, got %d", rec.Code)
	}

	if rec := requestAs(t, ah, accounts[smmodel.GroupWrite], ah.AreaAccessMiddleware(smmodel.AreaCustomRaces)); rec.Code != http.StatusOK {
		t.Errorf("custom races are open for writers by default, got %d", rec.Code)
	}
}

func TestAnyAreaAccessMiddleware(t *testing.T) {
	ah, accounts, cleanup := newTestHandler(t)
	defer cleanup()

	if rec := requestAs(t, ah, nil, ah.AnyAreaAccessMiddleware); rec.Code != http.StatusOK {
		t.Errorf("anonymous with public areas: expected access, got %d", rec.Code)
	}

	closed := smmodel.Visibility{}

	for _, a := range smmodel.AllAreas {
		closed[a] = smmodel.LevelLoggedIn
	}

	ah.accountManager.options.Visibility = closed

	if rec := requestAs(t, ah, nil, ah.AnyAreaAccessMiddleware); rec.Code != http.StatusFound || rec.Header().Get("Location") != "/login" {
		t.Errorf("anonymous on a closed server: expected redirect to /login, got %d %s", rec.Code, rec.Header().Get("Location"))
	}

	if rec := requestAs(t, ah, accounts[smmodel.GroupRead], ah.AnyAreaAccessMiddleware); rec.Code != http.StatusOK {
		t.Errorf("reader on a closed server: expected access, got %d", rec.Code)
	}

	// routes without an area never let anonymous visitors in
	ah.accountManager.options.Visibility = nil

	if rec := requestAs(t, ah, nil, ah.ReadAccessMiddleware); rec.Code != http.StatusFound {
		t.Errorf("anonymous on a read page: expected redirect, got %d", rec.Code)
	}
}

func TestAreaAccessScope(t *testing.T) {
	ah, accounts, cleanup := newTestHandler(t)
	defer cleanup()

	ah.accountManager.options.Visibility = smmodel.Visibility{smmodel.AreaStats: smmodel.LevelLoggedIn}

	check := func(account *smmodel.Account, want bool) {
		t.Helper()

		var got bool

		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = smweb.StatsAccess(r)() && smweb.AreaAccess(r, smmodel.AreaStats)
		})

		// the area middleware of a public area lets anonymous visitors in with the dummy account
		rec := requestAsHandler(t, ah, account, ah.AreaAccessMiddleware(smmodel.AreaResults)(inner))

		if rec.Code != http.StatusOK {
			t.Fatalf("unexpected status %d", rec.Code)
		}

		if got != want {
			t.Errorf("account=%v: expected stats access %t, got %t", account != nil, want, got)
		}
	}

	check(nil, false)
	check(accounts[smmodel.GroupRead], true)

	ah.accountManager.options.Visibility = nil
	check(nil, true)
}

func requestAsHandler(t *testing.T, ah *AccountHandler, account *smmodel.Account, h http.Handler) *httptest.ResponseRecorder {
	return requestAs(t, ah, account, func(http.Handler) http.Handler { return h })
}

func TestSetVisibility(t *testing.T) {
	ah, _, cleanup := newTestHandler(t)
	defer cleanup()

	post := func(area, level string) int {
		req := httptest.NewRequest(http.MethodPost, "/accounts/visibility", nil)
		req.Form = map[string][]string{"area": {area}, "level": {level}}
		req.Header.Set("Referer", "/accounts")
		req = smweb.WithScope(req, &smweb.Scope{SessionStore: ah.accountManager.sessionStore})
		rec := httptest.NewRecorder()

		ah.SetVisibility(rec, req)

		return rec.Code
	}

	if code := post("teams", "public"); code != http.StatusFound {
		t.Fatalf("expected a redirect, got %d", code)
	}

	if ah.AreaLevel(smmodel.AreaTeams) != smmodel.LevelPublic {
		t.Error("the level was not set")
	}

	reloaded := NewAccountManager(ah.config, ah.store, ah.accountManager.sessionStore, "test")

	if err := reloaded.LoadOptions(); err != nil || reloaded.options.Visibility.Level(smmodel.AreaTeams) != smmodel.LevelPublic {
		t.Errorf("the level was not persisted: %+v (%v)", reloaded.options, err)
	}

	for _, c := range [][2]string{{"nonsense", "public"}, {"teams", "nonsense"}} {
		if code := post(c[0], c[1]); code != http.StatusBadRequest {
			t.Errorf("%v: expected 400, got %d", c, code)
		}
	}
}
