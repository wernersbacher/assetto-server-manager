package race

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/wernersbacher/ACPitwall/internal/serverenv/envtest"
)

func formRequest(t *testing.T, values url.Values) *http.Request {
	t.Helper()

	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if err := r.ParseForm(); err != nil {
		t.Fatal(err)
	}

	return r
}

func TestBuildEntryListRejectsMismatchedArrays(t *testing.T) {
	rm := &RaceManager{} // the checks run before anything of the manager is used

	values := url.Values{}

	for _, field := range entryListFormFields {
		values[field] = []string{"a", "b"}
	}

	// one array is shorter than the entry list
	values["EntryList.Skin"] = []string{"a"}

	for _, c := range []struct{ start, length int }{{0, 2}, {1, 1}} {
		_, err := rm.BuildEntryList(formRequest(t, values), c.start, c.length)

		verrs, ok := err.(ValidationErrors)

		if !ok || len(verrs) != 1 || verrs[0].Field != "EntryList.Skin" {
			t.Fatalf("start %d length %d: expected a validation error for EntryList.Skin, got %v", c.start, c.length, err)
		}
	}

	for _, c := range []struct{ start, length int }{{-1, 1}, {0, -1}, {5, 1}} {
		if _, err := rm.BuildEntryList(formRequest(t, values), c.start, c.length); err == nil {
			t.Fatalf("start %d length %d: expected an error", c.start, c.length)
		}
	}

	if _, err := rm.BuildEntryList(formRequest(t, url.Values{}), 0, 1); err == nil {
		t.Fatal("an empty form can't hold an entrant")
	}
}

func TestBuildCustomRaceFromFormRejectsMismatchedWeatherArrays(t *testing.T) {
	rm := &RaceManager{env: envtest.New(t.TempDir())}

	// two weather entries, but BaseTemperatureRoad etc. are missing
	r := formRequest(t, url.Values{"Graphics": {"3_clear", "3_clear"}, "BaseTemperatureAmbient": {"20", "20"}})

	_, err := rm.BuildCustomRaceFromForm(r)

	verrs, ok := err.(ValidationErrors)

	if !ok || len(verrs) != 1 || verrs[0].Field != "BaseTemperatureRoad" {
		t.Fatalf("expected a validation error for BaseTemperatureRoad, got %v", err)
	}
}
