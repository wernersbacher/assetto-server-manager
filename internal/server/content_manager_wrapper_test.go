package server

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smcontent "github.com/wernersbacher/ACPitwall/internal/content"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/serverenv"
)

type descriptionTestEvent struct {
	smmodel.RaceEvent
}

func (descriptionTestEvent) EventName(trackSummary smmodel.TrackSummaryFunc) string {
	return "Event " + trackSummary("monza", "gp")
}

func (descriptionTestEvent) EventDescription() string { return "" }

func (descriptionTestEvent) IsLooping(serverID smmodel.ServerID) bool { return serverID == "srv" }

// the welcome message is a user template, which can use the methods of the race event (EventName, IsLooping)
func TestContentManagerWrapper_setDescriptionTextEventMethods(t *testing.T) {
	dir, err := ioutil.TempDir("", "cmw-description")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(dir)

	if err := os.MkdirAll(filepath.Join(dir, "content", "tracks"), 0755); err != nil {
		t.Fatal(err)
	}

	env := &serverenv.Env{
		InstallPath: func() string { return dir },
		ServerID:    func() smmodel.ServerID { return "srv" },
	}

	cmw := NewContentManagerWrapper(nil, nil, smcontent.NewCarManager(dir, false, false), smcontent.NewTrackManager(dir), env)
	cmw.serverConfig.GlobalServerConfig.ContentManagerWelcomeMessage = "{{ .EventName }} looping={{ .IsLooping }}"

	if err := cmw.setDescriptionText(descriptionTestEvent{}); err != nil {
		t.Fatal(err)
	}

	if expected := "Event Monza (GP) looping=true"; cmw.description != expected {
		t.Errorf("expected description %q, got %q", expected, cmw.description)
	}
}

func TestContentRequiresPassword(t *testing.T) {
	var g serverconfig.GlobalServerConfig

	if contentRequiresPassword(g) {
		t.Error("option off must not flag the content")
	}

	g.ContentManagerWrapperContentRequiresPassword = 1

	if contentRequiresPassword(g) {
		t.Error("server without password must not flag the content")
	}

	g.Password = "secret"

	if !contentRequiresPassword(g) {
		t.Error("option on and password set must flag the content")
	}
}
