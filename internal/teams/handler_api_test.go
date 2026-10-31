package teams

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/wernersbacher/ACPitwall/internal/api"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

func apiTeamNames(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("unexpected content type %q", got)
	}

	var teams []*api.Team

	if err := json.Unmarshal(rec.Body.Bytes(), &teams); err != nil {
		t.Fatalf("invalid JSON: %s: %s", err, rec.Body.String())
	}

	names := []string{}

	for _, team := range teams {
		names = append(names, team.Name)
	}

	return names
}

func joinNames(names []string) string {
	return strings.Join(names, ",")
}

func TestHandler_ListAPI_Visibility(t *testing.T) {
	e := newTestEnv(t)
	h := e.handler()

	ownerAccount, owner := e.in(smmodel.GroupRead)
	memberAccount, member := e.in(smmodel.GroupRead)
	invitedAccount, invited := e.in(smmodel.GroupRead)
	strangerAccount, _ := e.in(smmodel.GroupRead)
	moderatorAccount, _ := e.in(smmodel.GroupWrite)

	e.create(owner, "Zulu Public", smmodel.TeamPublic)
	secret := e.create(owner, "Alpha Private", smmodel.TeamPrivate)
	gone := e.create(owner, "Gone Public", smmodel.TeamPublic)

	if _, err := e.manager.Invite(owner, secret.ID.String(), member.AccountID); err != nil {
		t.Fatal(err)
	}

	if _, err := e.manager.Join(member, secret.ID.String(), ""); err != nil {
		t.Fatal(err)
	}

	if _, err := e.manager.Invite(owner, secret.ID.String(), invited.AccountID); err != nil {
		t.Fatal(err)
	}

	if err := e.manager.Delete(owner, gone.ID.String()); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		who     string
		account *smmodel.Account
		want    string
	}{
		// sorted by name, deleted teams are gone, private ones only for those who may see them
		{"anonymous", nil, "Zulu Public"},
		{"stranger", strangerAccount, "Zulu Public"},
		{"owner", ownerAccount, "Alpha Private,Zulu Public"},
		{"member", memberAccount, "Alpha Private,Zulu Public"},
		{"invited", invitedAccount, "Alpha Private,Zulu Public"},
		{"moderator", moderatorAccount, "Alpha Private,Zulu Public"},
	} {
		got := joinNames(apiTeamNames(t, e.call(h.ListAPI, c.account, nil, nil)))

		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.who, got, c.want)
		}
	}
}

func TestHandler_ListAPI_Empty(t *testing.T) {
	e := newTestEnv(t)

	rec := e.call(e.handler().ListAPI, nil, nil, nil)

	// an empty list is [] and not null, so clients can iterate it
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("expected an empty list: %d %q", rec.Code, rec.Body.String())
	}
}

func TestHandler_ListAPI_Fields(t *testing.T) {
	e := newTestEnv(t)
	h := e.handler()

	ownerAccount, owner := e.in(smmodel.GroupRead)
	ownerAccount.DriverName = "Fast Fred"

	if err := e.store.UpsertAccount(ownerAccount); err != nil {
		t.Fatal(err)
	}

	team := e.create(owner, "Fields", smmodel.TeamPublic)
	e.joinNew(team, owner)

	var teams []*api.Team

	if err := json.Unmarshal(e.call(h.ListAPI, nil, nil, nil).Body.Bytes(), &teams); err != nil || len(teams) != 1 {
		t.Fatalf("unexpected list: %v %v", teams, err)
	}

	got := teams[0]

	if got.ID != team.ID || got.Name != "Fields" || got.Visibility != "public" || got.MemberCount != 2 || got.Owner != "Fast Fred" {
		t.Errorf("unexpected team: %+v", got)
	}

	// the handler has the base URL for the links
	if got.URL != "https://acp.example/teams/"+team.ID.String() {
		t.Errorf("unexpected URL %q", got.URL)
	}
}

func TestHandler_ViewAPI(t *testing.T) {
	e := newTestEnv(t)
	h := e.handler()

	_, owner := e.in(smmodel.GroupRead)
	memberAccount, member := e.in(smmodel.GroupRead)
	strangerAccount, _ := e.in(smmodel.GroupRead)
	moderatorAccount, _ := e.in(smmodel.GroupWrite)

	public := e.create(owner, "Open API", smmodel.TeamPublic)
	private := e.create(owner, "Closed API", smmodel.TeamPrivate)
	link, err := e.manager.Invite(owner, private.ID.String(), uuid.Nil)

	if err != nil {
		t.Fatal(err)
	}

	if _, err := e.manager.Invite(owner, private.ID.String(), member.AccountID); err != nil {
		t.Fatal(err)
	}

	if _, err := e.manager.Join(member, private.ID.String(), ""); err != nil {
		t.Fatal(err)
	}

	get := func(account *smmodel.Account, team *smmodel.Team) *httptest.ResponseRecorder {
		return e.call(h.ViewAPI, account, map[string]string{"teamID": team.ID.String()}, nil)
	}

	// a public team is open to everybody, with its members
	for who, account := range map[string]*smmodel.Account{"anonymous": nil, "stranger": strangerAccount} {
		rec := get(account, public)

		var detail api.TeamDetail

		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &detail) != nil || detail.ID != public.ID || len(detail.Members) != 1 || detail.Members[0].Role != "owner" {
			t.Errorf("%s: public team: %d %s", who, rec.Code, rec.Body.String())
		}
	}

	// a private team does not exist for people who may not see it
	for who, account := range map[string]*smmodel.Account{"anonymous": nil, "stranger": strangerAccount} {
		if rec := get(account, private); rec.Code != http.StatusNotFound {
			t.Errorf("%s: expected 404 for a private team, got %d", who, rec.Code)
		}
	}

	for who, account := range map[string]*smmodel.Account{"member": memberAccount, "moderator": moderatorAccount} {
		rec := get(account, private)

		var detail api.TeamDetail

		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &detail) != nil || detail.Visibility != "private" || len(detail.Members) != 2 {
			t.Errorf("%s: private team: %d %s", who, rec.Code, rec.Body.String())
		}

		// not even members get the invite links or account ids through the API
		body := rec.Body.String()

		for _, secret := range []string{link.Token, owner.AccountID.String(), member.AccountID.String(), "Invites"} {
			if strings.Contains(body, secret) {
				t.Errorf("%s: the API response must not contain %q", who, secret)
			}
		}
	}

	// unknown, malformed and deleted teams are 404
	if err := e.manager.Delete(owner, public.ID.String()); err != nil {
		t.Fatal(err)
	}

	if rec := get(moderatorAccount, public); rec.Code != http.StatusNotFound {
		t.Errorf("a deleted team: expected 404, got %d", rec.Code)
	}

	for _, id := range []string{"unknown", uuid.New().String(), `..\..\x`, ""} {
		if rec := e.call(h.ViewAPI, moderatorAccount, map[string]string{"teamID": id}, nil); rec.Code != http.StatusNotFound {
			t.Errorf("ViewAPI(%q): expected 404, got %d", id, rec.Code)
		}
	}
}
