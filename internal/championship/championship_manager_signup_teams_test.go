package championship

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi"
	"github.com/google/uuid"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/teams"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

type signUpTeamsEnv struct {
	t            *testing.T
	cm           *ChampionshipManager
	teams        *teams.Manager
	championship *smmodel.Championship

	mutex   sync.Mutex
	counter int
}

// newSignUpTeamsEnv creates a championship with eight free slots and a sign up form that asks for the team.
func newSignUpTeamsEnv(t *testing.T, form smmodel.ChampionshipSignUpForm) *signUpTeamsEnv {
	t.Helper()

	e := &signUpTeamsEnv{t: t, cm: newTestChampionshipManager(), teams: teams.NewManager(testStore, nil)}
	e.cm.SetTeamDirectory(e.teams)

	form.Enabled = true
	form.HideCarChoice = true // take the first free slot, no matter which car

	e.championship = e.newChampionship(form)

	return e
}

func (e *signUpTeamsEnv) newChampionship(form smmodel.ChampionshipSignUpForm) *smmodel.Championship {
	e.t.Helper()

	championship := smmodel.NewChampionship("Teams " + uuid.New().String())
	class := smmodel.NewChampionshipClass("Default")

	class.Entrants = serverconfig.EntryList{}

	for i := 0; i < 8; i++ {
		class.Entrants.AddToBackOfGrid(serverconfig.NewEntrant())
	}

	championship.AddClass(class)
	championship.SignUpForm = form

	if err := e.cm.UpsertChampionship(championship); err != nil {
		e.t.Fatal(err)
	}

	return championship
}

func (e *signUpTeamsEnv) account() (*smmodel.Account, teams.Actor) {
	e.t.Helper()

	e.mutex.Lock()
	e.counter++
	e.mutex.Unlock()

	account := smmodel.NewAccount(testServerID, "test")
	account.Name = "signup-" + uuid.New().String()

	if err := testStore.UpsertAccount(account); err != nil {
		e.t.Fatal(err)
	}

	return account, teams.Actor{AccountID: account.ID}
}

func (e *signUpTeamsEnv) guid() string {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	e.counter++

	return fmt.Sprintf("7656119%010d", e.counter)
}

// member creates an account and puts it into the team (a private team needs an invite from the owner).
func (e *signUpTeamsEnv) member(team *smmodel.Team, owner teams.Actor) (*smmodel.Account, teams.Actor) {
	e.t.Helper()

	account, actor := e.account()

	if team.IsPrivate() {
		if _, err := e.teams.Invite(owner, team.ID.String(), account.ID); err != nil {
			e.t.Fatal(err)
		}
	}

	if _, err := e.teams.Join(actor, team.ID.String(), ""); err != nil {
		e.t.Fatal(err)
	}

	return account, actor
}

func (e *signUpTeamsEnv) team(name string, vis smmodel.TeamVisibility) (*smmodel.Team, teams.Actor) {
	e.t.Helper()

	_, owner := e.account()

	team, err := e.teams.Create(owner, name+" "+uuid.New().String()[:8], vis)

	if err != nil {
		e.t.Fatal(err)
	}

	return team, owner
}

// signUp posts the sign up form as the account (nil for an anonymous visitor). The account gets the GUID, which the
// form sends as well but which the server does not read from it.
func (e *signUpTeamsEnv) signUp(account *smmodel.Account, guid string, extra url.Values) (*smmodel.ChampionshipSignUpResponse, error) {
	return e.signUpTo(e.championship, account, guid, extra)
}

func (e *signUpTeamsEnv) signUpTo(championship *smmodel.Championship, account *smmodel.Account, guid string, extra url.Values) (*smmodel.ChampionshipSignUpResponse, error) {
	return e.signUpVia(e.cm, championship, account, guid, extra)
}

// signUpVia signs up through a specific manager, as every server slot has its own.
func (e *signUpTeamsEnv) signUpVia(cm *ChampionshipManager, championship *smmodel.Championship, account *smmodel.Account, guid string, extra url.Values) (*smmodel.ChampionshipSignUpResponse, error) {
	e.t.Helper()

	form := url.Values{"Name": {"Driver " + guid}, "GUID": {guid}, "Car": {"car"}}

	for key, values := range extra {
		form[key] = values
	}

	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("championshipID", championship.ID.String())

	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, routeContext))

	if account != nil {
		if account.ID != uuid.Nil {
			copied := *account
			copied.GUID = guid
			account = &copied
		}

		r = smweb.WithAccount(r, account)
	}

	response, _, err := cm.HandleChampionshipSignUp(r)

	return response, err
}

func (e *signUpTeamsEnv) reload() *smmodel.Championship {
	e.t.Helper()

	championship, err := e.cm.LoadChampionship(e.championship.ID.String())

	if err != nil {
		e.t.Fatal(err)
	}

	return championship
}

func (e *signUpTeamsEnv) setStatus(guid string, status smmodel.ChampionshipEntrantStatus) {
	e.t.Helper()

	championship := e.reload()
	championship.SignUpForm.Response(guid).Status = status

	if status == smmodel.ChampionshipEntrantRejected {
		championship.ClearEntrant(guid)
	}

	if err := e.cm.UpsertChampionship(championship); err != nil {
		e.t.Fatal(err)
	}
}

func selectForm() smmodel.ChampionshipSignUpForm {
	return smmodel.ChampionshipSignUpForm{AskForTeam: true, TeamMode: smmodel.SignUpTeamSelect, RequiresApproval: true}
}

func expectValidation(t *testing.T, what string, err error, contains string) {
	t.Helper()

	validation, ok := err.(ValidationError)

	if !ok {
		t.Errorf("%s: expected a ValidationError, got %T %v", what, err, err)
		return
	}

	if contains != "" && !strings.Contains(strings.ToLower(validation.Error()), strings.ToLower(contains)) {
		t.Errorf("%s: expected a message containing %q, got %q", what, contains, validation.Error())
	}
}

func teamEntry(c *smmodel.Championship, guid string) (string, bool) {
	for _, class := range c.Classes {
		for _, entrant := range class.Entrants {
			if entrant.GUID == guid {
				return entrant.Team, true
			}
		}
	}

	return "", false
}

func tid(team *smmodel.Team) url.Values {
	return url.Values{"TeamID": {team.ID.String()}}
}

func TestSignUpTeams_SelectedTeam(t *testing.T) {
	e := newSignUpTeamsEnv(t, smmodel.ChampionshipSignUpForm{AskForTeam: true, TeamMode: smmodel.SignUpTeamSelect})
	team, owner := e.team("Red", smmodel.TeamPublic)
	account, _ := e.member(team, owner)
	guid := e.guid()

	response, err := e.signUp(account, guid, tid(team))

	if err != nil {
		t.Fatal(err)
	}

	if response.TeamID != team.ID || response.Team != team.Name || response.Status != smmodel.ChampionshipEntrantAccepted {
		t.Errorf("unexpected response: %+v", response)
	}

	// the stored registration and the entry list carry the team
	stored := e.reload()

	if r := stored.SignUpForm.Response(guid); r == nil || r.TeamID != team.ID || r.Team != team.Name {
		t.Errorf("registration not stored with its team: %+v", r)
	}

	if got, ok := teamEntry(stored, guid); !ok || got != team.Name {
		t.Errorf("entry list team = %q (found %v), want %q", got, ok, team.Name)
	}
}

func TestSignUpTeams_NoTeamIsAllowed(t *testing.T) {
	e := newSignUpTeamsEnv(t, selectForm())
	account, _ := e.account()

	response, err := e.signUp(account, e.guid(), nil)

	if err != nil || response.TeamID != uuid.Nil || response.Team != "" {
		t.Errorf("a driver without team: %v %+v", err, response)
	}
}

func TestSignUpTeams_RequiresMembership(t *testing.T) {
	e := newSignUpTeamsEnv(t, selectForm())
	public, _ := e.team("Public", smmodel.TeamPublic)
	private, _ := e.team("Private", smmodel.TeamPrivate)
	stranger, _ := e.account()

	// being able to see a team is not enough, the driver has to be a member
	for _, team := range []*smmodel.Team{public, private} {
		_, err := e.signUp(stranger, e.guid(), tid(team))
		expectValidation(t, team.Name, err, "not a member")
	}

	if len(e.reload().SignUpForm.Responses) != 0 {
		t.Error("a refused registration must not be stored")
	}

	// joining makes it work
	if _, err := e.teams.Join(teams.Actor{AccountID: stranger.ID}, public.ID.String(), ""); err != nil {
		t.Fatal(err)
	}

	if _, err := e.signUp(stranger, e.guid(), tid(public)); err != nil {
		t.Errorf("after joining: %s", err)
	}
}

func TestSignUpTeams_Anonymous(t *testing.T) {
	e := newSignUpTeamsEnv(t, selectForm())
	team, _ := e.team("Any", smmodel.TeamPublic)

	// there is no registration without a login at all
	_, err := e.signUp(nil, e.guid(), tid(team))
	expectValidation(t, "anonymous", err, "log in")

	// the dummy account of an open server is not a login either
	_, err = e.signUp(&smmodel.Account{}, e.guid(), tid(team))
	expectValidation(t, "empty account", err, "log in")
}

func TestSignUp_RequiresSteamAccount(t *testing.T) {
	e := newSignUpTeamsEnv(t, smmodel.ChampionshipSignUpForm{})
	account, _ := e.account()

	// a login without GUID (not connected to Steam) cannot register
	_, err := e.signUp(account, "", nil)

	expectValidation(t, "no GUID", err, "connect your account with Steam")

	if len(e.reload().SignUpForm.Responses) != 0 {
		t.Error("a refused registration must not be stored")
	}

	// an anonymous visitor needs a login first
	_, err = e.signUp(nil, e.guid(), nil)

	expectValidation(t, "anonymous", err, "log in")
}

func TestSignUp_GUIDComesFromAccount(t *testing.T) {
	e := newSignUpTeamsEnv(t, smmodel.ChampionshipSignUpForm{})
	account, _ := e.account()

	mine, other := e.guid(), e.guid()

	// the form sends the GUID of somebody else, the one of the account counts
	response, err := e.signUp(account, mine, url.Values{"GUID": {other}})

	if err != nil || response.GUID != mine {
		t.Fatalf("GUID of the account: %v %+v", err, response)
	}

	if stored := e.reload().SignUpForm.Response(mine); stored == nil || e.reload().SignUpForm.Response(other) != nil {
		t.Error("the registration belongs to the GUID of the account")
	}
}

func TestSignUpTeams_InvalidTeams(t *testing.T) {
	e := newSignUpTeamsEnv(t, selectForm())
	team, owner := e.team("Gone", smmodel.TeamPublic)
	account, actor := e.member(team, owner)

	_, err := e.signUp(account, e.guid(), url.Values{"TeamID": {"not-a-uuid"}})
	expectValidation(t, "garbage id", err, "choose a team")

	_, err = e.signUp(account, e.guid(), url.Values{"TeamID": {uuid.New().String()}})
	expectValidation(t, "unknown id", err, "does not exist")

	// a deleted team is gone for new registrations, even for its former members
	if err := e.teams.Delete(owner, team.ID.String()); err != nil {
		t.Fatal(err)
	}

	_, err = e.signUp(account, e.guid(), tid(team))
	expectValidation(t, "deleted team", err, "does not exist")

	_ = actor
}

func TestSignUpTeams_LegacyFreeTextMode(t *testing.T) {
	e := newSignUpTeamsEnv(t, smmodel.ChampionshipSignUpForm{AskForTeam: true})
	team, owner := e.team("Real", smmodel.TeamPublic)
	account, _ := e.member(team, owner)

	response, err := e.signUp(account, e.guid(), url.Values{"Team": {"  Whatever   Racing "}})

	if err != nil || response.Team != "Whatever   Racing" || response.TeamID != uuid.Nil {
		t.Errorf("free text must keep working as before: %v %+v", err, response)
	}

	// the name of a real team is no problem in this mode, it is only text
	response, err = e.signUp(account, e.guid(), url.Values{"Team": {team.Name}})

	if err != nil || response.TeamID != uuid.Nil || response.Team != team.Name {
		t.Errorf("free text with the name of a team: %v %+v", err, response)
	}

	// a team id cannot be sent to a championship that does not offer the selection
	_, err = e.signUp(account, e.guid(), tid(team))
	expectValidation(t, "team id in free text mode", err, "does not offer")
}

func TestSignUpTeams_SelectModeIgnoresFreeText(t *testing.T) {
	e := newSignUpTeamsEnv(t, selectForm())
	account, _ := e.account()

	response, err := e.signUp(account, e.guid(), url.Values{"Team": {"Typed Anyway"}})

	if err != nil || response.Team != "" || response.TeamID != uuid.Nil {
		t.Errorf("free text must be ignored in select mode: %v %+v", err, response)
	}
}

func TestSignUpTeams_BothMode(t *testing.T) {
	form := selectForm()
	form.TeamMode = smmodel.SignUpTeamBoth

	e := newSignUpTeamsEnv(t, form)
	team, owner := e.team("Existing", smmodel.TeamPublic)
	private, _ := e.team("Hidden", smmodel.TeamPrivate)
	account, _ := e.member(team, owner)
	other, _ := e.account()

	response, err := e.signUp(other, e.guid(), url.Values{"Team": {"Garage Heroes"}})

	if err != nil || response.Team != "Garage Heroes" || response.TeamID != uuid.Nil {
		t.Errorf("free text team: %v %+v", err, response)
	}

	for _, name := range []string{team.Name, strings.ToUpper(team.Name), "  " + team.Name + "  ", private.Name} {
		_, err := e.signUp(other, e.guid(), url.Values{"Team": {name}})
		expectValidation(t, "free text colliding with "+name, err, "name of a team")
	}

	// a free text team is never limited: three drivers in the same free text team are fine
	for i := 0; i < 3; i++ {
		if _, err := e.signUp(account, e.guid(), url.Values{"Team": {"Free Riders"}}); err != nil {
			t.Errorf("free text driver %d: %s", i, err)
		}
	}

	// the team of the list wins over the typed name
	response, err = e.signUp(account, e.guid(), url.Values{"TeamID": {team.ID.String()}, "Team": {"Ignored"}})

	if err != nil || response.TeamID != team.ID || response.Team != team.Name {
		t.Errorf("list team with text: %v %+v", err, response)
	}
}

func TestSignUpTeams_TeamNotAskedFor(t *testing.T) {
	e := newSignUpTeamsEnv(t, smmodel.ChampionshipSignUpForm{TeamMode: smmodel.SignUpTeamSelect, RequiresApproval: true})
	team, owner := e.team("Quiet", smmodel.TeamPublic)
	account, _ := e.member(team, owner)

	response, err := e.signUp(account, e.guid(), url.Values{"TeamID": {team.ID.String()}, "Team": {"Typed"}})

	if err != nil || response.Team != "" || response.TeamID != uuid.Nil {
		t.Errorf("without AskForTeam no team is taken: %v %+v", err, response)
	}

	// and the limit does not exist either
	for i := 0; i < 4; i++ {
		if _, err := e.signUp(account, e.guid(), tid(team)); err != nil {
			t.Errorf("registration %d: %s", i, err)
		}
	}
}

func TestSignUpTeams_Limit(t *testing.T) {
	e := newSignUpTeamsEnv(t, selectForm())
	team, owner := e.team("Pair", smmodel.TeamPublic)
	otherTeam, otherOwner := e.team("Other", smmodel.TeamPublic)

	var members []*smmodel.Account

	for i := 0; i < 4; i++ {
		account, _ := e.member(team, owner)
		members = append(members, account)
	}

	otherMember, _ := e.member(otherTeam, otherOwner)

	guids := []string{e.guid(), e.guid(), e.guid()}

	for i := 0; i < 2; i++ {
		if _, err := e.signUp(members[i], guids[i], tid(team)); err != nil {
			t.Fatalf("driver %d: %s", i+1, err)
		}
	}

	// the third driver of the team is refused, with a hint to choose another team
	response, err := e.signUp(members[2], guids[2], tid(team))
	expectValidation(t, "third driver", err, "maximum of 2")

	if response == nil || response.TeamID != team.ID {
		t.Errorf("the response is returned so the form can be shown again: %+v", response)
	}

	if e.reload().SignUpForm.Response(guids[2]) != nil {
		t.Error("a refused registration must not be stored")
	}

	// other teams are not affected, and neither is the third driver without a team
	if _, err := e.signUp(otherMember, e.guid(), tid(otherTeam)); err != nil {
		t.Errorf("another team: %s", err)
	}

	if _, err := e.signUp(members[2], guids[2], nil); err != nil {
		t.Errorf("the third driver without team: %s", err)
	}
}

func TestSignUpTeams_LimitSettings(t *testing.T) {
	for _, c := range []struct {
		name  string
		max   int
		fits  int // how many drivers can register
		total int
	}{
		{"default", 0, 2, 5},
		{"one driver", 1, 1, 3},
		{"three drivers", 3, 3, 5},
		{"unlimited", -1, 5, 5},
	} {
		c := c

		t.Run(c.name, func(t *testing.T) {
			form := selectForm()
			form.MaxDriversPerTeam = c.max

			e := newSignUpTeamsEnv(t, form)
			team, owner := e.team("Limit", smmodel.TeamPublic)

			accepted := 0

			for i := 0; i < c.total; i++ {
				account, _ := e.member(team, owner)

				if _, err := e.signUp(account, e.guid(), tid(team)); err == nil {
					accepted++
				} else if err != nil {
					expectValidation(t, "refused driver", err, "maximum")
				}
			}

			if accepted != c.fits {
				t.Errorf("%d drivers registered, want %d", accepted, c.fits)
			}
		})
	}
}

func TestSignUpTeams_LimitCountsAcceptedAndPending_NotRejected(t *testing.T) {
	e := newSignUpTeamsEnv(t, selectForm())
	team, owner := e.team("Count", smmodel.TeamPublic)

	a1, _ := e.member(team, owner)
	a2, _ := e.member(team, owner)
	a3, _ := e.member(team, owner)
	g1, g2, g3 := e.guid(), e.guid(), e.guid()

	for i, account := range []*smmodel.Account{a1, a2} {
		if _, err := e.signUp(account, []string{g1, g2}[i], tid(team)); err != nil {
			t.Fatal(err)
		}
	}

	e.setStatus(g1, smmodel.ChampionshipEntrantAccepted)

	if _, err := e.signUp(a3, g3, tid(team)); err == nil {
		t.Fatal("one accepted and one pending driver fill the team")
	}

	// the administrator rejects one: the place is free
	e.setStatus(g2, smmodel.ChampionshipEntrantRejected)

	if _, err := e.signUp(a3, g3, tid(team)); err != nil {
		t.Errorf("after a rejection: %s", err)
	}

	// the rejected driver cannot take the place back by registering again
	_, err := e.signUp(a2, g2, tid(team))
	expectValidation(t, "rejected driver comes back", err, "maximum")
}

func TestSignUpTeams_LimitWithoutApproval_NoSlotLeft(t *testing.T) {
	form := selectForm()
	form.RequiresApproval = false

	e := newSignUpTeamsEnv(t, form)
	team, owner := e.team("Fast", smmodel.TeamPublic)

	// fill the entry list: eight free slots, taken by drivers without a team
	filler, _ := e.account()

	for i := 0; i < 8; i++ {
		if _, err := e.signUp(filler, e.guid(), nil); err != nil {
			t.Fatal(err)
		}
	}

	account, _ := e.member(team, owner)
	guid := e.guid()

	response, err := e.signUp(account, guid, tid(team))

	if err != nil || response.Status != smmodel.ChampionshipEntrantRejected {
		t.Fatalf("no slot: expected a rejected registration without error, got %v %+v", err, response)
	}

	// a registration without a slot does not count for the team
	other1, _ := e.member(team, owner)
	other2, _ := e.member(team, owner)

	e.championship = e.reload()

	for _, a := range []*smmodel.Account{other1, other2} {
		resp, err := e.signUp(a, e.guid(), tid(team))

		if err != nil || resp.Status != smmodel.ChampionshipEntrantRejected {
			t.Errorf("expected the team to still have room: %v %+v", err, resp)
		}
	}
}

func TestSignUpTeams_TeamCannotBeChanged(t *testing.T) {
	e := newSignUpTeamsEnv(t, selectForm())
	red, redOwner := e.team("Red", smmodel.TeamPublic)
	blue, blueOwner := e.team("Blue", smmodel.TeamPublic)

	account, actor := e.member(red, redOwner)

	if _, err := e.teams.Join(actor, blue.ID.String(), ""); err != nil {
		t.Fatal(err)
	}

	_ = blueOwner

	guid := e.guid()

	if _, err := e.signUp(account, guid, tid(red)); err != nil {
		t.Fatal(err)
	}

	// pending: other team, no team: all refused
	_, err := e.signUp(account, guid, tid(blue))
	expectValidation(t, "other team", err, "cannot be changed")

	_, err = e.signUp(account, guid, nil)
	expectValidation(t, "no team", err, "cannot be changed")

	// re-registering with the same team (to update the car) is fine and does not take a second place
	for i := 0; i < 3; i++ {
		if _, err := e.signUp(account, guid, url.Values{"Car": {"other_car"}, "TeamID": {red.ID.String()}}); err != nil {
			t.Fatalf("update %d: %s", i, err)
		}
	}

	stored := e.reload()

	if n := stored.SignUpForm.TeamDriverCount(red.ID, ""); n != 1 {
		t.Errorf("the driver is in the team once, got %d", n)
	}

	if r := stored.SignUpForm.Response(guid); r.TeamID != red.ID || r.Car != "other_car" {
		t.Errorf("the update must be stored: %+v", r)
	}

	// accepted: the same
	e.setStatus(guid, smmodel.ChampionshipEntrantAccepted)

	_, err = e.signUp(account, guid, tid(blue))
	expectValidation(t, "accepted, other team", err, "cannot be changed")

	if e.reload().SignUpForm.Response(guid).TeamID != red.ID {
		t.Error("the team must not have changed")
	}
}

func TestSignUpTeams_ChangeAfterRejection(t *testing.T) {
	e := newSignUpTeamsEnv(t, selectForm())
	red, redOwner := e.team("Red", smmodel.TeamPublic)
	blue, _ := e.team("Blue", smmodel.TeamPublic)

	account, actor := e.member(red, redOwner)

	if _, err := e.teams.Join(actor, blue.ID.String(), ""); err != nil {
		t.Fatal(err)
	}

	guid := e.guid()

	if _, err := e.signUp(account, guid, tid(red)); err != nil {
		t.Fatal(err)
	}

	e.setStatus(guid, smmodel.ChampionshipEntrantRejected)

	// a rejected driver is not in a team, so they can register again with another one
	response, err := e.signUp(account, guid, tid(blue))

	if err != nil || response.TeamID != blue.ID {
		t.Errorf("registering again after a rejection: %v %+v", err, response)
	}

	// and from then on blue is fixed
	_, err = e.signUp(account, guid, tid(red))
	expectValidation(t, "locked again", err, "cannot be changed")
}

func TestSignUpTeams_FreeTextCannotBeSwitchedToListTeam(t *testing.T) {
	form := selectForm()
	form.TeamMode = smmodel.SignUpTeamBoth

	e := newSignUpTeamsEnv(t, form)
	team, owner := e.team("Real", smmodel.TeamPublic)
	account, _ := e.member(team, owner)
	guid := e.guid()

	if _, err := e.signUp(account, guid, url.Values{"Team": {"Typed Racing"}}); err != nil {
		t.Fatal(err)
	}

	_, err := e.signUp(account, guid, tid(team))
	expectValidation(t, "free text to list team", err, "cannot be changed")

	_, err = e.signUp(account, guid, url.Values{"Team": {"Another Text"}})
	expectValidation(t, "free text to other text", err, "cannot be changed")

	// the same free text again (any case) is no change
	if _, err := e.signUp(account, guid, url.Values{"Team": {"typed racing"}}); err != nil {
		t.Errorf("same free text: %s", err)
	}
}

func TestSignUpTeams_LeavingTheTeamAfterRegistering(t *testing.T) {
	e := newSignUpTeamsEnv(t, selectForm())
	team, owner := e.team("Quitters", smmodel.TeamPublic)
	account, actor := e.member(team, owner)
	buddy, _ := e.member(team, owner)
	guid := e.guid()

	if _, err := e.signUp(account, guid, tid(team)); err != nil {
		t.Fatal(err)
	}

	if err := e.teams.Leave(actor, team.ID.String()); err != nil {
		t.Fatal(err)
	}

	// the registration keeps its team and it still takes a place of the team
	stored := e.reload()

	if r := stored.SignUpForm.Response(guid); r.TeamID != team.ID || r.Team != team.Name {
		t.Errorf("leaving the team must not change the registration: %+v", r)
	}

	if _, err := e.signUp(buddy, e.guid(), tid(team)); err != nil {
		t.Errorf("the second place is free: %s", err)
	}

	third, _ := e.member(team, owner)

	_, err := e.signUp(third, e.guid(), tid(team))
	expectValidation(t, "the team is full, the one who left still counts", err, "maximum")

	// the one who left can update their registration with the team they are locked into
	if _, err := e.signUp(account, guid, url.Values{"TeamID": {team.ID.String()}, "Car": {"new_car"}}); err != nil {
		t.Errorf("updating the registration after leaving: %s", err)
	}

	// but cannot register another driver for it
	_, err = e.signUp(account, e.guid(), tid(team))
	expectValidation(t, "a new registration of somebody who left", err, "not a member")
}

func TestSignUpTeams_TeamDeletedAfterRegistering(t *testing.T) {
	e := newSignUpTeamsEnv(t, selectForm())
	team, owner := e.team("Doomed", smmodel.TeamPublic)
	account, _ := e.member(team, owner)
	guid := e.guid()

	if _, err := e.signUp(account, guid, tid(team)); err != nil {
		t.Fatal(err)
	}

	if err := e.teams.Delete(owner, team.ID.String()); err != nil {
		t.Fatal(err)
	}

	// the registration keeps the name
	if r := e.reload().SignUpForm.Response(guid); r.Team != team.Name || r.TeamID != team.ID {
		t.Errorf("the registration must keep the team: %+v", r)
	}

	// and can be updated
	response, err := e.signUp(account, guid, url.Values{"TeamID": {team.ID.String()}, "Car": {"c2"}})

	if err != nil || response.Team != team.Name {
		t.Errorf("updating a registration of a deleted team: %v %+v", err, response)
	}
}

func TestSignUpTeams_ParallelRegistrationsRespectTheLimit(t *testing.T) {
	e := newSignUpTeamsEnv(t, selectForm())
	team, owner := e.team("Rush", smmodel.TeamPublic)

	const n = 10

	accounts := make([]*smmodel.Account, n)

	for i := range accounts {
		accounts[i], _ = e.member(team, owner)
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		success int
	)

	for _, account := range accounts {
		wg.Add(1)

		go func(account *smmodel.Account, guid string) {
			defer wg.Done()

			if _, err := e.signUp(account, guid, tid(team)); err == nil {
				mu.Lock()
				success++
				mu.Unlock()
			} else if _, ok := err.(ValidationError); !ok {
				t.Error(err)
			}
		}(account, e.guid())
	}

	wg.Wait()

	if success != 2 {
		t.Errorf("exactly the limit of 2 drivers may get in, %d did", success)
	}

	if got := e.reload().SignUpForm.TeamDriverCount(team.ID, ""); got != 2 {
		t.Errorf("%d registrations stored, want 2 (lost update or limit bypass)", got)
	}
}

func TestSignUpTeams_ParallelRegistrationsOnDifferentManagers(t *testing.T) {
	// every server slot has its own manager, but the championships are shared: the limit must hold across them
	e := newSignUpTeamsEnv(t, selectForm())
	other := newTestChampionshipManager()
	other.SetTeamDirectory(e.teams)

	team, owner := e.team("Slots", smmodel.TeamPublic)

	const n = 8

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		success int
	)

	for i := 0; i < n; i++ {
		account, _ := e.member(team, owner)

		wg.Add(1)

		go func(i int, account *smmodel.Account, guid string) {
			defer wg.Done()

			cm := e.cm

			if i%2 == 1 {
				cm = other
			}

			if _, err := e.signUpVia(cm, e.championship, account, guid, tid(team)); err == nil {
				mu.Lock()
				success++
				mu.Unlock()
			}
		}(i, account, e.guid())
	}

	wg.Wait()

	if success != 2 {
		t.Errorf("exactly 2 drivers may get in across both managers, %d did", success)
	}
}

func TestSignUpTeams_RenameUpdatesRunningChampionships(t *testing.T) {
	e := newSignUpTeamsEnv(t, smmodel.ChampionshipSignUpForm{AskForTeam: true, TeamMode: smmodel.SignUpTeamSelect})
	team, owner := e.team("Before", smmodel.TeamPublic)
	account, _ := e.member(team, owner)
	e.teams.SetRenameHook(e.cm.RenameTeam)

	guid := e.guid()

	if _, err := e.signUp(account, guid, tid(team)); err != nil {
		t.Fatal(err)
	}

	// a second, finished championship keeps the old name
	finished := e.newChampionship(smmodel.ChampionshipSignUpForm{Enabled: true, HideCarChoice: true, AskForTeam: true, TeamMode: smmodel.SignUpTeamSelect})

	if _, err := e.signUpTo(finished, account, guid, tid(team)); err != nil {
		t.Fatal(err)
	}

	finished, err := e.cm.LoadChampionship(finished.ID.String())

	if err != nil {
		t.Fatal(err)
	}

	event := smmodel.NewChampionshipEvent()
	event.CompletedTime = time.Now()
	finished.Events = append(finished.Events, event)

	if err := e.cm.UpsertChampionship(finished); err != nil {
		t.Fatal(err)
	}

	renamed, err := e.teams.Rename(owner, team.ID.String(), "After "+uuid.New().String()[:8])

	if err != nil {
		t.Fatal(err)
	}

	running := e.reload()

	if r := running.SignUpForm.Response(guid); r.Team != renamed.Name || r.TeamID != team.ID {
		t.Errorf("registration not renamed: %+v", r)
	}

	if got, _ := teamEntry(running, guid); got != renamed.Name {
		t.Errorf("entry list team = %q, want %q", got, renamed.Name)
	}

	old, err := e.cm.LoadChampionship(finished.ID.String())

	if err != nil {
		t.Fatal(err)
	}

	if r := old.SignUpForm.Response(guid); r.Team != team.Name {
		t.Errorf("a finished championship must keep the old name, got %q want %q", r.Team, team.Name)
	}

	if got, _ := teamEntry(old, guid); got != team.Name {
		t.Errorf("finished championship entry list team = %q, want %q", got, team.Name)
	}
}

func TestChangeEntrantTeam(t *testing.T) {
	e := newSignUpTeamsEnv(t, selectForm())
	red, redOwner := e.team("Red", smmodel.TeamPublic)
	blue, blueOwner := e.team("Blue", smmodel.TeamPublic)

	var guids []string

	for i := 0; i < 4; i++ {
		var team *smmodel.Team
		var owner teams.Actor

		if i < 2 {
			team, owner = red, redOwner
		} else {
			team, owner = blue, blueOwner
		}

		account, _ := e.member(team, owner)
		guid := e.guid()
		guids = append(guids, guid)

		if _, err := e.signUp(account, guid, tid(team)); err != nil {
			t.Fatal(err)
		}

		e.setStatus(guid, smmodel.ChampionshipEntrantAccepted)
	}

	// put the accepted drivers into the entry list as the accept action does
	championship := e.reload()

	for _, guid := range guids {
		response := championship.SignUpForm.Response(guid)

		if found, _, err := e.cm.AddEntrantFromSessionData(championship, response, true, true); err != nil || !found {
			t.Fatalf("could not add %s to the entry list: %v", guid, err)
		}
	}

	if err := e.cm.UpsertChampionship(championship); err != nil {
		t.Fatal(err)
	}

	id := e.championship.ID.String()

	// moving into a full team works for moderators, but they get a warning
	warning, err := e.cm.ChangeEntrantTeam(id, guids[0], blue.ID.String(), "")

	if err != nil {
		t.Fatal(err)
	}

	if warning == nil || !strings.Contains(warning.String(), "more than the 2") {
		t.Errorf("expected a warning about the limit, got %v", warning)
	}

	stored := e.reload()

	if r := stored.SignUpForm.Response(guids[0]); r.TeamID != blue.ID || r.Team != blue.Name {
		t.Errorf("registration not moved: %+v", r)
	}

	if got, _ := teamEntry(stored, guids[0]); got != blue.Name {
		t.Errorf("entry list team = %q, want %q", got, blue.Name)
	}

	if got, _ := teamEntry(stored, guids[1]); got != red.Name {
		t.Errorf("another driver must stay: %q", got)
	}

	// into a team with room: no warning
	if warning, err := e.cm.ChangeEntrantTeam(id, guids[0], red.ID.String(), ""); err != nil || warning != nil {
		t.Errorf("moving back: %v %v", err, warning)
	}

	// free text and no team
	if _, err := e.cm.ChangeEntrantTeam(id, guids[0], "", "  Custom   Team "); err != nil {
		t.Fatal(err)
	}

	r := e.reload().SignUpForm.Response(guids[0])

	if r.TeamID != uuid.Nil || r.Team != "Custom Team" {
		t.Errorf("free text move: %+v", r)
	}

	if _, err := e.cm.ChangeEntrantTeam(id, guids[0], "", ""); err != nil {
		t.Fatal(err)
	}

	if r := e.reload().SignUpForm.Response(guids[0]); r.Team != "" || r.TeamID != uuid.Nil {
		t.Errorf("removing the team: %+v", r)
	}

	// the driver is not locked by the moderator action: the rules still apply to the driver
	member, _ := e.member(red, redOwner)

	_, err = e.signUp(member, guids[0], tid(red))
	expectValidation(t, "driver cannot move themselves after a moderator moved them", err, "cannot be changed")
}

func TestChangeEntrantTeam_Errors(t *testing.T) {
	e := newSignUpTeamsEnv(t, selectForm())
	team, owner := e.team("Err", smmodel.TeamPublic)
	account, _ := e.member(team, owner)
	guid := e.guid()

	if _, err := e.signUp(account, guid, tid(team)); err != nil {
		t.Fatal(err)
	}

	id := e.championship.ID.String()

	_, err := e.cm.ChangeEntrantTeam(id, "76561190000000000", team.ID.String(), "")
	expectValidation(t, "unregistered driver", err, "not registered")

	_, err = e.cm.ChangeEntrantTeam(id, guid, "garbage", "")
	expectValidation(t, "garbage id", err, "unknown team")

	_, err = e.cm.ChangeEntrantTeam(id, guid, uuid.New().String(), "")
	expectValidation(t, "unknown team", err, "does not exist")

	if err := e.teams.Delete(owner, team.ID.String()); err != nil {
		t.Fatal(err)
	}

	_, err = e.cm.ChangeEntrantTeam(id, guid, team.ID.String(), "")
	expectValidation(t, "deleted team", err, "does not exist")

	if _, err := e.cm.ChangeEntrantTeam(uuid.New().String(), guid, "", ""); err == nil {
		t.Error("an unknown championship must fail")
	}

	if r := e.reload().SignUpForm.Response(guid); r.TeamID != team.ID {
		t.Errorf("a failed change must leave the registration alone: %+v", r)
	}

	// without a team directory moderators can still use free text but not the list
	bare := newTestChampionshipManager()

	_, err = bare.ChangeEntrantTeam(id, guid, uuid.New().String(), "")
	expectValidation(t, "no directory", err, "not available")

	if _, err := bare.ChangeEntrantTeam(id, guid, "", "Text"); err != nil {
		t.Errorf("free text without a directory: %s", err)
	}
}

func TestCheckAcceptTeamLimit(t *testing.T) {
	team := uuid.New()

	championship := smmodel.NewChampionship("Accept")
	championship.SignUpForm.Responses = []*smmodel.ChampionshipSignUpResponse{
		{GUID: "1", TeamID: team, Team: "T", Status: smmodel.ChampionshipEntrantAccepted},
		{GUID: "2", TeamID: team, Team: "T", Status: smmodel.ChampionshipEntrantPending},
		{GUID: "3", TeamID: team, Team: "T", Status: smmodel.ChampionshipEntrantRejected},
		{GUID: "4", Team: "Free", Status: smmodel.ChampionshipEntrantRejected},
	}

	responses := championship.SignUpForm.Responses

	// a pending registration is already counted, accepting it changes nothing
	if err := CheckAcceptTeamLimit(championship, responses[1]); err != nil {
		t.Errorf("accepting a pending registration: %s", err)
	}

	if err := CheckAcceptTeamLimit(championship, responses[0]); err != nil {
		t.Errorf("already accepted: %s", err)
	}

	// a rejected one would be the third driver
	expectValidation(t, "re-accepting into a full team", CheckAcceptTeamLimit(championship, responses[2]), "maximum of 2")

	// free text teams are never limited
	if err := CheckAcceptTeamLimit(championship, responses[3]); err != nil {
		t.Errorf("free text: %s", err)
	}

	// once there is room, it works
	responses[1].Status = smmodel.ChampionshipEntrantRejected

	if err := CheckAcceptTeamLimit(championship, responses[2]); err != nil {
		t.Errorf("with room in the team: %s", err)
	}

	// and a larger limit makes room
	responses[1].Status = smmodel.ChampionshipEntrantPending
	championship.SignUpForm.MaxDriversPerTeam = 3

	if err := CheckAcceptTeamLimit(championship, responses[2]); err != nil {
		t.Errorf("with a limit of 3: %s", err)
	}
}
