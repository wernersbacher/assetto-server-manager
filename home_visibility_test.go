package servermanager

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	smconfig "github.com/wernersbacher/ACPitwall/internal/config"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
	"github.com/wernersbacher/ACPitwall/internal/web/webtest"
)

// The overview and the navigation show only what the visitor may see: no links, no blocks.
func TestHomeFollowsAreaVisibility(t *testing.T) {
	resolver, _, cleanup := newTestResolver(t, &smconfig.Configuration{})
	defer cleanup()

	handler := resolver.resolveServerAdministrationHandler()

	account := func(group smmodel.Group) *smmodel.Account {
		return &smmodel.Account{Name: "someone", Groups: map[smmodel.ServerID]smmodel.Group{"": group}}
	}

	// the strings which show that a part is on the page
	const (
		navResults   = `href="/results"`
		navTeams     = `href="/teams"`
		navCalendar  = `href="/calendar"`
		navContent   = `href="/tracks"`
		navLive      = `href="/live-timing"`
		blockResults = `id="recent-results-heading"`
		blockCalDay  = `id="upcoming-heading"`
		blockCurrent = `id="current-session-heading"`
	)

	cases := []struct {
		name       string
		visibility smmodel.Visibility
		account    *smmodel.Account
		want       []string
		notWant    []string
	}{
		{
			name:    "anonymous, defaults",
			want:    []string{navResults, navCalendar, navContent, navLive, blockResults, blockCalDay, blockCurrent},
			notWant: []string{navTeams},
		},
		{
			name: "anonymous, only the calendar is public",
			visibility: smmodel.Visibility{
				smmodel.AreaStats: smmodel.LevelLoggedIn, smmodel.AreaDownloads: smmodel.LevelLoggedIn, smmodel.AreaResults: smmodel.LevelLoggedIn,
				smmodel.AreaChampionships: smmodel.LevelLoggedIn, smmodel.AreaRaceWeekends: smmodel.LevelLoggedIn,
				smmodel.AreaLiveTiming: smmodel.LevelLoggedIn, smmodel.AreaContent: smmodel.LevelLoggedIn,
			},
			want:    []string{navCalendar, blockCalDay},
			notWant: []string{navResults, navTeams, navContent, navLive, blockResults, blockCurrent},
		},
		{
			name:       "reader, results for writers only",
			visibility: smmodel.Visibility{smmodel.AreaResults: smmodel.LevelWrite},
			account:    account(smmodel.GroupRead),
			want:       []string{navTeams, navCalendar, blockCalDay, blockCurrent},
			notWant:    []string{navResults, blockResults},
		},
		{
			name:       "writer, results for writers only",
			visibility: smmodel.Visibility{smmodel.AreaResults: smmodel.LevelWrite},
			account:    account(smmodel.GroupWrite),
			want:       []string{navTeams, navResults, blockResults, blockCalDay},
		},
	}

	for _, tc := range cases {
		scope := webtest.RequestScope()
		scope.Visibility = tc.visibility

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req = smweb.WithScope(req, scope)

		if tc.account != nil {
			req = smweb.WithAccount(req, tc.account)
		}

		rec := httptest.NewRecorder()
		handler.Home(rec, req)

		body := rec.Body.String()

		if rec.Code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d: %s", tc.name, rec.Code, body)
			continue
		}

		for _, want := range tc.want {
			if !strings.Contains(body, want) {
				t.Errorf("%s: expected %q in the page", tc.name, want)
			}
		}

		for _, notWant := range tc.notWant {
			if strings.Contains(body, notWant) {
				t.Errorf("%s: %q must not be in the page", tc.name, notWant)
			}
		}
	}
}
