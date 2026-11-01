package servermanager

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

// championship stores a championship with a sign up form and eight free slots.
func (e *teamsTestEnv) championship(form smmodel.ChampionshipSignUpForm, responses ...*smmodel.ChampionshipSignUpResponse) *smmodel.Championship {
	e.t.Helper()

	championship := smmodel.NewChampionship("Teams UI " + uuid.New().String()[:8])
	class := smmodel.NewChampionshipClass("Default")
	class.Entrants = serverconfig.EntryList{}

	for i := 0; i < 8; i++ {
		class.Entrants.AddToBackOfGrid(serverconfig.NewEntrant())
	}

	championship.AddClass(class)

	form.Enabled = true
	form.HideCarChoice = true
	form.Responses = responses
	championship.SignUpForm = form

	if err := e.resolver.ResolveStore().UpsertChampionship(championship); err != nil {
		e.t.Fatal(err)
	}

	return championship
}

func (e *teamsTestEnv) response(guid string, team *smmodel.Team, status smmodel.ChampionshipEntrantStatus) *smmodel.ChampionshipSignUpResponse {
	response := &smmodel.ChampionshipSignUpResponse{GUID: guid, Name: "Driver " + guid, Status: status}

	if team != nil {
		response.TeamID, response.Team = team.ID, team.Name
	}

	return response
}

func (e *teamsTestEnv) reloadChampionship(c *smmodel.Championship) *smmodel.Championship {
	e.t.Helper()

	loaded, err := e.resolver.ResolveStore().LoadChampionship(c.ID.String())

	if err != nil {
		e.t.Fatal(err)
	}

	return loaded
}

func TestSignUpPageOffersTheTeamsOfTheDriver(t *testing.T) {
	e, cleanup := newTeamsRenderEnv(t)
	defer cleanup()

	manager := e.resolver.resolveTeamManager()
	handler := e.resolver.resolveChampionshipsHandler()

	member, memberActor := e.account(smmodel.GroupRead)
	_, otherOwner := e.account(smmodel.GroupRead)
	stranger, _ := e.account(smmodel.GroupRead)

	full, err := manager.Create(otherOwner, "Full House", smmodel.TeamPublic)

	if err != nil {
		t.Fatal(err)
	}

	roomy, err := manager.Create(otherOwner, "Roomy Racing", smmodel.TeamPublic)

	if err != nil {
		t.Fatal(err)
	}

	for _, team := range []*smmodel.Team{full, roomy} {
		if _, err := manager.Join(memberActor, team.ID.String(), ""); err != nil {
			t.Fatal(err)
		}
	}

	page := func(c *smmodel.Championship, account *smmodel.Account) *httptest.ResponseRecorder {
		return e.serve(handler.SignUpForm, http.MethodGet, account, map[string]string{"championshipID": c.ID.String()}, nil)
	}

	selectChampionship := e.championship(smmodel.ChampionshipSignUpForm{AskForTeam: true, TeamMode: smmodel.SignUpTeamSelect},
		e.response("76561190000000011", full, smmodel.ChampionshipEntrantAccepted),
		e.response("76561190000000012", full, smmodel.ChampionshipEntrantPending),
		e.response("76561190000000013", roomy, smmodel.ChampionshipEntrantRejected), // does not count
	)

	expectBody(t, "select mode, member", page(selectChampionship, member), http.StatusOK,
		[]string{`id="TeamID"`, `name="TeamID"`, "Roomy Racing (0/2 registered)", "Full House (2/2 registered) - full", `disabled="disabled"`, "create or join a team", "up to 2 drivers"},
		[]string{`id="Team" name="Team"`, "Sign in</a> to register"})

	// the teams of other people are not offered
	expectBody(t, "select mode, a member of no team", page(selectChampionship, stranger), http.StatusOK,
		[]string{`name="TeamID"`, "No team"},
		[]string{"Full House", "Roomy Racing"})

	// only accounts can register: visitors log in first, an account without Steam is asked to connect it
	expectBody(t, "select mode, anonymous", page(selectChampionship, nil), http.StatusFound, nil, nil)

	noSteam, _ := e.account(smmodel.GroupRead)
	noSteam.GUID = ""

	expectBody(t, "select mode, no Steam", page(selectChampionship, noSteam), http.StatusOK,
		[]string{"Connect with Steam", "/accounts/update"},
		[]string{`name="TeamID"`, `name="GUID"`})

	// free text: as before the teams existed
	free := e.championship(smmodel.ChampionshipSignUpForm{AskForTeam: true})

	expectBody(t, "free text mode", page(free, member), http.StatusOK,
		[]string{`id="Team" name="Team"`, "You may leave the team name blank"},
		[]string{`name="TeamID"`, "Roomy Racing"})

	both := e.championship(smmodel.ChampionshipSignUpForm{AskForTeam: true, TeamMode: smmodel.SignUpTeamBoth})

	expectBody(t, "both", page(both, member), http.StatusOK,
		[]string{`name="TeamID"`, `id="Team" name="Team"`, "Roomy Racing (0/2 registered)", "not the name of an existing team"},
		nil)

	unlimited := e.championship(smmodel.ChampionshipSignUpForm{AskForTeam: true, TeamMode: smmodel.SignUpTeamSelect, MaxDriversPerTeam: -1})

	expectBody(t, "no limit", page(unlimited, member), http.StatusOK,
		[]string{"Roomy Racing (0 registered)"},
		// specific strings: a bare "/2" would also match random UUIDs in links of the page
		[]string{"/2 registered", "can register up to"})

	// no team asked for: no field at all
	none := e.championship(smmodel.ChampionshipSignUpForm{TeamMode: smmodel.SignUpTeamSelect})

	expectBody(t, "team not asked for", page(none, member), http.StatusOK,
		nil, []string{`name="TeamID"`, `name="Team"`, "Roomy Racing"})
}

func TestSignUpPageShowsTheFixedTeam(t *testing.T) {
	e, cleanup := newTeamsRenderEnv(t)
	defer cleanup()

	manager := e.resolver.resolveTeamManager()
	handler := e.resolver.resolveChampionshipsHandler()

	member, memberActor := e.account(smmodel.GroupRead)
	member.GUID = "76561190000000021"

	if err := e.resolver.ResolveStore().UpsertAccount(member); err != nil {
		t.Fatal(err)
	}

	team, err := manager.Create(memberActor, "Fixed Team", smmodel.TeamPublic)

	if err != nil {
		t.Fatal(err)
	}

	page := func(c *smmodel.Championship) *httptest.ResponseRecorder {
		return e.serve(handler.SignUpForm, http.MethodGet, member, map[string]string{"championshipID": c.ID.String()}, nil)
	}

	form := smmodel.ChampionshipSignUpForm{AskForTeam: true, TeamMode: smmodel.SignUpTeamBoth}

	// registered with a team of the list: it is shown and sent again, the driver cannot pick another one
	withTeam := e.championship(form, e.response(member.GUID, team, smmodel.ChampionshipEntrantAccepted))

	expectBody(t, "registered with a team", page(withTeam), http.StatusOK,
		[]string{"<strong>Fixed Team</strong>", `type="hidden" name="TeamID" value="` + team.ID.String() + `"`, "your team is fixed"},
		[]string{`<select class="form-control" id="TeamID"`, `id="Team" name="Team"`})

	// the team may be gone: the registration still knows its name
	if err := manager.Delete(memberActor, team.ID.String()); err != nil {
		t.Fatal(err)
	}

	expectBody(t, "registered with a deleted team", page(withTeam), http.StatusOK,
		[]string{"<strong>Fixed Team</strong>", `name="TeamID"`}, nil)

	// free text is fixed as well
	freeText := e.championship(form, &smmodel.ChampionshipSignUpResponse{GUID: member.GUID, Name: "D", Team: "Typed Team", Status: smmodel.ChampionshipEntrantPending})

	expectBody(t, "registered with free text", page(freeText), http.StatusOK,
		[]string{"<strong>Typed Team</strong>", `type="hidden" name="Team" value="Typed Team"`},
		[]string{`name="TeamID"`, `id="Team" name="Team"`})

	// no team: fixed to no team
	noTeam := e.championship(form, e.response(member.GUID, nil, smmodel.ChampionshipEntrantAccepted))

	expectBody(t, "registered without a team", page(noTeam), http.StatusOK,
		[]string{"No team", "your team is fixed"},
		[]string{`type="hidden" name="TeamID"`, `type="hidden" name="Team"`})

	// a rejected driver can choose again
	rejected := e.championship(form, e.response(member.GUID, nil, smmodel.ChampionshipEntrantRejected))

	expectBody(t, "rejected", page(rejected), http.StatusOK,
		[]string{`name="TeamID"`}, []string{"your team is fixed"})
}

func TestEntrantsPageLetsModeratorsChangeTheTeam(t *testing.T) {
	e, cleanup := newTeamsRenderEnv(t)
	defer cleanup()

	manager := e.resolver.resolveTeamManager()
	handler := e.resolver.resolveChampionshipsHandler()

	moderator, _ := e.account(smmodel.GroupWrite)
	_, owner := e.account(smmodel.GroupRead)

	red, err := manager.Create(owner, "Red Team", smmodel.TeamPublic)

	if err != nil {
		t.Fatal(err)
	}

	if _, err := manager.Create(owner, "Secret Blue", smmodel.TeamPrivate); err != nil {
		t.Fatal(err)
	}

	asking := e.championship(smmodel.ChampionshipSignUpForm{AskForTeam: true, TeamMode: smmodel.SignUpTeamSelect},
		e.response("76561190000000031", red, smmodel.ChampionshipEntrantAccepted))

	rec := e.serve(handler.SignedUpEntrants, http.MethodGet, moderator, map[string]string{"championshipID": asking.ID.String()}, nil)

	// the moderator can pick every team, private ones too, and the current one is selected
	expectBody(t, "entrants, team asked", rec, http.StatusOK,
		[]string{"Change team", "?action=team", `<option value="` + red.ID.String() + `" selected="selected">Red Team</option>`, "Secret Blue"},
		nil)

	notAsking := e.championship(smmodel.ChampionshipSignUpForm{},
		e.response("76561190000000032", nil, smmodel.ChampionshipEntrantAccepted))

	rec = e.serve(handler.SignedUpEntrants, http.MethodGet, moderator, map[string]string{"championshipID": notAsking.ID.String()}, nil)

	expectBody(t, "entrants, team not asked", rec, http.StatusOK, []string{"Manage Entrant"}, []string{"Change team", "Secret Blue"})
}

// loginCookies logs the account in the way AccountManager.login does and returns the cookies of the session.
func (e *teamsTestEnv) loginCookies(account *smmodel.Account) []*http.Cookie {
	e.t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	scoped := smweb.WithScope(req, &smweb.Scope{SessionStore: e.resolver.sessionStore})

	session := smweb.Session(scoped)
	session.Values["account_id"] = account.ID.String()

	rec := httptest.NewRecorder()

	if err := session.Save(scoped, rec); err != nil {
		e.t.Fatal(err)
	}

	return rec.Result().Cookies()
}

// post sends a form through the whole router, logged in as the account (nil for anonymous).
func (e *teamsTestEnv) post(router http.Handler, target string, account *smmodel.Account, form url.Values) *httptest.ResponseRecorder {
	e.t.Helper()

	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if account != nil {
		for _, cookie := range e.loginCookies(account) {
			req.AddCookie(cookie)
		}
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	return rec
}

func TestChangingTheTeamOfAnEntrantNeedsWriteAccess(t *testing.T) {
	dir, err := ioutil.TempDir("", "teams-router")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(dir)

	e, cleanup := newTeamsRenderEnv(t)
	defer cleanup()

	e.resolver.resolveAccountManager().Init(teamsTestServerID)

	router := e.resolver.ResolveRouter(http.Dir(dir))
	manager := e.resolver.resolveTeamManager()

	reader, _ := e.account(smmodel.GroupRead)
	writer, _ := e.account(smmodel.GroupWrite)
	_, owner := e.account(smmodel.GroupRead)

	red, _ := manager.Create(owner, "Red", smmodel.TeamPublic)
	blue, _ := manager.Create(owner, "Blue", smmodel.TeamPublic)

	const guid = "76561190000000041"

	championship := e.championship(smmodel.ChampionshipSignUpForm{AskForTeam: true, TeamMode: smmodel.SignUpTeamSelect},
		e.response(guid, red, smmodel.ChampionshipEntrantAccepted))

	target := "/championship/" + championship.ID.String() + "/entrant/" + guid + "?action=team"
	form := url.Values{"teamID": {blue.ID.String()}}

	teamOf := func() uuid.UUID {
		return e.reloadChampionship(championship).SignUpForm.Response(guid).TeamID
	}

	// nobody but writers
	for who, account := range map[string]*smmodel.Account{"anonymous": nil, "reader": reader} {
		rec := e.post(router, target, account, form)

		if rec.Code == http.StatusOK || rec.Code == http.StatusNotFound {
			t.Errorf("%s: expected the request to be refused, got %d", who, rec.Code)
		}

		if teamOf() != red.ID {
			t.Fatalf("%s moved the driver to another team", who)
		}
	}

	rec := e.post(router, target, writer, form)

	if rec.Code != http.StatusFound {
		t.Fatalf("writer: expected a redirect, got %d: %.200s", rec.Code, rec.Body.String())
	}

	if teamOf() != blue.ID {
		t.Errorf("the writer must be able to move the driver, team is %s", teamOf())
	}
}

func TestAcceptingARejectedEntrantRespectsTheTeamLimit(t *testing.T) {
	dir, err := ioutil.TempDir("", "teams-router")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(dir)

	e, cleanup := newTeamsRenderEnv(t)
	defer cleanup()

	e.resolver.resolveAccountManager().Init(teamsTestServerID)

	router := e.resolver.ResolveRouter(http.Dir(dir))
	manager := e.resolver.resolveTeamManager()

	writer, _ := e.account(smmodel.GroupWrite)
	_, owner := e.account(smmodel.GroupRead)

	team, _ := manager.Create(owner, "Crowded", smmodel.TeamPublic)

	const (
		first  = "76561190000000051"
		second = "76561190000000052"
		third  = "76561190000000053"
	)

	championship := e.championship(smmodel.ChampionshipSignUpForm{AskForTeam: true, TeamMode: smmodel.SignUpTeamSelect, RequiresApproval: true},
		e.response(first, team, smmodel.ChampionshipEntrantAccepted),
		e.response(second, team, smmodel.ChampionshipEntrantPending),
		e.response(third, team, smmodel.ChampionshipEntrantRejected),
	)

	accept := func(guid string) *httptest.ResponseRecorder {
		return e.post(router, "/championship/"+championship.ID.String()+"/entrant/"+guid+"?action=accept", writer, nil)
	}

	statusOf := func(guid string) smmodel.ChampionshipEntrantStatus {
		return e.reloadChampionship(championship).SignUpForm.Response(guid).Status
	}

	// the team has two drivers: a third one cannot come back in
	accept(third)

	if statusOf(third) != smmodel.ChampionshipEntrantRejected {
		t.Fatalf("a rejected driver was accepted into a full team: %s", statusOf(third))
	}

	// accepting a pending driver is fine, they are in the team already
	accept(second)

	if statusOf(second) != smmodel.ChampionshipEntrantAccepted {
		t.Errorf("a pending driver must be accepted: %s", statusOf(second))
	}

	// with a higher limit the third driver fits
	loaded := e.reloadChampionship(championship)
	loaded.SignUpForm.MaxDriversPerTeam = 3

	if err := e.resolver.ResolveStore().UpsertChampionship(loaded); err != nil {
		t.Fatal(err)
	}

	accept(third)

	if statusOf(third) != smmodel.ChampionshipEntrantAccepted {
		t.Errorf("with a limit of 3 the driver fits: %s", statusOf(third))
	}
}
