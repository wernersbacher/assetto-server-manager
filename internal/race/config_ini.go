package race

import (
	"bytes"
	"text/template"

	"github.com/sirupsen/logrus"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
)

type serverNameTemplateOpts struct {
	serverconfig.GlobalServerConfig
	serverconfig.CurrentRaceConfig
	smmodel.RaceEvent

	ServerName string

	eventName string
	serverID  smmodel.ServerID
}

// EventName shadows RaceEvent.EventName so user server name templates can keep using {{ .EventName }}.
func (o serverNameTemplateOpts) EventName() string {
	return o.eventName
}

// IsLooping shadows RaceEvent.IsLooping so user server name templates can keep using {{ .IsLooping }}.
func (o serverNameTemplateOpts) IsLooping() bool {
	return o.RaceEvent.IsLooping(o.serverID)
}

func buildFinalServerName(userTemplate, eventName string, serverID smmodel.ServerID, event smmodel.RaceEvent, config serverconfig.ServerConfig) string {
	t, err := template.New("serverName").Parse(userTemplate)

	if err != nil {
		logrus.WithError(err).Errorf("could not parse user server name template.")
		return config.GlobalServerConfig.Name
	}

	out := new(bytes.Buffer)

	err = t.Execute(out, serverNameTemplateOpts{
		ServerName:         config.GlobalServerConfig.Name,
		CurrentRaceConfig:  config.CurrentRaceConfig,
		GlobalServerConfig: config.GlobalServerConfig,
		RaceEvent:          event,
		eventName:          eventName,
		serverID:           serverID,
	})

	if err != nil {
		logrus.WithError(err).Errorf("could not execute user server name template.")
		return config.GlobalServerConfig.Name
	}

	return out.String()
}
