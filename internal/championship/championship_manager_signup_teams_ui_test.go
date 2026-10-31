package championship

import (
	"net/url"
	"testing"

	"github.com/google/uuid"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

func TestParseSignUpTeamSettings(t *testing.T) {
	for _, c := range []struct {
		name, mode, max string
		wantMode        smmodel.SignUpTeamMode
		wantMax         int
	}{
		{"defaults", "", "", smmodel.SignUpTeamFreeText, 0},
		{"free text", "", "0", smmodel.SignUpTeamFreeText, 0},
		{"select", "select", "2", smmodel.SignUpTeamSelect, 2},
		{"both", "both", "3", smmodel.SignUpTeamBoth, 3},
		{"padded", " select ", " 4 ", smmodel.SignUpTeamSelect, 4},
		{"unlimited", "select", "-1", smmodel.SignUpTeamSelect, -1},
		{"any negative is unlimited", "select", "-50", smmodel.SignUpTeamSelect, -1},
		{"highest", "select", "1000", smmodel.SignUpTeamSelect, 1000},
		{"too high falls back", "select", "1001", smmodel.SignUpTeamSelect, 0},
		{"text falls back", "select", "many", smmodel.SignUpTeamSelect, 0},
		{"decimal falls back", "select", "2.5", smmodel.SignUpTeamSelect, 0},
		{"unknown mode is free text", "dropdown", "2", smmodel.SignUpTeamFreeText, 2},
		{"mode is case sensitive like the stored value", "SELECT", "2", smmodel.SignUpTeamFreeText, 2},
		{"huge number falls back", "select", "99999999999999999999", smmodel.SignUpTeamSelect, 0},
	} {
		mode, max := parseSignUpTeamSettings(c.mode, c.max)

		if mode != c.wantMode || max != c.wantMax {
			t.Errorf("%s: got (%q, %d), want (%q, %d)", c.name, mode, max, c.wantMode, c.wantMax)
		}
	}
}

func TestSignUpTeamOptions(t *testing.T) {
	e := newSignUpTeamsEnv(t, selectForm())

	zebra, zebraOwner := e.team("Zebra", smmodel.TeamPublic)
	alpha, alphaOwner := e.team("Alpha", smmodel.TeamPublic)
	e.team("Not mine", smmodel.TeamPublic)

	account, actor := e.member(zebra, zebraOwner)

	if _, err := e.teams.Join(actor, alpha.ID.String(), ""); err != nil {
		t.Fatal(err)
	}

	// two drivers are registered for alpha (which fills it), none for zebra
	for i := 0; i < 2; i++ {
		driver, _ := e.member(alpha, alphaOwner)

		if _, err := e.signUp(driver, e.guid(), tid(alpha)); err != nil {
			t.Fatal(err)
		}
	}

	options, err := e.cm.SignUpTeamOptions(e.reload(), account)

	if err != nil {
		t.Fatal(err)
	}

	// only the teams of the account, sorted by name (Alpha is capitalised, Zebra too, case does not decide)
	if len(options) != 2 || options[0].Team.ID != alpha.ID || options[1].Team.ID != zebra.ID {
		t.Fatalf("expected alpha and zebra, got %+v", options)
	}

	if options[0].Drivers != 2 || !options[0].Full {
		t.Errorf("alpha has two drivers and is full: %+v", options[0])
	}

	if options[1].Drivers != 0 || options[1].Full {
		t.Errorf("zebra is empty: %+v", options[1])
	}

	// the limit decides what is full
	unlimited := e.reload()
	unlimited.SignUpForm.MaxDriversPerTeam = -1

	if options, _ := e.cm.SignUpTeamOptions(unlimited, account); options[0].Full {
		t.Error("nothing is full without a limit")
	}

	unlimited.SignUpForm.MaxDriversPerTeam = 3

	if options, _ := e.cm.SignUpTeamOptions(unlimited, account); options[0].Full {
		t.Error("two of three drivers is not full")
	}

	unlimited.SignUpForm.MaxDriversPerTeam = 1

	if options, _ := e.cm.SignUpTeamOptions(unlimited, account); !options[0].Full {
		t.Error("two of one driver is full")
	}
}

func TestSignUpTeamOptions_Empty(t *testing.T) {
	selectEnv := newSignUpTeamsEnv(t, selectForm())
	team, owner := selectEnv.team("Mine", smmodel.TeamPublic)
	account, _ := selectEnv.member(team, owner)

	for name, c := range map[string]struct {
		env     *signUpTeamsEnv
		account *smmodel.Account
	}{
		"anonymous":       {selectEnv, nil},
		"empty account":   {selectEnv, &smmodel.Account{}},
		"free text mode":  {newSignUpTeamsEnv(t, smmodel.ChampionshipSignUpForm{AskForTeam: true}), account},
		"team not asked":  {newSignUpTeamsEnv(t, smmodel.ChampionshipSignUpForm{TeamMode: smmodel.SignUpTeamSelect}), account},
		"no team of mine": {selectEnv, func() *smmodel.Account { a, _ := selectEnv.account(); return a }()},
	} {
		options, err := c.env.cm.SignUpTeamOptions(c.env.reload(), c.account)

		if err != nil || len(options) != 0 {
			t.Errorf("%s: expected no options, got %v %v", name, options, err)
		}
	}

	// without a directory there are no teams to offer
	bare := newTestChampionshipManager()

	if options, err := bare.SignUpTeamOptions(selectEnv.reload(), account); err != nil || len(options) != 0 {
		t.Errorf("without a directory: %v %v", options, err)
	}
}

func TestSignUpTeamOptions_OnlyCurrentMemberships(t *testing.T) {
	e := newSignUpTeamsEnv(t, selectForm())
	team, owner := e.team("Left", smmodel.TeamPublic)
	account, actor := e.member(team, owner)

	if err := e.teams.Leave(actor, team.ID.String()); err != nil {
		t.Fatal(err)
	}

	if options, _ := e.cm.SignUpTeamOptions(e.reload(), account); len(options) != 0 {
		t.Errorf("a team that was left is not offered: %+v", options)
	}

	// and a deleted team is not offered either
	again, againOwner := e.team("Deleted", smmodel.TeamPublic)
	member, _ := e.member(again, againOwner)

	if err := e.teams.Delete(againOwner, again.ID.String()); err != nil {
		t.Fatal(err)
	}

	if options, _ := e.cm.SignUpTeamOptions(e.reload(), member); len(options) != 0 {
		t.Errorf("a deleted team is not offered: %+v", options)
	}
}

func TestSignUpTeamLock(t *testing.T) {
	e := newSignUpTeamsEnv(t, selectForm())
	team, owner := e.team("Fixed", smmodel.TeamPublic)
	account, _ := e.member(team, owner)
	guid := e.guid()

	if SignUpTeamLock(e.reload(), guid) != nil {
		t.Error("an unregistered driver can choose")
	}

	if _, err := e.signUp(account, guid, tid(team)); err != nil {
		t.Fatal(err)
	}

	lock := SignUpTeamLock(e.reload(), guid)

	if lock == nil || lock.TeamID != team.ID || !lock.HasListTeam() {
		t.Fatalf("a registered driver is locked: %+v", lock)
	}

	if SignUpTeamLock(e.reload(), "76561190000009999") != nil {
		t.Error("only the registered driver is locked")
	}

	e.setStatus(guid, smmodel.ChampionshipEntrantRejected)

	if SignUpTeamLock(e.reload(), guid) != nil {
		t.Error("a rejected driver can choose again")
	}

	// registered without a team: still locked, to no team
	noTeam := e.guid()

	if _, err := e.signUp(account, noTeam, url.Values{}); err != nil {
		t.Fatal(err)
	}

	if lock := SignUpTeamLock(e.reload(), noTeam); lock == nil || lock.HasListTeam() || lock.Team != "" {
		t.Errorf("a driver without a team is locked to no team: %+v", lock)
	}
}

func TestAllTeams(t *testing.T) {
	e := newSignUpTeamsEnv(t, selectForm())
	public, _ := e.team("Visible", smmodel.TeamPublic)
	private, _ := e.team("Secret", smmodel.TeamPrivate)
	gone, goneOwner := e.team("Gone", smmodel.TeamPublic)

	if err := e.teams.Delete(goneOwner, gone.ID.String()); err != nil {
		t.Fatal(err)
	}

	all, err := e.cm.AllTeams()

	if err != nil {
		t.Fatal(err)
	}

	found := map[uuid.UUID]bool{}

	for _, team := range all {
		found[team.ID] = true
	}

	if !found[public.ID] || !found[private.ID] || found[gone.ID] {
		t.Errorf("moderators see all teams that were not deleted, private ones too: %v", found)
	}

	bare := newTestChampionshipManager()

	if all, err := bare.AllTeams(); err != nil || len(all) != 0 {
		t.Errorf("without a directory: %v %v", all, err)
	}
}
