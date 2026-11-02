package schedule

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	"github.com/wernersbacher/ACPitwall/internal/api"
	"net/http/httptest"
	"testing"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

func TestHideInvisibleLinks(t *testing.T) {
	handler := &Handler{}

	entry := func(url string) CalendarObject {
		return CalendarObject{URL: url, SignUpURL: url + "/sign-up", ClassNames: []string{"calendar-card", "calendar-link"}}
	}

	request := func(visibility smmodel.Visibility) *smweb.Scope { return &smweb.Scope{Visibility: visibility} }

	for _, tc := range []struct {
		name       string
		visibility smmodel.Visibility
		url        string
		wantLink   bool
	}{
		{"championship visible", nil, "/championship/1", true},
		{"championship hidden", smmodel.Visibility{smmodel.AreaChampionships: smmodel.LevelLoggedIn}, "/championship/1", false},
		{"race weekend hidden", smmodel.Visibility{smmodel.AreaRaceWeekends: smmodel.LevelLoggedIn}, "/race-weekend/1", false},
		{"race weekend visible, championships hidden", smmodel.Visibility{smmodel.AreaChampionships: smmodel.LevelLoggedIn}, "/race-weekend/1", true},
		{"no page", smmodel.Visibility{smmodel.AreaChampionships: smmodel.LevelLoggedIn}, "", true},
	} {
		r := smweb.WithScope(httptest.NewRequest("GET", "/calendar.json", nil), request(tc.visibility))
		got := handler.hideInvisibleLinks(r, entry(tc.url))

		if (got.URL != "") != (tc.wantLink && tc.url != "") {
			t.Errorf("%s: url %q", tc.name, got.URL)
		}

		hasClass := false

		for _, c := range got.ClassNames {
			hasClass = hasClass || c == "calendar-link"
		}

		if tc.wantLink != hasClass {
			t.Errorf("%s: calendar-link class %t, want %t", tc.name, hasClass, tc.wantLink)
		}
	}
}

func TestFeedAndUpcomingOmitLinksToHiddenAreas(t *testing.T) {
	handler, store, cleanup := setupUpcomingAPITest(t)
	defer cleanup()

	manager := handler.scheduledRacesManager
	manager.config.HTTP.BaseURL = "https://example.test"

	now := time.Now().UTC().Truncate(time.Second)

	championship := smmodel.NewChampionship("Open Championship")
	event := smmodel.NewChampionshipEvent()
	event.RaceSetup = serverconfig.ConfigIniDefault().CurrentRaceConfig
	event.Scheduled = now.Add(time.Hour)
	event.ScheduledServerID = serverID
	championship.Events = []*smmodel.ChampionshipEvent{event}

	if err := store.UpsertChampionship(championship); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name       string
		visibility smmodel.Visibility
		wantLink   bool
	}{
		{"visible", nil, true},
		{"hidden", smmodel.Visibility{smmodel.AreaChampionships: smmodel.LevelLoggedIn}, false},
	} {
		r := smweb.WithScope(httptest.NewRequest("GET", "/events.ics", nil), &smweb.Scope{Visibility: tc.visibility})

		rec := httptest.NewRecorder()
		handler.AllScheduledRacesICalHandler(rec, r)

		feed := rec.Body.String()

		if !strings.Contains(feed, "VEVENT") {
			t.Fatalf("%s: event missing from the feed: %s", tc.name, feed)
		}

		if got := strings.Contains(feed, "/championship/"+championship.ID.String()); got != tc.wantLink {
			t.Errorf("%s: link in feed %t, want %t\n%s", tc.name, got, tc.wantLink, feed)
		}

		rec = httptest.NewRecorder()
		handler.UpcomingAPI(rec, smweb.WithScope(httptest.NewRequest("GET", "/api/upcoming-races", nil), &smweb.Scope{Visibility: tc.visibility}))

		var upcoming []*api.UpcomingRace

		if err := json.Unmarshal(rec.Body.Bytes(), &upcoming); err != nil || len(upcoming) != 1 {
			t.Fatalf("%s: %v %s", tc.name, err, rec.Body.String())
		}

		if got := upcoming[0].ChampionshipID != nil || upcoming[0].ChampionshipName != ""; got != tc.wantLink {
			t.Errorf("%s: championship reference %t, want %t", tc.name, got, tc.wantLink)
		}
	}
}
