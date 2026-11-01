package race

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi"
	"github.com/google/uuid"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

func importTestRace() *smmodel.CustomRace {
	config := validConfig()

	return &smmodel.CustomRace{
		Name:                "Monza sprint",
		HasCustomName:       true,
		OverridePassword:    true,
		ReplacementPassword: "secret",
		ForceStopTime:       30,
		UUID:                uuid.New(),
		Created:             time.Now().Add(-time.Hour),
		Starred:             true,
		LoopServer:          map[smmodel.ServerID]bool{"a": true},
		RaceConfig:          *config,
		EntryList: serverconfig.EntryList{
			"CAR_0": {PitBox: 0, Name: "Driver", Model: "ks_mazda_mx5_cup", Skin: "red"},
		},
		ScheduledEventBase: smmodel.ScheduledEventBase{Scheduled: time.Now().Add(time.Hour), ScheduledServerID: "a"},
	}
}

func TestCustomRaceExportImportRoundtrip(t *testing.T) {
	rm, store, cleanup := setupTestRaceManager(t)
	defer cleanup()

	original := importTestRace()

	if err := store.UpsertCustomRace(original); err != nil {
		t.Fatal(err)
	}

	export, err := rm.ExportCustomRace(original.UUID.String(), true)

	if err != nil {
		t.Fatal(err)
	}

	data, err := json.Marshal(export)

	if err != nil {
		t.Fatal(err)
	}

	imported, err := rm.ImportCustomRace(string(data))

	if err != nil {
		t.Fatal(err)
	}

	if imported.UUID == original.UUID || imported.UUID == uuid.Nil {
		t.Fatalf("import must get a new UUID, got %s (original %s)", imported.UUID, original.UUID)
	}

	if imported.Name != original.Name || !imported.HasCustomName || !imported.OverridePassword || imported.ReplacementPassword != "secret" || imported.ForceStopTime != 30 {
		t.Fatalf("setup not taken over: %+v", imported)
	}

	if imported.RaceConfig.Track != "monza" || imported.RaceConfig.Cars != "ks_mazda_mx5_cup" || len(imported.RaceConfig.Sessions) != 1 {
		t.Fatalf("race config not taken over: %+v", imported.RaceConfig)
	}

	if e := imported.EntryList["CAR_0"]; e == nil || e.Name != "Driver" || e.Skin != "red" {
		t.Fatalf("entry list not taken over: %+v", imported.EntryList)
	}

	if imported.Starred || len(imported.LoopServer) != 0 || !imported.Scheduled.IsZero() || imported.ScheduledServerID != "" {
		t.Fatalf("star, loop and schedule must not be imported: %+v", imported)
	}

	// both exist, the original is untouched
	stored, err := store.FindCustomRaceByID(original.UUID.String())

	if err != nil || !stored.Starred {
		t.Fatalf("original changed: %v %+v", err, stored)
	}

	if _, err := store.FindCustomRaceByID(imported.UUID.String()); err != nil {
		t.Fatalf("import not stored: %v", err)
	}

	// importing twice gives two races
	again, err := rm.ImportCustomRace(string(data))

	if err != nil || again.UUID == imported.UUID {
		t.Fatalf("second import: %v", err)
	}
}

func TestCustomRaceExportDropsPassword(t *testing.T) {
	rm, store, cleanup := setupTestRaceManager(t)
	defer cleanup()

	original := importTestRace()

	if err := store.UpsertCustomRace(original); err != nil {
		t.Fatal(err)
	}

	export, err := rm.ExportCustomRace(original.UUID.String(), false)

	if err != nil {
		t.Fatal(err)
	}

	if export.ReplacementPassword != "" || export.OverridePassword {
		t.Fatalf("password exported: %+v", export)
	}
}

func TestImportCustomRaceGeneratesName(t *testing.T) {
	rm, _, cleanup := setupTestRaceManager(t)
	defer cleanup()

	race := importTestRace()
	race.Name = "  "

	data, _ := json.Marshal(race)
	imported, err := rm.ImportCustomRace(string(data))

	if err != nil {
		t.Fatal(err)
	}

	if imported.HasCustomName || imported.Name == "" {
		t.Fatalf("expected a generated name, got %q (custom %v)", imported.Name, imported.HasCustomName)
	}
}

func TestImportCustomRaceRejectsInvalid(t *testing.T) {
	rm, store, cleanup := setupTestRaceManager(t)
	defer cleanup()

	tests := map[string]func(r *smmodel.CustomRace){
		"no track":        func(r *smmodel.CustomRace) { r.RaceConfig.Track = "" },
		"no sessions":     func(r *smmodel.CustomRace) { r.RaceConfig.Sessions = nil },
		"track traversal": func(r *smmodel.CustomRace) { r.RaceConfig.Track = "../../etc" },
		"car traversal":   func(r *smmodel.CustomRace) { r.RaceConfig.Cars = "ok;../x" },
		"skin traversal":  func(r *smmodel.CustomRace) { r.EntryList["CAR_0"].Skin = "a/b" },
		"setup traversal": func(r *smmodel.CustomRace) { r.EntryList["CAR_0"].FixedSetup = "../../x.ini" },
		"setup absolute":  func(r *smmodel.CustomRace) { r.EntryList["CAR_0"].FixedSetup = "/etc/x.ini" },
		"long name":       func(r *smmodel.CustomRace) { r.Name = strings.Repeat("a", 500) },
		"negative stop":   func(r *smmodel.CustomRace) { r.ForceStopTime = -1 },
	}

	for name, modify := range tests {
		t.Run(name, func(t *testing.T) {
			race := importTestRace()
			modify(race)

			data, _ := json.Marshal(race)

			if _, err := rm.ImportCustomRace(string(data)); err == nil {
				t.Fatal("expected an error")
			} else {
				var importErr ImportError
				var validation ValidationErrors

				if !errors.As(err, &importErr) && !errors.As(err, &validation) {
					t.Fatalf("unexpected error type %T: %v", err, err)
				}
			}
		})
	}

	for name, data := range map[string]string{
		"null":         "null",
		"not json":     "{",
		"null session": `{"Name":"x","RaceConfig":{"Track":"monza","Cars":"a","MaxClients":1,"Sessions":{"Race":null}}}`,
		"null entrant": `{"Name":"x","RaceConfig":{"Track":"monza","Cars":"a","MaxClients":1,"Sessions":{"Race":{"Laps":1}}},"EntryList":{"CAR_0":null}}`,
		"wrong type":   `{"Name":5}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := rm.ImportCustomRace(data); err == nil {
				t.Fatal("expected an error")
			}
		})
	}

	races, err := store.ListCustomRaces()

	if err == nil && len(races) != 0 {
		t.Fatalf("invalid imports saved %d races", len(races))
	}
}

func TestCustomRaceExportHandler(t *testing.T) {
	rm, store, cleanup := setupTestRaceManager(t)
	defer cleanup()

	original := importTestRace()

	if err := store.UpsertCustomRace(original); err != nil {
		t.Fatal(err)
	}

	handler := &CustomRaceHandler{managerFor: func(*http.Request) (*RaceManager, error) { return rm, nil }}

	serve := func(id string) *httptest.ResponseRecorder {
		router := chi.NewRouter()
		router.Get("/custom/export/{uuid}", handler.Export)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/custom/export/"+id, nil))

		return rec
	}

	rec := serve(original.UUID.String())

	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Disposition"), "Monza sprint.json") {
		t.Fatalf("unexpected response %d %v", rec.Code, rec.Header())
	}

	if strings.Contains(rec.Body.String(), "LoopServer\": {") && strings.Contains(rec.Body.String(), `"a": true`) {
		t.Fatal("loop settings exported")
	}

	for _, id := range []string{"..%2f..%2fx", "not-a-uuid", uuid.New().String()} {
		if rec := serve(id); rec.Code != http.StatusNotFound {
			t.Fatalf("%s: expected 404, got %d", id, rec.Code)
		}
	}
}
