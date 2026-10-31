package model

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

var teamNow = time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)

func newTestTeam(t *testing.T, visibility TeamVisibility) (*Team, uuid.UUID) {
	t.Helper()

	owner := uuid.New()
	team, err := NewTeam("  Team   Rocket ", visibility, owner, teamNow)

	if err != nil {
		t.Fatal(err)
	}

	return team, owner
}

func joinTestMember(t *testing.T, team *Team, joined time.Time) uuid.UUID {
	t.Helper()

	id := uuid.New()

	if err := team.Join(id, "", joined); err != nil {
		t.Fatalf("join: %s", err)
	}

	return id
}

func TestValidateTeamName(t *testing.T) {
	for name, valid := range map[string]bool{
		"":                                false,
		"A":                               false,
		"AB":                              true,
		"Ünïcödé Racing":                  true,
		strings.Repeat("x", 40):           true,
		strings.Repeat("x", 41):           false,
		strings.Repeat("ä", 40):           true, // counted in characters, not bytes
		"bad\x00name":                     false,
		"tab\tname":                       false,
		"new\nline":                       false,
		"<script>alert(1)</script>":       true, // allowed, the templates escape it
		strings.Repeat("ä", 41):           false,
		"Team " + strings.Repeat("a", 35): true,
	} {
		// the raw name is validated: normalizing would already turn tabs and newlines into spaces
		err := ValidateTeamName(name)

		if valid && err != nil {
			t.Errorf("%q: expected valid, got %s", name, err)
		} else if !valid && err != ErrTeamNameInvalid {
			t.Errorf("%q: expected ErrTeamNameInvalid, got %v", name, err)
		}
	}
}

func TestNormalizeTeamName_And_SameTeamName(t *testing.T) {
	if got := NormalizeTeamName("  Team \t  Rocket\n"); got != "Team Rocket" {
		t.Errorf("unexpected normalized name %q", got)
	}

	if !SameTeamName("Team Rocket", "  team   ROCKET ") {
		t.Error("names which differ only in case and whitespace must be the same")
	}

	if SameTeamName("Team Rocket", "Team Rockets") {
		t.Error("different names reported as the same")
	}

	if !SameTeamName("ÄÖÜ Racing", "äöü racing") {
		t.Error("unicode case folding is expected")
	}
}

func TestNewTeam(t *testing.T) {
	team, owner := newTestTeam(t, TeamPrivate)

	if team.Name != "Team Rocket" {
		t.Errorf("name was not normalized: %q", team.Name)
	}

	if team.ID == uuid.Nil || !team.IsPrivate() || team.IsDeleted() {
		t.Errorf("unexpected team: %+v", team)
	}

	if len(team.Members) != 1 || !team.IsOwner(owner) || !team.CanManage(owner) {
		t.Errorf("creator must be the only member and the owner: %+v", team.Members)
	}

	if _, err := NewTeam("x", TeamPublic, owner, teamNow); err != ErrTeamNameInvalid {
		t.Errorf("expected ErrTeamNameInvalid, got %v", err)
	}

	// unknown/empty visibility is public
	other, err := NewTeam("Public Team", "", owner, teamNow)

	if err != nil || other.IsPrivate() || other.Visibility != TeamPublic {
		t.Errorf("empty visibility must give a public team: %+v %v", other, err)
	}
}

func TestTeam_Join_Public(t *testing.T) {
	team, owner := newTestTeam(t, TeamPublic)
	member := uuid.New()

	if err := team.Join(member, "", teamNow); err != nil {
		t.Fatal(err)
	}

	if m := team.Member(member); m == nil || m.Role != TeamRoleMember {
		t.Errorf("joiner must be a normal member: %+v", m)
	}

	if team.CanManage(member) {
		t.Error("a normal member must not manage the team")
	}

	if err := team.Join(member, "", teamNow); err != ErrAlreadyTeamMember {
		t.Errorf("joining twice: expected ErrAlreadyTeamMember, got %v", err)
	}

	if err := team.Join(owner, "", teamNow); err != ErrAlreadyTeamMember {
		t.Errorf("owner joining: expected ErrAlreadyTeamMember, got %v", err)
	}

	if len(team.Members) != 2 {
		t.Errorf("expected 2 members, got %d", len(team.Members))
	}
}

func TestTeam_Join_Deleted(t *testing.T) {
	team, _ := newTestTeam(t, TeamPublic)
	team.Deleted = teamNow

	if err := team.Join(uuid.New(), "", teamNow); err != ErrTeamNotFound {
		t.Errorf("expected ErrTeamNotFound, got %v", err)
	}
}

func TestTeam_Join_Private(t *testing.T) {
	team, owner := newTestTeam(t, TeamPrivate)
	stranger := uuid.New()

	if err := team.Join(stranger, "", teamNow); err != ErrTeamInviteInvalid {
		t.Fatalf("private team without invite: expected ErrTeamInviteInvalid, got %v", err)
	}

	if err := team.Join(stranger, "made-up", teamNow); err != ErrTeamInviteInvalid {
		t.Fatalf("private team with a wrong token: expected ErrTeamInviteInvalid, got %v", err)
	}

	if team.IsMember(stranger) {
		t.Fatal("failed join must not add a member")
	}

	// link invite: usable by anybody, but only once
	if _, err := team.AddInvite("link-token", owner, uuid.Nil, time.Hour, teamNow); err != nil {
		t.Fatal(err)
	}

	first := uuid.New()

	if err := team.Join(first, "link-token", teamNow); err != nil {
		t.Fatalf("join with link token: %s", err)
	}

	if err := team.Join(stranger, "link-token", teamNow); err != ErrTeamInviteInvalid {
		t.Errorf("link token used twice: expected ErrTeamInviteInvalid, got %v", err)
	}

	if len(team.Invites) != 0 {
		t.Errorf("used invite must be removed, got %d", len(team.Invites))
	}
}

func TestTeam_Join_TargetedInvite(t *testing.T) {
	team, owner := newTestTeam(t, TeamPrivate)
	target, other := uuid.New(), uuid.New()

	if _, err := team.AddInvite("tok", owner, target, time.Hour, teamNow); err != nil {
		t.Fatal(err)
	}

	// somebody else knowing the token cannot use an invite meant for another account
	if err := team.Join(other, "tok", teamNow); err != ErrTeamInviteInvalid {
		t.Fatalf("foreign use of a targeted invite: expected ErrTeamInviteInvalid, got %v", err)
	}

	if len(team.Invites) != 1 {
		t.Fatal("a refused join must not consume the invite")
	}

	// the target can join without the token (accepting from their invite list)
	if err := team.Join(target, "", teamNow); err != nil {
		t.Fatalf("target joining without token: %s", err)
	}

	if len(team.Invites) != 0 {
		t.Error("invite must be consumed")
	}
}

func TestTeam_Join_TargetedInvite_WithToken(t *testing.T) {
	team, owner := newTestTeam(t, TeamPrivate)
	target := uuid.New()

	if _, err := team.AddInvite("tok", owner, target, time.Hour, teamNow); err != nil {
		t.Fatal(err)
	}

	if err := team.Join(target, "tok", teamNow); err != nil {
		t.Fatalf("target with token: %s", err)
	}
}

func TestTeam_Join_ExpiredInvite(t *testing.T) {
	team, owner := newTestTeam(t, TeamPrivate)
	target := uuid.New()

	if _, err := team.AddInvite("tok", owner, uuid.Nil, time.Hour, teamNow); err != nil {
		t.Fatal(err)
	}

	if _, err := team.AddInvite("tok2", owner, target, time.Hour, teamNow); err != nil {
		t.Fatal(err)
	}

	later := teamNow.Add(time.Hour) // expires is exclusive

	if err := team.Join(uuid.New(), "tok", later); err != ErrTeamInviteInvalid {
		t.Errorf("expired link invite: expected ErrTeamInviteInvalid, got %v", err)
	}

	if err := team.Join(target, "", later); err != ErrTeamInviteInvalid {
		t.Errorf("expired targeted invite: expected ErrTeamInviteInvalid, got %v", err)
	}

	just := teamNow.Add(time.Hour - time.Nanosecond)

	if err := team.Join(uuid.New(), "tok", just); err != nil {
		t.Errorf("invite must still work right before it expires: %s", err)
	}
}

func TestTeam_Join_PublicConsumesOnlyOwnInvite(t *testing.T) {
	team, owner := newTestTeam(t, TeamPublic)
	joiner := uuid.New()

	if _, err := team.AddInvite("link", owner, uuid.Nil, time.Hour, teamNow); err != nil {
		t.Fatal(err)
	}

	if _, err := team.AddInvite("mine", owner, joiner, time.Hour, teamNow); err != nil {
		t.Fatal(err)
	}

	if err := team.Join(joiner, "link", teamNow); err != nil {
		t.Fatal(err)
	}

	if len(team.Invites) != 1 || team.Invites[0].Token != "link" {
		t.Errorf("only the invite of the joiner may be removed: %+v", team.Invites)
	}
}

func TestTeam_Invites(t *testing.T) {
	team, owner := newTestTeam(t, TeamPrivate)
	member := joinViaLink(t, team, owner)

	if _, err := team.AddInvite("", owner, uuid.Nil, time.Hour, teamNow); err != ErrTeamInviteInvalid {
		t.Errorf("empty token: expected ErrTeamInviteInvalid, got %v", err)
	}

	if _, err := team.AddInvite("tok", owner, member, time.Hour, teamNow); err != ErrAlreadyTeamMember {
		t.Errorf("inviting a member: expected ErrAlreadyTeamMember, got %v", err)
	}

	target := uuid.New()

	if _, err := team.AddInvite("a", owner, target, time.Hour, teamNow); err != nil {
		t.Fatal(err)
	}

	// re-inviting the same account replaces the invite (old token no longer works)
	if _, err := team.AddInvite("b", owner, target, time.Hour, teamNow); err != nil {
		t.Fatal(err)
	}

	if len(team.Invites) != 1 || team.Invites[0].Token != "b" {
		t.Fatalf("expected the second invite to replace the first: %+v", team.Invites)
	}

	// no expiry with ttl 0
	invite, err := team.AddInvite("forever", owner, uuid.Nil, 0, teamNow)

	if err != nil || !invite.Expires.IsZero() || invite.Expired(teamNow.Add(1000*time.Hour)) {
		t.Errorf("invite without ttl must never expire: %+v %v", invite, err)
	}

	if err := team.RevokeInvite("nope", teamNow); err != ErrTeamInviteInvalid {
		t.Errorf("revoking an unknown invite: expected ErrTeamInviteInvalid, got %v", err)
	}

	if err := team.RevokeInvite("forever", teamNow); err != nil {
		t.Fatal(err)
	}

	if err := team.Join(uuid.New(), "forever", teamNow); err != ErrTeamInviteInvalid {
		t.Errorf("revoked invite must not work, got %v", err)
	}
}

func joinViaLink(t *testing.T, team *Team, owner uuid.UUID) uuid.UUID {
	t.Helper()

	token := uuid.New().String()

	if _, err := team.AddInvite(token, owner, uuid.Nil, time.Hour, teamNow); err != nil {
		t.Fatal(err)
	}

	id := uuid.New()

	if err := team.Join(id, token, teamNow); err != nil {
		t.Fatal(err)
	}

	return id
}

func TestTeam_PruneInvites(t *testing.T) {
	team, owner := newTestTeam(t, TeamPrivate)

	_, _ = team.AddInvite("old", owner, uuid.Nil, time.Minute, teamNow)
	_, _ = team.AddInvite("new", owner, uuid.Nil, time.Hour, teamNow)

	team.PruneInvites(teamNow.Add(30 * time.Minute))

	if len(team.Invites) != 1 || team.Invites[0].Token != "new" {
		t.Errorf("expected only the unexpired invite to remain: %+v", team.Invites)
	}

	// adding an invite prunes as well
	_, _ = team.AddInvite("third", owner, uuid.Nil, time.Hour, teamNow.Add(2*time.Hour))

	if len(team.Invites) != 1 || team.Invites[0].Token != "third" {
		t.Errorf("AddInvite must prune expired invites: %+v", team.Invites)
	}
}

func TestTeam_CanSee(t *testing.T) {
	public, publicOwner := newTestTeam(t, TeamPublic)
	private, privateOwner := newTestTeam(t, TeamPrivate)
	stranger, invited := uuid.New(), uuid.New()

	_, _ = private.AddInvite("tok", privateOwner, invited, time.Hour, teamNow)

	for _, c := range []struct {
		name    string
		team    *Team
		account uuid.UUID
		want    bool
	}{
		{"public, stranger", public, stranger, true},
		{"public, anonymous", public, uuid.Nil, true},
		{"public, owner", public, publicOwner, true},
		{"private, stranger", private, stranger, false},
		{"private, anonymous", private, uuid.Nil, false},
		{"private, member", private, privateOwner, true},
		{"private, invited", private, invited, true},
	} {
		if got := c.team.CanSee(c.account, teamNow); got != c.want {
			t.Errorf("%s: CanSee = %v, want %v", c.name, got, c.want)
		}
	}

	if private.CanSee(invited, teamNow.Add(2*time.Hour)) {
		t.Error("an expired invite must not make the team visible")
	}

	private.Deleted = teamNow

	if private.CanSee(privateOwner, teamNow) {
		t.Error("a deleted team is not visible, even to its owner")
	}
}

func TestTeam_RemoveMember_Plain(t *testing.T) {
	team, owner := newTestTeam(t, TeamPublic)
	member := joinTestMember(t, team, teamNow.Add(time.Minute))

	newOwner, empty, err := team.RemoveMember(member, teamNow)

	if err != nil || empty || newOwner != uuid.Nil {
		t.Fatalf("removing a plain member: newOwner=%s empty=%v err=%v", newOwner, empty, err)
	}

	if team.IsMember(member) || !team.IsOwner(owner) {
		t.Errorf("unexpected members: %+v", team.Members)
	}

	if _, _, err := team.RemoveMember(member, teamNow); err != ErrNotTeamMember {
		t.Errorf("removing twice: expected ErrNotTeamMember, got %v", err)
	}

	if _, _, err := team.RemoveMember(uuid.New(), teamNow); err != ErrNotTeamMember {
		t.Errorf("removing a stranger: expected ErrNotTeamMember, got %v", err)
	}
}

func TestTeam_RemoveMember_LastMemberEmptiesTeam(t *testing.T) {
	team, owner := newTestTeam(t, TeamPublic)

	newOwner, empty, err := team.RemoveMember(owner, teamNow)

	if err != nil || !empty || newOwner != uuid.Nil || len(team.Members) != 0 {
		t.Fatalf("expected an empty team: newOwner=%s empty=%v err=%v members=%d", newOwner, empty, err, len(team.Members))
	}
}

func TestTeam_RemoveMember_OwnerSuccession(t *testing.T) {
	t.Run("oldest member without admins", func(t *testing.T) {
		team, owner := newTestTeam(t, TeamPublic)
		late := joinTestMember(t, team, teamNow.Add(3*time.Hour))
		early := joinTestMember(t, team, teamNow.Add(time.Hour))

		newOwner, empty, err := team.RemoveMember(owner, teamNow)

		if err != nil || empty || newOwner != early {
			t.Fatalf("expected the oldest member to take over, got %s (empty=%v err=%v)", newOwner, empty, err)
		}

		if !team.IsOwner(early) || team.IsOwner(late) || team.Owner().AccountID != early {
			t.Errorf("unexpected roles: %+v", team.Members)
		}
	})

	t.Run("admin before older member", func(t *testing.T) {
		team, owner := newTestTeam(t, TeamPublic)
		older := joinTestMember(t, team, teamNow.Add(time.Hour))
		admin := joinTestMember(t, team, teamNow.Add(5*time.Hour))

		if err := team.SetAdmin(admin, true, teamNow); err != nil {
			t.Fatal(err)
		}

		newOwner, _, err := team.RemoveMember(owner, teamNow)

		if err != nil || newOwner != admin {
			t.Fatalf("expected the admin to take over, got %s (older member %s) err=%v", newOwner, older, err)
		}
	})

	t.Run("oldest of several admins", func(t *testing.T) {
		team, owner := newTestTeam(t, TeamPublic)
		a1 := joinTestMember(t, team, teamNow.Add(5*time.Hour))
		a2 := joinTestMember(t, team, teamNow.Add(2*time.Hour))

		_ = team.SetAdmin(a1, true, teamNow)
		_ = team.SetAdmin(a2, true, teamNow)

		newOwner, _, _ := team.RemoveMember(owner, teamNow)

		if newOwner != a2 {
			t.Errorf("expected the longest serving admin %s, got %s", a2, newOwner)
		}
	})

	t.Run("exactly one owner afterwards", func(t *testing.T) {
		team, owner := newTestTeam(t, TeamPublic)
		joinTestMember(t, team, teamNow.Add(time.Hour))
		joinTestMember(t, team, teamNow.Add(2*time.Hour))

		_, _, _ = team.RemoveMember(owner, teamNow)

		owners := 0

		for _, m := range team.Members {
			if m.Role == TeamRoleOwner {
				owners++
			}
		}

		if owners != 1 {
			t.Errorf("expected exactly one owner, got %d", owners)
		}
	})

	t.Run("removing an admin keeps the owner", func(t *testing.T) {
		team, owner := newTestTeam(t, TeamPublic)
		admin := joinTestMember(t, team, teamNow.Add(time.Hour))
		_ = team.SetAdmin(admin, true, teamNow)

		newOwner, _, err := team.RemoveMember(admin, teamNow)

		if err != nil || newOwner != uuid.Nil || !team.IsOwner(owner) {
			t.Errorf("owner must not change: newOwner=%s err=%v", newOwner, err)
		}
	})
}

func TestTeam_SetAdmin(t *testing.T) {
	team, owner := newTestTeam(t, TeamPublic)
	member := joinTestMember(t, team, teamNow)

	if err := team.SetAdmin(member, true, teamNow); err != nil || !team.CanManage(member) || team.IsOwner(member) {
		t.Fatalf("promote: %v %+v", err, team.Members)
	}

	if err := team.SetAdmin(member, false, teamNow); err != nil || team.CanManage(member) {
		t.Fatalf("demote: %v %+v", err, team.Members)
	}

	if err := team.SetAdmin(owner, false, teamNow); err != ErrTeamOwnerRole {
		t.Errorf("demoting the owner: expected ErrTeamOwnerRole, got %v", err)
	}

	if err := team.SetAdmin(owner, true, teamNow); err != ErrTeamOwnerRole {
		t.Errorf("promoting the owner: expected ErrTeamOwnerRole, got %v", err)
	}

	if err := team.SetAdmin(uuid.New(), true, teamNow); err != ErrNotTeamMember {
		t.Errorf("promoting a stranger: expected ErrNotTeamMember, got %v", err)
	}
}

func TestTeam_TransferOwnership(t *testing.T) {
	team, owner := newTestTeam(t, TeamPublic)
	member := joinTestMember(t, team, teamNow)
	stranger := uuid.New()

	if err := team.TransferOwnership(owner, owner, teamNow); err != ErrTeamTransferToSelf {
		t.Errorf("to self: expected ErrTeamTransferToSelf, got %v", err)
	}

	if err := team.TransferOwnership(member, owner, teamNow); err != ErrTeamPermission {
		t.Errorf("by a non-owner: expected ErrTeamPermission, got %v", err)
	}

	if err := team.TransferOwnership(owner, stranger, teamNow); err != ErrNotTeamMember {
		t.Errorf("to a non-member: expected ErrNotTeamMember, got %v", err)
	}

	if err := team.TransferOwnership(owner, member, teamNow); err != nil {
		t.Fatal(err)
	}

	if !team.IsOwner(member) || team.IsOwner(owner) || !team.CanManage(owner) {
		t.Errorf("the old owner must become admin: %+v", team.Members)
	}

	if team.Member(owner).Role != TeamRoleAdmin {
		t.Errorf("expected admin, got %s", team.Member(owner).Role)
	}
}

func TestTeam_Rename_And_Visibility(t *testing.T) {
	team, owner := newTestTeam(t, TeamPublic)

	if err := team.Rename("  New   Name ", teamNow.Add(time.Hour)); err != nil || team.Name != "New Name" {
		t.Fatalf("rename: %v %q", err, team.Name)
	}

	if err := team.Rename("x", teamNow); err != ErrTeamNameInvalid || team.Name != "New Name" {
		t.Errorf("an invalid rename must not change the name: %v %q", err, team.Name)
	}

	team.SetVisibility(TeamPrivate, teamNow)

	if !team.IsPrivate() {
		t.Error("expected a private team")
	}

	team.SetVisibility("garbage", teamNow)

	if team.IsPrivate() || team.Visibility != TeamPublic {
		t.Error("unknown visibility must fall back to public")
	}

	// members stay when a team becomes private
	team.SetVisibility(TeamPrivate, teamNow)

	if !team.IsMember(owner) {
		t.Error("owner must stay a member")
	}
}
