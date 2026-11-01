package web

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/cj123/sessions"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/portcheck"
)

// ServerParam is the query or form parameter which selects the server slot a request is for.
const ServerParam = "server"

const (
	serverSessionName = "server"
	serverSessionKey  = "id"
)

// ScheduleServerParam is the form field of the scheduling forms which selects the server an event is scheduled for.
// It is separate from ServerParam, so that scheduling an event for another server does not switch the server the user
// is looking at.
const ScheduleServerParam = "event-schedule-server"

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

// ScheduleTarget returns the request as it is for the server which a scheduling form selected, so that the manager of
// that server can be looked up. Requests without the field stay with the server of the request. The caller has to parse
// the form first; the lookup of the manager still rejects unknown ids.
func ScheduleTarget(r *http.Request) *http.Request {
	if id := formParam(r, ScheduleServerParam); id != "" {
		return WithRequestedServerID(r, smmodel.ServerID(id))
	}

	return r
}

// maxInspectedFormBody is the largest url encoded body which is looked at for a parameter. Larger bodies (uploads post
// their JSON with the url encoded content type) are left alone.
const maxInspectedFormBody = 1 << 20

// formParam reads a parameter from the query and, for small url encoded POST bodies, from the body. It never consumes
// the body: net/http's ParseForm would swallow up to 10 MB of it, which breaks handlers decoding the body themselves.
// A form which has already been parsed is used as it is.
func formParam(r *http.Request, name string) string {
	if id := r.URL.Query().Get(name); id != "" {
		return id
	}

	if r.PostForm != nil {
		return r.PostForm.Get(name)
	}

	if r.Method != http.MethodPost || r.Body == nil || r.Body == http.NoBody ||
		!strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") ||
		r.ContentLength <= 0 || r.ContentLength > maxInspectedFormBody {
		return ""
	}

	original := r.Body
	body, err := ioutil.ReadAll(io.LimitReader(original, maxInspectedFormBody+1))

	// restore what was read, followed by whatever is left, so that the handler sees the complete body.
	r.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(body), original), original}

	if err != nil || int64(len(body)) > maxInspectedFormBody {
		return ""
	}

	values, err := url.ParseQuery(string(body))

	if err != nil {
		return ""
	}

	return values.Get(name)
}

// serverParam reads the server parameter without consuming the body.
func serverParam(r *http.Request) string {
	return formParam(r, ServerParam)
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

	// Running tells if the server runs an event. It is only set for the lists the renderer gives to templates.
	Running bool
}

// ServerDir is a server slot together with its results directory.
type ServerDir struct {
	ServerRef

	ResultsDir string
}

// ServersFunc returns all server slots with their results directories. The resolver provides it, as the handlers
// cannot know the slots. Results are attributed to a server by the directory they are found in, which keeps the
// results format unchanged.
type ServersFunc func() []ServerDir

// Attributed returns the servers for attributing results: none if there are fewer than two (a single server setup
// looks and answers as before) or no function.
func (f ServersFunc) Attributed() []ServerDir {
	if f == nil {
		return nil
	}

	servers := f()

	if len(servers) < 2 {
		return nil
	}

	return servers
}

// ServerOfDir returns the server whose results directory is dir, nil if there is none.
func ServerOfDir(servers []ServerDir, dir string) *ServerRef {
	dir = filepath.Clean(dir)

	for _, server := range servers {
		if filepath.Clean(server.ResultsDir) == dir {
			ref := server.ServerRef

			return &ref
		}
	}

	return nil
}

// DirOfServer returns the results directory of the server with the id, false if there is no such server.
func DirOfServer(servers []ServerDir, id string) (string, bool) {
	for _, server := range servers {
		if string(server.ID) == id {
			return server.ResultsDir, true
		}
	}

	return "", false
}

// PortWarnings returns what is shown to the user about the ports the server slot with the id shares with other server
// slots (see the portcheck package), one message per conflict. The handler packages can't know the slots, so the
// resolver provides it.
type PortWarnings func(id smmodel.ServerID) []portcheck.Warning

// For returns the warnings of the slot, none if there is no function.
func (p PortWarnings) For(id smmodel.ServerID) []portcheck.Warning {
	if p == nil {
		return nil
	}

	return p(id)
}
