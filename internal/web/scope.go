// Package web is the web infrastructure shared by all HTTP handlers: the per-request scope (sessions, server identity,
// account), access checks, flash messages and the template renderer. It does not depend on the managers and handlers
// of the server manager itself.
package web

import (
	"context"
	"net/http"

	"github.com/cj123/sessions"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

// Scope carries the server wide state which request helpers (sessions, access checks) need. It is attached to every
// request by the account middleware, so that the helpers do not depend on package variables.
type Scope struct {
	// SessionStore keeps the login sessions and flash messages.
	SessionStore sessions.Store

	// ServerID identifies this server instance in the per-server account groups.
	ServerID smmodel.ServerID

	// ReadIsOpen is true if anonymous visitors may read.
	ReadIsOpen bool

	// StatsPublic is true if anonymous visitors of an open server may see driver statistics (profiles,
	// leaderboards, lap records).
	StatsPublic bool

	// OpenAccount is the dummy account which is used for visitors if read access is open.
	OpenAccount *smmodel.Account
}

type scopeContextKey struct{}

// ScopeFromRequest returns the scope of the request. It is empty if the request carries none.
func ScopeFromRequest(r *http.Request) *Scope {
	scope, _ := r.Context().Value(scopeContextKey{}).(*Scope)

	if scope == nil {
		return &Scope{}
	}

	return scope
}

// WithScope returns a copy of r which carries the given scope.
func WithScope(r *http.Request, scope *Scope) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), scopeContextKey{}, scope))
}

type accountContextKey struct{}

// WithAccount returns a copy of r which carries the account that is logged in.
func WithAccount(r *http.Request, account *smmodel.Account) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), accountContextKey{}, account))
}

// AccountFromRequest returns the account of the request, or an empty account for anonymous requests.
func AccountFromRequest(r *http.Request) *smmodel.Account {
	u, ok := r.Context().Value(accountContextKey{}).(*smmodel.Account)

	if !ok {
		return &smmodel.Account{}
	}

	return u
}

func ReadAccess(r *http.Request) func() bool {
	account := AccountFromRequest(r)
	scope := ScopeFromRequest(r)

	// anonymous visitors can read an open server, even on pages outside the read middleware (e.g. /login)
	ok := account.HasGroupPrivilege(scope.ServerID, smmodel.GroupRead) || (account.Name == "" && scope.ReadIsOpen)

	return func() bool {
		return ok
	}
}

// StatsAccess tells whether the requester may see driver statistics: every logged in account with read access,
// anonymous visitors (of an open server) only if the statistics are public.
func StatsAccess(r *http.Request) func() bool {
	account := AccountFromRequest(r)
	scope := ScopeFromRequest(r)

	var ok bool

	if account.Name == "" || account == scope.OpenAccount {
		ok = scope.ReadIsOpen && scope.StatsPublic
	} else {
		ok = account.HasGroupPrivilege(scope.ServerID, smmodel.GroupRead)
	}

	return func() bool {
		return ok
	}
}

func LoggedIn(r *http.Request) func() bool {
	account := AccountFromRequest(r)

	ok := account.Name != "" && account != ScopeFromRequest(r).OpenAccount

	return func() bool {
		return ok
	}
}

func WriteAccess(r *http.Request) func() bool {
	return groupAccess(r, smmodel.GroupWrite)
}

func DeleteAccess(r *http.Request) func() bool {
	return groupAccess(r, smmodel.GroupDelete)
}

func AdminAccess(r *http.Request) func() bool {
	return groupAccess(r, smmodel.GroupAdmin)
}

func groupAccess(r *http.Request, group smmodel.Group) func() bool {
	ok := AccountFromRequest(r).HasGroupPrivilege(ScopeFromRequest(r).ServerID, group)

	return func() bool {
		return ok
	}
}
