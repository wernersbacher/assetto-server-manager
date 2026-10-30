package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/cj123/sessions"

	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	smweb "github.com/wernersbacher/assetto-server-manager/internal/web"
)

// serve sends a request through the server selection middleware and returns the slot the handler saw, or the status
// code if the middleware answered itself.
func serve(t *testing.T, scope *smweb.Scope, req *http.Request) (smmodel.ServerID, int, []*http.Cookie) {
	t.Helper()

	var seen smmodel.ServerID

	handler := smweb.ServerSelectionMiddleware(func(id smmodel.ServerID) bool {
		return id == "a" || id == "b"
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = smweb.RequestedServerID(r)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, smweb.WithScope(req, scope))

	return seen, rec.Code, rec.Result().Cookies()
}

func TestServerSelectionMiddleware(t *testing.T) {
	scope := &smweb.Scope{SessionStore: sessions.NewCookieStore([]byte("test"))}

	t.Run("no parameter is the default slot", func(t *testing.T) {
		id, code, _ := serve(t, scope, httptest.NewRequest(http.MethodGet, "/live-timing", nil))

		if id != "" || code != http.StatusOK {
			t.Errorf("expected the default slot, got %q (%d)", id, code)
		}
	})

	t.Run("unknown slot", func(t *testing.T) {
		_, code, _ := serve(t, scope, httptest.NewRequest(http.MethodGet, "/live-timing?server=nope", nil))

		if code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", code)
		}
	})

	t.Run("query parameter", func(t *testing.T) {
		id, _, _ := serve(t, scope, httptest.NewRequest(http.MethodGet, "/live-timing?server=b", nil))

		if id != "b" {
			t.Errorf("expected slot b, got %q", id)
		}
	})

	t.Run("form parameter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/custom/new", strings.NewReader(url.Values{"server": {"a"}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		id, _, _ := serve(t, scope, req)

		if id != "a" {
			t.Errorf("expected slot a, got %q", id)
		}
	})

	t.Run("session fallback", func(t *testing.T) {
		_, _, cookies := serve(t, scope, httptest.NewRequest(http.MethodGet, "/live-timing?server=b", nil))

		if len(cookies) == 0 {
			t.Fatal("expected the slot to be remembered in a cookie")
		}

		req := httptest.NewRequest(http.MethodGet, "/custom", nil)

		for _, c := range cookies {
			req.AddCookie(c)
		}

		id, _, _ := serve(t, scope, req)

		if id != "b" {
			t.Errorf("expected the remembered slot b, got %q", id)
		}

		// the parameter wins over the session
		req = httptest.NewRequest(http.MethodGet, "/custom?server=a", nil)

		for _, c := range cookies {
			req.AddCookie(c)
		}

		id, _, _ = serve(t, scope, req)

		if id != "a" {
			t.Errorf("expected slot a from the parameter, got %q", id)
		}
	})

	t.Run("stale session is ignored", func(t *testing.T) {
		staleScope := &smweb.Scope{SessionStore: scope.SessionStore}

		_, _, cookies := serve(t, staleScope, httptest.NewRequest(http.MethodGet, "/?server=a", nil))

		// a slot which no longer exists
		handler := smweb.ServerSelectionMiddleware(func(id smmodel.ServerID) bool { return id == "b" })(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if id := smweb.RequestedServerID(r); id != "" {
				t.Errorf("expected the default slot, got %q", id)
			}
		}))

		req := httptest.NewRequest(http.MethodGet, "/", nil)

		for _, c := range cookies {
			req.AddCookie(c)
		}

		handler.ServeHTTP(httptest.NewRecorder(), smweb.WithScope(req, staleScope))
	})
}

func TestServerURL(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	if got := smweb.ServerURL(req, "/live-timing"); got != "/live-timing" {
		t.Errorf("unexpected URL %q", got)
	}

	req = smweb.WithRequestedServerID(req, "b")

	if got := smweb.ServerURL(req, "/live-timing"); got != "/live-timing?server=b" {
		t.Errorf("unexpected URL %q", got)
	}

	if got := smweb.ServerURL(req, "/event-details?custom-race=1"); got != "/event-details?custom-race=1&server=b" {
		t.Errorf("unexpected URL %q", got)
	}
}
