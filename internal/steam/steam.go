// Package steam provides the Steam OpenID login redirect handler.
package steam

import (
	"errors"
	"fmt"
	"html"
	"io/ioutil"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	openIDNamespace  = "http://specs.openid.net/auth/2.0"
	openIDIdentifier = "http://specs.openid.net/auth/2.0/identifier_select"
)

// loginURL is the OpenID endpoint of Steam (a variable for tests).
var loginURL = "https://steamcommunity.com/openid/login"

var claimedIDRegexp = regexp.MustCompile(`^https?://steamcommunity\.com/openid/id/([0-9]{15,25})$`)

var httpClient = &http.Client{Timeout: 15 * time.Second}

// LoginHandler is embedded by handlers that offer a Steam login.
type LoginHandler struct{}

// ErrCancelled is given to the failure callback of HandleLogin if the visitor cancelled at Steam.
var ErrCancelled = errors.New("steam: authorization cancelled")

// HandleLogin redirects to Steam and, once Steam's answer was validated, calls onSuccess with the verified
// SteamID64 in the same request. The SteamID is never taken from a query parameter that the client could set itself,
// and never passed on via a redirect: the caller has to act on it (e.g. start a session) right away. A cancelled or
// invalid answer calls onFailure (ErrCancelled, or the validation error).
func (slh *LoginHandler) HandleLogin(onSuccess func(w http.ResponseWriter, r *http.Request, steamID string), onFailure func(w http.ResponseWriter, r *http.Request, err error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("openid.mode") {
		case "":
			http.Redirect(w, r, authURL(r), http.StatusFound)
		case "cancel":
			onFailure(w, r, ErrCancelled)
		default:
			steamID, err := validate(r)

			if err != nil {
				onFailure(w, r, err)
				return
			}

			onSuccess(w, r, steamID)
		}
	}
}

// ClientSideRedirect sends the visitor on with an HTML redirect instead of a Location header. After the login at
// Steam a header redirect would still count as cross-site for cookies with SameSite=Strict.
func ClientSideRedirect(url string, w http.ResponseWriter) {
	clientSideRedirect(url, w)
}

// root returns scheme and host of the request as the browser sees them, also behind a reverse proxy.
func root(r *http.Request) string {
	scheme := "http"

	if r.TLS != nil {
		scheme = "https"
	}

	if proto := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); proto == "http" || proto == "https" {
		scheme = proto
	}

	host := r.Host

	if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0]); forwarded != "" {
		host = forwarded
	}

	return scheme + "://" + host
}

// returnTo is the URL of the current handler without any query.
func returnTo(r *http.Request) string {
	return root(r) + r.URL.Path
}

func authURL(r *http.Request) string {
	values := url.Values{}
	values.Set("openid.ns", openIDNamespace)
	values.Set("openid.mode", "checkid_setup")
	values.Set("openid.claimed_id", openIDIdentifier)
	values.Set("openid.identity", openIDIdentifier)
	values.Set("openid.realm", root(r))
	values.Set("openid.return_to", returnTo(r))

	return loginURL + "?" + values.Encode()
}

// validate checks the answer of Steam (return_to, signature via check_authentication) and returns the SteamID64.
func validate(r *http.Request) (string, error) {
	query := r.URL.Query()

	if mode := query.Get("openid.mode"); mode != "id_res" {
		return "", fmt.Errorf("unexpected openid.mode %q", mode)
	}

	if got, want := query.Get("openid.return_to"), returnTo(r); got != want {
		return "", fmt.Errorf("openid.return_to %q does not match the URL of this request %q (check the Host / X-Forwarded-Proto / X-Forwarded-Host headers of your proxy)", got, want)
	}

	params := url.Values{}

	for key, values := range query {
		if strings.HasPrefix(key, "openid.") && len(values) > 0 {
			params.Set(key, values[0])
		}
	}

	params.Set("openid.mode", "check_authentication")

	resp, err := httpClient.PostForm(loginURL, params)

	if err != nil {
		return "", fmt.Errorf("could not reach Steam: %w", err)
	}

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		return "", fmt.Errorf("could not read the answer of Steam: %w", err)
	}

	valid := false

	for _, line := range strings.Split(string(body), "\n") {
		if strings.TrimSpace(line) == "is_valid:true" {
			valid = true
		}
	}

	if !valid {
		return "", errors.New("Steam did not confirm the login (is_valid is not true)")
	}

	match := claimedIDRegexp.FindStringSubmatch(query.Get("openid.claimed_id"))

	if match == nil {
		return "", fmt.Errorf("unexpected claimed_id %q", query.Get("openid.claimed_id"))
	}

	return match[1], nil
}

const clientSideRedirectHTML = `
<!doctype html>
<html>
<head>
<title>Redirect</title>
<meta http-equiv="refresh" content="0;URL='%s'" />
</head>
<body>
<p>If you are not redirected automatically, please <a href="%s">click here</a>.</p>
</body>
</html>
`

func clientSideRedirect(url string, w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	escaped := html.EscapeString(url)
	_, _ = w.Write([]byte(fmt.Sprintf(clientSideRedirectHTML, escaped, escaped)))
}
