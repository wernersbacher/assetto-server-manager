package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

func apiTestTeam(t *testing.T) (*smmodel.Team, map[uuid.UUID]string) {
	t.Helper()

	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	owner, admin, early, late := uuid.New(), uuid.New(), uuid.New(), uuid.New()

	team, err := smmodel.NewTeam("API Team", smmodel.TeamPrivate, owner, now)

	if err != nil {
		t.Fatal(err)
	}

	// join in a mixed up order: the API lists owner, admins, members, each by joining time
	for _, join := range []struct {
		id uuid.UUID
		at time.Duration
	}{{late, 3 * time.Hour}, {admin, 2 * time.Hour}, {early, time.Hour}} {
		if _, err := team.AddInvite("token-"+join.id.String(), owner, join.id, time.Hour*24, now); err != nil {
			t.Fatal(err)
		}

		if err := team.Join(join.id, "", now.Add(join.at)); err != nil {
			t.Fatal(err)
		}
	}

	if err := team.SetAdmin(admin, true, now); err != nil {
		t.Fatal(err)
	}

	// an open invite that must never show up
	if _, err := team.AddInvite("secret-link-token", owner, uuid.Nil, time.Hour*24, now); err != nil {
		t.Fatal(err)
	}

	return team, map[uuid.UUID]string{owner: "Olivia Owner", admin: "Adam Admin", early: "Early Bird", late: "Late Larry"}
}

func TestNewTeam(t *testing.T) {
	team, names := apiTestTeam(t)

	got := NewTeam(team, names, "https://acp.example")

	if got.ID != team.ID || got.Name != "API Team" || got.Visibility != "private" || got.MemberCount != 4 || got.Owner != "Olivia Owner" {
		t.Errorf("unexpected team: %+v", got)
	}

	if got.URL != "https://acp.example/teams/"+team.ID.String() {
		t.Errorf("unexpected URL %q", got.URL)
	}

	// without a base URL there is no link
	if got := NewTeam(team, names, ""); got.URL != "" {
		t.Errorf("expected no URL, got %q", got.URL)
	}

	team.SetVisibility(smmodel.TeamPublic, time.Now())

	if got := NewTeam(team, names, ""); got.Visibility != "public" {
		t.Errorf("expected a public team, got %q", got.Visibility)
	}

	// an owner without a known name stays empty instead of failing
	if got := NewTeam(team, map[uuid.UUID]string{}, ""); got.Owner != "" || got.MemberCount != 4 {
		t.Errorf("unexpected team without names: %+v", got)
	}
}

func TestNewTeamDetail(t *testing.T) {
	team, names := apiTestTeam(t)

	got := NewTeamDetail(team, names, "")

	var order []string

	for _, member := range got.Members {
		order = append(order, member.Role+":"+member.Name)
	}

	want := []string{"owner:Olivia Owner", "admin:Adam Admin", "member:Early Bird", "member:Late Larry"}

	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Errorf("members: got %v, want %v", order, want)
	}

	if got.Members[2].Joined.After(got.Members[3].Joined) || got.Members[2].Joined.IsZero() {
		t.Error("members of the same role are listed in the order they joined")
	}

	// a team without members (cannot happen, but must not crash) has an empty list, not null
	empty := &smmodel.Team{ID: uuid.New(), Name: "Empty"}
	raw, err := json.Marshal(NewTeamDetail(empty, nil, ""))

	if err != nil || !strings.Contains(string(raw), `"Members":[]`) {
		t.Errorf("expected an empty member list: %s %v", raw, err)
	}
}

func TestTeamJSONHasNoSecrets(t *testing.T) {
	team, names := apiTestTeam(t)

	raw, err := json.Marshal(NewTeamDetail(team, names, "https://acp.example"))

	if err != nil {
		t.Fatal(err)
	}

	body := string(raw)

	// the team fields are at the top level next to the members, as documented
	for _, field := range []string{`"ID"`, `"Name"`, `"URL"`, `"Visibility"`, `"MemberCount"`, `"Owner"`, `"Members"`, `"Role"`, `"Joined"`} {
		if !strings.Contains(body, field) {
			t.Errorf("the JSON must have %s: %s", field, body)
		}
	}

	if strings.Contains(body, `"Team":`) {
		t.Errorf("the team fields must not be nested: %s", body)
	}

	// no account ids, invites or tokens
	secrets := []string{"secret-link-token", "token-", "Invites", "Token", "AccountID", "CreatedBy"}

	for _, member := range team.Members {
		secrets = append(secrets, member.AccountID.String())
	}

	for _, secret := range secrets {
		if strings.Contains(body, secret) {
			t.Errorf("the JSON must not contain %q: %s", secret, body)
		}
	}
}

func TestTeamGetURL(t *testing.T) {
	team := &smmodel.Team{ID: uuid.New()}

	if got := team.GetURL("https://x.example"); got != "https://x.example/teams/"+team.ID.String() {
		t.Errorf("unexpected URL %q", got)
	}

	if got := team.GetURL(""); got != "" {
		t.Errorf("expected no URL, got %q", got)
	}
}
