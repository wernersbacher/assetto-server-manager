package servermanager

import (
	"bytes"
	"text/template"

	"github.com/sirupsen/logrus"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
)

type serverNameTemplateOpts struct {
	serverconfig.GlobalServerConfig
	serverconfig.CurrentRaceConfig
	RaceEvent

	ServerName string
}

func buildFinalServerName(userTemplate string, event RaceEvent, config serverconfig.ServerConfig) string {
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
	})

	if err != nil {
		logrus.WithError(err).Errorf("could not execute user server name template.")
		return config.GlobalServerConfig.Name
	}

	return out.String()
}
