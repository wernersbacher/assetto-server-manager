package championship

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestParseDropWorstResults(t *testing.T) {
	for value, expected := range map[string]int{"": 0, "0": 0, " 3 ": 3} {
		if n, err := parseDropWorstResults(value); err != nil || n != expected {
			t.Errorf("parseDropWorstResults(%q) = %d, %v; expected %d", value, n, err, expected)
		}
	}

	for _, value := range []string{"-1", "abc", "1.5"} {
		if _, err := parseDropWorstResults(value); err == nil {
			t.Errorf("parseDropWorstResults(%q) should fail", value)
		}
	}
}

func TestChampionshipManager_HandleCreateChampionship_DropWorstResults(t *testing.T) {
	championshipManager := newTestChampionshipManager()

	newRequest := func(value string) *http.Request {
		form := url.Values{}
		form.Set("ChampionshipName", "Drop Form Test")
		form.Set("ChampionshipDropWorstResults", value)

		r, err := http.NewRequest(http.MethodPost, "/championship/new", strings.NewReader(form.Encode()))

		if err != nil {
			t.Fatal(err)
		}

		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		return r
	}

	championship, _, err := championshipManager.HandleCreateChampionship(newRequest("2"))

	if err != nil {
		t.Fatal(err)
	}

	if championship.DropWorstResults != 2 {
		t.Errorf("expected DropWorstResults 2, got %d", championship.DropWorstResults)
	}

	if championship, _, err = championshipManager.HandleCreateChampionship(newRequest("")); err != nil || championship.DropWorstResults != 0 {
		t.Errorf("expected empty value to mean 0, got %v, %v", championship, err)
	}

	if _, _, err = championshipManager.HandleCreateChampionship(newRequest("-1")); err == nil {
		t.Error("expected a negative value to be rejected")
	}
}
