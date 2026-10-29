package servermanager

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestReadAccess(t *testing.T) {
	oldServerID, oldIsOpen := serverID, accountOptions.IsOpen
	serverID = "test-server"
	defer func() {
		serverID, accountOptions.IsOpen = oldServerID, oldIsOpen
	}()

	withAccount := func(account *Account) func() bool {
		r := httptest.NewRequest("GET", "/login", nil)

		if account != nil {
			r = r.WithContext(context.WithValue(r.Context(), requestContextKeyAccount, account))
		}

		return ReadAccess(r)
	}

	reader := &Account{Name: "reader", Groups: map[ServerID]Group{serverID: GroupRead}}
	noAccess := &Account{Name: "nobody", Groups: map[ServerID]Group{serverID: GroupNoAccess}}

	for _, open := range []bool{false, true} {
		accountOptions.IsOpen = open

		if got := withAccount(nil)(); got != open {
			t.Errorf("anonymous visitor (open=%t): expected read access %t, got %t", open, open, got)
		}

		if !withAccount(reader)() {
			t.Errorf("reader (open=%t): expected read access", open)
		}

		if withAccount(noAccess)() {
			t.Errorf("no_access account (open=%t): expected no read access", open)
		}
	}
}
