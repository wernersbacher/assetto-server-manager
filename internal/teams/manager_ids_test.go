package teams

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
)

// the stores build file names from the team id, so ids from the outside must be UUIDs and nothing else
func TestIDsFromOutsideMustBeUUIDs(t *testing.T) {
	e := newTestEnv(t)
	owner := e.account()
	team := e.create(owner, "Safe", smmodel.TeamPublic)

	for _, id := range []string{
		"",
		" ",
		"..",
		"../teams/" + team.ID.String(),
		`..\teams\` + team.ID.String(),
		`..\..\accounts\admin`,
		team.ID.String() + "/../" + team.ID.String(),
		team.ID.String() + ".json",
		"not-a-uuid",
		strings.Repeat("a", 500),
		"%2e%2e%2f",
		"\x00",
	} {
		if _, err := e.manager.Get(owner, id); err != smmodel.ErrTeamNotFound {
			t.Errorf("Get(%q): expected ErrTeamNotFound, got %v", id, err)
		}

		if _, err := e.manager.Lookup(id); err != smmodel.ErrTeamNotFound {
			t.Errorf("Lookup(%q): expected ErrTeamNotFound, got %v", id, err)
		}

		if _, err := e.manager.Join(owner, id, ""); err != smmodel.ErrTeamNotFound {
			t.Errorf("Join(%q): expected ErrTeamNotFound, got %v", id, err)
		}

		if err := e.manager.Delete(owner, id); err != smmodel.ErrTeamNotFound {
			t.Errorf("Delete(%q): expected ErrTeamNotFound, got %v", id, err)
		}

		if err := e.manager.Leave(owner, id); err != smmodel.ErrTeamNotFound {
			t.Errorf("Leave(%q): expected ErrTeamNotFound, got %v", id, err)
		}
	}

	if got := e.reload(team); got.IsDeleted() || len(got.Members) != 1 {
		t.Errorf("the team must be untouched: %+v", got)
	}
}

// a team id in upper case or with braces is the same team, the canonical form is used for the store
func TestIDsAreCanonicalised(t *testing.T) {
	e := newTestEnv(t)
	owner := e.account()
	team := e.create(owner, "Canon", smmodel.TeamPublic)

	for _, id := range []string{strings.ToUpper(team.ID.String()), "{" + team.ID.String() + "}", "urn:uuid:" + team.ID.String()} {
		got, err := e.manager.Lookup(id)

		if err != nil || got.ID != team.ID {
			t.Errorf("Lookup(%q): %v %v", id, got, err)
		}
	}
}

// a store file outside of the team directory must not be readable through a team id
func TestIDsCannotReachOtherFiles(t *testing.T) {
	dir, err := ioutil.TempDir("", "teams-traversal")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(dir)

	store := smstore.NewJSONStore(filepath.Join(dir, "store"), filepath.Join(dir, "shared"))
	manager := NewManager(store, nil)

	// a file that looks like a team, next to the team directory
	secret := `{"ID":"` + uuid.New().String() + `","Name":"Secret","Members":[]}`

	if err := ioutil.WriteFile(filepath.Join(dir, "shared", "secret.json"), []byte(secret), 0644); err != nil {
		if err := os.MkdirAll(filepath.Join(dir, "shared"), 0755); err != nil {
			t.Fatal(err)
		}

		if err := ioutil.WriteFile(filepath.Join(dir, "shared", "secret.json"), []byte(secret), 0644); err != nil {
			t.Fatal(err)
		}
	}

	for _, id := range []string{"../secret", `..\secret`} {
		if team, err := manager.Lookup(id); err != smmodel.ErrTeamNotFound || team != nil {
			t.Errorf("Lookup(%q) reached a file outside of the team directory: %v %v", id, team, err)
		}
	}
}

func TestFindByInvite(t *testing.T) {
	e := newTestEnv(t)
	owner := e.account()
	team := e.create(owner, "Invites", smmodel.TeamPrivate)
	other := e.create(owner, "Other", smmodel.TeamPrivate)

	link, err := e.manager.Invite(owner, team.ID.String(), uuid.Nil)

	if err != nil {
		t.Fatal(err)
	}

	otherLink, _ := e.manager.Invite(owner, other.ID.String(), uuid.Nil)

	if got, err := e.manager.FindByInvite(link.Token); err != nil || got.ID != team.ID {
		t.Errorf("expected the team of the invite: %v %v", got, err)
	}

	if got, err := e.manager.FindByInvite(otherLink.Token); err != nil || got.ID != other.ID {
		t.Errorf("expected the other team: %v %v", got, err)
	}

	for _, token := range []string{"", "unknown", link.Token + "x", strings.ToUpper(link.Token)} {
		if _, err := e.manager.FindByInvite(token); err != smmodel.ErrTeamInviteInvalid {
			t.Errorf("FindByInvite(%q): expected ErrTeamInviteInvalid, got %v", token, err)
		}
	}

	// looking up does not use the invite up
	if _, err := e.manager.FindByInvite(link.Token); err != nil {
		t.Errorf("the invite must still be there: %v", err)
	}

	// expired
	e.clock = e.clock.Add(DefaultInviteTTL + time.Minute)

	if _, err := e.manager.FindByInvite(link.Token); err != smmodel.ErrTeamInviteInvalid {
		t.Errorf("expired invite: expected ErrTeamInviteInvalid, got %v", err)
	}

	// a deleted team has no invites
	fresh := e.create(owner, "Fresh", smmodel.TeamPrivate)
	freshInvite, _ := e.manager.Invite(owner, fresh.ID.String(), uuid.Nil)

	if err := e.manager.Delete(owner, fresh.ID.String()); err != nil {
		t.Fatal(err)
	}

	if _, err := e.manager.FindByInvite(freshInvite.Token); err != smmodel.ErrTeamInviteInvalid {
		t.Errorf("deleted team: expected ErrTeamInviteInvalid, got %v", err)
	}
}

func TestCanCreate(t *testing.T) {
	e := newTestEnv(t)
	user, mod := Actor{AccountID: uuid.New()}, Actor{AccountID: uuid.New(), Moderator: true}

	if !e.manager.CanCreate(user) || !e.manager.CanCreate(mod) {
		t.Error("everybody can create teams by default")
	}

	e.options.DisableCreation = true

	if e.manager.CanCreate(user) {
		t.Error("creation is disabled for normal accounts")
	}

	if !e.manager.CanCreate(mod) {
		t.Error("moderators can always create teams")
	}
}
