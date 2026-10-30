package web_test

import (
	"encoding/json"
	"io/ioutil"
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

const formType = "application/x-www-form-urlencoded"

// bodyKept sends a POST through the middleware (and ScheduleTarget) and returns the slot, the body the handler could
// read, and the slot ScheduleTarget selected.
func bodyKept(t *testing.T, contentType, target, body string) (smmodel.ServerID, smmodel.ServerID, string) {
	t.Helper()

	scope := &smweb.Scope{SessionStore: sessions.NewCookieStore([]byte("test"))}

	var seen, scheduled smmodel.ServerID
	var got []byte

	handler := smweb.ServerSelectionMiddleware(func(id smmodel.ServerID) bool {
		return id == "a" || id == "b"
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = smweb.RequestedServerID(r)
		scheduled = smweb.RequestedServerID(smweb.ScheduleTarget(r))

		var err error
		got, err = ioutil.ReadAll(r.Body)

		if err != nil {
			t.Error(err)
		}
	}))

	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)

	handler.ServeHTTP(httptest.NewRecorder(), smweb.WithScope(req, scope))

	return seen, scheduled, string(got)
}

func TestServerSelectionKeepsBody(t *testing.T) {
	small := `[{"name":"a","dataBase64":"` + strings.Repeat("QUJD", 1000) + `"}]`
	large := `[{"name":"a","dataBase64":"` + strings.Repeat("QUJD", 3*1024*1024) + `"}]` // ~12 MB

	for name, body := range map[string]string{"small": small, "12MB": large} {
		seen, scheduled, got := bodyKept(t, formType, "/upload", body)

		if got != body {
			t.Errorf("%s: body was altered (%d of %d bytes)", name, len(got), len(body))
		}

		if seen != "" || scheduled != "" {
			t.Errorf("%s: unexpected slot %q / %q", name, seen, scheduled)
		}
	}

	var decoded []map[string]string

	if err := json.Unmarshal([]byte(small), &decoded); err != nil {
		t.Fatal(err)
	}

	t.Run("server in a small form body", func(t *testing.T) {
		body := "Name=x&server=b&event-schedule-server=a"
		seen, scheduled, got := bodyKept(t, formType, "/x", body)

		if seen != "b" || scheduled != "a" || got != body {
			t.Errorf("got slot %q, schedule %q, body %q", seen, scheduled, got)
		}
	})

	t.Run("server in a large body is not used", func(t *testing.T) {
		body := "server=b&pad=" + strings.Repeat("x", 2<<20)
		seen, scheduled, got := bodyKept(t, formType, "/x", body)

		if seen != "" || scheduled != "" || got != body {
			t.Errorf("got slot %q, schedule %q, body length %d of %d", seen, scheduled, len(got), len(body))
		}
	})

	t.Run("other content types are untouched", func(t *testing.T) {
		body := "server=b"
		seen, _, got := bodyKept(t, "application/json", "/x", body)

		if seen != "" || got != body {
			t.Errorf("got slot %q, body %q", seen, got)
		}
	})

	t.Run("query wins and body is kept", func(t *testing.T) {
		seen, _, got := bodyKept(t, formType, "/x?server=a", "server=b")

		if seen != "a" || got != "server=b" {
			t.Errorf("got slot %q, body %q", seen, got)
		}
	})
}
