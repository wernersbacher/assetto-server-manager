package accounts

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/cj123/sessions"
	"github.com/go-chi/chi"
	"github.com/google/uuid"
	"github.com/sethvargo/go-diceware/diceware"
	"github.com/sirupsen/logrus"
	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	"github.com/wernersbacher/ACPitwall/internal/downloads"
	smconfig "github.com/wernersbacher/ACPitwall/internal/config"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/steam"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
	"golang.org/x/crypto/scrypt"
)

var steamGUIDRegexp = regexp.MustCompile("^[0-9]{15,25}$")

const (
	sessionAccountID           = "account_id"
	serverAccountOptionsMetaKey = "server-account-options"
)

type ServerAccountOptions struct {
	IsOpen bool

	// PublicStats makes driver profiles, leaderboards and lap records visible to anonymous visitors of an open
	// server. If false, they need a login even on an open server.
	PublicStats bool

	// DownloadAccess restricts the downloads of the content directory (cars, tracks, ...), setups and car skin
	// zips. Pictures shown on the pages stay readable. The zero value (Public) keeps the previous behaviour.
	DownloadAccess downloads.Access

	// DisableTeamCreation stops accounts without write access from creating teams. The zero value allows it, as
	// before the option existed.
	DisableTeamCreation bool

	// MaxOwnedTeams is how many teams one account may own. 0 means the default of the teams package, a negative
	// number is unlimited.
	MaxOwnedTeams int

	// Skins are the settings for skin uploads of users.
	Skins smmodel.SkinSettings

	// SteamSignup lets visitors without an account create one by logging in with Steam. Off by default; Steam
	// logins of existing accounts (matched by their GUID) always work.
	SteamSignup bool
}

// MustLoginMiddleware determines whether an account needs to log in to access a given Group page
func (ah *AccountHandler) MustLoginMiddleware(requiredGroup smmodel.Group, next http.Handler) http.Handler {
	return ah.mustLogin(requiredGroup, func() bool { return ah.accountManager.options.IsOpen }, next)
}

// StatsAccessMiddleware guards the driver statistics (profiles, leaderboards, lap records). They need read access
// like other pages, but anonymous visitors of an open server only get in if the "public driver statistics" option is on.
func (ah *AccountHandler) StatsAccessMiddleware(next http.Handler) http.Handler {
	return ah.mustLogin(smmodel.GroupRead, func() bool {
		return ah.accountManager.options.IsOpen && ah.accountManager.options.PublicStats
	}, next)
}

// DownloadAccess returns the current download access rule.
func (ah *AccountHandler) DownloadAccess() downloads.Access {
	return ah.accountManager.options.DownloadAccess
}

// DownloadAccessMiddleware applies the download access option on top of read access (use it inside a read group):
// Public lets everybody through, LoggedIn turns away the anonymous visitors of an open server and Off lets only
// admins download.
func (ah *AccountHandler) DownloadAccessMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		account := smweb.AccountFromRequest(r)
		anonymous := account == nil || account.Name == smmodel.OpenAccountName

		switch ah.DownloadAccess() {
		case downloads.Public:
			next.ServeHTTP(w, r)
		case downloads.LoggedIn:
			if anonymous {
				smweb.AddErrorFlash(w, r, smweb.T(r, "flash.accounts.login_to_download"))
				http.Redirect(w, r, "/login", http.StatusFound)
				return
			}

			next.ServeHTTP(w, r)
		default:
			if anonymous || !account.HasGroupPrivilege(ah.accountManager.serverID, smmodel.GroupAdmin) {
				http.Error(w, "downloads are disabled", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		}
	})
}

// mustLogin is MustLoginMiddleware with the condition under which anonymous visitors may read.
func (ah *AccountHandler) mustLogin(requiredGroup smmodel.Group, anonymousAllowed func() bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverID := ah.accountManager.serverID

		sess := smweb.Session(r)

		accountID, ok := sess.Values[sessionAccountID].(string)

		if ok {
			account, err := ah.store.FindAccountByID(accountID)

			if err != nil {
				logrus.WithError(err).Errorf("Could not find account for id: %s", accountID)
				delete(sess.Values, sessionAccountID)
				_ = sessions.Save(r, w)

				smweb.AddFlash(w, r, smweb.T(r, "flash.accounts.logged_out"))

				http.Redirect(w, r, "/", http.StatusFound)
				return
			}

			if !account.HasGroupPrivilege(serverID, requiredGroup) {
				if account.GroupFor(serverID) == smmodel.GroupNoAccess {
					smweb.AddErrorFlash(w, r, smweb.T(r, "flash.accounts.no_access"))
					http.Redirect(w, r, "/login", http.StatusFound)
					return
				}

				smweb.AddErrorFlash(w, r, smweb.T(r, "flash.accounts.no_permission"))
				http.Redirect(w, r, "/", http.StatusFound)
				return
			}

			next.ServeHTTP(w, smweb.WithAccount(r, account))
			return
		}

		if requiredGroup == smmodel.GroupRead && anonymousAllowed() {
			// if read is open, allow access and use a dummy account so the UI doesn't break
			next.ServeHTTP(w, smweb.WithAccount(r, ah.accountManager.openAccount))
			return
		}

		if !ok {
			smweb.AddErrorFlash(w, r, smweb.T(r, "flash.accounts.login_first"))
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
	})
}

// RequestScopeMiddleware attaches the session store and account state to each request.
func (ah *AccountHandler) RequestScopeMiddleware(next http.Handler) http.Handler {
	return ah.accountManager.RequestScopeMiddleware(next)
}

func (ah *AccountHandler) ReadAccessMiddleware(next http.Handler) http.Handler {
	return ah.MustLoginMiddleware(smmodel.GroupRead, next)
}

func (ah *AccountHandler) WriteAccessMiddleware(next http.Handler) http.Handler {
	return ah.MustLoginMiddleware(smmodel.GroupWrite, next)
}

func (ah *AccountHandler) DeleteAccessMiddleware(next http.Handler) http.Handler {
	return ah.MustLoginMiddleware(smmodel.GroupDelete, next)
}

func (ah *AccountHandler) AdminAccessMiddleware(next http.Handler) http.Handler {
	return ah.MustLoginMiddleware(smmodel.GroupAdmin, next)
}

func (ah *AccountHandler) DismissChangelog(w http.ResponseWriter, r *http.Request) {
	account := smweb.AccountFromRequest(r)

	if account.Name == smmodel.OpenAccountName {
		// don't save the open account
		return
	}

	err := ah.accountManager.SetCurrentVersion(account)

	if err != nil {
		logrus.WithError(err).Error("could not save current account version")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
}

type AccountHandler struct {
	*smweb.BaseHandler
	steam.LoginHandler

	config         *smconfig.Configuration
	store          smstore.Store
	accountManager *AccountManager

	onAccountDeleted func(id uuid.UUID) error
}

// SetAccountDeletedHook registers a function that is called after an account was deleted, e.g. to take it out of its
// teams. An error is logged, the account stays deleted.
func (ah *AccountHandler) SetAccountDeletedHook(hook func(id uuid.UUID) error) {
	ah.onAccountDeleted = hook
}

// TeamOptions returns the team settings: whether accounts without write access may not create teams, and how many
// teams one account can own (0 is the default, negative is unlimited).
func (ah *AccountHandler) TeamOptions() (disableCreation bool, maxOwned int) {
	return ah.accountManager.options.DisableTeamCreation, ah.accountManager.options.MaxOwnedTeams
}

func NewAccountHandler(config *smconfig.Configuration, baseHandler *smweb.BaseHandler, store smstore.Store, accountManager *AccountManager) *AccountHandler {
	return &AccountHandler{
		config:         config,
		BaseHandler:    baseHandler,
		store:          store,
		accountManager: accountManager,
	}
}

// SteamLogin is the public /login/steam route: it redirects to Steam, validates the answer there and starts the
// session in the same request (the SteamID is never read from a query parameter set by the client).
func (ah *AccountHandler) SteamLogin() http.HandlerFunc {
	return ah.HandleLogin(func(w http.ResponseWriter, r *http.Request, steamID string) {
		ah.finishSteamLogin(w, r, steamID)
	}, func(w http.ResponseWriter, r *http.Request, err error) {
		if err != steam.ErrCancelled {
			logrus.WithError(err).Error("Could not validate the Steam login")
		}

		smweb.AddErrorFlash(w, r, smweb.T(r, "flash.accounts.steam_failed"))
		http.Redirect(w, r, "/login", http.StatusFound)
	})
}

// finishSteamLogin starts the session for a validated SteamID and sends the visitor on.
func (ah *AccountHandler) finishSteamLogin(w http.ResponseWriter, r *http.Request, steamID string) {
	account, created, err := ah.accountManager.loginWithSteam(r, w, steamID)

	switch {
	case err == ErrSteamSignupDisabled:
		smweb.AddErrorFlash(w, r, smweb.T(r, "flash.accounts.steam_not_allowed"))
		steam.ClientSideRedirect("/login", w)
		return
	case err != nil:
		logrus.WithError(err).Error("Couldn't log in with Steam")
		smweb.AddErrorFlash(w, r, smweb.T(r, "flash.accounts.steam_failed"))
		steam.ClientSideRedirect("/login", w)
		return
	}

	logrus.Infof("Account %q logged in with Steam (new account: %t)", account.Name, created)
	smweb.AddFlash(w, r, smweb.T(r, "flash.accounts.logged_in"))

	if created {
		// let the new account enter its driver name
		steam.ClientSideRedirect("/accounts/update", w)
	} else {
		steam.ClientSideRedirect("/", w)
	}
}

func (ah *AccountHandler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		err := ah.accountManager.login(r, w)

		switch {
		case err == ErrInvalidUsernameOrPassword:
			smweb.AddErrorFlash(w, r, smweb.T(r, "flash.accounts.invalid_login"))
		case err == ErrAccountNeedsPassword:
			smweb.AddFlash(w, r, smweb.T(r, "flash.accounts.set_password"))
			http.Redirect(w, r, "/accounts/new-password", http.StatusFound)
			return
		case err != nil:
			logrus.WithError(err).Errorf("Couldn't log in account")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		default: // err == nil, successful auth
			smweb.AddFlash(w, r, smweb.T(r, "flash.accounts.logged_in"))
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
	}

	ah.ViewRenderer.MustLoadTemplate(w, r, "accounts/login.html", nil)
}

func (ah *AccountHandler) ToggleServerOpenStatus(w http.ResponseWriter, r *http.Request) {
	ah.toggleOption(w, r, smweb.T(r, "flash.accounts.openness_changed"), func(o *ServerAccountOptions) { o.IsOpen = !o.IsOpen })
}

// TogglePublicStats switches the "public driver statistics" option.
func (ah *AccountHandler) TogglePublicStats(w http.ResponseWriter, r *http.Request) {
	ah.toggleOption(w, r, smweb.T(r, "flash.accounts.public_stats_changed"), func(o *ServerAccountOptions) { o.PublicStats = !o.PublicStats })
}

// ToggleSteamSignup switches the "registration with Steam" option.
func (ah *AccountHandler) ToggleSteamSignup(w http.ResponseWriter, r *http.Request) {
	ah.toggleOption(w, r, smweb.T(r, "flash.accounts.steam_signup_changed"), func(o *ServerAccountOptions) { o.SteamSignup = !o.SteamSignup })
}

// SetDownloadAccess sets the "downloads" option from the form value "access" (public, login or off).
func (ah *AccountHandler) SetDownloadAccess(w http.ResponseWriter, r *http.Request) {
	access, ok := downloads.ParseAccess(r.FormValue("access"))

	if !ok {
		http.Error(w, "unknown download access", http.StatusBadRequest)
		return
	}

	ah.toggleOption(w, r, smweb.T(r, "flash.accounts.download_access_changed"), func(o *ServerAccountOptions) { o.DownloadAccess = access })
}

// SetTeamOptions sets the team options from the form values "creation" (enabled or disabled) and "max" (a whole
// number: 0 is the default, negative is unlimited).
func (ah *AccountHandler) SetTeamOptions(w http.ResponseWriter, r *http.Request) {
	var disable bool

	switch r.FormValue("creation") {
	case "enabled":
		disable = false
	case "disabled":
		disable = true
	default:
		http.Error(w, "unknown team creation setting", http.StatusBadRequest)
		return
	}

	max, err := strconv.Atoi(strings.TrimSpace(r.FormValue("max")))

	if err != nil || max < -1 || max > 1000 {
		http.Error(w, "the maximum number of teams must be a whole number between -1 and 1000", http.StatusBadRequest)
		return
	}

	ah.toggleOption(w, r, smweb.T(r, "flash.accounts.team_options_changed"), func(o *ServerAccountOptions) {
		o.DisableTeamCreation = disable
		o.MaxOwnedTeams = max
	})
}

// SkinSettings returns the current skin upload settings.
func (ah *AccountHandler) SkinSettings() smmodel.SkinSettings {
	return ah.accountManager.options.Skins
}

// SetSkinSettings sets the skin options from the form values "uploads" and "global" (on or off) and the
// whole numbers "max_files", "max_skin_mb", "user_quota_mb" and "min_free_mb" (0 is the default).
func (ah *AccountHandler) SetSkinSettings(w http.ResponseWriter, r *http.Request) {
	var settings smmodel.SkinSettings

	for _, f := range []struct {
		name string
		dest *bool
	}{{"uploads", &settings.UserUploads}, {"global", &settings.GlobalShare}} {
		switch r.FormValue(f.name) {
		case "on":
			*f.dest = true
		case "off":
			*f.dest = false
		default:
			http.Error(w, "unknown value for "+f.name, http.StatusBadRequest)
			return
		}
	}

	for _, f := range []struct {
		name string
		max  int
		dest *int
	}{
		{"max_files", 100000, &settings.MaxFiles},
		{"max_skin_mb", 100000, &settings.MaxSkinMB},
		{"user_quota_mb", 1000000, &settings.UserQuotaMB},
		{"min_free_mb", 1000000, &settings.MinFreeMB},
	} {
		n, err := strconv.Atoi(strings.TrimSpace(r.FormValue(f.name)))

		if err != nil || n < 0 || n > f.max {
			http.Error(w, fmt.Sprintf("%s must be a whole number between 0 and %d", f.name, f.max), http.StatusBadRequest)
			return
		}

		*f.dest = n
	}

	ah.toggleOption(w, r, smweb.T(r, "flash.accounts.skin_options_changed"), func(o *ServerAccountOptions) { o.Skins = settings })
}

// toggleOption changes the server account options and flashes the (translated) message saved.
func (ah *AccountHandler) toggleOption(w http.ResponseWriter, r *http.Request, saved string, toggle func(*ServerAccountOptions)) {
	err := ah.store.GetMeta(serverAccountOptionsMetaKey, &ah.accountManager.options)

	if err != nil && err != smstore.ErrValueNotSet {
		logrus.WithError(err).Errorf("Could not determine server open status")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	toggle(ah.accountManager.options)

	err = ah.store.SetMeta(serverAccountOptionsMetaKey, ah.accountManager.options)

	if err != nil {
		logrus.WithError(err).Errorf("Could not save server open status")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	smweb.AddFlash(w, r, saved)
	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

type newPasswordTemplateVars struct {
	smweb.BaseTemplateVars

	NewAccount bool
}

func (ah *AccountHandler) NewPassword(w http.ResponseWriter, r *http.Request) {
	account := smweb.AccountFromRequest(r)

	if r.Method == http.MethodPost {
		set := true

		if account.IsPasswordless() {
			// passwordless (Steam) accounts do not set a password
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}

		password, repeatPassword, currentPassword := r.FormValue("Password"), r.FormValue("RepeatPassword"), r.FormValue("CurrentPassword")

		if !account.NeedsPasswordReset(ah.config.Accounts.AdminPasswordOverride) {
			currentPasswordHash, err := hashPassword([]byte(currentPassword), []byte(account.PasswordSalt))

			if err != nil {
				smweb.AddErrorFlash(w, r, smweb.T(r, "flash.accounts.password_not_changed"))
				set = false
			}

			if !(subtle.ConstantTimeCompare([]byte(currentPasswordHash), []byte(account.PasswordHash)) == 1) {
				smweb.AddErrorFlash(w, r, smweb.T(r, "flash.accounts.password_not_changed"))
				set = false
			}
		}

		if set {
			if password == repeatPassword {
				updateDetails := account.NeedsPasswordReset(ah.config.Accounts.AdminPasswordOverride)
				err := ah.accountManager.ChangePassword(account, password)

				if err == nil {
					smweb.AddFlash(w, r, smweb.T(r, "flash.accounts.password_changed"))
					if updateDetails {
						http.Redirect(w, r, "/accounts/update", http.StatusFound)
					} else {
						http.Redirect(w, r, "/", http.StatusFound)
					}
					return
				}

				smweb.AddErrorFlash(w, r, smweb.T(r, "flash.accounts.password_not_changed"))
				logrus.WithError(err).Errorf("Could not change password for account id: %s", account.ID.String())
			} else {
				smweb.AddErrorFlash(w, r, smweb.T(r, "flash.accounts.passwords_must_match"))
			}
		}
	}

	ah.ViewRenderer.MustLoadTemplate(w, r, "accounts/new-password.html", &newPasswordTemplateVars{
		NewAccount: account.NeedsPasswordReset(ah.config.Accounts.AdminPasswordOverride),
	})
}

type updateAccountTemplateVars struct {
	smweb.BaseTemplateVars

	Account           *smmodel.Account
	ThemeOptions      []themeDetails
	SteamGUIDOverride string
}

func (ah *AccountHandler) Update(w http.ResponseWriter, r *http.Request) {
	account := smweb.AccountFromRequest(r)

	if r.Method == http.MethodPost {
		driverName, team := r.FormValue("DriverName"), r.FormValue("DriverTeam")
		theme, language := r.FormValue("Theme"), r.FormValue("Language")

		if language != "" && !ah.ViewRenderer.Bundle().Supports(language) {
			language = ""
		}

		if driverName != "" || team != "" || theme != "" || language != account.Language {
			err := ah.accountManager.updateDetails(account, driverName, team, theme, language)

			if err != nil {
				smweb.AddErrorFlash(w, r, smweb.T(r, "flash.accounts.details_not_updated"))
				logrus.WithError(err).Errorf("Could not update details for account id: %s", account.ID.String())
			} else {
				if account.GUID != "" {
					err := ah.store.UpsertEntrant(serverconfig.Entrant{
						Name: account.DriverName,
						GUID: account.GUID,
						Team: account.Team,
					})

					if err != nil {
						logrus.WithError(err).Errorf("Successfully updated details, but could not add to autofill entry list. Account id: %s", account.ID.String())
					}
				}

				if account.Language == "" {
					smweb.ForgetLanguage(w)
				} else {
					smweb.RememberLanguage(w, account.Language)
				}

				smweb.AddFlash(w, r, smweb.T(r, "flash.accounts.details_changed"))
				http.Redirect(w, r, "/", http.StatusFound)
				return
			}
		}
	}

	ah.ViewRenderer.MustLoadTemplate(w, r, "accounts/update.html", &updateAccountTemplateVars{
		Account:           account,
		ThemeOptions:      themeOptions,
		SteamGUIDOverride: r.URL.Query().Get("steamGUID"),
	})
}

func (ah *AccountHandler) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	requestAccount := smweb.AccountFromRequest(r)

	accountID := chi.URLParam(r, "id")

	if requestAccount.ID.String() == accountID {
		smweb.AddErrorFlash(w, r, smweb.T(r, "flash.accounts.cannot_delete_self"))
		http.Redirect(w, r, r.Referer(), http.StatusFound)
		return
	}

	if err := ah.store.DeleteAccount(accountID); err != nil {
		logrus.WithError(err).Errorf("Could not delete account")
		smweb.AddErrorFlash(w, r, smweb.T(r, "flash.accounts.not_deleted"))
	} else {
		smweb.AddFlash(w, r, smweb.T(r, "flash.accounts.deleted"))

		if id, err := uuid.Parse(accountID); err == nil && ah.onAccountDeleted != nil {
			if err := ah.onAccountDeleted(id); err != nil {
				logrus.WithError(err).Errorf("Could not clean up after the deleted account %s", accountID)
			}
		}
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (ah *AccountHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	accountID := chi.URLParam(r, "id")

	account, err := ah.accountManager.resetPassword(accountID)

	if err != nil {
		smweb.AddErrorFlash(w, r, smweb.T(r, "flash.accounts.password_not_reset"))
		logrus.WithError(err).Errorf("Could not reset password for account id: %s", accountID)
	} else {
		smweb.AddFlash(w, r, smweb.T(r, "flash.accounts.password_reset", "name", account.Name, "password", account.DefaultPassword))
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

var ErrAccountNeedsPassword = errors.New("servermanager: account needs to set a password")

var ErrInvalidUsernameOrPassword = errors.New("servermanager: invalid username or password")

// ErrSteamSignupDisabled is returned if a SteamID without a (usable) account logs in while the Steam sign up is off.
var ErrSteamSignupDisabled = errors.New("servermanager: no account for this Steam ID and sign up with Steam is disabled")

// steamAccountName is the internal (unique) name of an account that was created by a Steam login.
func steamAccountName(steamID string) string {
	return "steam-" + steamID
}

// loginWithSteam logs in the account whose GUID is the (already verified) steamID and starts the session. If there
// is none, an account is created if the Steam sign up is allowed. created tells whether this happened.
func (am *AccountManager) loginWithSteam(r *http.Request, w http.ResponseWriter, steamID string) (account *smmodel.Account, created bool, err error) {
	if !steamGUIDRegexp.MatchString(steamID) {
		return nil, false, fmt.Errorf("invalid SteamID %q", steamID)
	}

	am.steamMutex.Lock()
	defer am.steamMutex.Unlock()

	account, err = am.store.FindAccountByGUID(steamID)

	switch {
	case err == nil && !account.Deleted.IsZero():
		return nil, false, ErrSteamSignupDisabled
	case err == smstore.ErrAccountNotFound:
		if !am.options.SteamSignup {
			return nil, false, ErrSteamSignupDisabled
		}

		if _, err := am.store.FindAccountByName(steamAccountName(steamID)); err == nil {
			return nil, false, fmt.Errorf("an account named %q exists already", steamAccountName(steamID))
		}

		account = smmodel.NewAccount(am.serverID, am.buildVersion)
		account.Name = steamAccountName(steamID)
		account.GUID = steamID
		created = true

		if err := am.store.UpsertAccount(account); err != nil {
			return nil, false, err
		}
	case err != nil:
		return nil, false, err
	}

	sess := smweb.Session(r)
	sess.Values[sessionAccountID] = account.ID.String()
	smweb.RememberLanguage(w, account.Language)

	return account, created, sess.Save(r, w)
}

type AccountManager struct {
	// steamMutex serialises the Steam logins so that one SteamID cannot create two accounts.
	steamMutex sync.Mutex

	config *smconfig.Configuration

	// sessionStore keeps the login sessions and flash messages.
	sessionStore sessions.Store

	// options are the server wide account options, loaded from the store by LoadOptions.
	options *ServerAccountOptions

	// serverID identifies this server instance in the per-server account groups. It is set by Init.
	serverID     smmodel.ServerID
	buildVersion string

	// openAccount is the dummy account which is used for visitors if read access is open.
	openAccount *smmodel.Account
	store       smstore.Store
}

func NewAccountManager(config *smconfig.Configuration, store smstore.Store, sessionStore sessions.Store, buildVersion string) *AccountManager {
	return &AccountManager{
		config:       config,
		buildVersion: buildVersion,
		sessionStore: sessionStore,
		options:      &ServerAccountOptions{},
		store:        store,
	}
}

// LoadOptions reads the server wide account options from the store.
func (am *AccountManager) LoadOptions() error {
	err := am.store.GetMeta(serverAccountOptionsMetaKey, &am.options)

	if err != nil && err != smstore.ErrValueNotSet {
		return err
	}

	return nil
}

// Init sets the server ID and creates the dummy account for visitors if read access is open. The server ID
// is only known after the store was opened, so this has to be called before requests are served.
func (am *AccountManager) Init(serverID smmodel.ServerID) {
	am.serverID = serverID

	am.openAccount = &smmodel.Account{
		Name:            smmodel.OpenAccountName,
		Groups:          map[smmodel.ServerID]smmodel.Group{am.serverID: smmodel.GroupRead},
		LastSeenVersion: am.buildVersion,
		Theme:           smmodel.ThemeDefault,
	}
}

// RequestScopeMiddleware attaches the session store and account state to each request.
func (am *AccountManager) RequestScopeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, smweb.WithScope(r, &smweb.Scope{
			SessionStore: am.sessionStore,
			ServerID:     am.serverID,
			ReadIsOpen:   am.options.IsOpen,
			StatsPublic:  am.options.PublicStats,
			OpenAccount:  am.openAccount,
		}))
	})
}

func (am *AccountManager) login(r *http.Request, w http.ResponseWriter) error {
	if err := r.ParseForm(); err != nil {
		return err
	}

	username, password := r.FormValue("Username"), r.FormValue("Password")

	accounts, err := am.store.ListAccounts()

	if err != nil {
		return err
	}

	for _, account := range accounts {
		if username == account.Name {
			if (account.NeedsPasswordReset(am.config.Accounts.AdminPasswordOverride) && password == account.DefaultPassword && account.DefaultPassword != "") ||
				(account.Name == smmodel.AdminUserName && am.config.Accounts.AdminPasswordOverride != "" && password == am.config.Accounts.AdminPasswordOverride) {
				// first log in of the account, direct them to a reset password form
				sess := smweb.Session(r)
				sess.Values[sessionAccountID] = account.ID.String()
				smweb.RememberLanguage(w, account.Language)

				err := sess.Save(r, w)

				if err != nil {
					return err
				}

				return ErrAccountNeedsPassword
			}

			if account.IsPasswordless() {
				// e.g. Steam accounts: there is no password to compare with, whatever is entered
				break
			}

			passwordHash, err := hashPassword([]byte(password), []byte(account.PasswordSalt))

			if err != nil {
				return err
			}

			if account.PasswordHash != "" && subtle.ConstantTimeCompare([]byte(account.PasswordHash), []byte(passwordHash)) == 1 {
				sess := smweb.Session(r)
				sess.Values[sessionAccountID] = account.ID.String()
				smweb.RememberLanguage(w, account.Language)

				return sess.Save(r, w)
			}

			break
		}
	}

	return ErrInvalidUsernameOrPassword
}

func (am *AccountManager) resetPassword(accountID string) (*smmodel.Account, error) {
	account, err := am.store.FindAccountByID(accountID)

	if err != nil {
		return nil, err
	}

	defaultPass, err := diceware.Generate(4)

	if err != nil {
		return nil, err
	}

	account.DefaultPassword = strings.Join(defaultPass, "-")
	account.PasswordSalt = ""
	account.PasswordHash = ""

	return account, am.store.UpsertAccount(account)
}

func (am *AccountManager) SetCurrentVersion(account *smmodel.Account) error {
	account.LastSeenVersion = am.buildVersion

	return am.store.UpsertAccount(account)
}

func (am *AccountManager) ChangePassword(account *smmodel.Account, password string) error {
	salt, err := generateSalt()

	if err != nil {
		return err
	}

	pass, err := hashPassword([]byte(password), []byte(salt))

	if err != nil {
		return err
	}

	account.DefaultPassword = ""
	account.PasswordSalt = salt
	account.PasswordHash = pass

	return am.store.UpsertAccount(account)
}

func (am *AccountManager) updateDetails(account *smmodel.Account, name, team, theme, language string) error {
	account.DriverName = name
	account.Team = team
	account.Theme = smmodel.Theme(theme)
	account.Language = language

	return am.store.UpsertAccount(account)
}

func (ah *AccountHandler) Logout(w http.ResponseWriter, r *http.Request) {
	sess := smweb.Session(r)
	delete(sess.Values, sessionAccountID)

	_ = sess.Save(r, w)

	http.Redirect(w, r, "/", http.StatusFound)
}

type createAccountTemplateVars struct {
	smweb.BaseTemplateVars

	Account   *smmodel.Account
	IsEditing bool
}

func (ah *AccountHandler) CreateOrEditAccount(w http.ResponseWriter, r *http.Request) {
	serverID := ah.accountManager.serverID
	buildVersion := ah.accountManager.buildVersion

	var account *smmodel.Account
	isEditing := false

	if id := chi.URLParam(r, "id"); id != "" {
		var err error
		account, err = ah.store.FindAccountByID(id)

		if err != nil {
			logrus.WithError(err).Errorf("Could not find account for id: %s", id)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		isEditing = true
	} else {
		defaultPass, err := diceware.Generate(4)

		if err != nil {
			logrus.WithError(err).Errorf("Could not generate password")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		account = smmodel.NewAccount(serverID, buildVersion)
		account.DefaultPassword = strings.Join(defaultPass, "-")
	}

	if r.Method == http.MethodPost {
		username := r.FormValue("Username")
		group := smmodel.Group(r.FormValue("Group"))

		if !isEditing {
			// creating new account
			account = smmodel.NewAccount(serverID, buildVersion)
			account.DefaultPassword = r.FormValue("DefaultPassword")
		}

		account.Name = username
		account.Groups[serverID] = group

		if smweb.FormValueAsInt(r.FormValue("UpdateGroupForAllServers")) == 1 {
			for serverID := range account.Groups {
				account.Groups[serverID] = group
			}
		}

		err := ah.store.UpsertAccount(account)

		if err != nil {
			logrus.WithError(err).Errorf("Couldn't save account with id: %s", account.ID)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		if isEditing {
			smweb.AddFlash(w, r, smweb.T(r, "flash.accounts.edited"))
		} else {
			smweb.AddFlash(w, r, smweb.T(r, "flash.accounts.created"))
		}

		http.Redirect(w, r, "/accounts", http.StatusFound)
		return
	}

	ah.ViewRenderer.MustLoadTemplate(w, r, "accounts/new.html", &createAccountTemplateVars{
		Account:   account,
		IsEditing: isEditing,
	})
}

type manageAccountsTemplateVars struct {
	smweb.BaseTemplateVars

	Accounts         []*smmodel.Account
	ServerReadIsOpen bool
	PublicStats      bool
	SteamSignup      bool
	DownloadAccess   string

	TeamCreationEnabled bool
	MaxOwnedTeams       int

	Skins        smmodel.SkinSettings
	SkinDefaults smmodel.SkinSettings
}

func (ah *AccountHandler) ManageAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := ah.store.ListAccounts()

	if err != nil {
		logrus.WithError(err).Errorf("Could not list accounts")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	ah.ViewRenderer.MustLoadTemplate(w, r, "accounts/manage.html", &manageAccountsTemplateVars{
		Accounts:         accounts,
		ServerReadIsOpen: ah.accountManager.options.IsOpen,
		PublicStats:      ah.accountManager.options.PublicStats,
		SteamSignup:      ah.accountManager.options.SteamSignup,
		DownloadAccess:   ah.accountManager.options.DownloadAccess.String(),

		TeamCreationEnabled: !ah.accountManager.options.DisableTeamCreation,
		MaxOwnedTeams:       ah.accountManager.options.MaxOwnedTeams,

		Skins: ah.accountManager.options.Skins,
		SkinDefaults: smmodel.SkinSettings{
			MaxFiles:    smmodel.DefaultSkinMaxFiles,
			MaxSkinMB:   smmodel.DefaultSkinMaxMB,
			UserQuotaMB: smmodel.DefaultSkinUserQuotaMB,
			MinFreeMB:   smmodel.DefaultSkinMinFreeMB,
		},
	})
}

func hashPassword(password, salt []byte) (string, error) {
	pass, err := scrypt.Key(password, salt, 16384, 8, 1, 64)

	if err != nil {
		return "", err
	}

	return hex.EncodeToString(pass), nil
}

func generateSalt() (string, error) {
	salt := make([]byte, 32)
	_, err := io.ReadFull(rand.Reader, salt)

	if err != nil {
		return "", err
	}

	return hex.EncodeToString(salt), err
}
