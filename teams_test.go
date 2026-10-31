package servermanager

import (
	"context"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi"
	"github.com/google/uuid"

	smconfig "github.com/wernersbacher/ACPitwall/internal/config"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/teams"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
	"github.com/wernersbacher/ACPitwall/internal/web/webtest"
)

const teamsTestServerID = smmodel.ServerID("teams-test")

type teamsTestEnv struct {
	t        *testing.T
	resolver *Resolver
	open     *smmodel.Account
	n        int
}

func newTeamsTestEnv(t *testing.T, config *smconfig.Configuration) (*teamsTestEnv, func()) {
	t.Helper()

	resolver, _, cleanup := newTestResolver(t, config)
	resolver.Servers()

	return &teamsTestEnv{t: t, resolver: resolver, open: &smmodel.Account{Name: smmodel.OpenAccountName}}, cleanup
}

func (e *teamsTestEnv) account(group smmodel.Group) (*smmodel.Account, teams.Actor) {
	e.t.Helper()

	e.n++

	account := smmodel.NewAccount(teamsTestServerID, "test")
	account.Name = "teams-user-" + uuid.New().String()[:8]
	account.Groups[teamsTestServerID] = group

	if err := e.resolver.ResolveStore().UpsertAccount(account); err != nil {
		e.t.Fatal(err)
	}

	moderator := group == smmodel.GroupWrite || group == smmodel.GroupDelete || group == smmodel.GroupAdmin

	return account, teams.Actor{AccountID: account.ID, Moderator: moderator}
}

// serve calls a handler function as the account (nil for an anonymous visitor).
func (e *teamsTestEnv) serve(fn http.HandlerFunc, method string, account *smmodel.Account, params map[string]string, form url.Values) *httptest.ResponseRecorder {
	e.t.Helper()

	req := httptest.NewRequest(method, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	scope := webtest.RequestScope()
	scope.ServerID = teamsTestServerID
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

func expectBody(t *testing.T, what string, rec *httptest.ResponseRecorder, status int, contains []string, excludes []string) {
	t.Helper()

	if rec.Code != status {
		t.Errorf("%s: expected status %d, got %d: %.300s", what, status, rec.Code, rec.Body.String())
		return
	}

	body := rec.Body.String()

	for _, s := range contains {
		if !strings.Contains(body, s) {
			t.Errorf("%s: the page must contain %q", what, s)
		}
	}

	for _, s := range excludes {
		if strings.Contains(body, s) {
			t.Errorf("%s: the page must not contain %q", what, s)
		}
	}
}

func TestTeamsPagesRender(t *testing.T) {
	e, cleanup := newTeamsTestEnv(t, &smconfig.Configuration{})
	defer cleanup()

	manager := e.resolver.resolveTeamManager()
	handler := e.resolver.resolveTeamsHandler()

	ownerAccount, owner := e.account(smmodel.GroupRead)
	adminAccount, admin := e.account(smmodel.GroupRead)
	memberAccount, member := e.account(smmodel.GroupRead)
	strangerAccount, _ := e.account(smmodel.GroupRead)
	moderatorAccount, _ := e.account(smmodel.GroupWrite)

	// the name contains HTML, which must come out escaped
	public, err := manager.Create(owner, "<b>Loud</b> Public", smmodel.TeamPublic)

	if err != nil {
		t.Fatal(err)
	}

	private, err := manager.Create(owner, "Quiet Private", smmodel.TeamPrivate)

	if err != nil {
		t.Fatal(err)
	}

	for _, a := range []teams.Actor{admin, member} {
		if _, err := manager.Join(a, public.ID.String(), ""); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := manager.SetAdmin(owner, public.ID.String(), admin.AccountID, true); err != nil {
		t.Fatal(err)
	}

	list := func(account *smmodel.Account) *httptest.ResponseRecorder {
		return e.serve(handler.List, http.MethodGet, account, nil, nil)
	}

	view := func(account *smmodel.Account, team *smmodel.Team) *httptest.ResponseRecorder {
		return e.serve(handler.View, http.MethodGet, account, map[string]string{"teamID": team.ID.String()}, nil)
	}

	// list
	expectBody(t, "list, anonymous", list(nil), http.StatusOK,
		[]string{"&lt;b&gt;Loud&lt;/b&gt; Public", "Sign in"},
		[]string{"Quiet Private", "<b>Loud</b>", "<h2>Create a team</h2>"})

	expectBody(t, "list, stranger", list(strangerAccount), http.StatusOK,
		[]string{"<h2>Create a team</h2>", "&lt;b&gt;Loud&lt;/b&gt; Public", "You are not in a team yet"},
		[]string{"Quiet Private"})

	expectBody(t, "list, owner", list(ownerAccount), http.StatusOK,
		[]string{"Your teams", "Quiet Private", "&lt;b&gt;Loud&lt;/b&gt; Public"},
		[]string{"You are not in a team yet"})

	expectBody(t, "list, moderator", list(moderatorAccount), http.StatusOK, []string{"Quiet Private"}, nil)

	// team page
	expectBody(t, "view, stranger", view(strangerAccount, public), http.StatusOK,
		[]string{"Join team", "&lt;b&gt;Loud&lt;/b&gt; Public", "Public team"},
		[]string{"Leave team", "Delete team", "Rename", "<b>Loud</b>"})

	expectBody(t, "view, member", view(memberAccount, public), http.StatusOK,
		[]string{"Leave team"},
		[]string{"Join team", "Delete team", "Rename", "Make owner"})

	expectBody(t, "view, admin", view(adminAccount, public), http.StatusOK,
		[]string{"Leave team", "Rename", "Invite", "Remove"},
		[]string{"Delete team", "Make owner", "Make admin"})

	expectBody(t, "view, owner", view(ownerAccount, public), http.StatusOK,
		[]string{"Leave team", "Rename", "Delete team", "Make owner", "Make admin", "Remove admin"},
		[]string{"Join team"})

	// a moderator who is not a member can manage the team and may join it like everybody else
	expectBody(t, "view, moderator", view(moderatorAccount, public), http.StatusOK,
		[]string{"Delete team", "Rename", "Make owner", "Join team"},
		[]string{"Leave team"})

	expectBody(t, "view, anonymous", view(nil, public), http.StatusOK,
		[]string{"Sign in"},
		[]string{"Join team", "Leave team", "Rename"})

	// a private team does not exist for people who are not in it
	expectBody(t, "private team, stranger", view(strangerAccount, private), http.StatusNotFound, nil, nil)
	expectBody(t, "private team, anonymous", view(nil, private), http.StatusNotFound, nil, nil)
	expectBody(t, "private team, owner", view(ownerAccount, private), http.StatusOK, []string{"Private team", "Quiet Private"}, nil)

	// the invite link page
	link, err := manager.Invite(owner, private.ID.String(), uuid.Nil)

	if err != nil {
		t.Fatal(err)
	}

	invitePage := func(account *smmodel.Account) *httptest.ResponseRecorder {
		return e.serve(handler.InvitePage, http.MethodGet, account, map[string]string{"token": link.Token}, nil)
	}

	expectBody(t, "invite page, stranger", invitePage(strangerAccount), http.StatusOK,
		[]string{"Join Quiet Private", "/teams/invite/" + link.Token}, []string{"Sign in"})

	expectBody(t, "invite page, anonymous", invitePage(nil), http.StatusOK,
		[]string{"Sign in"}, []string{"/teams/invite/" + link.Token})

	expectBody(t, "invite page, member", invitePage(ownerAccount), http.StatusOK,
		[]string{"member of this team already"}, []string{"/teams/invite/" + link.Token})

	// the owner sees the invite link on the team page, a member does not
	expectBody(t, "invite link, owner", view(ownerAccount, private), http.StatusOK, []string{"/teams/invite/" + link.Token}, nil)
	expectBody(t, "invite link, member", view(memberAccount, public), http.StatusOK, nil, []string{link.Token})
}

func TestAccountsPageShowsTheTeamOptions(t *testing.T) {
	e, cleanup := newTeamsTestEnv(t, &smconfig.Configuration{})
	defer cleanup()

	admin, _ := e.account(smmodel.GroupAdmin)
	handler := e.resolver.resolveAccountHandler()

	rec := e.serve(handler.ManageAccounts, http.MethodGet, admin, nil, nil)

	expectBody(t, "accounts page", rec, http.StatusOK,
		[]string{"Creating teams", "/accounts/team-options", `<option value="enabled" selected>`},
		[]string{`<option value="disabled" selected>`})

	// once creation is turned off the page shows it
	save := e.serve(handler.SetTeamOptions, http.MethodPost, admin, nil, url.Values{"creation": {"disabled"}, "max": {"7"}})

	if save.Code != http.StatusFound {
		t.Fatalf("saving failed: %d", save.Code)
	}

	expectBody(t, "accounts page after saving", e.serve(handler.ManageAccounts, http.MethodGet, admin, nil, nil), http.StatusOK,
		[]string{`<option value="disabled" selected>`, `value="7"`},
		[]string{`<option value="enabled" selected>`})
}

func TestTeamOptionsReachTheTeamManager(t *testing.T) {
	e, cleanup := newTeamsTestEnv(t, &smconfig.Configuration{})
	defer cleanup()

	handler := e.resolver.resolveAccountHandler()
	manager := e.resolver.resolveTeamManager()
	admin, _ := e.account(smmodel.GroupAdmin)
	_, user := e.account(smmodel.GroupRead)

	save := func(creation, max string) {
		rec := e.serve(handler.SetTeamOptions, http.MethodPost, admin, nil, url.Values{"creation": {creation}, "max": {max}})

		if rec.Code != http.StatusFound {
			t.Fatalf("saving the team options failed: %d", rec.Code)
		}
	}

	if _, err := manager.Create(user, "Before Options", smmodel.TeamPublic); err != nil {
		t.Fatalf("creation is allowed by default: %s", err)
	}

	save("disabled", "5")

	if _, err := manager.Create(user, "Blocked", smmodel.TeamPublic); err != smmodel.ErrTeamCreationBlocked {
		t.Errorf("expected ErrTeamCreationBlocked, got %v", err)
	}

	save("enabled", "2")

	if _, err := manager.Create(user, "Second", smmodel.TeamPublic); err != nil {
		t.Fatalf("the second team: %s", err)
	}

	if _, err := manager.Create(user, "Third", smmodel.TeamPublic); err != smmodel.ErrTeamLimitReached {
		t.Errorf("expected ErrTeamLimitReached with a limit of 2, got %v", err)
	}

	save("enabled", "-1")

	if _, err := manager.Create(user, "Third", smmodel.TeamPublic); err != nil {
		t.Errorf("unlimited: %s", err)
	}
}

func TestDeletingAnAccountTakesItOutOfItsTeams(t *testing.T) {
	e, cleanup := newTeamsTestEnv(t, &smconfig.Configuration{})
	defer cleanup()

	manager := e.resolver.resolveTeamManager()
	accounts := e.resolver.resolveAccountHandler()

	admin, _ := e.account(smmodel.GroupAdmin)
	victim, victimActor := e.account(smmodel.GroupRead)
	_, buddy := e.account(smmodel.GroupRead)

	owned, err := manager.Create(victimActor, "Victims Team", smmodel.TeamPublic)

	if err != nil {
		t.Fatal(err)
	}

	alone, err := manager.Create(victimActor, "Victims Solo", smmodel.TeamPrivate)

	if err != nil {
		t.Fatal(err)
	}

	if _, err := manager.Join(buddy, owned.ID.String(), ""); err != nil {
		t.Fatal(err)
	}

	rec := e.serve(accounts.DeleteAccount, http.MethodPost, admin, map[string]string{"id": victim.ID.String()}, nil)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected the usual redirect, got %d", rec.Code)
	}

	reloaded, err := manager.Lookup(owned.ID.String())

	if err != nil || reloaded.IsDeleted() || reloaded.IsMember(victim.ID) || !reloaded.IsOwner(buddy.AccountID) {
		t.Errorf("the ownership must move on to the other member: %v %+v", err, reloaded)
	}

	soloTeam, err := manager.Lookup(alone.ID.String())

	if err != nil || !soloTeam.IsDeleted() {
		t.Errorf("a team without members must be deleted: %v %+v", err, soloTeam)
	}
}

// a championship asks the team manager about the team, on every server slot
func TestChampionshipManagersKnowTheTeams(t *testing.T) {
	dir, err := ioutil.TempDir("", "teams-slots-install")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(dir)

	e, cleanup := newTeamsTestEnv(t, &smconfig.Configuration{Servers: []smconfig.ServerSlotConfig{
		{ID: "a", Name: "Server A", InstallPath: filepath.Join(dir, "a")},
		{ID: "b", Name: "Server B", InstallPath: filepath.Join(dir, "b")},
	}})
	defer cleanup()

	manager := e.resolver.resolveTeamManager()
	_, owner := e.account(smmodel.GroupRead)

	team, err := manager.Create(owner, "Everywhere", smmodel.TeamPublic)

	if err != nil {
		t.Fatal(err)
	}

	championship := smmodel.NewChampionship("Slots")
	championship.SignUpForm.Enabled = true
	championship.SignUpForm.Responses = []*smmodel.ChampionshipSignUpResponse{
		{GUID: "76561190000000001", Name: "Driver", Status: smmodel.ChampionshipEntrantAccepted},
		{GUID: "76561190000000002", Name: "Driver 2", TeamID: team.ID, Team: team.Name, Status: smmodel.ChampionshipEntrantAccepted},
	}

	if err := e.resolver.ResolveStore().UpsertChampionship(championship); err != nil {
		t.Fatal(err)
	}

	for _, slot := range e.resolver.Servers().All() {
		if _, err := slot.ChampionshipManager.ChangeEntrantTeam(championship.ID.String(), "76561190000000001", team.ID.String(), ""); err != nil {
			t.Errorf("slot %s does not know the teams: %s", slot.ID(), err)
		}

		// back to no team for the next slot
		if _, err := slot.ChampionshipManager.ChangeEntrantTeam(championship.ID.String(), "76561190000000001", "", ""); err != nil {
			t.Errorf("slot %s: %s", slot.ID(), err)
		}
	}

	// renaming the team updates the registration (the championship is not finished)
	if _, err := manager.Rename(owner, team.ID.String(), "Renamed Everywhere"); err != nil {
		t.Fatal(err)
	}

	stored, err := e.resolver.ResolveStore().LoadChampionship(championship.ID.String())

	if err != nil {
		t.Fatal(err)
	}

	if got := stored.SignUpForm.Response("76561190000000002").Team; got != "Renamed Everywhere" {
		t.Errorf("the registration must follow the rename, got %q", got)
	}

	if got := stored.SignUpForm.Response("76561190000000001").Team; got != "" {
		t.Errorf("a registration without the team must not change, got %q", got)
	}
}

func teamRoutes(t *testing.T, resolver *Resolver, dir string) map[string][]string {
	t.Helper()

	router, ok := resolver.ResolveRouter(http.Dir(dir)).(chi.Routes)

	if !ok {
		t.Fatal("router does not implement chi.Routes")
	}

	routes := make(map[string][]string)

	err := chi.Walk(router, func(method string, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(route, "/teams") {
			routes[route] = append(routes[route], method)
		}

		return nil
	})

	if err != nil {
		t.Fatal(err)
	}

	return routes
}

// everything that changes a team is a POST, only the pages can be fetched
func TestTeamRoutesAreReadOnlyViaGet(t *testing.T) {
	dir, err := ioutil.TempDir("", "teams-router")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(dir)

	e, cleanup := newTeamsTestEnv(t, &smconfig.Configuration{})
	defer cleanup()

	pages := map[string]bool{"/teams": true, "/teams/{teamID}": true, "/teams/invite/{token}": true}

	routes := teamRoutes(t, e.resolver, dir)

	if len(routes) < 15 {
		t.Fatalf("expected the team routes, got %v", routes)
	}

	for route, methods := range routes {
		for _, method := range methods {
			switch {
			case method == http.MethodGet && pages[route]:
			case method == http.MethodPost && route != "/teams":
			default:
				t.Errorf("unexpected route %s %s", method, route)
			}
		}
	}

	for route := range pages {
		if len(routes[route]) == 0 {
			t.Errorf("missing page %s", route)
		}
	}
}

// the team pages need read access like the rest of the server: on a closed server visitors are sent to the login
func TestTeamRoutesNeedReadAccess(t *testing.T) {
	dir, err := ioutil.TempDir("", "teams-router")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(dir)

	e, cleanup := newTeamsTestEnv(t, &smconfig.Configuration{})
	defer cleanup()

	e.resolver.resolveAccountManager().Init(teamsTestServerID)

	router := e.resolver.ResolveRouter(http.Dir(dir))
	_, owner := e.account(smmodel.GroupRead)

	team, err := e.resolver.resolveTeamManager().Create(owner, "Closed Server Team", smmodel.TeamPublic)

	if err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/teams"},
		{http.MethodGet, "/teams/" + team.ID.String()},
		{http.MethodGet, "/teams/invite/anything"},
		{http.MethodPost, "/teams/new"},
		{http.MethodPost, "/teams/" + team.ID.String() + "/join"},
		{http.MethodPost, "/teams/" + team.ID.String() + "/delete"},
	} {
		req := httptest.NewRequest(c.method, c.path, strings.NewReader("name=Intruder"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "login") {
			t.Errorf("%s %s: expected a redirect to the login, got %d %q", c.method, c.path, rec.Code, rec.Header().Get("Location"))
		}
	}

	if taken, _ := e.resolver.resolveTeamManager().NameTaken("Intruder"); taken {
		t.Error("an anonymous request created a team")
	}

	if got, err := e.resolver.resolveTeamManager().Lookup(team.ID.String()); err != nil || got.IsDeleted() {
		t.Errorf("an anonymous request changed the team: %v %+v", err, got)
	}
}
