package web

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/wernersbacher/ACPitwall/internal/skins"
)

// SkinActorFromRequest returns who the request acts as for skins: nobody for visitors without a login, the account
// otherwise, with Writer set for accounts with write access.
func SkinActorFromRequest(r *http.Request) skins.Actor {
	account := AccountFromRequest(r)

	if !LoggedIn(r)() || account.ID == uuid.Nil {
		return skins.Actor{}
	}

	return skins.Actor{ID: account.ID, Writer: WriteAccess(r)()}
}
