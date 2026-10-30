package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/cj123/sessions"

	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
)

// ServerParam is the query or form parameter which selects the server slot a request is for.
const ServerParam = "server"

const (
	serverSessionName = "server"
	serverSessionKey  = "id"
)

type requestedServerContextKey struct{}

// RequestedServerID returns the id of the server slot the request is for: the server parameter, or else the slot which
// is remembered in the session. It is empty if the request does not select a slot, which means the default slot. The
// web package does not know the slots, the lookup is up to the caller (see ServerSelectionMiddleware).
func RequestedServerID(r *http.Request) smmodel.ServerID {
	id, _ := r.Context().Value(requestedServerContextKey{}).(smmodel.ServerID)

	return id
}

// WithRequestedServerID returns a copy of r which is for the given server slot.
func WithRequestedServerID(r *http.Request, id smmodel.ServerID) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), requestedServerContextKey{}, id))
}

// serverParam reads the server parameter without parsing the body of requests other than url encoded forms.
func serverParam(r *http.Request) string {
	if id := r.URL.Query().Get(ServerParam); id != "" {
		return id
	}

	if r.Method == http.MethodPost && strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		return r.PostFormValue(ServerParam)
	}

	return ""
}

// ServerSelectionMiddleware attaches the requested server slot to each request. Slot ids come from the server
// parameter; a valid one is remembered in the session, so that later requests without it (links, redirects, ajax calls)
// stay with the same slot. Without a parameter the remembered slot is used if it still exists, otherwise the request is
// for the default slot. An unknown id in the parameter is answered with 404. valid tells which ids are slots. It has to
// run after the request scope has been attached.
func ServerSelectionMiddleware(valid func(id smmodel.ServerID) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session := serverSession(r)

			var remembered smmodel.ServerID

			if session != nil {
				id, _ := session.Values[serverSessionKey].(string)
				remembered = smmodel.ServerID(id)
			}

			if param := serverParam(r); param != "" {
				id := smmodel.ServerID(param)

				if !valid(id) {
					http.Error(w, fmt.Sprintf("unknown server %q", param), http.StatusNotFound)
					return
				}

				if session != nil && id != remembered {
					session.Values[serverSessionKey] = param
					_ = session.Save(r, w)
				}

				next.ServeHTTP(w, WithRequestedServerID(r, id))
				return
			}

			if remembered != "" && valid(remembered) {
				r = WithRequestedServerID(r, remembered)
			}

			next.ServeHTTP(w, r)
		})
	}
}

func serverSession(r *http.Request) *sessions.Session {
	store := ScopeFromRequest(r).SessionStore

	if store == nil {
		return nil
	}

	session, err := store.Get(r, serverSessionName)

	if err != nil && session == nil {
		return nil
	}

	return session
}

// ServerURL returns the path with the server parameter of the request added, if the request selects a slot. Links
// which are generated for pages of the same server use it.
func ServerURL(r *http.Request, path string) string {
	id := RequestedServerID(r)

	if id == "" {
		return path
	}

	u, err := url.Parse(path)

	if err != nil {
		return path
	}

	q := u.Query()

	if q.Get(ServerParam) == "" {
		q.Set(ServerParam, string(id))
	}

	u.RawQuery = q.Encode()

	return u.String()
}

// ServerRef is how templates refer to a server slot.
type ServerRef struct {
	ID   smmodel.ServerID
	Name string
}
