package accounts

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
	"github.com/wernersbacher/ACPitwall/internal/web/webtest"
)

// postLanguage posts the language selection, logged in as account if it is not nil.
func postLanguage(t *testing.T, ah *AccountHandler, account *smmodel.Account, lang, referer string) *httptest.ResponseRecorder {
	t.Helper()

	newRequest := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/language", strings.NewReader(url.Values{"lang": {lang}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Referer", referer)

		return smweb.WithScope(req, &smweb.Scope{SessionStore: ah.accountManager.sessionStore})
	}

	req := newRequest()
	rec := httptest.NewRecorder()

	if account != nil {
		sess := smweb.Session(req)
		sess.Values[sessionAccountID] = account.ID.String()

		if err := sess.Save(req, rec); err != nil {
			t.Fatal(err)
		}

		req = newRequest()

		for _, c := range rec.Result().Cookies() {
			req.AddCookie(c)
		}

		rec = httptest.NewRecorder()
	}

	ah.SetLanguage(rec, req)

	return rec
}

func languageCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == smweb.LanguageCookie {
			return c
		}
	}

	return nil
}

func TestSetLanguage(t *testing.T) {
	ah, accounts, cleanup := newTestHandler(t)
	defer cleanup()

	ah.BaseHandler = smweb.NewBaseHandler(webtest.Renderer(t, nil))

	t.Run("anonymous visitors get a cookie", func(t *testing.T) {
		rec := postLanguage(t, ah, nil, "de", "http://example.com/results?server=b")

		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/results?server=b" {
			t.Fatalf("expected a redirect back, got %d to %q", rec.Code, rec.Header().Get("Location"))
		}

		if c := languageCookie(rec); c == nil || c.Value != "de" || c.MaxAge <= 0 {
			t.Fatalf("expected a language cookie, got %+v", c)
		}
	})

	t.Run("unknown languages are refused", func(t *testing.T) {
		if rec := postLanguage(t, ah, nil, "xx", "/"); rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("redirects stay on the site", func(t *testing.T) {
		for _, referer := range []string{"", "https://evil.example//evil.example/x", "javascript:alert(1)", `http://a/\evil`} {
			if got := postLanguage(t, ah, nil, "en", referer).Header().Get("Location"); got != "/" {
				t.Errorf("referer %q: redirected to %q", referer, got)
			}
		}
	})

	t.Run("the account keeps the language", func(t *testing.T) {
		account := accounts[smmodel.GroupRead]

		postLanguage(t, ah, account, "de", "/")

		stored, err := ah.store.FindAccountByID(account.ID.String())

		if err != nil {
			t.Fatal(err)
		}

		if stored.Language != "de" {
			t.Fatalf("expected the account language de, got %q", stored.Language)
		}

		rec := postLanguage(t, ah, account, "", "/")

		if c := languageCookie(rec); c == nil || c.MaxAge >= 0 {
			t.Fatalf("expected the language cookie to be removed, got %+v", c)
		}

		stored, err = ah.store.FindAccountByID(account.ID.String())

		if err != nil {
			t.Fatal(err)
		}

		if stored.Language != "" {
			t.Fatalf("expected the browser language again, got %q", stored.Language)
		}
	})
}
