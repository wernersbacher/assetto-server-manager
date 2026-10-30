package web

import (
	"strings"
	"testing"
	"time"
)

func TestCompactTimeFormat(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

	if got := compactTimeFormat(time.Date(2026, 10, 29, 23, 18, 0, 0, time.UTC), now); got != "Thu 29 Oct, 23:18" {
		t.Errorf("same year: got %q", got)
	}

	if got := compactTimeFormat(time.Date(2025, 12, 1, 9, 5, 0, 0, time.UTC), now); got != "Mon 1 Dec 2025, 09:05" {
		t.Errorf("other year: got %q", got)
	}
}

func TestLocalFormatKeepsMachineData(t *testing.T) {
	ts := time.Date(2026, 10, 29, 23, 18, 0, 0, time.UTC)
	out := string(localFormatHelper(ts))

	for _, want := range []string{`data-time="2026-10-29T23:18:00Z"`, `title="Thursday, October 29, 2026 11:18 PM (UTC)"`, "Thu 29 Oct"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
}
