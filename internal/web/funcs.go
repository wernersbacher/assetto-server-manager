package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"regexp"
	"strings"
	"time"
)

func shortenTrackName(name string) string {
	nameParts := strings.Split(name, " ")

	out := nameParts[0]

	if len(out) > 4 {
		out = out[0:4]
	}

	return out
}

func appendQuery(r *http.Request, query, value string) string {
	q := r.URL.Query()
	q.Set(query, value)
	r.URL.RawQuery = q.Encode()

	return r.URL.String()
}

func formatDuration(d time.Duration, trimLeadingZeroes bool) string {
	hours := d.Hours()
	minutes := d.Minutes() - float64(int(hours)*60)
	seconds := d.Seconds() - float64(int(hours)*60*60) - float64(int(minutes)*60)

	duration := fmt.Sprintf("%02d:%02d:%06.3f", int(hours), int(minutes), seconds)

	if trimLeadingZeroes && strings.HasPrefix(duration, "00:") {
		return duration[3:]
	}

	return duration
}

func templateDict(values ...interface{}) (map[string]interface{}, error) {
	if len(values)%2 != 0 {
		return nil, errors.New("invalid dict call")
	}
	dict := make(map[string]interface{}, len(values)/2)
	for i := 0; i < len(values); i += 2 {
		key, ok := values[i].(string)
		if !ok {
			return nil, errors.New("dict keys must be strings")
		}
		dict[key] = values[i+1]
	}
	return dict, nil
}

func addTime(t time.Time, plusMinutes int64) time.Time {
	return t.Add(time.Minute * time.Duration(plusMinutes))
}

func timeZone(t time.Time) string {
	name, _ := t.Zone()

	return name
}

func isBefore(t time.Time) bool {
	return time.Now().Before(t)
}

func sunAngleToTimeOfDay(angle int) string {
	t := time.Date(2000, 1, 1, 13, 00, 00, 0, time.UTC)
	t = t.Add(time.Minute * time.Duration((angle/16)*60))

	return t.Format("15:04")
}

func multiplyFloats(a, b float64) float64 {
	return a * b
}

func percentage(a, b int) int {
	return int(100 * (float64(a) / float64(b)))
}

func stripGeotagCrap(tag string, north bool) string {
	re := regexp.MustCompile("[0-9]+")
	geoTags := re.FindAllString(tag, -1)

	if len(geoTags) == 2 {
		// "52.9452° N" format
		// @TODO +- some amount for the bbox

		return geoTags[0] + "." + geoTags[1]
	} else if len(geoTags) == 3 {
		// "50� 13' 57 N" format
		/*for _, thing := range geoTags {
			println(thing)
		}*/
	} else if len(geoTags) == 1 {
		// dunno, some crazy format, just return
		return geoTags[0]
	}

	// Geotags of "lost" - a hamlet in Scotland
	if north {
		return "57.2050"
	}

	return "-3.0774"
}

func stringArrayToCSV(array []string) string {
	return strings.Join(array, ", ")
}

func jsonEncode(v interface{}) template.JS {
	buf := new(bytes.Buffer)

	_ = json.NewEncoder(buf).Encode(v)

	return template.JS(buf.String())
}
