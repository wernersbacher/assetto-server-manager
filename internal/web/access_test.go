package web

import (
	"net/http/httptest"
	"testing"

	"github.com/cj123/sessions"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

func TestReadAccess(t *testing.T) {
	const serverID smmodel.ServerID = "test-server"

	closed := smmodel.Visibility{}

	for _, a := range smmodel.AllAreas {
		closed[a] = smmodel.LevelLoggedIn
	}

	withAccount := func(account *smmodel.Account, open bool) func() bool {
		r := httptest.NewRequest("GET", "/login", nil)

		scope := &Scope{
			SessionStore: sessions.NewCookieStore([]byte("test")),
			ServerID:     serverID,
		}

		if !open {
			scope.Visibility = closed
		}

		r = WithScope(r, scope)

		if account != nil {
			r = WithAccount(r, account)
		}

		return ReadAccess(r)
	}

	reader := &smmodel.Account{Name: "reader", Groups: map[smmodel.ServerID]smmodel.Group{serverID: smmodel.GroupRead}}
	noAccess := &smmodel.Account{Name: "nobody", Groups: map[smmodel.ServerID]smmodel.Group{serverID: smmodel.GroupNoAccess}}

	for _, open := range []bool{false, true} {
		if got := withAccount(nil, open)(); got != open {
			t.Errorf("anonymous visitor (open=%t): expected read access %t, got %t", open, open, got)
		}

		if !withAccount(reader, open)() {
			t.Errorf("reader (open=%t): expected read access", open)
		}

		if withAccount(noAccess, open)() {
			t.Errorf("no_access account (open=%t): expected no read access", open)
		}
	}
}
