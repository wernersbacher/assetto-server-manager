package accounts

import (
	"net/http"
	"testing"

	"github.com/wernersbacher/ACPitwall/internal/downloads"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

func TestDownloadAccessMiddleware(t *testing.T) {
	ah, accounts, cleanup := newTestHandler(t)
	defer cleanup()

	// the download rule sits behind read access, like in the router
	chain := func(next http.Handler) http.Handler {
		return ah.ReadAccessMiddleware(ah.DownloadAccessMiddleware(next))
	}

	type result struct{ anonymous, read, admin int }

	for _, c := range []struct {
		access downloads.Access
		open   bool
		want   result
	}{
		// public keeps the behaviour from before the option: on a closed server only the login matters
		{downloads.Public, true, result{http.StatusOK, http.StatusOK, http.StatusOK}},
		{downloads.Public, false, result{http.StatusFound, http.StatusOK, http.StatusOK}},
		{downloads.LoggedIn, true, result{http.StatusFound, http.StatusOK, http.StatusOK}},
		{downloads.LoggedIn, false, result{http.StatusFound, http.StatusOK, http.StatusOK}},
		{downloads.Off, true, result{http.StatusForbidden, http.StatusForbidden, http.StatusOK}},
		{downloads.Off, false, result{http.StatusFound, http.StatusForbidden, http.StatusOK}},
	} {
		ah.accountManager.options.IsOpen = c.open
		ah.accountManager.options.DownloadAccess = c.access

		got := result{
			anonymous: requestAs(t, ah, nil, chain).Code,
			read:      requestAs(t, ah, accounts[smmodel.GroupRead], chain).Code,
			admin:     requestAs(t, ah, accounts[smmodel.GroupAdmin], chain).Code,
		}

		if got != c.want {
			t.Errorf("access=%s open=%t: got %+v, want %+v", c.access, c.open, got, c.want)
		}
	}
}

func TestDownloadAccessOptionPersistence(t *testing.T) {
	ah, _, cleanup := newTestHandler(t)
	defer cleanup()

	// options stored before the option existed read as public
	if err := ah.store.SetMeta(serverAccountOptionsMetaKey, struct{ IsOpen, PublicStats bool }{true, true}); err != nil {
		t.Fatal(err)
	}

	if err := ah.accountManager.LoadOptions(); err != nil || ah.DownloadAccess() != downloads.Public {
		t.Fatalf("expected public after loading old options, got %v (%v)", ah.DownloadAccess(), err)
	}

	if err := ah.store.SetMeta(serverAccountOptionsMetaKey, ServerAccountOptions{IsOpen: true, DownloadAccess: downloads.LoggedIn}); err != nil {
		t.Fatal(err)
	}

	if err := ah.accountManager.LoadOptions(); err != nil || ah.DownloadAccess() != downloads.LoggedIn {
		t.Fatalf("expected login after loading, got %v (%v)", ah.DownloadAccess(), err)
	}
}

func TestParseAccess(t *testing.T) {
	for _, a := range []downloads.Access{downloads.Public, downloads.LoggedIn, downloads.Off} {
		if got, ok := downloads.ParseAccess(a.String()); !ok || got != a {
			t.Errorf("round trip of %s failed", a)
		}
	}

	if _, ok := downloads.ParseAccess("nonsense"); ok {
		t.Error("nonsense accepted")
	}
}
