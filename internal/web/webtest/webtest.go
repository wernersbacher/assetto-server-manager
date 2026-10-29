// Package webtest has helpers for testing HTTP handlers.
package webtest

import (
	"github.com/cj123/sessions"

	smweb "github.com/wernersbacher/assetto-server-manager/internal/web"
)

// RequestScope returns a request scope with a cookie session store, as the handlers need it to flash messages.
func RequestScope() *smweb.Scope {
	return &smweb.Scope{
		SessionStore: sessions.NewCookieStore([]byte("test")),
	}
}
