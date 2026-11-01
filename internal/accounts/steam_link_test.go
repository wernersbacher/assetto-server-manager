package accounts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi"
	"github.com/google/uuid"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

const otherSteamID = "76561198000000043"

func newAccount(t *testing.T, ah *AccountHandler, name, guid string) *smmodel.Account {
	t.Helper()

	account := smmodel.NewAccount(testServerID, "test")
	account.Name = name
	account.GUID = guid
	account.PasswordHash = "x"

	if err := ah.store.UpsertAccount(account); err != nil {
		t.Fatal(err)
	}

	return account
}

// sessionCookies logs the account in and returns the cookies that a browser would send afterwards.
func sessionCookies(t *testing.T, ah *AccountHandler, account *smmodel.Account) []*http.Cookie {
	t.Helper()

	req := smweb.WithScope(httptest.NewRequest(http.MethodGet, "/", nil), &smweb.Scope{SessionStore: ah.accountManager.sessionStore})
	rec := httptest.NewRecorder()

	smweb.Session(req).Values[sessionAccountID] = account.ID.String()

	if err := smweb.Session(req).Save(req, rec); err != nil {
		t.Fatal(err)
	}

	return rec.Result().Cookies()
}

func contextWithRoute(req *http.Request, rctx *chi.Context) context.Context {
	return context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
}

func linkRequest(ah *AccountHandler, target string, cookies ...*http.Cookie) *http.Request {
	req := httptest.NewRequest(http.MethodGet, target, nil)

	for _, c := range cookies {
		req.AddCookie(c)
	}

	return smweb.WithScope(req, &smweb.Scope{SessionStore: ah.accountManager.sessionStore})
}

func TestLinkSteam(t *testing.T) {
	ah, _, cleanup := newTestHandler(t)
	defer cleanup()

	account := newAccount(t, ah, "driver", "")
	newAccount(t, ah, "other", otherSteamID)

	for _, id := range []string{"", "abc", "1;2"} {
		if err := ah.accountManager.linkSteam(account, id); err != ErrInvalidSteamID {
			t.Errorf("id %q: expected ErrInvalidSteamID, got %v", id, err)
		}
	}

	// another account has the ID
	if err := ah.accountManager.linkSteam(account, otherSteamID); err != ErrSteamIDInUse {
		t.Fatalf("expected ErrSteamIDInUse, got %v", err)
	}

	if stored, _ := ah.store.FindAccountByID(account.ID.String()); stored.GUID != "" {
		t.Error("a refused link must not change the account")
	}

	// success
	if err := ah.accountManager.linkSteam(account, testSteamID); err != nil {
		t.Fatal(err)
	}

	if stored, _ := ah.store.FindAccountByID(account.ID.String()); stored.GUID != testSteamID {
		t.Errorf("stored GUID %q", stored.GUID)
	}

	// connected already
	if err := ah.accountManager.linkSteam(account, "76561198000000099"); err != ErrSteamAlreadyLinked {
		t.Fatalf("expected ErrSteamAlreadyLinked, got %v", err)
	}
}

func TestLinkSteam_AdminCanLogInWithSteamAfterwards(t *testing.T) {
	ah, _, cleanup := newTestHandler(t)
	defer cleanup()

	admin := smmodel.NewAccount(testServerID, "test")
	admin.Name = smmodel.AdminUserName
	admin.PasswordHash = "x"
	admin.Groups[testServerID] = smmodel.GroupAdmin

	if err := ah.store.UpsertAccount(admin); err != nil {
		t.Fatal(err)
	}

	if err := ah.accountManager.linkSteam(admin, testSteamID); err != nil {
		t.Fatal(err)
	}

	req := steamRequest(ah)
	account, created, err := ah.accountManager.loginWithSteam(req, httptest.NewRecorder(), testSteamID)

	if err != nil || created || account.ID != admin.ID {
		t.Fatalf("the admin must log in with Steam: %v %v %v", account, created, err)
	}

	if sessionAccount(t, ah, req) != admin.ID.String() {
		t.Error("the session must hold the admin account")
	}
}

func TestSteamLink_Flow(t *testing.T) {
	ah, _, cleanup := newTestHandler(t)
	defer cleanup()

	account := newAccount(t, ah, "driver", "")
	account.DriverName = "Max Driver"

	if err := ah.store.UpsertAccount(account); err != nil {
		t.Fatal(err)
	}

	session := sessionCookies(t, ah, account)

	// start: needs the login
	rec := httptest.NewRecorder()
	ah.SteamLink().ServeHTTP(rec, linkRequest(ah, steamLinkPath))

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/login" {
		t.Fatalf("anonymous start: %d %q", rec.Code, rec.Header().Get("Location"))
	}

	// start with the session: cookie, then to Steam
	rec = httptest.NewRecorder()
	ah.SteamLink().ServeHTTP(rec, linkRequest(ah, steamLinkPath, session...))

	if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "https://steamcommunity.com/openid/login") {
		t.Fatalf("start: %d %q", rec.Code, rec.Header().Get("Location"))
	}

	var link *http.Cookie

	for _, c := range rec.Result().Cookies() {
		if c.Name == steamLinkCookie {
			link = c
		}
	}

	if link == nil || link.SameSite != http.SameSiteLaxMode || !link.HttpOnly || link.Value == "" {
		t.Fatalf("unexpected link cookie %+v", link)
	}

	// the answer of Steam (only the cookie of the link, no session): remembered, then same site on
	if !ah.accountManager.answerSteamLink(link.Value, testSteamID, false) {
		t.Fatal("the started link must accept an answer")
	}

	if ah.accountManager.answerSteamLink(link.Value, otherSteamID, false) {
		t.Fatal("a link must only be answered once")
	}

	// finish with another account is refused and uses up the link
	stranger := newAccount(t, ah, "stranger", "")

	finish := func(a *smmodel.Account) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		ah.SteamLinkFinish(rec, smweb.WithAccount(linkRequest(ah, steamLinkFinish, link), a))

		return rec
	}

	if rec := finish(stranger); rec.Code != http.StatusFound {
		t.Fatalf("finish of a stranger: %d", rec.Code)
	}

	if stored, _ := ah.store.FindAccountByID(stranger.ID.String()); stored.GUID != "" {
		t.Fatal("another account must not get the SteamID of the link")
	}

	if stored, _ := ah.store.FindAccountByID(account.ID.String()); stored.GUID != "" {
		t.Fatal("the link must be used up")
	}

	// a new link, finished by its account
	token, err := ah.accountManager.startSteamLink(account.ID.String())

	if err != nil || !ah.accountManager.answerSteamLink(token, testSteamID, false) {
		t.Fatal("could not start the second link")
	}

	link = &http.Cookie{Name: steamLinkCookie, Value: token}

	rec = finish(account)

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/accounts/update" {
		t.Fatalf("finish: %d %q", rec.Code, rec.Header().Get("Location"))
	}

	stored, _ := ah.store.FindAccountByID(account.ID.String())

	if stored.GUID != testSteamID {
		t.Fatalf("GUID %q, want %q", stored.GUID, testSteamID)
	}

	// and the driver is in the autofill entry list
	entrants, err := ah.store.ListEntrants()

	if err != nil {
		t.Fatal(err)
	}

	found := false

	for _, e := range entrants {
		found = found || (e.GUID == testSteamID && e.Name == "Max Driver")
	}

	if !found {
		t.Errorf("the entrant was not added to the autofill list: %+v", entrants)
	}

	// the same link cannot be used again
	if rec := finish(account); rec.Code != http.StatusFound {
		t.Fatalf("replay: %d", rec.Code)
	}

	// a connected account cannot start again
	rec = httptest.NewRecorder()
	ah.SteamLink().ServeHTTP(rec, linkRequest(ah, steamLinkPath, session...))

	if rec.Header().Get("Location") != "/accounts/update" {
		t.Errorf("connected account start: %q", rec.Header().Get("Location"))
	}
}

func TestSteamLink_NeedsStartedLink(t *testing.T) {
	ah, _, cleanup := newTestHandler(t)
	defer cleanup()

	account := newAccount(t, ah, "driver", "")

	// the answer of Steam without a started link (no cookie, or a made up one) is not stored
	for _, c := range []*http.Cookie{nil, {Name: steamLinkCookie, Value: "made-up"}} {
		var cookies []*http.Cookie

		if c != nil {
			cookies = append(cookies, c)
		}

		rec := httptest.NewRecorder()
		ah.answerLink(rec, linkRequest(ah, steamLinkPath, cookies...), testSteamID, false)

		if !strings.Contains(rec.Body.String(), "/accounts/update") || strings.Contains(rec.Body.String(), steamLinkFinish) {
			t.Errorf("cookie %v: unexpected answer %q", c, rec.Body.String())
		}
	}

	// finish without any link
	rec := httptest.NewRecorder()
	ah.SteamLinkFinish(rec, smweb.WithAccount(linkRequest(ah, steamLinkFinish), account))

	if stored, _ := ah.store.FindAccountByID(account.ID.String()); stored.GUID != "" || rec.Code != http.StatusFound {
		t.Errorf("finish without link: %d GUID %q", rec.Code, stored.GUID)
	}

	// a cancelled answer links nothing
	token, _ := ah.accountManager.startSteamLink(account.ID.String())
	ah.accountManager.answerSteamLink(token, "", true)

	rec = httptest.NewRecorder()
	ah.SteamLinkFinish(rec, smweb.WithAccount(linkRequest(ah, steamLinkFinish, &http.Cookie{Name: steamLinkCookie, Value: token}), account))

	if stored, _ := ah.store.FindAccountByID(account.ID.String()); stored.GUID != "" {
		t.Error("a cancelled link must not connect the account")
	}
}

func TestSteamLink_Expires(t *testing.T) {
	ah, _, cleanup := newTestHandler(t)
	defer cleanup()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ah.accountManager.now = func() time.Time { return now }

	account := newAccount(t, ah, "driver", "")

	token, _ := ah.accountManager.startSteamLink(account.ID.String())
	now = now.Add(steamLinkTTL + time.Second)

	if ah.accountManager.answerSteamLink(token, testSteamID, false) {
		t.Error("an expired link must not take an answer")
	}

	token, _ = ah.accountManager.startSteamLink(account.ID.String())

	if !ah.accountManager.answerSteamLink(token, testSteamID, false) {
		t.Fatal("answer")
	}

	now = now.Add(steamLinkTTL + time.Second)

	if ah.accountManager.takeSteamLink(token) != nil {
		t.Error("an expired link must not be taken")
	}
}

func TestCreateSteamAccount(t *testing.T) {
	ah, _, cleanup := newTestHandler(t)
	defer cleanup()

	for _, id := range []string{"", "abc", "1234567890123456", "123456789012345678", testSteamID + "x"} {
		if _, err := ah.accountManager.createSteamAccount(id, smmodel.GroupRead, ""); err != ErrInvalidSteamID {
			t.Errorf("id %q: expected ErrInvalidSteamID, got %v", id, err)
		}
	}

	account, err := ah.accountManager.createSteamAccount(" "+testSteamID+" ", smmodel.GroupWrite, " Max ")

	if err != nil {
		t.Fatal(err)
	}

	stored, err := ah.store.FindAccountByGUID(testSteamID)

	if err != nil || stored.ID != account.ID {
		t.Fatalf("not stored: %v", err)
	}

	if stored.Name != "steam-"+testSteamID || stored.DriverName != "Max" || !stored.IsPasswordless() || stored.Groups[testServerID] != smmodel.GroupWrite {
		t.Errorf("unexpected account %+v", stored)
	}

	// the SteamID is unique, also against accounts that were connected later
	if _, err := ah.accountManager.createSteamAccount(testSteamID, smmodel.GroupRead, ""); err != ErrSteamIDInUse {
		t.Errorf("expected ErrSteamIDInUse, got %v", err)
	}

	newAccount(t, ah, "driver", otherSteamID)

	if _, err := ah.accountManager.createSteamAccount(otherSteamID, smmodel.GroupRead, ""); err != ErrSteamIDInUse {
		t.Errorf("expected ErrSteamIDInUse, got %v", err)
	}

	// and the driver signs in with Steam into this account, also without the self sign up
	req := steamRequest(ah)
	got, created, err := ah.accountManager.loginWithSteam(req, httptest.NewRecorder(), testSteamID)

	if err != nil || created || got.ID != account.ID {
		t.Fatalf("login with the created account: %v %v %v", got, created, err)
	}
}

func postAccountForm(ah *AccountHandler, path, id string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = smweb.WithScope(req, &smweb.Scope{SessionStore: ah.accountManager.sessionStore})

	if id != "" {
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", id)
		req = req.WithContext(contextWithRoute(req, rctx))
	}

	rec := httptest.NewRecorder()
	ah.CreateOrEditAccount(rec, req)

	return rec
}

func TestCreateOrEditAccount_Steam(t *testing.T) {
	ah, _, cleanup := newTestHandler(t)
	defer cleanup()

	rec := postAccountForm(ah, "/accounts/new", "", url.Values{"AccountType": {"steam"}, "SteamID": {testSteamID}, "Group": {"write"}, "DisplayName": {"Max"}})

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/accounts" {
		t.Fatalf("create: %d %q", rec.Code, rec.Header().Get("Location"))
	}

	account, err := ah.store.FindAccountByGUID(testSteamID)

	if err != nil || account.Groups[testServerID] != smmodel.GroupWrite || account.DriverName != "Max" {
		t.Fatalf("account %+v err %v", account, err)
	}

	// invalid and duplicate IDs go back to the form
	for _, id := range []string{"123", testSteamID} {
		rec = postAccountForm(ah, "/accounts/new", "", url.Values{"AccountType": {"steam"}, "SteamID": {id}, "Group": {"read"}})

		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/accounts/new" {
			t.Errorf("id %q: %d %q", id, rec.Code, rec.Header().Get("Location"))
		}
	}

	// an unknown group is refused
	if rec := postAccountForm(ah, "/accounts/new", "", url.Values{"AccountType": {"steam"}, "SteamID": {otherSteamID}, "Group": {"god"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("group: %d", rec.Code)
	}

	// changing the group keeps the name (also if a name is posted)
	rec = postAccountForm(ah, "/accounts/edit/"+account.ID.String(), account.ID.String(), url.Values{"Username": {"hacked"}, "Group": {"delete"}})

	if rec.Code != http.StatusFound {
		t.Fatalf("edit: %d", rec.Code)
	}

	edited, _ := ah.store.FindAccountByID(account.ID.String())

	if edited.Name != "steam-"+testSteamID || edited.Groups[testServerID] != smmodel.GroupDelete || edited.GUID != testSteamID || !edited.IsPasswordless() {
		t.Errorf("unexpected edited account %+v", edited)
	}
}

func TestCreateOrEditAccount_PasswordAccountStillWorks(t *testing.T) {
	ah, _, cleanup := newTestHandler(t)
	defer cleanup()

	rec := postAccountForm(ah, "/accounts/new", "", url.Values{"Username": {"classic"}, "Group": {"read"}, "DefaultPassword": {"one-two"}})

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/accounts" {
		t.Fatalf("create: %d", rec.Code)
	}

	account, err := ah.store.FindAccountByName("classic")

	if err != nil || account.DefaultPassword != "one-two" || account.IsPasswordless() {
		t.Fatalf("account %+v err %v", account, err)
	}

	rec = postAccountForm(ah, "/accounts/edit/"+account.ID.String(), account.ID.String(), url.Values{"Username": {"classic"}, "Group": {"admin"}})

	if rec.Code != http.StatusFound {
		t.Fatalf("edit: %d", rec.Code)
	}

	if edited, _ := ah.store.FindAccountByID(account.ID.String()); edited.Name != "classic" || edited.DefaultPassword != "one-two" || edited.Groups[testServerID] != smmodel.GroupAdmin {
		t.Errorf("unexpected edited account %+v", edited)
	}
}

func TestDeleteSteamAccount(t *testing.T) {
	ah, _, cleanup := newTestHandler(t)
	defer cleanup()

	account, err := ah.accountManager.createSteamAccount(testSteamID, smmodel.GroupRead, "")

	if err != nil {
		t.Fatal(err)
	}

	admin := newAccount(t, ah, "boss", "")

	req := httptest.NewRequest(http.MethodPost, "/accounts/delete/"+account.ID.String(), nil)
	req.Header.Set("Referer", "/accounts")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", account.ID.String())
	req = smweb.WithAccount(smweb.WithScope(req.WithContext(contextWithRoute(req, rctx)), &smweb.Scope{SessionStore: ah.accountManager.sessionStore}), admin)

	var deleted string

	ah.SetAccountDeletedHook(func(id uuid.UUID) error {
		deleted = id.String()
		return nil
	})

	ah.DeleteAccount(httptest.NewRecorder(), req)

	if deleted != account.ID.String() {
		t.Errorf("the deleted hook got %q", deleted)
	}

	if found, err := ah.store.FindAccountByGUID(testSteamID); err == nil && found.Deleted.IsZero() {
		t.Error("the Steam account must be deleted")
	}
}
