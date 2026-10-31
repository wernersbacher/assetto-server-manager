package accounts

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cj123/sessions"
	"github.com/etcd-io/bbolt"

	smconfig "github.com/wernersbacher/ACPitwall/internal/config"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

func TestStatsAccessMiddleware(t *testing.T) {
	ah, accounts, cleanup := newTestHandler(t)
	defer cleanup()

	for _, open := range []bool{false, true} {
		for _, public := range []bool{false, true} {
			ah.accountManager.options.IsOpen = open
			ah.accountManager.options.PublicStats = public

			// anonymous visitors only get in on an open server with public statistics
			rec := requestAs(t, ah, nil, ah.StatsAccessMiddleware)

			if open && public {
				if rec.Code != http.StatusOK {
					t.Errorf("open=%t public=%t anonymous: expected access, got %d", open, public, rec.Code)
				}
			} else if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/login" {
				t.Errorf("open=%t public=%t anonymous: expected redirect to /login, got %d %s", open, public, rec.Code, rec.Header().Get("Location"))
			}

			// accounts with read access always get in
			for _, group := range []smmodel.Group{smmodel.GroupRead, smmodel.GroupWrite, smmodel.GroupAdmin} {
				if rec := requestAs(t, ah, accounts[group], ah.StatsAccessMiddleware); rec.Code != http.StatusOK {
					t.Errorf("open=%t public=%t %s: expected access, got %d", open, public, group, rec.Code)
				}
			}

			if rec := requestAs(t, ah, accounts[smmodel.GroupNoAccess], ah.StatsAccessMiddleware); rec.Code != http.StatusFound {
				t.Errorf("open=%t public=%t no_access: expected redirect, got %d", open, public, rec.Code)
			}

			// other read pages are unaffected by the option
			if rec := requestAs(t, ah, nil, ah.ReadAccessMiddleware); (rec.Code == http.StatusOK) != open {
				t.Errorf("open=%t public=%t anonymous on a read page: got %d", open, public, rec.Code)
			}
		}
	}
}

func TestStatsAccessFunc(t *testing.T) {
	ah, accounts, cleanup := newTestHandler(t)
	defer cleanup()

	check := func(account *smmodel.Account, open, public, expected bool) {
		ah.accountManager.options.IsOpen = open
		ah.accountManager.options.PublicStats = public

		var got bool

		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = smweb.StatsAccess(r)()
		})

		// the read middleware lets anonymous visitors of an open server in with the dummy account
		handler := ah.RequestScopeMiddleware(ah.ReadAccessMiddleware(inner))
		req := httptest.NewRequest("GET", "/page", nil)

		if account != nil {
			rec := httptest.NewRecorder()
			scoped := smweb.WithScope(req, &smweb.Scope{SessionStore: ah.accountManager.sessionStore})
			sess := smweb.Session(scoped)
			sess.Values[sessionAccountID] = account.ID.String()
			_ = sess.Save(scoped, rec)

			for _, c := range rec.Result().Cookies() {
				req.AddCookie(c)
			}
		}

		handler.ServeHTTP(httptest.NewRecorder(), req)

		if got != expected {
			t.Errorf("account=%v open=%t public=%t: expected StatsAccess %t, got %t", account != nil, open, public, expected, got)
		}
	}

	reader := accounts[smmodel.GroupRead]

	check(nil, true, false, false)
	check(nil, true, true, true)
	check(reader, true, false, true)
	check(reader, false, false, true)
	check(reader, false, true, true)
}

func TestPublicStatsOptionPersistence(t *testing.T) {
	dir, err := ioutil.TempDir("", "accounts-stats")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(dir)

	db, err := bbolt.Open(filepath.Join(dir, "server_manager.db"), 0644, &bbolt.Options{Timeout: 2 * time.Second})

	if err != nil {
		t.Fatal(err)
	}

	defer db.Close()

	stores := map[string]smstore.Store{
		"BoltStore": smstore.NewBoltStore(db),
		"JSONStore": smstore.NewJSONStore(filepath.Join(dir, "store"), filepath.Join(dir, "store-shared")),
	}

	for name, store := range stores {
		am := NewAccountManager(&smconfig.Configuration{}, store, sessions.NewCookieStore([]byte("test")), "test")

		// nothing stored (or stored by a version without the option): private
		if err := am.LoadOptions(); err != nil || am.options.PublicStats {
			t.Errorf("%s: expected private by default, got %+v (%v)", name, am.options, err)
		}

		if err := store.SetMeta(serverAccountOptionsMetaKey, struct{ IsOpen bool }{IsOpen: true}); err != nil {
			t.Fatal(err)
		}

		if err := am.LoadOptions(); err != nil || !am.options.IsOpen || am.options.PublicStats {
			t.Errorf("%s: expected open and private for legacy options, got %+v (%v)", name, am.options, err)
		}

		if err := store.SetMeta(serverAccountOptionsMetaKey, ServerAccountOptions{IsOpen: true, PublicStats: true}); err != nil {
			t.Fatal(err)
		}

		am = NewAccountManager(&smconfig.Configuration{}, store, sessions.NewCookieStore([]byte("test")), "test")

		if err := am.LoadOptions(); err != nil || !am.options.IsOpen || !am.options.PublicStats {
			t.Errorf("%s: expected round trip, got %+v (%v)", name, am.options, err)
		}
	}
}
