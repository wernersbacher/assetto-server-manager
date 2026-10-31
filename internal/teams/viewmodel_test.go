package teams

import (
	"testing"
	"time"

	"github.com/google/uuid"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

func memberByName(vars *viewTemplateVars, name string) *memberView {
	for _, member := range vars.Members {
		if member.Name == name {
			return member
		}
	}

	return nil
}

// teamWithRoles creates a public team with an owner, two admins and two members. The accounts are named by their role.
func (e *testEnv) teamWithRoles() (team *smmodel.Team, actors map[string]Actor) {
	e.t.Helper()

	actors = make(map[string]Actor)

	name := func(role string, actor Actor) Actor {
		// the driver name is what the pages show, the account name is the key of the account in the store
		account := e.accounts[actor.AccountID]
		account.DriverName = role

		if err := e.store.UpsertAccount(account); err != nil {
			e.t.Fatal(err)
		}

		actors[role] = actor

		return actor
	}

	owner := name("owner", e.account())
	team = e.create(owner, "View Team", smmodel.TeamPublic)

	for _, role := range []string{"admin1", "admin2", "member1", "member2"} {
		e.clock = e.clock.Add(time.Minute)

		actor := name(role, e.account())

		if _, err := e.manager.Join(actor, team.ID.String(), ""); err != nil {
			e.t.Fatal(err)
		}

		if role[:5] == "admin" {
			if _, err := e.manager.SetAdmin(owner, team.ID.String(), actor.AccountID, true); err != nil {
				e.t.Fatal(err)
			}
		}
	}

	actors["stranger"] = name("stranger", e.account())

	mod := e.moderator()
	actors["moderator"] = name("moderator", mod)
	actors["moderator"] = Actor{AccountID: mod.AccountID, Moderator: true}

	return e.reload(team), actors
}

func TestBuildView_Flags(t *testing.T) {
	e := newTestEnv(t)
	h := e.handler()
	team, actors := e.teamWithRoles()

	view := func(who string) *viewTemplateVars {
		vars, err := h.buildView(actors[who], true, team)

		if err != nil {
			t.Fatal(err)
		}

		return vars
	}

	type flags struct{ kick, admin, transfer bool }

	for _, c := range []struct {
		viewer string
		want   map[string]flags
	}{
		{"owner", map[string]flags{
			"owner": {false, false, false}, "admin1": {true, true, true}, "admin2": {true, true, true},
			"member1": {true, true, true}, "member2": {true, true, true},
		}},
		// an admin removes members, not other admins or the owner, and cannot promote or hand over
		{"admin1", map[string]flags{
			"owner": {false, false, false}, "admin1": {false, false, false}, "admin2": {false, false, false},
			"member1": {true, false, false}, "member2": {true, false, false},
		}},
		{"member1", map[string]flags{
			"owner": {false, false, false}, "admin1": {false, false, false}, "member1": {false, false, false}, "member2": {false, false, false},
		}},
		{"stranger", map[string]flags{
			"owner": {false, false, false}, "member1": {false, false, false},
		}},
		// a moderator may remove everybody (the owner too), promote and hand over, but not touch the owner's role
		{"moderator", map[string]flags{
			"owner": {true, false, false}, "admin1": {true, true, true}, "member1": {true, true, true},
		}},
	} {
		vars := view(c.viewer)

		for name, want := range c.want {
			member := memberByName(vars, name)

			if member == nil {
				t.Fatalf("%s: member %s missing", c.viewer, name)
			}

			got := flags{member.CanKick, member.CanSetAdmin, member.CanTransfer}

			if got != want {
				t.Errorf("%s looking at %s: got %+v, want %+v", c.viewer, name, got, want)
			}
		}
	}
}

func TestBuildView_ViewerState(t *testing.T) {
	e := newTestEnv(t)
	h := e.handler()
	team, actors := e.teamWithRoles()

	for _, c := range []struct {
		who                                           string
		loggedIn                                      bool
		member, owner, manage, moderator, invited, ok bool
	}{
		{"owner", true, true, true, true, false, false, false},
		{"admin1", true, true, false, true, false, false, false},
		{"member1", true, true, false, false, false, false, false},
		{"stranger", true, false, false, false, false, false, true},
		{"moderator", true, false, false, true, true, false, true},
	} {
		vars, err := h.buildView(actors[c.who], c.loggedIn, team)

		if err != nil {
			t.Fatal(err)
		}

		if vars.IsMember != c.member || vars.IsOwner != c.owner || vars.CanManage != c.manage || vars.IsModerator != c.moderator || vars.Invited != c.invited || vars.CanJoin != c.ok {
			t.Errorf("%s: member=%v owner=%v manage=%v moderator=%v invited=%v canJoin=%v", c.who, vars.IsMember, vars.IsOwner, vars.CanManage, vars.IsModerator, vars.Invited, vars.CanJoin)
		}
	}

	// anonymous visitors can look but not join
	vars, err := h.buildView(Actor{}, false, team)

	if err != nil || vars.CanJoin || vars.IsMember || vars.CanManage || len(vars.Invites) != 0 {
		t.Errorf("anonymous: %v %+v", err, vars)
	}
}

func TestBuildView_MembersAreSortedByRole(t *testing.T) {
	e := newTestEnv(t)
	h := e.handler()
	team, actors := e.teamWithRoles()

	vars, err := h.buildView(actors["owner"], true, team)

	if err != nil {
		t.Fatal(err)
	}

	var roles []smmodel.TeamRole

	for _, member := range vars.Members {
		roles = append(roles, member.Role)
	}

	want := []smmodel.TeamRole{smmodel.TeamRoleOwner, smmodel.TeamRoleAdmin, smmodel.TeamRoleAdmin, smmodel.TeamRoleMember, smmodel.TeamRoleMember}

	if len(roles) != len(want) {
		t.Fatalf("got %v", roles)
	}

	for i := range want {
		if roles[i] != want[i] {
			t.Fatalf("expected owner, admins, members, got %v", roles)
		}
	}

	// the order inside of a role is the order they joined in
	if vars.Members[1].Name != "admin1" || vars.Members[3].Name != "member1" {
		t.Errorf("the join order must be kept inside of a role: %s %s", vars.Members[1].Name, vars.Members[3].Name)
	}

	if !vars.Members[0].IsYou || vars.Members[1].IsYou {
		t.Error("only the viewer is marked as 'you'")
	}
}

func TestBuildView_PrivateTeamAndInvites(t *testing.T) {
	e := newTestEnv(t)
	h := e.handler()
	owner, invited, stranger := e.account(), e.account(), e.account()

	team := e.create(owner, "Private View", smmodel.TeamPrivate)

	if _, err := e.manager.Invite(owner, team.ID.String(), invited.AccountID); err != nil {
		t.Fatal(err)
	}

	link, err := e.manager.Invite(owner, team.ID.String(), uuid.Nil)

	if err != nil {
		t.Fatal(err)
	}

	team = e.reload(team)

	ownerView, err := h.buildView(owner, true, team)

	if err != nil {
		t.Fatal(err)
	}

	if len(ownerView.Invites) != 2 {
		t.Fatalf("the owner sees both invites: %+v", ownerView.Invites)
	}

	var targeted, linked *inviteView

	for _, invite := range ownerView.Invites {
		if invite.Target != "" {
			targeted = invite
		} else {
			linked = invite
		}
	}

	if targeted == nil || targeted.Path != "" || targeted.Target != e.accounts[invited.AccountID].Name {
		t.Errorf("a targeted invite shows who it is for and has no link: %+v", targeted)
	}

	if linked == nil || linked.Path != "/teams/invite/"+link.Token || linked.Token != link.Token {
		t.Errorf("a link invite shows its link: %+v", linked)
	}

	// the tokens are secret: only people who manage the team see them
	for who, actor := range map[string]Actor{"invited": invited, "stranger": stranger} {
		vars, err := h.buildView(actor, true, team)

		if err != nil {
			t.Fatal(err)
		}

		if len(vars.Invites) != 0 {
			t.Errorf("%s must not see the invites: %+v", who, vars.Invites)
		}
	}

	invitedView, _ := h.buildView(invited, true, team)
	strangerView, _ := h.buildView(stranger, true, team)

	if !invitedView.Invited || !invitedView.CanJoin {
		t.Errorf("an invited account can join a private team: %+v", invitedView)
	}

	if strangerView.Invited || strangerView.CanJoin {
		t.Errorf("a stranger cannot join a private team: %+v", strangerView)
	}

	// expired invites disappear from the page
	e.clock = e.clock.Add(DefaultInviteTTL + time.Hour)

	expired, err := h.buildView(owner, true, team)

	if err != nil || len(expired.Invites) != 0 {
		t.Errorf("expired invites must not be listed: %v %+v", err, expired.Invites)
	}

	if vars, _ := h.buildView(invited, true, team); vars.Invited || vars.CanJoin {
		t.Error("an expired invite does not let the account join")
	}
}

func TestBuildView_UsesDriverNames(t *testing.T) {
	e := newTestEnv(t)
	h := e.handler()
	owner := e.account()
	team := e.create(owner, "Names", smmodel.TeamPublic)

	account := e.accounts[owner.AccountID]
	account.DriverName = "Fast Fred"

	if err := e.store.UpsertAccount(account); err != nil {
		t.Fatal(err)
	}

	vars, err := h.buildView(owner, true, team)

	if err != nil || len(vars.Members) != 1 || vars.Members[0].Name != "Fast Fred" {
		t.Errorf("the driver name is shown, not the login: %v %+v", err, vars.Members)
	}
}

func TestBuildList(t *testing.T) {
	e := newTestEnv(t)
	h := e.handler()

	owner, member, invited, stranger := e.account(), e.account(), e.account(), e.account()
	mod := e.moderator()

	public := e.create(owner, "Public", smmodel.TeamPublic)
	private := e.create(owner, "Private", smmodel.TeamPrivate)
	e.create(stranger, "Strangers", smmodel.TeamPublic)

	if _, err := e.manager.Join(member, public.ID.String(), ""); err != nil {
		t.Fatal(err)
	}

	if _, err := e.manager.Invite(owner, private.ID.String(), invited.AccountID); err != nil {
		t.Fatal(err)
	}

	names := func(summaries []*teamSummary) []string {
		var out []string

		for _, s := range summaries {
			out = append(out, s.Team.Name)
		}

		return out
	}

	for _, c := range []struct {
		who                  string
		actor                Actor
		loggedIn             bool
		mine, invites, other []string
	}{
		{"owner", owner, true, []string{"Private", "Public"}, nil, []string{"Strangers"}},
		{"member", member, true, []string{"Public"}, nil, []string{"Strangers"}},
		{"invited", invited, true, nil, []string{"Private"}, []string{"Public", "Strangers"}},
		{"stranger", stranger, true, []string{"Strangers"}, nil, []string{"Public"}},
		{"anonymous", Actor{}, false, nil, nil, []string{"Public", "Strangers"}},
		{"moderator", mod, true, nil, nil, []string{"Private", "Public", "Strangers"}},
	} {
		vars, err := h.buildList(c.actor, c.loggedIn)

		if err != nil {
			t.Fatal(err)
		}

		for what, pair := range map[string][2][]string{
			"mine": {names(vars.MyTeams), c.mine}, "invitations": {names(vars.Invitations), c.invites}, "other": {names(vars.OtherTeams), c.other},
		} {
			got, want := pair[0], pair[1]

			if len(got) != len(want) {
				t.Errorf("%s / %s: got %v, want %v", c.who, what, got, want)
				continue
			}

			for i := range want {
				if got[i] != want[i] {
					t.Errorf("%s / %s: got %v, want %v", c.who, what, got, want)
					break
				}
			}
		}
	}

	// the summary has the role of the viewer, the member count and the owner
	vars, _ := h.buildList(member, true)

	if len(vars.MyTeams) != 1 || vars.MyTeams[0].Role != smmodel.TeamRoleMember || vars.MyTeams[0].Members != 2 || vars.MyTeams[0].Owner != e.accounts[owner.AccountID].Name {
		t.Errorf("unexpected summary: %+v", vars.MyTeams)
	}
}

func TestBuildList_CanCreate(t *testing.T) {
	e := newTestEnv(t)
	h := e.handler()
	user, mod := e.account(), e.moderator()

	can := func(actor Actor, loggedIn bool) bool {
		vars, err := h.buildList(actor, loggedIn)

		if err != nil {
			t.Fatal(err)
		}

		return vars.CanCreate
	}

	if !can(user, true) || !can(mod, true) {
		t.Error("everybody who is logged in can create teams by default")
	}

	if can(Actor{}, false) {
		t.Error("visitors who are not logged in cannot create teams")
	}

	e.options.DisableCreation = true

	if can(user, true) {
		t.Error("creation is turned off for normal accounts")
	}

	if !can(mod, true) {
		t.Error("moderators can still create teams")
	}
}
