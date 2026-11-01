package teams

import (
	"context"
	"errors"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi"
	"github.com/google/uuid"

	"github.com/wernersbacher/ACPitwall/internal/i18n"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
	"github.com/wernersbacher/ACPitwall/internal/web/webtest"
)

func (e *testEnv) handler() *Handler {
	return NewHandler(&smweb.BaseHandler{}, e.manager, e.store, "https://acp.example")
}

// in returns a logged in account that is in the given group of the test server.
func (e *testEnv) in(group smmodel.Group) (*smmodel.Account, Actor) {
	e.t.Helper()

	actor := e.account()
	account := e.accounts[actor.AccountID]
	account.Groups[testServerID] = group

	if err := e.store.UpsertAccount(account); err != nil {
		e.t.Fatal(err)
	}

	return account, Actor{AccountID: account.ID, Moderator: group == smmodel.GroupWrite || group == smmodel.GroupDelete || group == smmodel.GroupAdmin}
}

// call serves a request to the handler function as the account (nil for anonymous) with the route parameters.
func (e *testEnv) call(fn http.HandlerFunc, account *smmodel.Account, params map[string]string, form url.Values) *httptest.ResponseRecorder {
	e.t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	scope := webtest.RequestScope()
	scope.ServerID = testServerID
	scope.ReadIsOpen = true
	scope.OpenAccount = e.open

	req = smweb.WithScope(req, scope)

	if account != nil {
		req = smweb.WithAccount(req, account)
	}

	routeContext := chi.NewRouteContext()

	for key, value := range params {
		routeContext.URLParams.Add(key, value)
	}

	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeContext))

	rec := httptest.NewRecorder()
	fn(rec, req)

	return rec
}

func expectRedirect(t *testing.T, what string, rec *httptest.ResponseRecorder, location string) {
	t.Helper()

	if rec.Code != http.StatusFound {
		t.Errorf("%s: expected a redirect, got %d", what, rec.Code)
		return
	}

	if got := rec.Header().Get("Location"); got != location {
		t.Errorf("%s: redirected to %q, want %q", what, got, location)
	}
}

func teamParams(team *smmodel.Team, extra ...string) map[string]string {
	params := map[string]string{"teamID": team.ID.String()}

	for i := 0; i+1 < len(extra); i += 2 {
		params[extra[i]] = extra[i+1]
	}

	return params
}

func TestHandler_ActionsNeedALogin(t *testing.T) {
	e := newTestEnv(t)
	h := e.handler()
	owner := e.account()
	team := e.create(owner, "Locked", smmodel.TeamPublic)
	member := e.joinNew(team, owner)
	token, _ := e.manager.Invite(owner, team.ID.String(), uuid.Nil)

	visitors := map[string]*smmodel.Account{
		"anonymous":     nil,
		"empty account": {},
		// the dummy account an open server gives to visitors is not a login either
		"open account": e.open,
	}

	params := teamParams(team, "accountID", member.AccountID.String(), "token", token.Token)
	form := url.Values{"name": {"Hacked"}, "visibility": {"private"}, "driver": {""}, "token": {token.Token}, "admin": {"on"}}

	for name, fn := range map[string]http.HandlerFunc{
		"Create": h.Create, "Join": h.Join, "Leave": h.Leave, "Rename": h.Rename, "SetVisibility": h.SetVisibility,
		"Invite": h.Invite, "RevokeInvite": h.RevokeInvite, "DeclineInvite": h.DeclineInvite, "Kick": h.Kick,
		"SetAdmin": h.SetAdmin, "Transfer": h.Transfer, "Delete": h.Delete, "AcceptInvite": h.AcceptInvite,
	} {
		for who, account := range visitors {
			expectRedirect(t, name+" as "+who, e.call(fn, account, params, form), "/login")
		}
	}

	stored := e.reload(team)

	if stored.Name != "Locked" || stored.IsPrivate() || stored.IsDeleted() || len(stored.Members) != 2 || len(stored.Invites) != 1 {
		t.Errorf("nothing may have changed: %+v", stored)
	}
}

func TestHandler_Create(t *testing.T) {
	e := newTestEnv(t)
	h := e.handler()
	user, _ := e.in(smmodel.GroupRead)

	rec := e.call(h.Create, user, nil, url.Values{"name": {"  New   Team "}, "visibility": {"private"}})

	teams, _ := e.store.ListTeams()

	if len(teams) != 1 {
		t.Fatalf("expected one team, got %d", len(teams))
	}

	created := teams[0]

	expectRedirect(t, "create", rec, "/teams/"+created.ID.String())

	if created.Name != "New Team" || !created.IsPrivate() || !created.IsOwner(user.ID) {
		t.Errorf("unexpected team: %+v", created)
	}

	// anything but "private" is public
	e.call(h.Create, user, nil, url.Values{"name": {"Second"}, "visibility": {"whatever"}})

	if second, _ := e.manager.NameTaken("Second"); !second {
		t.Fatal("second team was not created")
	}

	teams, _ = e.store.ListTeams()

	for _, team := range teams {
		if team.Name == "Second" && team.IsPrivate() {
			t.Error("an unknown visibility must give a public team")
		}
	}

	// errors go back to the list and create nothing
	for name, form := range map[string]url.Values{
		"too short": {"name": {"x"}},
		"empty":     {"name": {""}},
		"taken":     {"name": {"NEW team"}},
	} {
		expectRedirect(t, name, e.call(h.Create, user, nil, form), "/teams")
	}

	if teams, _ := e.store.ListTeams(); len(teams) != 2 {
		t.Errorf("failed requests must not create teams, got %d", len(teams))
	}
}

func TestHandler_Create_DisabledAndLimited(t *testing.T) {
	e := newTestEnv(t)
	h := e.handler()
	user, _ := e.in(smmodel.GroupRead)
	moderator, _ := e.in(smmodel.GroupWrite)

	e.options.DisableCreation = true

	expectRedirect(t, "disabled", e.call(h.Create, user, nil, url.Values{"name": {"Blocked"}}), "/teams")

	if taken, _ := e.manager.NameTaken("Blocked"); taken {
		t.Error("creation is disabled")
	}

	// accounts with write access are moderators and may still create teams
	rec := e.call(h.Create, moderator, nil, url.Values{"name": {"Allowed"}})

	if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "/teams/") {
		t.Errorf("moderator: %d %q", rec.Code, rec.Header().Get("Location"))
	}

	e.options.DisableCreation = false
	e.options.MaxOwnedTeams = 1

	e.call(h.Create, user, nil, url.Values{"name": {"First"}})
	expectRedirect(t, "over the limit", e.call(h.Create, user, nil, url.Values{"name": {"Second"}}), "/teams")

	if taken, _ := e.manager.NameTaken("Second"); taken {
		t.Error("the limit of owned teams was ignored")
	}
}

func TestHandler_JoinLeaveAndInvites(t *testing.T) {
	e := newTestEnv(t)
	h := e.handler()
	owner := e.account()
	public := e.create(owner, "Open", smmodel.TeamPublic)
	private := e.create(owner, "Closed", smmodel.TeamPrivate)
	user, _ := e.in(smmodel.GroupRead)

	expectRedirect(t, "join public", e.call(h.Join, user, teamParams(public), nil), "/teams/"+public.ID.String())

	if !e.reload(public).IsMember(user.ID) {
		t.Error("the user must be a member of the public team")
	}

	// a private team needs an invite
	expectRedirect(t, "join private", e.call(h.Join, user, teamParams(private), nil), "/teams")

	if e.reload(private).IsMember(user.ID) {
		t.Error("joining a private team without invite must fail")
	}

	invite, err := e.manager.Invite(owner, private.ID.String(), uuid.Nil)

	if err != nil {
		t.Fatal(err)
	}

	expectRedirect(t, "wrong token", e.call(h.AcceptInvite, user, map[string]string{"token": "nope"}, nil), "/teams")

	if e.reload(private).IsMember(user.ID) {
		t.Fatal("a wrong token must not let anybody in")
	}

	expectRedirect(t, "accept the link", e.call(h.AcceptInvite, user, map[string]string{"token": invite.Token}, nil), "/teams/"+private.ID.String())

	if !e.reload(private).IsMember(user.ID) {
		t.Error("the link must let the user in")
	}

	expectRedirect(t, "link used twice", e.call(h.AcceptInvite, user, map[string]string{"token": invite.Token}, nil), "/teams")

	// leaving
	expectRedirect(t, "leave", e.call(h.Leave, user, teamParams(public), nil), "/teams")

	if e.reload(public).IsMember(user.ID) {
		t.Error("the user must have left")
	}

	expectRedirect(t, "leave twice", e.call(h.Leave, user, teamParams(public), nil), "/teams")

	// a targeted invite can be accepted and declined
	other, otherActor := e.in(smmodel.GroupRead)
	other2, _ := e.in(smmodel.GroupRead)

	if _, err := e.manager.Invite(owner, private.ID.String(), otherActor.AccountID); err != nil {
		t.Fatal(err)
	}

	if _, err := e.manager.Invite(owner, private.ID.String(), other2.ID); err != nil {
		t.Fatal(err)
	}

	expectRedirect(t, "decline", e.call(h.DeclineInvite, other2, teamParams(private), nil), "/teams")
	expectRedirect(t, "join after decline", e.call(h.Join, other2, teamParams(private), nil), "/teams")

	if e.reload(private).IsMember(other2.ID) {
		t.Error("a declined invite must not let the user in")
	}

	expectRedirect(t, "accept", e.call(h.Join, other, teamParams(private), nil), "/teams/"+private.ID.String())

	if !e.reload(private).IsMember(other.ID) {
		t.Error("the invited user must be able to join")
	}
}

func TestHandler_ManagementNeedsTheRightRole(t *testing.T) {
	e := newTestEnv(t)
	h := e.handler()
	ownerAccount, owner := e.in(smmodel.GroupRead)
	team := e.create(owner, "Roles", smmodel.TeamPublic)

	adminAccount, adminActor := e.in(smmodel.GroupRead)
	memberAccount, memberActor := e.in(smmodel.GroupRead)
	_, victimActor := e.in(smmodel.GroupRead)
	strangerAccount, _ := e.in(smmodel.GroupRead)
	moderatorAccount, _ := e.in(smmodel.GroupWrite)

	for _, a := range []Actor{adminActor, memberActor, victimActor} {
		if _, err := e.manager.Join(a, team.ID.String(), ""); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := e.manager.SetAdmin(owner, team.ID.String(), adminActor.AccountID, true); err != nil {
		t.Fatal(err)
	}

	id := team.ID.String()
	memberParams := teamParams(team, "accountID", victimActor.AccountID.String())

	// people without management rights change nothing
	for who, account := range map[string]*smmodel.Account{"member": memberAccount, "stranger": strangerAccount} {
		e.call(h.Rename, account, teamParams(team), url.Values{"name": {"Hacked"}})
		e.call(h.SetVisibility, account, teamParams(team), url.Values{"visibility": {"private"}})
		e.call(h.Invite, account, teamParams(team), url.Values{})
		e.call(h.Kick, account, memberParams, nil)
		e.call(h.SetAdmin, account, memberParams, url.Values{"admin": {"on"}})
		e.call(h.Transfer, account, memberParams, nil)
		e.call(h.Delete, account, teamParams(team), nil)

		got := e.reload(team)

		if got.Name != "Roles" || got.IsPrivate() || got.IsDeleted() || !got.IsMember(victimActor.AccountID) || got.Member(victimActor.AccountID).Role != smmodel.TeamRoleMember || !got.IsOwner(owner.AccountID) || len(got.Invites) != 0 {
			t.Fatalf("%s changed the team: %+v", who, got)
		}
	}

	// an admin may rename, change the visibility, invite and remove a member, but not promote, hand over or delete
	e.call(h.Rename, adminAccount, teamParams(team), url.Values{"name": {"By Admin"}})
	e.call(h.SetVisibility, adminAccount, teamParams(team), url.Values{"visibility": {"private"}})
	e.call(h.Invite, adminAccount, teamParams(team), url.Values{})
	e.call(h.SetAdmin, adminAccount, teamParams(team, "accountID", memberActor.AccountID.String()), url.Values{"admin": {"on"}})
	e.call(h.Transfer, adminAccount, teamParams(team, "accountID", memberActor.AccountID.String()), nil)
	e.call(h.Delete, adminAccount, teamParams(team), nil)

	got := e.reload(team)

	if got.Name != "By Admin" || !got.IsPrivate() || len(got.Invites) != 1 {
		t.Errorf("the admin could not manage the team: %+v", got)
	}

	if got.IsDeleted() || !got.IsOwner(owner.AccountID) || got.Member(memberActor.AccountID).Role != smmodel.TeamRoleMember {
		t.Errorf("the admin did more than an admin may: %+v", got.Members)
	}

	e.call(h.Kick, adminAccount, memberParams, nil)

	if e.reload(team).IsMember(victimActor.AccountID) {
		t.Error("an admin must be able to remove a member")
	}

	// the owner can do everything, a moderator too (without being in the team)
	e.call(h.SetAdmin, ownerAccount, teamParams(team, "accountID", memberActor.AccountID.String()), url.Values{"admin": {"on"}})

	if e.reload(team).Member(memberActor.AccountID).Role != smmodel.TeamRoleAdmin {
		t.Error("the owner must be able to promote")
	}

	e.call(h.Transfer, moderatorAccount, teamParams(team, "accountID", memberActor.AccountID.String()), nil)

	if !e.reload(team).IsOwner(memberActor.AccountID) {
		t.Error("a moderator must be able to transfer the ownership")
	}

	e.call(h.Delete, moderatorAccount, teamParams(team), nil)

	if _, err := e.manager.Lookup(id); err != nil || !e.reload(team).IsDeleted() {
		t.Error("a moderator must be able to delete the team")
	}
}

func TestHandler_InviteByName(t *testing.T) {
	e := newTestEnv(t)
	h := e.handler()
	ownerAccount, owner := e.in(smmodel.GroupRead)
	team := e.create(owner, "Names", smmodel.TeamPrivate)
	target, targetActor := e.in(smmodel.GroupRead)
	member := e.joinNew(team, owner)

	// the account name is looked up as it was typed, surrounding spaces are fine
	expectRedirect(t, "by name", e.call(h.Invite, ownerAccount, teamParams(team), url.Values{"driver": {" " + target.Name + " "}}), "/teams/"+team.ID.String())

	got := e.reload(team)

	if len(got.Invites) != 1 || got.Invites[0].AccountID != targetActor.AccountID || got.Invites[0].CreatedBy != owner.AccountID {
		t.Fatalf("expected an invite for the account: %+v", got.Invites)
	}

	// an unknown account creates nothing (and does not reveal more than that)
	e.call(h.Invite, ownerAccount, teamParams(team), url.Values{"driver": {"nobody-by-that-name"}})

	if len(e.reload(team).Invites) != 1 {
		t.Error("an unknown account must not create an invite")
	}

	// a member cannot be invited
	e.call(h.Invite, ownerAccount, teamParams(team), url.Values{"driver": {e.accounts[member.AccountID].Name}})

	if len(e.reload(team).Invites) != 1 {
		t.Error("a member must not get an invite")
	}

	// without a name it is a link anybody can use
	e.call(h.Invite, ownerAccount, teamParams(team), url.Values{"driver": {"   "}})

	got = e.reload(team)

	if len(got.Invites) != 2 || got.Invites[1].AccountID != uuid.Nil {
		t.Errorf("expected a link invite: %+v", got.Invites)
	}

	// withdrawing
	e.call(h.RevokeInvite, ownerAccount, teamParams(team), url.Values{"token": {got.Invites[1].Token}})

	if len(e.reload(team).Invites) != 1 {
		t.Error("the invite must be withdrawn")
	}
}

func TestHandler_BadIDs(t *testing.T) {
	e := newTestEnv(t)
	h := e.handler()
	ownerAccount, owner := e.in(smmodel.GroupRead)
	team := e.create(owner, "Bad IDs", smmodel.TeamPublic)
	member := e.joinNew(team, owner)

	for _, id := range []string{"", "x", "../x", `..\..\accounts\x`, "00000000-0000-0000-0000-000000000000"} {
		memberParams := teamParams(team, "accountID", id)

		e.call(h.Kick, ownerAccount, memberParams, nil)
		e.call(h.SetAdmin, ownerAccount, memberParams, url.Values{"admin": {"on"}})
		e.call(h.Transfer, ownerAccount, memberParams, nil)

		teamOnly := map[string]string{"teamID": id}

		for _, fn := range []http.HandlerFunc{h.Join, h.Leave, h.Rename, h.SetVisibility, h.Invite, h.RevokeInvite, h.DeclineInvite, h.Delete} {
			rec := e.call(fn, ownerAccount, teamOnly, url.Values{"name": {"Changed"}})

			if rec.Code != http.StatusFound {
				t.Errorf("a bad team id %q must redirect, got %d", id, rec.Code)
			}
		}
	}

	got := e.reload(team)

	if got.Name != "Bad IDs" || got.IsDeleted() || !got.IsMember(member.AccountID) || !got.IsOwner(owner.AccountID) || len(got.Members) != 2 {
		t.Errorf("bad ids must not change anything: %+v", got)
	}
}

func TestHandler_ViewAndInvitePageNotFound(t *testing.T) {
	e := newTestEnv(t)
	h := e.handler()
	owner := e.account()
	private := e.create(owner, "Hidden Team", smmodel.TeamPrivate)
	stranger, _ := e.in(smmodel.GroupRead)

	// a stranger cannot find out that a private team exists
	for who, account := range map[string]*smmodel.Account{"stranger": stranger, "anonymous": nil} {
		if rec := e.call(h.View, account, teamParams(private), nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s: expected 404 for a private team, got %d", who, rec.Code)
		}
	}

	for _, id := range []string{"unknown", uuid.New().String(), `..\..\x`} {
		if rec := e.call(h.View, stranger, map[string]string{"teamID": id}, nil); rec.Code != http.StatusNotFound {
			t.Errorf("View(%q): expected 404, got %d", id, rec.Code)
		}
	}

	// an invalid or expired invite goes back to the list
	expectRedirect(t, "invalid invite page", e.call(h.InvitePage, stranger, map[string]string{"token": "nope"}, nil), "/teams")

	link, _ := e.manager.Invite(owner, private.ID.String(), uuid.Nil)
	e.clock = e.clock.Add(DefaultInviteTTL + time.Hour)

	expectRedirect(t, "expired invite page", e.call(h.InvitePage, stranger, map[string]string{"token": link.Token}, nil), "/teams")
}

func TestHandler_RenameHookFailureStillRenames(t *testing.T) {
	e := newTestEnv(t)
	h := e.handler()
	ownerAccount, owner := e.in(smmodel.GroupRead)
	team := e.create(owner, "Before", smmodel.TeamPublic)

	e.manager.SetRenameHook(func(*smmodel.Team) error { return errors.New("championships are gone") })

	rec := e.call(h.Rename, ownerAccount, teamParams(team), url.Values{"name": {"After"}})

	expectRedirect(t, "rename with failing hook", rec, "/teams/"+team.ID.String())

	if e.reload(team).Name != "After" {
		t.Error("the team must be renamed even though the follow up failed")
	}
}

func TestHandler_RenameErrors(t *testing.T) {
	e := newTestEnv(t)
	h := e.handler()
	ownerAccount, owner := e.in(smmodel.GroupRead)
	team := e.create(owner, "Mine", smmodel.TeamPublic)
	e.create(owner, "Taken", smmodel.TeamPublic)

	for _, name := range []string{"x", "", "taken", "  TAKEN  "} {
		expectRedirect(t, "rename to "+name, e.call(h.Rename, ownerAccount, teamParams(team), url.Values{"name": {name}}), "/teams/"+team.ID.String())

		if e.reload(team).Name != "Mine" {
			t.Errorf("rename to %q must not work", name)
		}
	}
}

func TestTeamErrorMessage(t *testing.T) {
	// the English flash messages of the catalogs
	catalog, err := ioutil.ReadFile(filepath.Join("..", "..", "cmd", "acpitwall", "views", "locales", "flash.en.yaml"))

	if err != nil {
		t.Fatal(err)
	}

	bundle, err := i18n.NewBundle(map[string][]byte{"flash.en.yaml": catalog})

	if err != nil {
		t.Fatal(err)
	}

	r := smweb.WithBundle(httptest.NewRequest(http.MethodGet, "/", nil), bundle)

	for _, err := range []error{
		smmodel.ErrTeamNotFound, smmodel.ErrTeamNameInvalid, smmodel.ErrTeamNameTaken, smmodel.ErrNotTeamMember,
		smmodel.ErrAlreadyTeamMember, smmodel.ErrTeamInviteInvalid, smmodel.ErrTeamPermission, smmodel.ErrTeamOwnerRole,
		smmodel.ErrTeamTransferToSelf, smmodel.ErrTeamLimitReached, smmodel.ErrTeamCreationBlocked, smstore.ErrAccountNotFound,
	} {
		if message, ok := teamErrorMessage(r, err); !ok || message == "" || strings.HasPrefix(message, "flash.") {
			t.Errorf("%v: no message (%q)", err, message)
		}
	}

	if _, ok := teamErrorMessage(r, errors.New("disk is on fire")); ok {
		t.Error("unknown errors must not be shown to the user")
	}

	if message, _ := teamErrorMessage(r, smmodel.ErrTeamNameInvalid); !strings.Contains(message, "2") || !strings.Contains(message, "40") {
		t.Errorf("the name rules belong in the message: %q", message)
	}
}

func TestDisplayName(t *testing.T) {
	if got := displayName(&smmodel.Account{Name: "login", DriverName: "Driver"}); got != "Driver" {
		t.Errorf("the driver name is shown: %q", got)
	}

	if got := displayName(&smmodel.Account{Name: "login"}); got != "login" {
		t.Errorf("the account name is the fallback: %q", got)
	}
}
