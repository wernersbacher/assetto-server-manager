package web

import (
	"html/template"
	"strings"
	"testing"
	"time"

	"github.com/wernersbacher/ACPitwall/internal/i18n"
)

func catalogTimeFormatter(t *testing.T, lang string) timeFormatter {
	t.Helper()

	bundle, err := i18n.NewBundle(map[string][]byte{
		"en.yaml": []byte(`
date:
  layout: {compact: "Mon 2 Jan, 15:04", compact_year: "Mon 2 Jan 2006, 15:04", full: "Monday, January 2, 2006 3:04 PM (MST)"}
  days: {thursday: Thursday, monday: Monday}
  days_short: {thursday: Thu, monday: Mon}
  months: {october: October, december: December}
  months_short: {october: Oct, december: Dec}
`),
		"de.yaml": []byte(`
date:
  layout: {compact: "Mon 2. Jan, 15:04", compact_year: "Mon 2. Jan 2006, 15:04", full: "Monday, 2. January 2006, 15:04 (MST)"}
  days: {thursday: Donnerstag, monday: Montag}
  days_short: {thursday: Do, monday: Mo}
  months: {october: Oktober, december: Dezember}
  months_short: {october: Okt, december: Dez}
`),
	})

	if err != nil {
		t.Fatal(err)
	}

	return timeFormatter{tr: bundle.Translator(lang)}
}

func TestCompactTimeFormat(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		lang string
		t    time.Time
		want string
	}{
		{"en", time.Date(2026, 10, 29, 23, 18, 0, 0, time.UTC), "Thu 29 Oct, 23:18"},
		{"en", time.Date(2025, 12, 1, 9, 5, 0, 0, time.UTC), "Mon 1 Dec 2025, 09:05"},
		{"de", time.Date(2026, 10, 29, 23, 18, 0, 0, time.UTC), "Do 29. Okt, 23:18"},
		{"de", time.Date(2025, 12, 1, 9, 5, 0, 0, time.UTC), "Mo 1. Dez 2025, 09:05"},
	}

	for _, c := range cases {
		if got := catalogTimeFormatter(t, c.lang).compact(c.t, now); got != c.want {
			t.Errorf("%s: got %q, want %q", c.lang, got, c.want)
		}
	}
}

func TestLocalFormatKeepsMachineData(t *testing.T) {
	ts := time.Date(2026, 10, 29, 23, 18, 0, 0, time.UTC)

	for lang, wants := range map[string][]string{
		"en": {`data-time="2026-10-29T23:18:00Z"`, `title="Thursday, October 29, 2026 11:18 PM (UTC)"`, "Thu 29 Oct"},
		"de": {`data-time="2026-10-29T23:18:00Z"`, `title="Donnerstag, 29. Oktober 2026, 23:18 (UTC)"`, "Do 29. Okt"},
	} {
		localFormat := catalogTimeFormatter(t, lang).funcs()["localFormat"].(func(time.Time) template.HTML)
		out := string(localFormat(ts))

		for _, want := range wants {
			if !strings.Contains(out, want) {
				t.Errorf("%s: missing %q in %s", lang, want, out)
			}
		}
	}
}
