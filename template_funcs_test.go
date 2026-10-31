package servermanager

import (
	"bytes"
	"html/template"
	"path/filepath"
	"testing"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/serverenv/envtest"
)

// the template functions which wrap methods of the domain types are only called when a page is rendered, so a changed
// signature goes unnoticed until then
func TestServerTemplateFuncs(t *testing.T) {
	env := envtest.New(filepath.Join("cmd", "server-manager", "assetto"))

	rw := smmodel.NewRaceWeekend()
	rw.Sessions = []*smmodel.RaceWeekendSession{{}, {}}
	rw.Sessions[0].RaceConfig.Track = "monza"
	rw.Sessions[1].RaceConfig.Track = "monza"

	account := smmodel.Account{Groups: map[smmodel.ServerID]smmodel.Group{envtest.ServerID: smmodel.GroupWrite}}

	tmpl, err := template.New("test").Funcs(serverTemplateFuncs(env)).Parse(
		`{{ EventName .Event }}|{{ EventIsLooping .Event }}|{{ AccountGroup .Account }}|{{ raceWeekendTrackOverview .RaceWeekend }}`,
	)

	if err != nil {
		t.Fatal(err)
	}

	event := &smmodel.QuickRace{}
	event.RaceConfig.Track = "monza"

	out := new(bytes.Buffer)

	err = tmpl.Execute(out, map[string]interface{}{
		"Event":       event,
		"Account":     &account,
		"RaceWeekend": rw,
	})

	if err != nil {
		t.Fatal(err)
	}

	if expected := "Monza|false|write|Monza"; out.String() != expected {
		t.Errorf("expected %q, got %q", expected, out.String())
	}
}
