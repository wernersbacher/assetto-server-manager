package accounts

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

func postSkinOptions(ah *AccountHandler, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/accounts/skin-options", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "/accounts")
	req = smweb.WithScope(req, &smweb.Scope{SessionStore: ah.accountManager.sessionStore})

	rec := httptest.NewRecorder()
	ah.SetSkinSettings(rec, req)

	return rec
}

func skinForm(overrides map[string]string) url.Values {
	form := url.Values{
		"uploads": {"on"}, "global": {"off"},
		"max_files": {"10"}, "max_skin_mb": {"20"}, "user_quota_mb": {"300"}, "min_free_mb": {"4096"},
	}

	for k, v := range overrides {
		form.Set(k, v)
	}

	return form
}

func TestSkinSettings_Default(t *testing.T) {
	ah, _, cleanup := newTestHandler(t)
	defer cleanup()

	s := ah.SkinSettings()

	if !s.UploadsEnabled() || s.GlobalShare {
		t.Errorf("by default users can upload but not share skins globally: %+v", s)
	}

	l := s.Limits()

	if l.MaxFiles != smmodel.DefaultSkinMaxFiles || l.MaxSkinBytes != 100<<20 || l.UserQuotaBytes != 500<<20 || l.MinFreeBytes != 2048<<20 {
		t.Errorf("unexpected default limits: %+v", l)
	}
}

func TestSetSkinSettings(t *testing.T) {
	ah, _, cleanup := newTestHandler(t)
	defer cleanup()

	if rec := postSkinOptions(ah, skinForm(map[string]string{"global": "on"})); rec.Code != http.StatusFound {
		t.Fatalf("expected a redirect, got %d: %s", rec.Code, rec.Body.String())
	}

	want := smmodel.SkinSettings{GlobalShare: true, MaxFiles: 10, MaxSkinMB: 20, UserQuotaMB: 300, MinFreeMB: 4096}

	if got := ah.SkinSettings(); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}

	if l := ah.SkinSettings().Limits(); l.MaxFiles != 10 || l.MaxSkinBytes != 20<<20 || l.UserQuotaBytes != 300<<20 || l.MinFreeBytes != 4096<<20 {
		t.Errorf("unexpected limits: %+v", l)
	}

	// persisted
	reloaded := NewAccountManager(ah.config, ah.store, ah.accountManager.sessionStore, "test")

	if err := reloaded.LoadOptions(); err != nil {
		t.Fatal(err)
	}

	if reloaded.options.Skins != want {
		t.Errorf("not persisted: %+v", reloaded.options.Skins)
	}
}

func TestSetSkinSettings_RejectsBadInput(t *testing.T) {
	ah, _, cleanup := newTestHandler(t)
	defer cleanup()

	if rec := postSkinOptions(ah, skinForm(nil)); rec.Code != http.StatusFound {
		t.Fatalf("setup failed: %d", rec.Code)
	}

	for name, form := range map[string]url.Values{
		"no values":     {},
		"bad uploads":   skinForm(map[string]string{"uploads": "maybe"}),
		"bad global":    skinForm(map[string]string{"global": ""}),
		"negative":      skinForm(map[string]string{"user_quota_mb": "-1"}),
		"text":          skinForm(map[string]string{"max_files": "many"}),
		"decimal":       skinForm(map[string]string{"max_skin_mb": "1.5"}),
		"too many":      skinForm(map[string]string{"max_files": "100001"}),
		"too large":     skinForm(map[string]string{"min_free_mb": "1000001"}),
		"missing quota": func() url.Values { f := skinForm(nil); f.Del("user_quota_mb"); return f }(),
	} {
		if rec := postSkinOptions(ah, form); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d", name, rec.Code)
		}
	}

	if got := ah.SkinSettings(); got.MaxFiles != 10 || got.UserUploadsDisabled {
		t.Errorf("the previous settings must stay, got %+v", got)
	}
}

func TestSkinSettings_OldOptionsReadAsDefault(t *testing.T) {
	ah, _, cleanup := newTestHandler(t)
	defer cleanup()

	if err := ah.store.SetMeta(serverAccountOptionsMetaKey, struct{ IsOpen, PublicStats bool }{true, true}); err != nil {
		t.Fatal(err)
	}

	if err := ah.accountManager.LoadOptions(); err != nil {
		t.Fatal(err)
	}

	if ah.SkinSettings() != (smmodel.SkinSettings{}) {
		t.Errorf("old options must read as the default (uploads on, no global sharing): %+v", ah.SkinSettings())
	}
}

func TestSetSkinSettings_UploadsOff(t *testing.T) {
	ah, _, cleanup := newTestHandler(t)
	defer cleanup()

	if rec := postSkinOptions(ah, skinForm(map[string]string{"uploads": "off"})); rec.Code != http.StatusFound {
		t.Fatalf("expected a redirect, got %d: %s", rec.Code, rec.Body.String())
	}

	if s := ah.SkinSettings(); s.UploadsEnabled() || !s.UserUploadsDisabled || s.GlobalShare {
		t.Errorf("uploads must be off: %+v", s)
	}
}
