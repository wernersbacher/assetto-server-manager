package model

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
)

func signUp(guid string, teamID uuid.UUID, team string, status ChampionshipEntrantStatus) *ChampionshipSignUpResponse {
	return &ChampionshipSignUpResponse{GUID: guid, TeamID: teamID, Team: team, Status: status}
}

func TestSignUpTeamModes(t *testing.T) {
	for _, c := range []struct {
		name                string
		form                ChampionshipSignUpForm
		selection, freeText bool
	}{
		{"not asked", ChampionshipSignUpForm{TeamMode: SignUpTeamSelect}, false, false},
		{"not asked, both", ChampionshipSignUpForm{TeamMode: SignUpTeamBoth}, false, false},
		{"legacy free text", ChampionshipSignUpForm{AskForTeam: true}, false, true},
		{"select", ChampionshipSignUpForm{AskForTeam: true, TeamMode: SignUpTeamSelect}, true, false},
		{"both", ChampionshipSignUpForm{AskForTeam: true, TeamMode: SignUpTeamBoth}, true, true},
		{"unknown mode asks for nothing", ChampionshipSignUpForm{AskForTeam: true, TeamMode: "bogus"}, false, false},
	} {
		if got := c.form.UsesTeamSelection(); got != c.selection {
			t.Errorf("%s: UsesTeamSelection = %v, want %v", c.name, got, c.selection)
		}

		if got := c.form.AllowsFreeTextTeam(); got != c.freeText {
			t.Errorf("%s: AllowsFreeTextTeam = %v, want %v", c.name, got, c.freeText)
		}
	}
}

func TestTeamLimit(t *testing.T) {
	for _, c := range []struct {
		max     int
		limit   int
		limited bool
	}{
		{0, DefaultMaxDriversPerTeam, true},
		{1, 1, true},
		{2, 2, true},
		{5, 5, true},
		{-1, 0, false},
		{-100, 0, false},
	} {
		limit, limited := ChampionshipSignUpForm{MaxDriversPerTeam: c.max}.TeamLimit()

		if limit != c.limit || limited != c.limited {
			t.Errorf("MaxDriversPerTeam %d: got (%d, %v), want (%d, %v)", c.max, limit, limited, c.limit, c.limited)
		}
	}

	if DefaultMaxDriversPerTeam != 2 {
		t.Error("the default is two drivers per team")
	}
}

func TestTeamDriverCount(t *testing.T) {
	team, other := uuid.New(), uuid.New()

	form := ChampionshipSignUpForm{Responses: []*ChampionshipSignUpResponse{
		signUp("1", team, "T", ChampionshipEntrantAccepted),
		signUp("2", team, "T", ChampionshipEntrantPending),
		signUp("3", team, "T", ChampionshipEntrantRejected),
		signUp("4", other, "O", ChampionshipEntrantAccepted),
		signUp("5", uuid.Nil, "T", ChampionshipEntrantAccepted), // free text with the same name is not the team
		signUp("6", team, "T", ""),                              // an old response without status still counts
	}}

	if got := form.TeamDriverCount(team, ""); got != 3 {
		t.Errorf("accepted, pending and unset count, rejected does not: got %d, want 3", got)
	}

	if got := form.TeamDriverCount(team, "1"); got != 2 {
		t.Errorf("the driver in question is not counted: got %d, want 2", got)
	}

	if got := form.TeamDriverCount(other, ""); got != 1 {
		t.Errorf("got %d, want 1", got)
	}

	if got := form.TeamDriverCount(uuid.Nil, ""); got != 0 {
		t.Errorf("free text teams are never counted: got %d", got)
	}

	if got := form.TeamDriverCount(uuid.New(), ""); got != 0 {
		t.Errorf("unknown team: got %d", got)
	}
}

func TestSameTeam(t *testing.T) {
	a, b := uuid.New(), uuid.New()

	for _, c := range []struct {
		name string
		x, y ChampionshipSignUpResponse
		want bool
	}{
		{"same id", ChampionshipSignUpResponse{TeamID: a, Team: "A"}, ChampionshipSignUpResponse{TeamID: a, Team: "renamed"}, true},
		{"different id, same name", ChampionshipSignUpResponse{TeamID: a, Team: "X"}, ChampionshipSignUpResponse{TeamID: b, Team: "X"}, false},
		{"list team against free text of the same name", ChampionshipSignUpResponse{TeamID: a, Team: "X"}, ChampionshipSignUpResponse{Team: "X"}, false},
		{"free text, case", ChampionshipSignUpResponse{Team: "Red Bull"}, ChampionshipSignUpResponse{Team: " red  BULL "}, true},
		{"free text, different", ChampionshipSignUpResponse{Team: "Red Bull"}, ChampionshipSignUpResponse{Team: "Ferrari"}, false},
		{"no team twice", ChampionshipSignUpResponse{}, ChampionshipSignUpResponse{}, true},
		{"no team against a team", ChampionshipSignUpResponse{}, ChampionshipSignUpResponse{TeamID: a, Team: "A"}, false},
		{"no team against free text", ChampionshipSignUpResponse{}, ChampionshipSignUpResponse{Team: "A"}, false},
	} {
		if got := c.x.SameTeam(c.y); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}

		if got := c.y.SameTeam(c.x); got != c.want {
			t.Errorf("%s (reversed): got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestCheckTeamLimit(t *testing.T) {
	team, other := uuid.New(), uuid.New()

	form := ChampionshipSignUpForm{Responses: []*ChampionshipSignUpResponse{
		signUp("1", team, "T", ChampionshipEntrantAccepted),
		signUp("2", team, "T", ChampionshipEntrantPending),
		signUp("3", other, "O", ChampionshipEntrantAccepted),
	}}

	// the team is full with two drivers (the default)
	if err := form.CheckTeamLimit(signUp("9", team, "T", ChampionshipEntrantPending)); err != ErrSignUpTeamFull {
		t.Errorf("third driver: expected ErrSignUpTeamFull, got %v", err)
	}

	// the other team has room
	if err := form.CheckTeamLimit(signUp("9", other, "O", ChampionshipEntrantPending)); err != nil {
		t.Errorf("second driver of another team: %s", err)
	}

	// a registered driver registering again is not counted twice
	if err := form.CheckTeamLimit(signUp("1", team, "T", ChampionshipEntrantPending)); err != nil {
		t.Errorf("re-registration of a driver of a full team: %s", err)
	}

	// free text is never limited
	if err := form.CheckTeamLimit(signUp("9", uuid.Nil, "T", ChampionshipEntrantPending)); err != nil {
		t.Errorf("free text: %s", err)
	}

	// custom limits
	form.MaxDriversPerTeam = 3

	if err := form.CheckTeamLimit(signUp("9", team, "T", ChampionshipEntrantPending)); err != nil {
		t.Errorf("limit 3: %s", err)
	}

	form.MaxDriversPerTeam = 1

	if err := form.CheckTeamLimit(signUp("9", other, "O", ChampionshipEntrantPending)); err != ErrSignUpTeamFull {
		t.Errorf("limit 1: expected ErrSignUpTeamFull, got %v", err)
	}

	form.MaxDriversPerTeam = -1

	if err := form.CheckTeamLimit(signUp("9", team, "T", ChampionshipEntrantPending)); err != nil {
		t.Errorf("unlimited: %s", err)
	}
}

func TestCheckTeamLimit_RejectedFreesTheSlot(t *testing.T) {
	team := uuid.New()

	form := ChampionshipSignUpForm{Responses: []*ChampionshipSignUpResponse{
		signUp("1", team, "T", ChampionshipEntrantAccepted),
		signUp("2", team, "T", ChampionshipEntrantRejected),
	}}

	if err := form.CheckTeamLimit(signUp("3", team, "T", ChampionshipEntrantPending)); err != nil {
		t.Errorf("a rejected driver must not block the team: %s", err)
	}

	// and the rejected driver cannot come back once the slot was taken
	form.Responses = append(form.Responses, signUp("3", team, "T", ChampionshipEntrantPending))

	if err := form.CheckTeamLimit(signUp("2", team, "T", ChampionshipEntrantPending)); err != ErrSignUpTeamFull {
		t.Errorf("expected ErrSignUpTeamFull, got %v", err)
	}
}

func TestCheckTeamLock(t *testing.T) {
	a, b := uuid.New(), uuid.New()

	form := ChampionshipSignUpForm{Responses: []*ChampionshipSignUpResponse{
		signUp("accepted", a, "A", ChampionshipEntrantAccepted),
		signUp("pending", a, "A", ChampionshipEntrantPending),
		signUp("rejected", a, "A", ChampionshipEntrantRejected),
		signUp("free", uuid.Nil, "Free", ChampionshipEntrantAccepted),
		signUp("none", uuid.Nil, "", ChampionshipEntrantAccepted),
	}}

	for _, c := range []struct {
		name string
		resp *ChampionshipSignUpResponse
		want error
	}{
		{"new driver", signUp("new", b, "B", ChampionshipEntrantPending), nil},
		{"accepted, same team", signUp("accepted", a, "A", ChampionshipEntrantPending), nil},
		{"accepted, other team", signUp("accepted", b, "B", ChampionshipEntrantPending), ErrSignUpTeamLocked},
		{"accepted, free text instead", signUp("accepted", uuid.Nil, "A", ChampionshipEntrantPending), ErrSignUpTeamLocked},
		{"accepted, no team", signUp("accepted", uuid.Nil, "", ChampionshipEntrantPending), ErrSignUpTeamLocked},
		{"pending, other team", signUp("pending", b, "B", ChampionshipEntrantPending), ErrSignUpTeamLocked},
		{"rejected, other team", signUp("rejected", b, "B", ChampionshipEntrantPending), nil},
		{"free text, same name other case", signUp("free", uuid.Nil, "FREE", ChampionshipEntrantPending), nil},
		{"free text to list team", signUp("free", a, "A", ChampionshipEntrantPending), ErrSignUpTeamLocked},
		{"free text to other free text", signUp("free", uuid.Nil, "Other", ChampionshipEntrantPending), ErrSignUpTeamLocked},
		{"no team to a team", signUp("none", a, "A", ChampionshipEntrantPending), ErrSignUpTeamLocked},
		{"no team stays no team", signUp("none", uuid.Nil, "", ChampionshipEntrantPending), nil},
	} {
		if got := form.CheckTeamLock(c.resp); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestCheckTeam_Override(t *testing.T) {
	a, b := uuid.New(), uuid.New()

	form := ChampionshipSignUpForm{Responses: []*ChampionshipSignUpResponse{
		signUp("1", a, "A", ChampionshipEntrantAccepted),
		signUp("2", b, "B", ChampionshipEntrantAccepted),
		signUp("3", b, "B", ChampionshipEntrantAccepted),
	}}

	move := signUp("1", b, "B", ChampionshipEntrantAccepted) // locked and the team is full

	if err := form.CheckTeam(move, false); err != ErrSignUpTeamLocked {
		t.Errorf("the lock is reported first, got %v", err)
	}

	if err := form.CheckTeam(move, true); err != nil {
		t.Errorf("moderators override both rules, got %v", err)
	}

	// a new driver into the full team: the limit applies
	if err := form.CheckTeam(signUp("4", b, "B", ChampionshipEntrantPending), false); err != ErrSignUpTeamFull {
		t.Errorf("expected ErrSignUpTeamFull, got %v", err)
	}

	if err := form.CheckTeam(signUp("4", b, "B", ChampionshipEntrantPending), true); err != nil {
		t.Errorf("override of the limit: %s", err)
	}
}

func newChampionshipWithEntrants() *Championship {
	c := NewChampionship("Test")

	c.Classes = []*ChampionshipClass{{
		ID: uuid.New(),
		Entrants: serverconfig.EntryList{
			"CAR_0": &serverconfig.Entrant{GUID: "1", Name: "One", Team: "Old"},
			"CAR_1": &serverconfig.Entrant{GUID: "2", Name: "Two", Team: "Old"},
			"CAR_2": &serverconfig.Entrant{GUID: "3", Name: "Three", Team: "Other"},
			"CAR_3": &serverconfig.Entrant{},
		},
	}}

	return c
}

func entrantTeam(c *Championship, guid string) string {
	for _, class := range c.Classes {
		for _, entrant := range class.Entrants {
			if entrant.GUID == guid {
				return entrant.Team
			}
		}
	}

	return "<missing>"
}

func TestChampionship_SetEntrantTeam(t *testing.T) {
	c := newChampionshipWithEntrants()
	old, next := uuid.New(), uuid.New()

	c.SignUpForm.Responses = []*ChampionshipSignUpResponse{
		signUp("1", old, "Old", ChampionshipEntrantAccepted),
		signUp("2", old, "Old", ChampionshipEntrantAccepted),
	}

	if !c.SetEntrantTeam("1", next, "New") {
		t.Fatal("the driver is registered")
	}

	if r := c.SignUpForm.Response("1"); r.TeamID != next || r.Team != "New" {
		t.Errorf("registration not updated: %+v", r)
	}

	if entrantTeam(c, "1") != "New" {
		t.Error("entry list not updated")
	}

	if entrantTeam(c, "2") != "Old" || c.SignUpForm.Response("2").TeamID != old {
		t.Error("other drivers must not change")
	}

	// to no team at all
	if !c.SetEntrantTeam("1", uuid.Nil, "") || entrantTeam(c, "1") != "" || c.SignUpForm.Response("1").TeamID != uuid.Nil {
		t.Error("removing the team failed")
	}

	if c.SetEntrantTeam("unknown", next, "New") {
		t.Error("an unregistered driver cannot be moved")
	}
}

func TestChampionship_SetEntrantTeam_NotInEntryList(t *testing.T) {
	// a pending registration has no entry list entry yet
	c := newChampionshipWithEntrants()
	team := uuid.New()

	c.SignUpForm.Responses = []*ChampionshipSignUpResponse{signUp("pending", uuid.Nil, "", ChampionshipEntrantPending)}

	if !c.SetEntrantTeam("pending", team, "New") || c.SignUpForm.Response("pending").Team != "New" {
		t.Error("the registration must be updated even without an entry")
	}
}

func TestChampionship_RenameTeam(t *testing.T) {
	c := newChampionshipWithEntrants()
	team, other := uuid.New(), uuid.New()

	c.SignUpForm.Responses = []*ChampionshipSignUpResponse{
		signUp("1", team, "Old", ChampionshipEntrantAccepted),
		signUp("2", team, "Old", ChampionshipEntrantPending),
		signUp("3", other, "Other", ChampionshipEntrantAccepted),
		signUp("4", uuid.Nil, "Old", ChampionshipEntrantAccepted), // free text of the same name stays
	}

	if got := c.RenameTeam(team, "Brand New"); got != 2 {
		t.Errorf("expected 2 changed registrations, got %d", got)
	}

	for guid, want := range map[string]string{"1": "Brand New", "2": "Brand New", "3": "Other", "4": "Old"} {
		if got := c.SignUpForm.Response(guid).Team; got != want {
			t.Errorf("registration %s: got %q, want %q", guid, got, want)
		}
	}

	if entrantTeam(c, "1") != "Brand New" || entrantTeam(c, "2") != "Brand New" || entrantTeam(c, "3") != "Other" {
		t.Error("entry list not updated correctly")
	}

	// nothing to do the second time
	if got := c.RenameTeam(team, "Brand New"); got != 0 {
		t.Errorf("unchanged name: got %d changes", got)
	}

	if got := c.RenameTeam(uuid.Nil, "Whatever"); got != 0 {
		t.Errorf("free text registrations are never renamed: got %d", got)
	}
}

func TestChampionship_RenameTeam_FinishedChampionshipKeepsHistory(t *testing.T) {
	c := newChampionshipWithEntrants()
	team := uuid.New()

	c.SignUpForm.Responses = []*ChampionshipSignUpResponse{signUp("1", team, "Old", ChampionshipEntrantAccepted)}

	event := &ChampionshipEvent{ID: uuid.New()}
	event.CompletedTime = time.Now()
	c.Events = []*ChampionshipEvent{event}

	if c.Progress() < 100 {
		t.Fatalf("test setup: the championship must be finished, progress is %f", c.Progress())
	}

	if got := c.RenameTeam(team, "New"); got != 0 {
		t.Errorf("a finished championship must keep the old name, %d changed", got)
	}

	if c.SignUpForm.Response("1").Team != "Old" || entrantTeam(c, "1") != "Old" {
		t.Error("history was rewritten")
	}
}
