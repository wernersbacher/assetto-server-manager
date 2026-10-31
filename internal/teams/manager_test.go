package teams

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/serverenv/envtest"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
)

type testEnv struct {
	t       *testing.T
	store   smstore.Store
	manager *Manager
	options Options
	clock   time.Time
	accNum  int
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	store, cleanup := envtest.TempJSONStore()
	t.Cleanup(cleanup)

	env := &testEnv{t: t, store: store, clock: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)}

	env.manager = NewManager(store, func() Options { return env.options })
	env.manager.now = func() time.Time { return env.clock }

	return env
}

// account creates an account and returns it as a normal actor.
func (e *testEnv) account() Actor {
	e.t.Helper()

	e.accNum++

	account := smmodel.NewAccount("test-server", "test")
	account.Name = fmt.Sprintf("user-%d", e.accNum)

	if err := e.store.UpsertAccount(account); err != nil {
		e.t.Fatal(err)
	}

	return Actor{AccountID: account.ID}
}

func (e *testEnv) moderator() Actor {
	a := e.account()
	a.Moderator = true

	return a
}

func (e *testEnv) create(owner Actor, name string, vis smmodel.TeamVisibility) *smmodel.Team {
	e.t.Helper()

	team, err := e.manager.Create(owner, name, vis)

	if err != nil {
		e.t.Fatalf("create %q: %s", name, err)
	}

	return team
}

// joinNew lets a new account join the team (via invite for private teams) and advances the clock so join order is clear.
func (e *testEnv) joinNew(team *smmodel.Team, owner Actor) Actor {
	e.t.Helper()

	e.clock = e.clock.Add(time.Minute)

	member := e.account()

	if team.IsPrivate() {
		if _, err := e.manager.Invite(owner, team.ID.String(), member.AccountID); err != nil {
			e.t.Fatal(err)
		}
	}

	if _, err := e.manager.Join(member, team.ID.String(), ""); err != nil {
		e.t.Fatalf("join: %s", err)
	}

	return member
}

func (e *testEnv) reload(team *smmodel.Team) *smmodel.Team {
	e.t.Helper()

	reloaded, err := e.store.FindTeamByID(team.ID.String())

	if err != nil {
		e.t.Fatal(err)
	}

	return reloaded
}

func expectErr(t *testing.T, what string, got, want error) {
	t.Helper()

	if got != want {
		t.Errorf("%s: expected %v, got %v", what, want, got)
	}
}

func TestCreate(t *testing.T) {
	e := newTestEnv(t)
	owner := e.account()

	team := e.create(owner, "  Red   Bull ", smmodel.TeamPrivate)

	if team.Name != "Red Bull" || !team.IsPrivate() || !team.IsOwner(owner.AccountID) {
		t.Errorf("unexpected team: %+v", team)
	}

	stored := e.reload(team)

	if stored.Name != "Red Bull" || len(stored.Members) != 1 {
		t.Errorf("team was not persisted: %+v", stored)
	}
}

func TestCreate_NameRules(t *testing.T) {
	e := newTestEnv(t)
	a, b := e.account(), e.account()

	e.create(a, "Red Bull", smmodel.TeamPublic)

	for _, name := range []string{"Red Bull", "red bull", "  RED   BULL  "} {
		_, err := e.manager.Create(b, name, smmodel.TeamPublic)
		expectErr(t, "duplicate name "+name, err, smmodel.ErrTeamNameTaken)
	}

	// a private team still blocks the name, otherwise one could impersonate it
	e.create(a, "Secret Squad", smmodel.TeamPrivate)
	_, err := e.manager.Create(b, "secret squad", smmodel.TeamPublic)
	expectErr(t, "name of a private team", err, smmodel.ErrTeamNameTaken)

	_, err = e.manager.Create(b, "x", smmodel.TeamPublic)
	expectErr(t, "too short", err, smmodel.ErrTeamNameInvalid)

	_, err = e.manager.Create(b, "", smmodel.TeamPublic)
	expectErr(t, "empty", err, smmodel.ErrTeamNameInvalid)

	taken, err := e.manager.NameTaken("RED BULL")

	if err != nil || !taken {
		t.Errorf("NameTaken: %v %v", taken, err)
	}
}

func TestCreate_NameIsFreeAgainAfterDelete(t *testing.T) {
	e := newTestEnv(t)
	a, b := e.account(), e.account()

	team := e.create(a, "Phoenix", smmodel.TeamPublic)

	if err := e.manager.Delete(a, team.ID.String()); err != nil {
		t.Fatal(err)
	}

	again := e.create(b, "Phoenix", smmodel.TeamPublic)

	if again.ID == team.ID {
		t.Error("expected a new team")
	}
}

func TestCreate_Anonymous_And_DeletedAccount(t *testing.T) {
	e := newTestEnv(t)

	_, err := e.manager.Create(Actor{}, "Ghosts", smmodel.TeamPublic)
	expectErr(t, "anonymous", err, smmodel.ErrTeamPermission)

	_, err = e.manager.Create(Actor{AccountID: uuid.New()}, "Ghosts", smmodel.TeamPublic)
	expectErr(t, "unknown account", err, smmodel.ErrTeamPermission)

	actor := e.account()

	if err := e.store.DeleteAccount(actor.AccountID.String()); err != nil {
		t.Fatal(err)
	}

	_, err = e.manager.Create(actor, "Zombies", smmodel.TeamPublic)
	expectErr(t, "deleted account", err, smmodel.ErrTeamPermission)

	// even a moderator needs an account
	_, err = e.manager.Create(Actor{Moderator: true}, "Mods", smmodel.TeamPublic)
	expectErr(t, "moderator without account", err, smmodel.ErrTeamPermission)
}

func TestCreate_Limits(t *testing.T) {
	t.Run("default limit counts owned teams", func(t *testing.T) {
		e := newTestEnv(t)
		a, b := e.account(), e.account()

		for i := 0; i < DefaultMaxOwnedTeams; i++ {
			e.create(a, fmt.Sprintf("Team %d", i), smmodel.TeamPublic)
		}

		_, err := e.manager.Create(a, "One too many", smmodel.TeamPublic)
		expectErr(t, "above the limit", err, smmodel.ErrTeamLimitReached)

		// the limit is per account
		e.create(b, "Other owner", smmodel.TeamPublic)
	})

	t.Run("joined teams do not count", func(t *testing.T) {
		e := newTestEnv(t)
		a, b := e.account(), e.account()

		// b owns as many teams as allowed, a joins all of them and can still create the full number of own teams
		for i := 0; i < DefaultMaxOwnedTeams; i++ {
			team := e.create(b, fmt.Sprintf("B%d", i), smmodel.TeamPublic)

			if _, err := e.manager.Join(a, team.ID.String(), ""); err != nil {
				t.Fatal(err)
			}
		}

		for i := 0; i < DefaultMaxOwnedTeams; i++ {
			e.create(a, fmt.Sprintf("A%d", i), smmodel.TeamPublic)
		}

		_, err := e.manager.Create(a, "A-extra", smmodel.TeamPublic)
		expectErr(t, "above the limit", err, smmodel.ErrTeamLimitReached)
	})

	t.Run("deleting a team frees a slot", func(t *testing.T) {
		e := newTestEnv(t)
		a := e.account()

		var first *smmodel.Team

		for i := 0; i < DefaultMaxOwnedTeams; i++ {
			team := e.create(a, fmt.Sprintf("Team %d", i), smmodel.TeamPublic)

			if first == nil {
				first = team
			}
		}

		if err := e.manager.Delete(a, first.ID.String()); err != nil {
			t.Fatal(err)
		}

		e.create(a, "Replacement", smmodel.TeamPublic)
	})

	t.Run("a transferred team counts for the new owner", func(t *testing.T) {
		e := newTestEnv(t)
		a, b := e.account(), e.account()
		e.options.MaxOwnedTeams = 1

		team := e.create(a, "Handover", smmodel.TeamPublic)

		if _, err := e.manager.Join(b, team.ID.String(), ""); err != nil {
			t.Fatal(err)
		}

		if _, err := e.manager.TransferOwnership(a, team.ID.String(), b.AccountID); err != nil {
			t.Fatal(err)
		}

		_, err := e.manager.Create(b, "Second", smmodel.TeamPublic)
		expectErr(t, "new owner at limit", err, smmodel.ErrTeamLimitReached)

		e.create(a, "Second", smmodel.TeamPublic) // the old owner has a free slot now
	})

	t.Run("custom, unlimited and moderators", func(t *testing.T) {
		e := newTestEnv(t)
		a, mod := e.account(), e.moderator()

		e.options.MaxOwnedTeams = 1
		e.create(a, "Only one", smmodel.TeamPublic)

		_, err := e.manager.Create(a, "Two", smmodel.TeamPublic)
		expectErr(t, "custom limit", err, smmodel.ErrTeamLimitReached)

		for i := 0; i < 3; i++ {
			e.create(mod, fmt.Sprintf("Mod %d", i), smmodel.TeamPublic)
		}

		e.options.MaxOwnedTeams = -1

		for i := 0; i < 10; i++ {
			e.create(a, fmt.Sprintf("Unlimited %d", i), smmodel.TeamPublic)
		}
	})
}

func TestCreate_Disabled(t *testing.T) {
	e := newTestEnv(t)
	user, mod := e.account(), e.moderator()

	e.options.DisableCreation = true

	_, err := e.manager.Create(user, "Nope", smmodel.TeamPublic)
	expectErr(t, "creation disabled", err, smmodel.ErrTeamCreationBlocked)

	e.create(mod, "Allowed", smmodel.TeamPublic)

	// existing teams keep working and can still be joined
	teams, _ := e.manager.List(user)

	if len(teams) != 1 {
		t.Fatalf("expected the team of the moderator, got %d", len(teams))
	}

	if _, err := e.manager.Join(user, teams[0].ID.String(), ""); err != nil {
		t.Errorf("joining must still work: %s", err)
	}

	// the option is read on every call
	e.options.DisableCreation = false
	e.create(user, "Now fine", smmodel.TeamPublic)
}

func TestVisibility(t *testing.T) {
	e := newTestEnv(t)
	owner, stranger, invited, mod := e.account(), e.account(), e.account(), e.moderator()

	public := e.create(owner, "Public Team", smmodel.TeamPublic)
	private := e.create(owner, "Private Team", smmodel.TeamPrivate)

	if _, err := e.manager.Invite(owner, private.ID.String(), invited.AccountID); err != nil {
		t.Fatal(err)
	}

	names := func(actor Actor) []string {
		teams, err := e.manager.List(actor)

		if err != nil {
			t.Fatal(err)
		}

		var out []string

		for _, team := range teams {
			out = append(out, team.Name)
		}

		return out
	}

	for _, c := range []struct {
		who  string
		by   Actor
		want int
	}{
		{"stranger", stranger, 1},
		{"anonymous", Actor{}, 1},
		{"owner", owner, 2},
		{"invited", invited, 2},
		{"moderator", mod, 2},
	} {
		if got := names(c.by); len(got) != c.want {
			t.Errorf("%s sees %v, want %d teams", c.who, got, c.want)
		}
	}

	if _, err := e.manager.Get(stranger, public.ID.String()); err != nil {
		t.Errorf("a stranger must see a public team: %s", err)
	}

	// a private team does not exist for a stranger (no hint it is there)
	_, err := e.manager.Get(stranger, private.ID.String())
	expectErr(t, "stranger gets a private team", err, smmodel.ErrTeamNotFound)

	for _, actor := range []Actor{owner, invited, mod} {
		if _, err := e.manager.Get(actor, private.ID.String()); err != nil {
			t.Errorf("expected access for %+v: %s", actor, err)
		}
	}

	_, err = e.manager.Get(owner, uuid.New().String())
	expectErr(t, "unknown id", err, smmodel.ErrTeamNotFound)
}

func TestVisibility_BecomingPrivateHidesIt(t *testing.T) {
	e := newTestEnv(t)
	owner, stranger := e.account(), e.account()

	team := e.create(owner, "Flip", smmodel.TeamPublic)
	member := e.joinNew(team, owner)

	if _, err := e.manager.SetVisibility(owner, team.ID.String(), smmodel.TeamPrivate); err != nil {
		t.Fatal(err)
	}

	if teams, _ := e.manager.List(stranger); len(teams) != 0 {
		t.Error("a team that became private must disappear for strangers")
	}

	if teams, _ := e.manager.List(member); len(teams) != 1 {
		t.Error("members keep seeing their team")
	}

	// existing members stay, new people need an invite
	_, err := e.manager.Join(stranger, team.ID.String(), "")
	expectErr(t, "join after becoming private", err, smmodel.ErrTeamInviteInvalid)

	if mine, _ := e.manager.MemberOf(member.AccountID); len(mine) != 1 {
		t.Error("member must stay in the team")
	}

	// and back to public
	if _, err := e.manager.SetVisibility(owner, team.ID.String(), smmodel.TeamPublic); err != nil {
		t.Fatal(err)
	}

	if _, err := e.manager.Join(stranger, team.ID.String(), ""); err != nil {
		t.Errorf("join after becoming public: %s", err)
	}
}

func TestSetVisibility_Permissions(t *testing.T) {
	e := newTestEnv(t)
	owner, stranger, mod := e.account(), e.account(), e.moderator()
	team := e.create(owner, "Perm", smmodel.TeamPublic)
	member := e.joinNew(team, owner)
	admin := e.joinNew(team, owner)

	if _, err := e.manager.SetAdmin(owner, team.ID.String(), admin.AccountID, true); err != nil {
		t.Fatal(err)
	}

	_, err := e.manager.SetVisibility(member, team.ID.String(), smmodel.TeamPrivate)
	expectErr(t, "normal member", err, smmodel.ErrTeamPermission)

	_, err = e.manager.SetVisibility(stranger, team.ID.String(), smmodel.TeamPrivate)
	expectErr(t, "stranger", err, smmodel.ErrTeamPermission)

	if _, err := e.manager.SetVisibility(admin, team.ID.String(), smmodel.TeamPrivate); err != nil {
		t.Errorf("admin: %s", err)
	}

	if _, err := e.manager.SetVisibility(mod, team.ID.String(), smmodel.TeamPublic); err != nil {
		t.Errorf("moderator: %s", err)
	}

	// a stranger of a private team does not learn that it exists
	if _, err := e.manager.SetVisibility(owner, team.ID.String(), smmodel.TeamPrivate); err != nil {
		t.Fatal(err)
	}

	_, err = e.manager.SetVisibility(stranger, team.ID.String(), smmodel.TeamPublic)
	expectErr(t, "stranger of a private team", err, smmodel.ErrTeamNotFound)
}

func TestJoin_Public_And_Multiple(t *testing.T) {
	e := newTestEnv(t)
	owner, user := e.account(), e.account()

	a := e.create(owner, "Alpha", smmodel.TeamPublic)
	b := e.create(owner, "Beta", smmodel.TeamPublic)

	for _, team := range []*smmodel.Team{a, b} {
		if _, err := e.manager.Join(user, team.ID.String(), ""); err != nil {
			t.Fatal(err)
		}
	}

	if mine, err := e.manager.MemberOf(user.AccountID); err != nil || len(mine) != 2 {
		t.Errorf("an account can be in several teams: %v %d", err, len(mine))
	}

	_, err := e.manager.Join(user, a.ID.String(), "")
	expectErr(t, "joining twice", err, smmodel.ErrAlreadyTeamMember)

	if len(e.reload(a).Members) != 2 {
		t.Error("a failed join must not add a member")
	}
}

func TestJoin_Rejections(t *testing.T) {
	e := newTestEnv(t)
	owner, user := e.account(), e.account()
	team := e.create(owner, "Closed", smmodel.TeamPrivate)
	pub := e.create(owner, "Open", smmodel.TeamPublic)

	_, err := e.manager.Join(Actor{}, pub.ID.String(), "")
	expectErr(t, "anonymous", err, smmodel.ErrTeamPermission)

	_, err = e.manager.Join(user, team.ID.String(), "")
	expectErr(t, "private without invite", err, smmodel.ErrTeamInviteInvalid)

	_, err = e.manager.Join(user, team.ID.String(), "guess")
	expectErr(t, "private with a wrong token", err, smmodel.ErrTeamInviteInvalid)

	_, err = e.manager.Join(user, uuid.New().String(), "")
	expectErr(t, "unknown team", err, smmodel.ErrTeamNotFound)

	if err := e.manager.Delete(owner, pub.ID.String()); err != nil {
		t.Fatal(err)
	}

	_, err = e.manager.Join(user, pub.ID.String(), "")
	expectErr(t, "deleted team", err, smmodel.ErrTeamNotFound)

	gone := e.account()

	if err := e.store.DeleteAccount(gone.AccountID.String()); err != nil {
		t.Fatal(err)
	}

	open := e.create(owner, "Open 2", smmodel.TeamPublic)

	_, err = e.manager.Join(gone, open.ID.String(), "")
	expectErr(t, "deleted account", err, smmodel.ErrTeamPermission)
}

func TestInvites_LinkToken(t *testing.T) {
	e := newTestEnv(t)
	owner, a, b := e.account(), e.account(), e.account()
	team := e.create(owner, "Link Team", smmodel.TeamPrivate)

	invite, err := e.manager.Invite(owner, team.ID.String(), uuid.Nil)

	if err != nil || invite.Token == "" {
		t.Fatalf("invite: %v %+v", err, invite)
	}

	_, err = e.manager.JoinByToken(a, "wrong")
	expectErr(t, "unknown token", err, smmodel.ErrTeamInviteInvalid)

	_, err = e.manager.JoinByToken(a, "")
	expectErr(t, "empty token", err, smmodel.ErrTeamInviteInvalid)

	joined, err := e.manager.JoinByToken(a, invite.Token)

	if err != nil || joined.ID != team.ID || !joined.IsMember(a.AccountID) {
		t.Fatalf("join by token: %v %+v", err, joined)
	}

	// single use
	_, err = e.manager.JoinByToken(b, invite.Token)
	expectErr(t, "token used twice", err, smmodel.ErrTeamInviteInvalid)

	// the token also works through Join with the team id
	second, _ := e.manager.Invite(owner, team.ID.String(), uuid.Nil)

	if _, err := e.manager.Join(b, team.ID.String(), second.Token); err != nil {
		t.Errorf("join with team id and token: %s", err)
	}

	// already a member: the invite is not used up
	third, _ := e.manager.Invite(owner, team.ID.String(), uuid.Nil)
	_, err = e.manager.JoinByToken(a, third.Token)
	expectErr(t, "member using an invite", err, smmodel.ErrAlreadyTeamMember)

	if len(e.reload(team).Invites) != 1 {
		t.Error("the invite must stay when it was not used")
	}
}

func TestInvites_Targeted(t *testing.T) {
	e := newTestEnv(t)
	owner, target, other := e.account(), e.account(), e.account()
	team := e.create(owner, "Targeted", smmodel.TeamPrivate)

	invite, err := e.manager.Invite(owner, team.ID.String(), target.AccountID)

	if err != nil {
		t.Fatal(err)
	}

	invited, err := e.manager.InvitedTo(target.AccountID)

	if err != nil || len(invited) != 1 || invited[0].ID != team.ID {
		t.Fatalf("InvitedTo: %v %v", err, invited)
	}

	if other, _ := e.manager.InvitedTo(other.AccountID); len(other) != 0 {
		t.Error("only the target sees the invite")
	}

	// another account cannot use the token of a targeted invite
	_, err = e.manager.JoinByToken(other, invite.Token)
	expectErr(t, "foreign use", err, smmodel.ErrTeamInviteInvalid)

	if _, err := e.manager.Join(target, team.ID.String(), ""); err != nil {
		t.Fatalf("accepting: %s", err)
	}

	if invited, _ := e.manager.InvitedTo(target.AccountID); len(invited) != 0 {
		t.Error("a team that was joined is no longer an open invitation")
	}
}

func TestInvites_UnknownOrDeletedTarget(t *testing.T) {
	e := newTestEnv(t)
	owner, gone := e.account(), e.account()
	team := e.create(owner, "Targets", smmodel.TeamPrivate)

	if _, err := e.manager.Invite(owner, team.ID.String(), uuid.New()); err == nil {
		t.Error("inviting an unknown account must fail")
	}

	if err := e.store.DeleteAccount(gone.AccountID.String()); err != nil {
		t.Fatal(err)
	}

	if _, err := e.manager.Invite(owner, team.ID.String(), gone.AccountID); err == nil {
		t.Error("inviting a deleted account must fail")
	}

	if len(e.reload(team).Invites) != 0 {
		t.Error("a failed invite must not be stored")
	}

	member := e.joinNew(team, owner)
	_, err := e.manager.Invite(owner, team.ID.String(), member.AccountID)
	expectErr(t, "inviting a member", err, smmodel.ErrAlreadyTeamMember)
}

func TestInvites_Expiry(t *testing.T) {
	e := newTestEnv(t)
	owner, user, target := e.account(), e.account(), e.account()
	team := e.create(owner, "Expiry", smmodel.TeamPrivate)

	link, _ := e.manager.Invite(owner, team.ID.String(), uuid.Nil)
	_, _ = e.manager.Invite(owner, team.ID.String(), target.AccountID)

	e.clock = e.clock.Add(DefaultInviteTTL + time.Second)

	_, err := e.manager.JoinByToken(user, link.Token)
	expectErr(t, "expired link", err, smmodel.ErrTeamInviteInvalid)

	_, err = e.manager.Join(target, team.ID.String(), "")
	expectErr(t, "expired targeted invite", err, smmodel.ErrTeamInviteInvalid)

	if invited, _ := e.manager.InvitedTo(target.AccountID); len(invited) != 0 {
		t.Error("an expired invite is not shown")
	}

	if _, err := e.manager.Get(target, team.ID.String()); err != smmodel.ErrTeamNotFound {
		t.Errorf("an expired invite must not reveal the private team, got %v", err)
	}
}

func TestInvites_Permissions_Revoke_Decline(t *testing.T) {
	e := newTestEnv(t)
	owner, stranger, target := e.account(), e.account(), e.account()
	team := e.create(owner, "Rights", smmodel.TeamPrivate)
	member := e.joinNew(team, owner)

	_, err := e.manager.Invite(member, team.ID.String(), uuid.Nil)
	expectErr(t, "normal member invites", err, smmodel.ErrTeamPermission)

	_, err = e.manager.Invite(stranger, team.ID.String(), uuid.Nil)
	expectErr(t, "stranger invites into private team", err, smmodel.ErrTeamNotFound)

	invite, _ := e.manager.Invite(owner, team.ID.String(), uuid.Nil)

	expectErr(t, "member revokes", e.manager.RevokeInvite(member, team.ID.String(), invite.Token), smmodel.ErrTeamPermission)
	expectErr(t, "unknown token", e.manager.RevokeInvite(owner, team.ID.String(), "nope"), smmodel.ErrTeamInviteInvalid)

	if err := e.manager.RevokeInvite(owner, team.ID.String(), invite.Token); err != nil {
		t.Fatal(err)
	}

	_, err = e.manager.JoinByToken(stranger, invite.Token)
	expectErr(t, "revoked invite", err, smmodel.ErrTeamInviteInvalid)

	// decline
	_, _ = e.manager.Invite(owner, team.ID.String(), target.AccountID)

	expectErr(t, "decline without invite", e.manager.DeclineInvite(stranger, team.ID.String()), smmodel.ErrTeamInviteInvalid)
	expectErr(t, "anonymous decline", e.manager.DeclineInvite(Actor{}, team.ID.String()), smmodel.ErrTeamInviteInvalid)

	if err := e.manager.DeclineInvite(target, team.ID.String()); err != nil {
		t.Fatal(err)
	}

	_, err = e.manager.Join(target, team.ID.String(), "")
	expectErr(t, "join after declining", err, smmodel.ErrTeamInviteInvalid)
}

func TestLeave(t *testing.T) {
	e := newTestEnv(t)
	owner, stranger := e.account(), e.account()
	team := e.create(owner, "Leavers", smmodel.TeamPublic)
	member := e.joinNew(team, owner)

	expectErr(t, "stranger leaves", e.manager.Leave(stranger, team.ID.String()), smmodel.ErrNotTeamMember)
	expectErr(t, "unknown team", e.manager.Leave(member, uuid.New().String()), smmodel.ErrTeamNotFound)

	if err := e.manager.Leave(member, team.ID.String()); err != nil {
		t.Fatal(err)
	}

	if e.reload(team).IsMember(member.AccountID) {
		t.Error("member must be gone")
	}

	expectErr(t, "leaving twice", e.manager.Leave(member, team.ID.String()), smmodel.ErrNotTeamMember)

	// leaving does not delete the team and it can be joined again
	if _, err := e.manager.Join(member, team.ID.String(), ""); err != nil {
		t.Errorf("rejoin: %s", err)
	}
}

func TestLeave_OwnerSuccession(t *testing.T) {
	e := newTestEnv(t)
	owner := e.account()
	team := e.create(owner, "Succession", smmodel.TeamPublic)
	early := e.joinNew(team, owner)
	late := e.joinNew(team, owner)

	if err := e.manager.Leave(owner, team.ID.String()); err != nil {
		t.Fatal(err)
	}

	stored := e.reload(team)

	if !stored.IsOwner(early.AccountID) || stored.IsOwner(late.AccountID) || stored.IsDeleted() {
		t.Errorf("the longest member must be the new owner: %+v", stored.Members)
	}

	// the new owner has the owner rights: they can delete the team
	if err := e.manager.Delete(early, team.ID.String()); err != nil {
		t.Errorf("new owner must be able to delete: %s", err)
	}
}

func TestLeave_LastMemberDeletesTeam(t *testing.T) {
	e := newTestEnv(t)
	owner, other := e.account(), e.account()
	team := e.create(owner, "Solo", smmodel.TeamPrivate)

	if err := e.manager.Leave(owner, team.ID.String()); err != nil {
		t.Fatal(err)
	}

	if teams, _ := e.store.ListTeams(); len(teams) != 0 {
		t.Error("the team must be deleted")
	}

	// registrations can still resolve it
	if found, err := e.manager.Lookup(team.ID.String()); err != nil || !found.IsDeleted() || found.Name != "Solo" {
		t.Errorf("lookup of the deleted team: %v %+v", err, found)
	}

	_, err := e.manager.Get(owner, team.ID.String())
	expectErr(t, "get of a deleted team", err, smmodel.ErrTeamNotFound)

	// the name is free again
	e.create(other, "Solo", smmodel.TeamPublic)
}

func TestKick(t *testing.T) {
	e := newTestEnv(t)
	owner := e.account()
	team := e.create(owner, "Kicks", smmodel.TeamPublic)
	admin1, admin2 := e.joinNew(team, owner), e.joinNew(team, owner)
	m1, m2, m3 := e.joinNew(team, owner), e.joinNew(team, owner), e.joinNew(team, owner)
	stranger := e.account()
	id := team.ID.String()

	for _, a := range []Actor{admin1, admin2} {
		if _, err := e.manager.SetAdmin(owner, id, a.AccountID, true); err != nil {
			t.Fatal(err)
		}
	}

	expectErr(t, "member kicks", e.manager.Kick(m1, id, m2.AccountID), smmodel.ErrTeamPermission)
	expectErr(t, "stranger kicks", e.manager.Kick(stranger, id, m2.AccountID), smmodel.ErrTeamPermission)
	expectErr(t, "admin kicks admin", e.manager.Kick(admin1, id, admin2.AccountID), smmodel.ErrTeamPermission)
	expectErr(t, "admin kicks owner", e.manager.Kick(admin1, id, owner.AccountID), smmodel.ErrTeamPermission)
	expectErr(t, "kick a non-member", e.manager.Kick(owner, id, stranger.AccountID), smmodel.ErrNotTeamMember)

	if err := e.manager.Kick(admin1, id, m1.AccountID); err != nil {
		t.Errorf("admin kicks member: %s", err)
	}

	if err := e.manager.Kick(owner, id, admin2.AccountID); err != nil {
		t.Errorf("owner kicks admin: %s", err)
	}

	// admins and members can "kick" themselves, which is leaving
	if err := e.manager.Kick(admin1, id, admin1.AccountID); err != nil {
		t.Errorf("admin kicks self: %s", err)
	}

	stored := e.reload(team)

	if stored.IsMember(m1.AccountID) || stored.IsMember(admin2.AccountID) || stored.IsMember(admin1.AccountID) {
		t.Errorf("kicked members are still in the team: %+v", stored.Members)
	}

	if !stored.IsMember(m2.AccountID) || !stored.IsMember(m3.AccountID) || !stored.IsOwner(owner.AccountID) {
		t.Errorf("the others must stay: %+v", stored.Members)
	}

	// a kicked member can rejoin a public team
	if _, err := e.manager.Join(m1, id, ""); err != nil {
		t.Errorf("rejoin after kick: %s", err)
	}
}

func TestKick_Moderator(t *testing.T) {
	e := newTestEnv(t)
	owner, mod := e.account(), e.moderator()
	team := e.create(owner, "Mod Kick", smmodel.TeamPrivate)
	member := e.joinNew(team, owner)
	id := team.ID.String()

	// a moderator who is not a member can remove the owner, the ownership moves on
	if err := e.manager.Kick(mod, id, owner.AccountID); err != nil {
		t.Fatal(err)
	}

	stored := e.reload(team)

	if stored.IsMember(owner.AccountID) || !stored.IsOwner(member.AccountID) {
		t.Errorf("unexpected team after the moderator removed the owner: %+v", stored.Members)
	}
}

func TestSetAdmin_And_Transfer(t *testing.T) {
	e := newTestEnv(t)
	owner, mod, stranger := e.account(), e.moderator(), e.account()
	team := e.create(owner, "Roles", smmodel.TeamPublic)
	m1, m2 := e.joinNew(team, owner), e.joinNew(team, owner)
	id := team.ID.String()

	_, err := e.manager.SetAdmin(m1, id, m2.AccountID, true)
	expectErr(t, "member promotes", err, smmodel.ErrTeamPermission)

	if _, err := e.manager.SetAdmin(owner, id, m1.AccountID, true); err != nil {
		t.Fatal(err)
	}

	_, err = e.manager.SetAdmin(m1, id, m2.AccountID, true)
	expectErr(t, "admin promotes (only the owner may)", err, smmodel.ErrTeamPermission)

	_, err = e.manager.SetAdmin(owner, id, owner.AccountID, false)
	expectErr(t, "demote the owner", err, smmodel.ErrTeamOwnerRole)

	_, err = e.manager.SetAdmin(owner, id, stranger.AccountID, true)
	expectErr(t, "promote a stranger", err, smmodel.ErrNotTeamMember)

	if _, err := e.manager.SetAdmin(mod, id, m1.AccountID, false); err != nil {
		t.Errorf("moderator demotes: %s", err)
	}

	// transfer
	_, err = e.manager.TransferOwnership(m1, id, m2.AccountID)
	expectErr(t, "member transfers", err, smmodel.ErrTeamPermission)

	_, err = e.manager.TransferOwnership(owner, id, stranger.AccountID)
	expectErr(t, "transfer to a stranger", err, smmodel.ErrNotTeamMember)

	_, err = e.manager.TransferOwnership(owner, id, owner.AccountID)
	expectErr(t, "transfer to self", err, smmodel.ErrTeamTransferToSelf)

	got, err := e.manager.TransferOwnership(owner, id, m1.AccountID)

	if err != nil || !got.IsOwner(m1.AccountID) || got.Member(owner.AccountID).Role != smmodel.TeamRoleAdmin {
		t.Fatalf("transfer: %v %+v", err, got)
	}

	// the former owner is an admin now and cannot transfer anymore
	_, err = e.manager.TransferOwnership(owner, id, m2.AccountID)
	expectErr(t, "former owner transfers", err, smmodel.ErrTeamPermission)

	// a moderator can hand the team over, even without being a member
	got, err = e.manager.TransferOwnership(mod, id, m2.AccountID)

	if err != nil || !got.IsOwner(m2.AccountID) {
		t.Errorf("moderator transfer: %v %+v", err, got)
	}
}

func TestRename(t *testing.T) {
	e := newTestEnv(t)
	owner, stranger, mod := e.account(), e.account(), e.moderator()
	team := e.create(owner, "Old Name", smmodel.TeamPublic)
	other := e.create(owner, "Other", smmodel.TeamPublic)
	member := e.joinNew(team, owner)
	id := team.ID.String()

	_, err := e.manager.Rename(member, id, "Hacked")
	expectErr(t, "member renames", err, smmodel.ErrTeamPermission)

	_, err = e.manager.Rename(stranger, id, "Hacked")
	expectErr(t, "stranger renames", err, smmodel.ErrTeamPermission)

	_, err = e.manager.Rename(owner, id, " other ")
	expectErr(t, "name of another team", err, smmodel.ErrTeamNameTaken)

	_, err = e.manager.Rename(owner, id, "x")
	expectErr(t, "invalid name", err, smmodel.ErrTeamNameInvalid)

	if e.reload(team).Name != "Old Name" {
		t.Error("a failed rename must not change the team")
	}

	got, err := e.manager.Rename(owner, id, "  New   Name ")

	if err != nil || got.Name != "New Name" || e.reload(team).Name != "New Name" {
		t.Fatalf("rename: %v %+v", err, got)
	}

	// changing only the case of the own name is not a conflict with itself
	if _, err := e.manager.Rename(owner, id, "NEW NAME"); err != nil {
		t.Errorf("case change of the own name: %s", err)
	}

	// the old name can be taken by another team now
	if _, err := e.manager.Rename(owner, other.ID.String(), "Old Name"); err != nil {
		t.Errorf("reusing the old name: %s", err)
	}

	if _, err := e.manager.Rename(mod, id, "By Moderator"); err != nil {
		t.Errorf("moderator renames: %s", err)
	}
}

func TestRename_Hook(t *testing.T) {
	e := newTestEnv(t)
	owner := e.account()
	team := e.create(owner, "Hooked", smmodel.TeamPublic)
	id := team.ID.String()

	var calls []string

	e.manager.SetRenameHook(func(team *smmodel.Team) error {
		calls = append(calls, team.Name)

		return nil
	})

	if _, err := e.manager.Rename(owner, id, "Hooked 2"); err != nil {
		t.Fatal(err)
	}

	if _, err := e.manager.Rename(owner, id, "Hooked 2"); err != nil {
		t.Fatal(err)
	}

	if len(calls) != 1 || calls[0] != "Hooked 2" {
		t.Errorf("the hook runs once per real change, got %v", calls)
	}

	// a failing hook is reported, but the rename stays
	hookErr := errors.New("boom")

	e.manager.SetRenameHook(func(*smmodel.Team) error { return hookErr })

	got, err := e.manager.Rename(owner, id, "Hooked 3")

	var renameErr ErrRenameHook

	if !errors.As(err, &renameErr) || renameErr.Err != hookErr {
		t.Fatalf("expected ErrRenameHook, got %v", err)
	}

	if got == nil || got.Name != "Hooked 3" || e.reload(team).Name != "Hooked 3" {
		t.Error("the rename must be kept when the hook fails")
	}

	// a rename that failed does not call the hook
	calls = nil
	e.manager.SetRenameHook(func(team *smmodel.Team) error { calls = append(calls, team.Name); return nil })

	_, _ = e.manager.Rename(owner, id, "x")

	if len(calls) != 0 {
		t.Errorf("hook called for a failed rename: %v", calls)
	}
}

func TestDelete(t *testing.T) {
	e := newTestEnv(t)
	owner, mod, stranger := e.account(), e.moderator(), e.account()
	team := e.create(owner, "Doomed", smmodel.TeamPublic)
	admin := e.joinNew(team, owner)
	member := e.joinNew(team, owner)
	id := team.ID.String()

	if _, err := e.manager.SetAdmin(owner, id, admin.AccountID, true); err != nil {
		t.Fatal(err)
	}

	expectErr(t, "member deletes", e.manager.Delete(member, id), smmodel.ErrTeamPermission)
	expectErr(t, "admin deletes", e.manager.Delete(admin, id), smmodel.ErrTeamPermission)
	expectErr(t, "stranger deletes", e.manager.Delete(stranger, id), smmodel.ErrTeamPermission)

	if err := e.manager.Delete(owner, id); err != nil {
		t.Fatal(err)
	}

	expectErr(t, "delete twice", e.manager.Delete(owner, id), smmodel.ErrTeamNotFound)
	expectErr(t, "rename deleted", func() error { _, err := e.manager.Rename(owner, id, "Zombie"); return err }(), smmodel.ErrTeamNotFound)

	if mine, _ := e.manager.MemberOf(member.AccountID); len(mine) != 0 {
		t.Error("a deleted team is not in the teams of its former members")
	}

	// a moderator can delete a team of somebody else
	other := e.create(owner, "Doomed 2", smmodel.TeamPrivate)

	if err := e.manager.Delete(mod, other.ID.String()); err != nil {
		t.Errorf("moderator delete: %s", err)
	}
}

func TestRemoveAccount(t *testing.T) {
	e := newTestEnv(t)
	leaving, other, third := e.account(), e.account(), e.account()

	owned := e.create(leaving, "Owned", smmodel.TeamPublic)
	joined := e.create(other, "Joined", smmodel.TeamPublic)
	solo := e.create(leaving, "Solo Team", smmodel.TeamPrivate)
	invitedTo := e.create(other, "Invited", smmodel.TeamPrivate)
	untouched := e.create(other, "Untouched", smmodel.TeamPublic)

	if _, err := e.manager.Join(third, owned.ID.String(), ""); err != nil {
		t.Fatal(err)
	}

	if _, err := e.manager.Join(leaving, joined.ID.String(), ""); err != nil {
		t.Fatal(err)
	}

	if _, err := e.manager.Invite(other, invitedTo.ID.String(), leaving.AccountID); err != nil {
		t.Fatal(err)
	}

	if err := e.manager.RemoveAccount(leaving.AccountID); err != nil {
		t.Fatal(err)
	}

	if o := e.reload(owned); o.IsMember(leaving.AccountID) || !o.IsOwner(third.AccountID) || o.IsDeleted() {
		t.Errorf("ownership must move on: %+v", o.Members)
	}

	if j := e.reload(joined); j.IsMember(leaving.AccountID) || !j.IsOwner(other.AccountID) {
		t.Errorf("must be removed from joined teams: %+v", j.Members)
	}

	if !e.reload(solo).IsDeleted() {
		t.Error("a team whose only member was removed must be deleted")
	}

	if len(e.reload(invitedTo).Invites) != 0 {
		t.Error("invites addressed to the account must be withdrawn")
	}

	if len(e.reload(untouched).Members) != 1 {
		t.Error("other teams must stay as they are")
	}

	// no-op for an unknown account
	if err := e.manager.RemoveAccount(uuid.New()); err != nil {
		t.Error(err)
	}
}

func TestConcurrentJoinsAreNotLost(t *testing.T) {
	e := newTestEnv(t)
	owner := e.account()
	team := e.create(owner, "Crowd", smmodel.TeamPublic)

	const n = 20

	users := make([]Actor, n)

	for i := range users {
		users[i] = e.account()
	}

	var wg sync.WaitGroup

	for _, u := range users {
		wg.Add(1)

		go func(u Actor) {
			defer wg.Done()

			if _, err := e.manager.Join(u, team.ID.String(), ""); err != nil {
				t.Error(err)
			}
		}(u)
	}

	wg.Wait()

	if got := len(e.reload(team).Members); got != n+1 {
		t.Errorf("expected %d members, got %d (lost updates)", n+1, got)
	}
}

func TestConcurrentCreateKeepsNamesUnique(t *testing.T) {
	e := newTestEnv(t)

	const n = 10

	users := make([]Actor, n)

	for i := range users {
		users[i] = e.account()
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		success int
	)

	for _, u := range users {
		wg.Add(1)

		go func(u Actor) {
			defer wg.Done()

			if _, err := e.manager.Create(u, "Highlander", smmodel.TeamPublic); err == nil {
				mu.Lock()
				success++
				mu.Unlock()
			} else if err != smmodel.ErrTeamNameTaken {
				t.Error(err)
			}
		}(u)
	}

	wg.Wait()

	if success != 1 {
		t.Errorf("exactly one team may get the name, %d did", success)
	}
}
