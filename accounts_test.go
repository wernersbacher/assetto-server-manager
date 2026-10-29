package servermanager

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/cj123/sessions"
)

// testRequestScope returns a request scope with a cookie session store and a closed server.
func testRequestScope() *requestScope {
	return &requestScope{
		sessionStore:   sessions.NewCookieStore([]byte("test")),
		accountOptions: &ServerAccountOptions{},
	}
}

func TestReadAccess(t *testing.T) {
	oldServerID := serverID
	serverID = "test-server"
	defer func() {
		serverID = oldServerID
	}()

	withAccount := func(account *Account, open bool) func() bool {
		r := httptest.NewRequest("GET", "/login", nil)

		scope := testRequestScope()
		scope.accountOptions.IsOpen = open
		r = withRequestScope(r, scope)

		if account != nil {
			r = r.WithContext(context.WithValue(r.Context(), requestContextKeyAccount, account))
		}

		return ReadAccess(r)
	}

	reader := &Account{Name: "reader", Groups: map[ServerID]Group{serverID: GroupRead}}
	noAccess := &Account{Name: "nobody", Groups: map[ServerID]Group{serverID: GroupNoAccess}}

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
