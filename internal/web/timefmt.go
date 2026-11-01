package web

import (
	"fmt"
	"html/template"
	"strings"
	"time"

	"github.com/wernersbacher/ACPitwall/internal/i18n"
)

// timeFormatter formats times in a language: the layouts come from the catalog (date.layout.*, Go layouts) and the
// names of days and months (date.days.*, date.months.*) replace the English ones Go writes.
type timeFormatter struct {
	tr *i18n.Translator
}

// nameTokens are the name parts of Go layouts, long ones first (Monday before Mon), with the marker which stands in for
// them while Go formats the time. The markers are control characters, which Go copies as they are.
var nameTokens = []struct {
	token, marker string
}{
	{"Monday", "\x01D"},
	{"Mon", "\x01d"},
	{"January", "\x01M"},
	{"Jan", "\x01m"},
}

var (
	dayKeys   = []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}
	monthKeys = []string{"", "january", "february", "march", "april", "may", "june", "july", "august", "september", "october", "november", "december"}
)

// format formats t with the layout of the key below date.layout.
func (f timeFormatter) format(t time.Time, layoutKey string) string {
	return f.formatLayout(t, f.tr.T("date.layout."+layoutKey))
}

// formatLayout formats t with a Go layout, with the names of days and months in the language.
func (f timeFormatter) formatLayout(t time.Time, layout string) string {
	for _, n := range nameTokens {
		layout = strings.Replace(layout, n.token, n.marker, -1)
	}

	out := t.Format(layout)

	if !strings.Contains(out, "\x01") {
		return out
	}

	day, month := dayKeys[t.Weekday()], monthKeys[t.Month()]

	return strings.NewReplacer(
		"\x01D", f.tr.T("date.days."+day),
		"\x01d", f.tr.T("date.days_short."+day),
		"\x01M", f.tr.T("date.months."+month),
		"\x01m", f.tr.T("date.months_short."+month),
	).Replace(out)
}

// funcs are the template functions which format times. They replace the English ones of the renderer.
func (f timeFormatter) funcs() template.FuncMap {
	return template.FuncMap{
		"timeFormat": func(t time.Time) string {
			return f.format(t, "time")
		},
		"dateFormat": func(t time.Time) string {
			return f.format(t, "date")
		},
		"fullTimeFormat": func(t time.Time) string {
			return f.format(t, "full")
		},
		"hourAndZone": func(t time.Time, plusMinutes int64) string {
			return f.format(t.Add(time.Minute*time.Duration(plusMinutes)), "hour_zone")
		},
		// {{ formatDate .Start "weekday_short" }}: a time with a layout of date.layout
		"formatDate": func(t time.Time, layoutKey string) string {
			return f.format(t, layoutKey)
		},
		"localFormat": func(t time.Time) template.HTML {
			return template.HTML(fmt.Sprintf(`<span class="time-local" data-toggle="tooltip" data-time="%s" title="%s">%s</span>`,
				t.Format(time.RFC3339), template.HTMLEscapeString(f.format(t, "full")), template.HTMLEscapeString(f.compact(t, time.Now()))))
		},
		"localFormatHourAndZone": func(t time.Time) template.HTML {
			return template.HTML(fmt.Sprintf(`<span class="time-local-kitchen" data-toggle="tooltip" data-time="%s" title="%s">%s</span>`,
				t.Format(time.RFC3339), template.HTMLEscapeString(f.format(t, "full")), t.Format("15:04")))
		},
	}
}

// compact formats a time like "Thu 29 Oct, 23:18"; the year is only added when it is not the year of now. The browser
// replaces the text of .time-local elements with the same format in the viewer's time zone.
func (f timeFormatter) compact(t, now time.Time) string {
	if t.Year() != now.Year() {
		return f.format(t, "compact_year")
	}

	return f.format(t, "compact")
}
