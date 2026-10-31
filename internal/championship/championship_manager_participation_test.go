package championship

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestParseParticipationMode(t *testing.T) {
	tests := []struct {
		name         string
		form         url.Values
		open, signUp bool
	}{
		{"fixed", url.Values{"ParticipationMode": {"fixed"}}, false, false},
		{"open", url.Values{"ParticipationMode": {"open"}}, true, false},
		{"signup", url.Values{"ParticipationMode": {"signup"}}, false, true},
		{"mode wins over legacy fields", url.Values{"ParticipationMode": {"open"}, "Championship.SignUpForm.Enabled": {"on"}}, true, false},
		{"legacy open", url.Values{"ChampionshipOpenEntrants": {"on"}}, true, false},
		{"legacy sign up", url.Values{"Championship.SignUpForm.Enabled": {"1"}}, false, true},
		{"nothing", url.Values{}, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := http.NewRequest(http.MethodPost, "/", strings.NewReader(tt.form.Encode()))

			if err != nil {
				t.Fatal(err)
			}

			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

			open, signUp := parseParticipationMode(r)

			if open != tt.open || signUp != tt.signUp {
				t.Errorf("got open=%v signUp=%v, want open=%v signUp=%v", open, signUp, tt.open, tt.signUp)
			}
		})
	}
}
