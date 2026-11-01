package steam

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func fakeSteam(t *testing.T, valid bool) func() {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()

		if r.Form.Get("openid.mode") != "check_authentication" || r.Form.Get("openid.sig") != "abc" {
			t.Errorf("unexpected verification request: %v", r.Form)
		}

		if valid {
			_, _ = w.Write([]byte("ns:http://specs.openid.net/auth/2.0\nis_valid:true\n"))
		} else {
			_, _ = w.Write([]byte("ns:http://specs.openid.net/auth/2.0\nis_valid:false\n"))
		}
	}))

	old := loginURL
	loginURL = srv.URL

	return func() {
		loginURL = old
		srv.Close()
	}
}

func TestRedirectToSteamLogin(t *testing.T) {
	defer fakeSteam(t, true)()

	h := (&LoginHandler{}).RedirectToSteamLogin(func(r *http.Request) string { return "/back" })

	// start: redirect to Steam, return_to follows the proxy headers
	req := httptest.NewRequest("GET", "http://internal:8772/championship/1/sign-up/steam", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "race.example.com")
	rec := httptest.NewRecorder()
	h(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status %d, want 302", rec.Code)
	}

	loc, _ := url.Parse(rec.Header().Get("Location"))

	if got := loc.Query().Get("openid.return_to"); got != "https://race.example.com/championship/1/sign-up/steam" {
		t.Errorf("return_to = %q", got)
	}

	if got := loc.Query().Get("openid.realm"); got != "https://race.example.com" {
		t.Errorf("realm = %q", got)
	}

	// return from Steam
	q := url.Values{}
	q.Set("openid.mode", "id_res")
	q.Set("openid.ns", openIDNamespace)
	q.Set("openid.sig", "abc")
	q.Set("openid.signed", "signed,claimed_id")
	q.Set("openid.claimed_id", "https://steamcommunity.com/openid/id/76561198000000001")
	q.Set("openid.return_to", "https://race.example.com/championship/1/sign-up/steam")

	req = httptest.NewRequest("GET", "http://internal:8772/championship/1/sign-up/steam?"+q.Encode(), nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "race.example.com")
	rec = httptest.NewRecorder()
	h(rec, req)

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "/back?steamGUID=76561198000000001") {
		t.Fatalf("status %d body %q", rec.Code, rec.Body.String())
	}

	// without the proxy headers return_to does not match: error instead of a silent failure
	req = httptest.NewRequest("GET", "http://internal:8772/championship/1/sign-up/steam?"+q.Encode(), nil)
	rec = httptest.NewRecorder()
	h(rec, req)

	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "return_to") {
		t.Fatalf("status %d body %q", rec.Code, rec.Body.String())
	}
}

func TestRedirectToSteamLoginRejectsInvalid(t *testing.T) {
	defer fakeSteam(t, false)()

	q := url.Values{}
	q.Set("openid.mode", "id_res")
	q.Set("openid.sig", "abc")
	q.Set("openid.claimed_id", "https://steamcommunity.com/openid/id/76561198000000001")
	q.Set("openid.return_to", "http://example.com/x")

	req := httptest.NewRequest("GET", "http://example.com/x?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	(&LoginHandler{}).RedirectToSteamLogin(func(r *http.Request) string { return "/back" })(rec, req)

	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "steamGUID") {
		t.Fatalf("status %d body %q", rec.Code, rec.Body.String())
	}
}
