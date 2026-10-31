package servermanager

import (
	"bytes"
	"io/ioutil"
	"os"
	"html/template"
	"path/filepath"
	"testing"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
	"github.com/wernersbacher/ACPitwall/internal/serverenv/envtest"
)

// the template functions which wrap methods of the domain types are only called when a page is rendered, so a changed
// signature goes unnoticed until then
func TestServerTemplateFuncs(t *testing.T) {
	env := envtest.New(filepath.Join("cmd", "acpitwall", "assetto"))

	rw := smmodel.NewRaceWeekend()
	rw.Sessions = []*smmodel.RaceWeekendSession{{}, {}}
	rw.Sessions[0].RaceConfig.Track = "monza"
	rw.Sessions[1].RaceConfig.Track = "monza"

	account := smmodel.Account{Groups: map[smmodel.ServerID]smmodel.Group{envtest.ServerID: smmodel.GroupWrite}}

	tmpl, err := template.New("test").Funcs(serverTemplateFuncs(env, nil)).Parse(
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

func TestResultServer(t *testing.T) {
	env := envtest.New(filepath.Join("cmd", "acpitwall", "assetto"))
	otherDir, err := ioutil.TempDir("", "result-server")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(otherDir)

	if err := ioutil.WriteFile(filepath.Join(otherDir, "other.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	env.ResultsDirs = func() []string { return []string{filepath.Join(env.InstallPath(), "results"), otherDir} }

	servers := smweb.ServersFunc(func() []smweb.ServerDir {
		return []smweb.ServerDir{
			{ServerRef: smweb.ServerRef{ID: "a", Name: "A"}, ResultsDir: filepath.Join(env.InstallPath(), "results")},
			{ServerRef: smweb.ServerRef{ID: "b", Name: "B"}, ResultsDir: otherDir},
		}
	})

	if got := resultServer(env, servers, "other.json"); got == nil || got.ID != "b" {
		t.Errorf("expected server b, got %+v", got)
	}

	if got := resultServer(env, servers, "unknown.json"); got == nil || got.ID != "a" {
		t.Errorf("expected the own server a for an unknown file, got %+v", got)
	}

	if got := resultServer(env, func() []smweb.ServerDir { return servers()[:1] }, "other.json"); got != nil {
		t.Errorf("expected no server with a single server, got %+v", got)
	}
}
