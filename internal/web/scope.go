// Package web is the web infrastructure shared by all HTTP handlers: the per-request scope (sessions, server identity,
// account), access checks, flash messages and the template renderer. It does not depend on the managers and handlers
// of ACPitwall itself.
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

	// Visibility says who may see which area.
	Visibility smmodel.Visibility

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

// ReadAccess tells whether the requester gets into the pages that belong to no area (navigation, home page): accounts
// with read access, anonymous visitors as long as at least one area is public. What they see inside is decided
// per area by AreaAccess.
func ReadAccess(r *http.Request) func() bool {
	account := AccountFromRequest(r)
	scope := ScopeFromRequest(r)

	ok := account.HasGroupPrivilege(scope.ServerID, smmodel.GroupRead) || (smmodel.IsAnonymous(account) && scope.Visibility.AnyPublic())

	return func() bool {
		return ok
	}
}

// AreaAccess tells whether the requester may see the area, by the level the admin set for it. Anonymous visitors
// are those without an account or with the dummy open account.
func AreaAccess(r *http.Request, area smmodel.Area) bool {
	scope := ScopeFromRequest(r)

	return scope.Visibility.Allowed(area, AccountFromRequest(r), scope.ServerID)
}

// StatsAccess tells whether the requester may see driver statistics (profiles, leaderboards, lap records).
func StatsAccess(r *http.Request) func() bool {
	ok := AreaAccess(r, smmodel.AreaStats)

	return func() bool {
		return ok
	}
}

// CanSee is the template function canSee: it tells whether the requester may see the area with the given name.
func CanSee(r *http.Request) func(area string) bool {
	return func(area string) bool {
		a, ok := smmodel.ParseArea(area)

		return ok && AreaAccess(r, a)
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

// CanSeeHidden tells whether the requester may see hidden championships and race weekends: only logged in accounts
// with read access, never the anonymous visitors of an open server.
func CanSeeHidden(r *http.Request) bool {
	account := AccountFromRequest(r)
	scope := ScopeFromRequest(r)

	return LoggedIn(r)() && account.HasGroupPrivilege(scope.ServerID, smmodel.GroupRead)
}
