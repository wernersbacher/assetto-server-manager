package race

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smcontent "github.com/wernersbacher/ACPitwall/internal/content"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
	"github.com/wernersbacher/ACPitwall/internal/web/webtest"
)

func validConfig() *serverconfig.CurrentRaceConfig {
	config := &serverconfig.CurrentRaceConfig{Track: "monza", Cars: "ks_mazda_mx5_cup", MaxClients: 10}
	config.AddSession(serverconfig.SessionTypeRace, &serverconfig.SessionConfig{Name: "Race", Laps: 5})

	return config
}

func fields(errs ValidationErrors) []string {
	var names []string

	for _, e := range errs {
		names = append(names, e.Field)
	}

	return names
}

func TestValidateCustomRace(t *testing.T) {
	if errs := ValidateCustomRace(validConfig(), nil, url.Values{}); len(errs) != 0 {
		t.Fatalf("valid config reported errors: %v", errs)
	}

	t.Run("missing track, cars, sessions", func(t *testing.T) {
		errs := ValidateCustomRace(&serverconfig.CurrentRaceConfig{MaxClients: 1}, nil, url.Values{})

		if strings.Join(fields(errs), ",") != "Track,Cars,Practice.Enabled" {
			t.Fatalf("unexpected errors: %v", errs)
		}
	})

	t.Run("session without time or laps", func(t *testing.T) {
		config := validConfig()
		config.AddSession(serverconfig.SessionTypeQualifying, &serverconfig.SessionConfig{Name: "Qualify"})

		errs := ValidateCustomRace(config, nil, url.Values{})

		if len(errs) != 1 || errs[0].Field != serverconfig.SessionTypeQualifying.String()+".Time" {
			t.Fatalf("unexpected errors: %v", errs)
		}
	})

	t.Run("booking needs time", func(t *testing.T) {
		config := validConfig()
		config.AddSession(serverconfig.SessionTypeBooking, &serverconfig.SessionConfig{Name: "Booking", Laps: 3})

		if errs := ValidateCustomRace(config, nil, url.Values{}); len(errs) != 1 {
			t.Fatalf("unexpected errors: %v", errs)
		}
	})

	t.Run("rules", func(t *testing.T) {
		config := validConfig()
		config.MaxClients = 0
		config.RacePitWindowStart = 10
		config.RacePitWindowEnd = 5

		if got := strings.Join(fields(ValidateCustomRace(config, nil, url.Values{})), ","); got != "MaxClients,RacePitWindowEnd" {
			t.Fatalf("unexpected fields: %s", got)
		}
	})

	t.Run("entrant without car", func(t *testing.T) {
		list := serverconfig.EntryList{}
		list.AddInPitBox(&serverconfig.Entrant{Name: "A"}, 2)

		errs := ValidateCustomRace(validConfig(), list, url.Values{})

		if len(errs) != 1 || errs[0].Field != "EntryList.Car" || !strings.Contains(errs[0].Message, "Entry 3") {
			t.Fatalf("unexpected errors: %v", errs)
		}
	})

	t.Run("preset needs a name", func(t *testing.T) {
		if errs := ValidateCustomRace(validConfig(), nil, url.Values{"action": {"justSave"}}); len(errs) != 1 || errs[0].Field != "CustomRaceName" {
			t.Fatalf("unexpected errors: %v", errs)
		}

		if errs := ValidateCustomRace(validConfig(), nil, url.Values{"action": {"justSave"}, "CustomRaceName": {"x"}}); len(errs) != 0 {
			t.Fatalf("unexpected errors: %v", errs)
		}
	})
}

func TestValidateQuickRace(t *testing.T) {
	if errs := validateQuickRace([]string{"car"}, 10, nil, 10, nil, 0, nil); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	errs := validateQuickRace(nil, 0, errors.New("x"), -1, nil, 0, errors.New("y"))

	if got := strings.Join(fields(errs), ","); got != "Cars,Qualifying.Time,Race.Time,Race.Laps" {
		t.Fatalf("unexpected fields: %s", got)
	}
}

// withContent gives the manager car and track managers for its (empty) install.
func withContent(t *testing.T, manager *RaceManager) *RaceManager {
	for _, dir := range []string{"cars", "tracks", "weather", "setups"} {
		_ = os.MkdirAll(filepath.Join(manager.env.InstallPath(), "content", dir), 0755)
	}

	manager.carManager = smcontent.NewCarManager(manager.env.InstallPath(), false, false)
	manager.trackManager = smcontent.NewTrackManager(manager.env.InstallPath())

	t.Cleanup(func() { _ = manager.carManager.CloseSearchIndex() })

	if err := manager.carManager.CreateOrOpenSearchIndex(); err != nil {
		t.Fatal(err)
	}

	return manager
}

func postForm(target string, form url.Values) *http.Request {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	return req
}

func TestSetupCustomRaceInvalidForm(t *testing.T) {
	slots, store := newTestSlots(t, smmodel.ServerID("a"))
	manager := withContent(t, slots["a"].manager)

	form := url.Values{
		"action":         {"justSave"},
		"CustomRaceName": {"My race"},
		"MaxClients":     {"12"},
		"Track":          {""},
	}

	err := manager.SetupCustomRace(postForm("/custom/new/submit", form))

	var validationErrors ValidationErrors

	if !errors.As(err, &validationErrors) {
		t.Fatalf("expected ValidationErrors, got %v", err)
	}

	if races, _ := store.ListCustomRaces(); len(races) != 0 {
		t.Fatalf("an invalid race must not be saved, got %d", len(races))
	}

	r := smweb.WithScope(postForm("/custom/new/submit", form), webtest.RequestScope())
	_ = r.ParseForm()

	opts, err := manager.BuildRaceOptsFromSubmission(r, validationErrors)

	if err != nil {
		t.Fatal(err)
	}

	if len(opts.FormErrors) != len(validationErrors) || opts.CustomRaceName != "My race" || opts.Current.MaxClients != 12 {
		t.Fatalf("form input was not kept: %+v", opts)
	}
}

func TestSetupCustomRaceInvalidScheduleDate(t *testing.T) {
	slots, store := newTestSlots(t, smmodel.ServerID("a"))

	form := url.Values{
		"action":                  {"schedule"},
		"CustomRaceName":          {"My race"},
		"CustomRaceScheduled":     {"not-a-date"},
		"CustomRaceScheduledTime": {"12:00"},
		"MaxClients":              {"12"},
		"Track":                   {"monza"},
		"Cars":                    {"ks_mazda_mx5_cup"},
		"Race.Enabled":            {"1"},
		"Race.Laps":               {"5"},
	}

	err := withContent(t, slots["a"].manager).SetupCustomRace(postForm("/custom/new/submit", form))

	var validationErrors ValidationErrors

	if !errors.As(err, &validationErrors) || validationErrors[0].Field != "CustomRaceScheduled" {
		t.Fatalf("expected a schedule date error, got %v", err)
	}

	if races, _ := store.ListCustomRaces(); len(races) != 0 {
		t.Fatalf("the race must not be saved with a bad date, got %d", len(races))
	}
}

func TestValidateEventSetup(t *testing.T) {
	config := serverconfig.ConfigIniDefault().CurrentRaceConfig
	config.Track = "monza"
	config.Cars = "" // comes from the championship or race weekend

	if errs := ValidateEventSetup(&config); len(errs) != 0 {
		t.Fatalf("a valid event setup without cars must pass, got %v", errs)
	}

	config.Track = ""
	config.MaxClients = 0

	errs := ValidateEventSetup(&config)
	fields := map[string]bool{}

	for _, e := range errs {
		fields[e.Field] = true
	}

	if !fields["Track"] || !fields["MaxClients"] || fields["Cars"] || fields["CustomRaceName"] {
		t.Fatalf("unexpected fields: %v", errs)
	}
}
