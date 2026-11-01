package accounts

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/sirupsen/logrus"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

// SetLanguage sets the language of the UI from the form value "lang": the code of a language with a catalog, or empty
// to follow the browser language again. It is remembered in a cookie and, if an account is logged in, in the account,
// so that it follows the account to other browsers. It then redirects back to the page the form was sent from.
func (ah *AccountHandler) SetLanguage(w http.ResponseWriter, r *http.Request) {
	code := r.FormValue("lang")

	if code != "" && !ah.ViewRenderer.Bundle().Supports(code) {
		http.Error(w, "unknown language", http.StatusBadRequest)
		return
	}

	if code == "" {
		smweb.ForgetLanguage(w)
	} else {
		smweb.RememberLanguage(w, code)
	}

	if accountID, ok := smweb.Session(r).Values[sessionAccountID].(string); ok {
		account, err := ah.store.FindAccountByID(accountID)

		if err == nil && account.Name != smmodel.OpenAccountName && account.Language != code {
			account.Language = code

			if err := ah.store.UpsertAccount(account); err != nil {
				logrus.WithError(err).Errorf("Could not save the language of account id: %s", accountID)
			}
		}
	}

	http.Redirect(w, r, localReferer(r), http.StatusFound)
}

// localReferer returns the path (and query) of the page a request came from, "/" if there is none. Only the path is
// used, so that the redirect can't lead to another site.
func localReferer(r *http.Request) string {
	u, err := url.Parse(r.Referer())

	// "//host" and "/\host" are taken as other hosts by browsers
	if err != nil || u.Path == "" || u.Path[0] != '/' || strings.HasPrefix(u.Path, "//") || strings.HasPrefix(u.Path, "/\\") {
		return "/"
	}

	back := url.URL{Path: u.Path, RawQuery: u.RawQuery}

	return back.String()
}
