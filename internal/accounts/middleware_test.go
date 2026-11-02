package accounts

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/cj123/sessions"

	smconfig "github.com/wernersbacher/ACPitwall/internal/config"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

const testServerID smmodel.ServerID = "test-server"

// newTestHandler returns an AccountHandler with a JSON store, which contains one account per group.
func newTestHandler(t *testing.T) (*AccountHandler, map[smmodel.Group]*smmodel.Account, func()) {
	dir, err := ioutil.TempDir("", "accounts-test")

	if err != nil {
		t.Fatal(err)
	}

	store := smstore.NewJSONStore(filepath.Join(dir, "store"), filepath.Join(dir, "store-shared"))
	manager := NewAccountManager(&smconfig.Configuration{}, store, sessions.NewCookieStore([]byte("test")), "test-version")
	manager.Init(testServerID)

	accounts := make(map[smmodel.Group]*smmodel.Account)

	for _, group := range []smmodel.Group{smmodel.GroupNoAccess, smmodel.GroupRead, smmodel.GroupWrite, smmodel.GroupDelete, smmodel.GroupAdmin} {
		account := smmodel.NewAccount(testServerID, "test-version")
		account.Name = string(group)
		account.Groups[testServerID] = group

		if err := store.UpsertAccount(account); err != nil {
			t.Fatal(err)
		}

		accounts[group] = account
	}

	return NewAccountHandler(&smconfig.Configuration{}, nil, store, manager), accounts, func() { _ = os.RemoveAll(dir) }
}

// requestAs serves a request through the middleware for the given group. The account is nil for anonymous visitors.
func requestAs(t *testing.T, ah *AccountHandler, account *smmodel.Account, middleware func(http.Handler) http.Handler) *httptest.ResponseRecorder {
	var inner http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := ah.RequestScopeMiddleware(middleware(inner))

	req := httptest.NewRequest("GET", "/page", nil)
	rec := httptest.NewRecorder()

	if account != nil {
		// log in the way AccountManager.login does
		scoped := smweb.WithScope(req, &smweb.Scope{SessionStore: ah.accountManager.sessionStore})
		sess := smweb.Session(scoped)
		sess.Values[sessionAccountID] = account.ID.String()

		if err := sess.Save(scoped, rec); err != nil {
			t.Fatal(err)
		}

		for _, c := range rec.Result().Cookies() {
			req.AddCookie(c)
		}

		rec = httptest.NewRecorder()
	}

	handler.ServeHTTP(rec, req)

	return rec
}

func TestAccessMiddlewares(t *testing.T) {
	ah, accounts, cleanup := newTestHandler(t)
	defer cleanup()

	middlewares := map[smmodel.Group]func(http.Handler) http.Handler{
		smmodel.GroupRead:   ah.ReadAccessMiddleware,
		smmodel.GroupWrite:  ah.WriteAccessMiddleware,
		smmodel.GroupDelete: ah.DeleteAccessMiddleware,
		smmodel.GroupAdmin:  ah.AdminAccessMiddleware,
	}

	order := []smmodel.Group{smmodel.GroupRead, smmodel.GroupWrite, smmodel.GroupDelete, smmodel.GroupAdmin}
	level := map[smmodel.Group]int{smmodel.GroupNoAccess: -1, smmodel.GroupRead: 0, smmodel.GroupWrite: 1, smmodel.GroupDelete: 2, smmodel.GroupAdmin: 3}

	for _, required := range order {
		// anonymous visitors on a closed server are sent to the login page
		rec := requestAs(t, ah, nil, middlewares[required])

		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/login" {
			t.Errorf("anonymous on %s page: expected redirect to /login, got %d %s", required, rec.Code, rec.Header().Get("Location"))
		}

		for group, account := range accounts {
			rec := requestAs(t, ah, account, middlewares[required])

			switch {
			case group == smmodel.GroupNoAccess:
				if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/login" {
					t.Errorf("%s account on %s page: expected redirect to /login, got %d %s", group, required, rec.Code, rec.Header().Get("Location"))
				}
			case level[group] >= level[required]:
				if rec.Code != http.StatusOK {
					t.Errorf("%s account on %s page: expected access, got %d", group, required, rec.Code)
				}
			default:
				if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
					t.Errorf("%s account on %s page: expected redirect to /, got %d %s", group, required, rec.Code, rec.Header().Get("Location"))
				}
			}
		}
	}
}

func TestOpenAreaAllowsAnonymousReading(t *testing.T) {
	ah, _, cleanup := newTestHandler(t)
	defer cleanup()

	if rec := requestAs(t, ah, nil, ah.AreaAccessMiddleware(smmodel.AreaResults)); rec.Code != http.StatusOK {
		t.Errorf("anonymous on a public area: expected access, got %d", rec.Code)
	}

	if rec := requestAs(t, ah, nil, ah.WriteAccessMiddleware); rec.Code != http.StatusFound {
		t.Errorf("anonymous on a write page: expected redirect, got %d", rec.Code)
	}
}
