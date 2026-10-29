package race

import (
	"testing"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
)

type loopingEvent struct {
	smmodel.RaceEvent
	looping bool
}

func (e loopingEvent) IsLooping(smmodel.ServerID) bool { return e.looping }

func TestBuildFinalServerName(t *testing.T) {
	cfg := serverconfig.ServerConfig{}
	cfg.GlobalServerConfig.Name = "My Server"
	cfg.CurrentRaceConfig.Track = "monza"

	for _, tc := range []struct {
		name     string
		template string
		looping  bool
		expected string
	}{
		{"default template", serverconfig.DefaultServerNameTemplate, false, "My Server - Monza GP"},
		{"looping", "{{ .ServerName }}{{ if .IsLooping }} (looping){{ end }}", true, "My Server (looping)"},
		{"race config field", "{{ .ServerName }} on {{ .Track }}", false, "My Server on monza"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := buildFinalServerName(tc.template, "Monza GP", "", loopingEvent{looping: tc.looping}, cfg)

			if got != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}
