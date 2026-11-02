package schedule

import (
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
