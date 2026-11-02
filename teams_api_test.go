package servermanager

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/wernersbacher/ACPitwall/internal/api"
	smconfig "github.com/wernersbacher/ACPitwall/internal/config"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

func (e *teamsTestEnv) get(router http.Handler, target string, account *smmodel.Account) *httptest.ResponseRecorder {
	e.t.Helper()

	req := httptest.NewRequest(http.MethodGet, target, nil)

	if account != nil {
		for _, cookie := range e.loginCookies(account) {
			req.AddCookie(cookie)
		}
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	return rec
}

func TestTeamsAPIThroughTheRouter(t *testing.T) {
	dir, err := ioutil.TempDir("", "teams-api-router")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(dir)

	e, cleanup := newTeamsTestEnv(t, &smconfig.Configuration{})
	defer cleanup()

	e.resolver.resolveAccountManager().Init(teamsTestServerID)

	router := e.resolver.ResolveRouter(http.Dir(dir))
	manager := e.resolver.resolveTeamManager()

	reader, readerActor := e.account(smmodel.GroupRead)
	admin, _ := e.account(smmodel.GroupAdmin)
	outsider, _ := e.account(smmodel.GroupRead)

	public, err := manager.Create(readerActor, "Public API Team", smmodel.TeamPublic)

	if err != nil {
		t.Fatal(err)
	}

	private, err := manager.Create(readerActor, "Private API Team", smmodel.TeamPrivate)

	if err != nil {
		t.Fatal(err)
	}

	names := func(rec *httptest.ResponseRecorder) string {
		t.Helper()

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %.200s", rec.Code, rec.Body.String())
		}

		var teams []*api.Team

		if err := json.Unmarshal(rec.Body.Bytes(), &teams); err != nil {
			t.Fatal(err)
		}

		var out []string

		for _, team := range teams {
			out = append(out, team.Name)
		}

		return strings.Join(out, ",")
	}

	// a closed server sends visitors to the login, like every other API endpoint
	for _, target := range []string{"/api/teams", "/api/team/" + public.ID.String()} {
		rec := e.get(router, target, nil)

		if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "login") {
			t.Errorf("anonymous GET %s on a closed server: expected a redirect to the login, got %d %q", target, rec.Code, rec.Header().Get("Location"))
		}
	}

	if got := names(e.get(router, "/api/teams", outsider)); got != "Public API Team" {
		t.Errorf("a logged in account without a team sees the public teams, got %q", got)
	}

	if got := names(e.get(router, "/api/teams", reader)); got != "Private API Team,Public API Team" {
		t.Errorf("the owner sees both teams, got %q", got)
	}

	if rec := e.get(router, "/api/team/"+private.ID.String(), outsider); rec.Code != http.StatusNotFound {
		t.Errorf("a private team is a 404 for an outsider, got %d", rec.Code)
	}

	if rec := e.get(router, "/api/team/"+private.ID.String(), reader); rec.Code != http.StatusOK {
		t.Errorf("the owner sees the private team, got %d", rec.Code)
	}

	// public teams are shown to anonymous visitors once the teams area is public, the private ones stay hidden
	if rec := e.serve(e.resolver.resolveAccountHandler().SetVisibility, http.MethodPost, admin, nil, url.Values{"area": {"teams"}, "level": {"public"}}); rec.Code != http.StatusFound {
		t.Fatalf("could not make the teams public: %d", rec.Code)
	}

	if got := names(e.get(router, "/api/teams", nil)); got != "Public API Team" {
		t.Errorf("anonymous visitors of an open server see the public teams only, got %q", got)
	}

	if rec := e.get(router, "/api/team/"+private.ID.String(), nil); rec.Code != http.StatusNotFound {
		t.Errorf("anonymous GET of a private team: expected 404, got %d", rec.Code)
	}

	if rec := e.get(router, "/api/team/"+uuid.New().String(), nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown team: expected 404, got %d", rec.Code)
	}

	rec := e.get(router, "/api/team/"+public.ID.String(), nil)

	var detail api.TeamDetail

	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &detail) != nil || detail.Name != "Public API Team" || len(detail.Members) != 1 {
		t.Errorf("anonymous GET of a public team: %d %s", rec.Code, rec.Body.String())
	}
}
