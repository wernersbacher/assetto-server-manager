package servermanager

import (
	"github.com/cj123/sessions"

	smweb "github.com/wernersbacher/assetto-server-manager/internal/web"
)

// testRequestScope returns a request scope with a cookie session store and a closed server.
func testRequestScope() *smweb.Scope {
	return &smweb.Scope{
		SessionStore: sessions.NewCookieStore([]byte("test")),
	}
}
