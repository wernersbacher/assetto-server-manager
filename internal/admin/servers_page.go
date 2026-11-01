package admin

import (
	"net/http"

	"github.com/sirupsen/logrus"

	"github.com/wernersbacher/ACPitwall/internal/portcheck"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

// ServerPort is one port a server uses, for the Servers page.
type ServerPort struct {
	What     string
	Key      string
	Port     int
	Protocol string
}

// ServerPlugin is a plugin of a server with the ports it uses while it is enabled.
type ServerPlugin struct {
	Name       string
	Enabled    bool
	OptionsURL string
	Ports      []ServerPort
}

// ServerDetails is what the Servers page shows about one server.
type ServerDetails struct {
	*ServerSummary

	// Ports are the acServer ports (and the UDP plugin and Content Manager wrapper ports ACPitwall sets up).
	Ports   []ServerPort
	Plugins []ServerPlugin

	// Warnings are the ports this server shares with other servers.
	Warnings []portcheck.Warning
}

type serversTemplateVars struct {
	smweb.BaseTemplateVars

	Details []*ServerDetails
}

func serverPorts(uses []portcheck.Use) []ServerPort {
	ports := make([]ServerPort, 0, len(uses))

	for _, use := range uses {
		ports = append(ports, ServerPort{What: use.What, Key: use.Key, Port: use.Port, Protocol: string(use.Protocol)})
	}

	return ports
}

// newServerDetails collects the ports, plugins and port warnings of a server from its stored options. Options which
// can't be loaded are logged and left out, the page still shows the server.
func (sah *ServerAdministrationHandler) newServerDetails(slot NamedSlot) *ServerDetails {
	details := &ServerDetails{
		ServerSummary: NewServerSummary(slot),
		Warnings:      sah.portWarnings.For(slot.ID),
	}

	if slot.Store == nil {
		return details
	}

	ports := portcheck.Slot{ID: string(slot.ID), Name: slot.Name}
	var err error

	if ports.Server, err = slot.Store.LoadServerOptions(); err != nil {
		logrus.WithError(err).Errorf("Could not load the server options of server %q", slot.Name)
	}

	if ports.Stracker, err = slot.Store.LoadStrackerOptions(); err != nil {
		logrus.WithError(err).Errorf("Could not load the stracker options of server %q", slot.Name)
	}

	if ports.KissMyRank, err = slot.Store.LoadKissMyRankOptions(); err != nil {
		logrus.WithError(err).Errorf("Could not load the KissMyRank options of server %q", slot.Name)
	}

	if ports.RealPenalty, err = slot.Store.LoadRealPenaltyOptions(); err != nil {
		logrus.WithError(err).Errorf("Could not load the Real Penalty options of server %q", slot.Name)
	}

	// every plugin on its own: Uses only lists the ports of enabled plugins
	only := func(f func(*portcheck.Slot)) []ServerPort {
		p := portcheck.Slot{ID: ports.ID, Name: ports.Name}
		f(&p)

		return serverPorts(portcheck.Uses(p))
	}

	details.Ports = serverPorts(portcheck.Uses(portcheck.Slot{ID: ports.ID, Name: ports.Name, Server: ports.Server}))

	details.Plugins = []ServerPlugin{
		{
			Name:       "sTracker",
			Enabled:    ports.Stracker != nil && ports.Stracker.EnableStracker,
			OptionsURL: "/stracker/options",
			Ports:      only(func(p *portcheck.Slot) { p.Stracker = ports.Stracker }),
		},
		{
			Name:       "KissMyRank",
			Enabled:    ports.KissMyRank != nil && ports.KissMyRank.EnableKissMyRank,
			OptionsURL: "/kissmyrank/options",
			Ports:      only(func(p *portcheck.Slot) { p.KissMyRank = ports.KissMyRank }),
		},
		{
			Name:       "Real Penalty",
			Enabled:    ports.RealPenalty != nil && ports.RealPenalty.RealPenaltyAppConfig.General.EnableRealPenalty,
			OptionsURL: "/realpenalty/options",
			Ports:      only(func(p *portcheck.Slot) { p.RealPenalty = ports.RealPenalty }),
		},
	}

	return details
}

// Servers shows all servers with their status, ports, plugins and port conflicts in one place. It does not depend on
// the server the request selects.
func (sah *ServerAdministrationHandler) Servers(w http.ResponseWriter, r *http.Request) {
	slots := sah.slots()
	details := make([]*ServerDetails, 0, len(slots))

	for _, slot := range slots {
		details = append(details, sah.newServerDetails(slot))
	}

	sah.ViewRenderer.MustLoadTemplate(w, r, "server/servers.html", &serversTemplateVars{
		Details: details,
	})
}
