package accounts

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/steam"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

var (
	// ErrSteamIDInUse is returned if another account has the SteamID already.
	ErrSteamIDInUse = errors.New("servermanager: another account is connected to this Steam ID")

	// ErrSteamAlreadyLinked is returned if the account is connected to a Steam ID already.
	ErrSteamAlreadyLinked = errors.New("servermanager: the account is connected to Steam already")

	// ErrInvalidSteamID is returned for a SteamID64 that does not consist of 17 digits.
	ErrInvalidSteamID = errors.New("servermanager: a SteamID64 has 17 digits")
)

const (
	steamLinkCookie = "steam_link"
	steamLinkTTL    = 5 * time.Minute
	steamLinkPath   = "/accounts/steam-link"
	steamLinkFinish = "/accounts/steam-link/finish"
)

// steamIDRegexp is a SteamID64 as entered by an admin.
var steamIDRegexp = regexp.MustCompile("^[0-9]{17}$")

// pendingSteamLink is a started connection of an account to Steam. Steam sends the browser back from another site,
// so the session cookie (SameSite=Strict) does not come with that request: the account is remembered when the
// connection is started (a request with the session), the verified SteamID when Steam answered, and the connection
// is made by a later same site request that has the session again. The SteamID never leaves the server.
type pendingSteamLink struct {
	accountID string
	steamID   string
	failed    bool
	expires   time.Time
}

type steamLinks struct {
	mu      sync.Mutex
	pending map[string]*pendingSteamLink
}

func (am *AccountManager) clock() time.Time {
	if am.now != nil {
		return am.now()
	}

	return time.Now()
}

// startSteamLink remembers that the account wants to connect to Steam and returns the token for the cookie.
func (am *AccountManager) startSteamLink(accountID string) (string, error) {
	raw := make([]byte, 32)

	if _, err := rand.Read(raw); err != nil {
		return "", err
	}

	token := hex.EncodeToString(raw)

	am.links.mu.Lock()
	defer am.links.mu.Unlock()

	if am.links.pending == nil {
		am.links.pending = make(map[string]*pendingSteamLink)
	}

	now := am.clock()

	for t, p := range am.links.pending {
		if now.After(p.expires) {
			delete(am.links.pending, t)
		}
	}

	am.links.pending[token] = &pendingSteamLink{accountID: accountID, expires: now.Add(steamLinkTTL)}

	return token, nil
}

// answerSteamLink records Steam's answer (the verified steamID, or a failure) for a started connection. It returns
// false if there is no valid started connection with this token, or if it was answered already.
func (am *AccountManager) answerSteamLink(token, steamID string, failed bool) bool {
	am.links.mu.Lock()
	defer am.links.mu.Unlock()

	p, ok := am.links.pending[token]

	if !ok || am.clock().After(p.expires) || p.steamID != "" || p.failed {
		return false
	}

	p.steamID, p.failed = steamID, failed

	return true
}

// takeSteamLink removes and returns the connection of the token (nil if there is none or it expired).
func (am *AccountManager) takeSteamLink(token string) *pendingSteamLink {
	am.links.mu.Lock()
	defer am.links.mu.Unlock()

	p, ok := am.links.pending[token]

	if !ok {
		return nil
	}

	delete(am.links.pending, token)

	if am.clock().After(p.expires) {
		return nil
	}

	return p
}

// linkSteam connects the account to the (verified) steamID, if no other account has it.
func (am *AccountManager) linkSteam(account *smmodel.Account, steamID string) error {
	if !steamGUIDRegexp.MatchString(steamID) {
		return ErrInvalidSteamID
	}

	am.steamMutex.Lock()
	defer am.steamMutex.Unlock()

	if account.GUID != "" {
		return ErrSteamAlreadyLinked
	}

	switch other, err := am.store.FindAccountByGUID(steamID); {
	case err == nil && other.ID != account.ID:
		return ErrSteamIDInUse
	case err != nil && err != smstore.ErrAccountNotFound:
		return err
	}

	account.GUID = steamID

	return am.store.UpsertAccount(account)
}

// createSteamAccount creates the account of a driver for the SteamID64 in advance: it has no password, the driver
// logs in with Steam. displayName is optional and becomes the driver name.
func (am *AccountManager) createSteamAccount(steamID string, group smmodel.Group, displayName string) (*smmodel.Account, error) {
	steamID = strings.TrimSpace(steamID)

	if !steamIDRegexp.MatchString(steamID) {
		return nil, ErrInvalidSteamID
	}

	am.steamMutex.Lock()
	defer am.steamMutex.Unlock()

	if _, err := am.store.FindAccountByGUID(steamID); err == nil {
		return nil, ErrSteamIDInUse
	} else if err != smstore.ErrAccountNotFound {
		return nil, err
	}

	if steamAccountNameTaken(am.store, steamID) {
		return nil, ErrSteamIDInUse
	}

	account := smmodel.NewAccount(am.serverID, am.buildVersion)
	account.Name = steamAccountName(steamID)
	account.GUID = steamID
	account.DriverName = strings.TrimSpace(displayName)
	account.Groups[am.serverID] = group

	return account, am.store.UpsertAccount(account)
}

// sessionAccount is the account of the session of the request, nil if there is none.
func (ah *AccountHandler) sessionAccount(r *http.Request) *smmodel.Account {
	id, ok := smweb.Session(r).Values[sessionAccountID].(string)

	if !ok {
		return nil
	}

	account, err := ah.store.FindAccountByID(id)

	if err != nil || !account.Deleted.IsZero() {
		return nil
	}

	return account
}

func steamLinkCookieFor(r *http.Request, value string, maxAge int) *http.Cookie {
	secure := r.TLS != nil || strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]), "https")

	// Lax, because the cookie has to come with the request back from Steam (a top level navigation of another site)
	return &http.Cookie{Name: steamLinkCookie, Value: value, Path: steamLinkPath, MaxAge: maxAge, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode}
}

// SteamLink is the public route /accounts/steam-link. A logged in account starts the connection to Steam here;
// Steam's answer comes back to the same URL, which cannot be behind the login check (the session cookie is not sent
// then, and nothing may be written to the session). It validates the answer, remembers the verified SteamID on the
// server and sends the browser on to SteamLinkFinish, where the session is there again.
func (ah *AccountHandler) SteamLink() http.HandlerFunc {
	login := ah.HandleLogin(func(w http.ResponseWriter, r *http.Request, steamID string) {
		ah.answerLink(w, r, steamID, false)
	}, func(w http.ResponseWriter, r *http.Request, err error) {
		if err != steam.ErrCancelled {
			logrus.WithError(err).Error("Could not validate the Steam connection")
		}

		ah.answerLink(w, r, "", true)
	})

	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("openid.mode") != "" {
			login(w, r)
			return
		}

		// start
		account := ah.sessionAccount(r)

		if account == nil {
			smweb.AddErrorFlash(w, r, smweb.T(r, "flash.accounts.login_first"))
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		if account.GUID != "" {
			smweb.AddErrorFlash(w, r, smweb.T(r, "flash.accounts.steam_already_linked"))
			http.Redirect(w, r, "/accounts/update", http.StatusFound)
			return
		}

		token, err := ah.accountManager.startSteamLink(account.ID.String())

		if err != nil {
			logrus.WithError(err).Error("Could not start the Steam connection")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		http.SetCookie(w, steamLinkCookieFor(r, token, int(steamLinkTTL.Seconds())))
		login(w, r)
	}
}

// answerLink stores Steam's answer for the started connection and continues same site. Without a started
// connection nothing is stored.
func (ah *AccountHandler) answerLink(w http.ResponseWriter, r *http.Request, steamID string, failed bool) {
	cookie, err := r.Cookie(steamLinkCookie)

	if err != nil || !ah.accountManager.answerSteamLink(cookie.Value, steamID, failed) {
		steam.ClientSideRedirect("/accounts/update", w)
		return
	}

	steam.ClientSideRedirect(steamLinkFinish, w)
}

// SteamLinkFinish (logged in) connects the account of the session to the SteamID that Steam confirmed.
func (ah *AccountHandler) SteamLinkFinish(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, steamLinkCookieFor(r, "", -1))

	account := smweb.AccountFromRequest(r)

	cookie, err := r.Cookie(steamLinkCookie)

	if err != nil || account.Name == "" || account == ah.accountManager.openAccount {
		http.Redirect(w, r, "/accounts/update", http.StatusFound)
		return
	}

	link := ah.accountManager.takeSteamLink(cookie.Value)

	switch {
	case link == nil || link.accountID != account.ID.String() || link.failed || link.steamID == "":
		// expired, started by another account, cancelled or invalid
		smweb.AddErrorFlash(w, r, smweb.T(r, "flash.accounts.steam_failed"))
	default:
		switch err := ah.accountManager.linkSteam(account, link.steamID); err {
		case nil:
			ah.addAutofillEntrant(account)
			smweb.AddFlash(w, r, smweb.T(r, "flash.accounts.steam_linked"))
		case ErrSteamIDInUse:
			smweb.AddErrorFlash(w, r, smweb.T(r, "flash.accounts.steam_in_use"))
		case ErrSteamAlreadyLinked:
			smweb.AddErrorFlash(w, r, smweb.T(r, "flash.accounts.steam_already_linked"))
		default:
			logrus.WithError(err).Errorf("Could not connect account %s to Steam", account.ID)
			smweb.AddErrorFlash(w, r, smweb.T(r, "flash.accounts.steam_failed"))
		}
	}

	http.Redirect(w, r, "/accounts/update", http.StatusFound)
}

// addAutofillEntrant adds the driver to the autofill entry list, if the account has a GUID and a name.
func (ah *AccountHandler) addAutofillEntrant(account *smmodel.Account) {
	if account.GUID == "" || account.DriverName == "" {
		return
	}

	err := ah.store.UpsertEntrant(serverconfig.Entrant{
		Name: account.DriverName,
		GUID: account.GUID,
		Team: account.Team,
	})

	if err != nil {
		logrus.WithError(err).Errorf("Could not add to autofill entry list. Account id: %s", account.ID.String())
	}
}
