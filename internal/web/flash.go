package web

import (
	"net/http"

	"github.com/cj123/sessions"
)

// Session returns the message session of the request, which holds the login and the flash messages.
func Session(r *http.Request) *sessions.Session {
	session, _ := ScopeFromRequest(r).SessionStore.Get(r, "messages")

	return session
}

func errorSession(r *http.Request) *sessions.Session {
	session, _ := ScopeFromRequest(r).SessionStore.Get(r, "errors")

	return session
}

// AddFlash adds a message to the message session
func AddFlash(w http.ResponseWriter, r *http.Request, message string) {
	session := Session(r)

	session.AddFlash(message)

	// gorilla sessions is dumb and errors weirdly
	_ = session.Save(r, w)
}

func AddErrorFlash(w http.ResponseWriter, r *http.Request, message string) {
	session := errorSession(r)

	session.AddFlash(message)

	// gorilla sessions is dumb and errors weirdly
	_ = session.Save(r, w)
}
