package servermanager

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/sirupsen/logrus"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/pluginhttp"
	"github.com/wernersbacher/ACPitwall/internal/portcheck"
	"github.com/wernersbacher/ACPitwall/internal/servers"
)

// pluginSlotFor looks up the server slot of a request for the plugin handlers.
func (r *Resolver) pluginSlotFor(req *http.Request) (*pluginhttp.Slot, error) {
	slot, err := r.slotForRequest(req)

	if err != nil {
		return nil, err
	}

	return &pluginhttp.Slot{Store: slot.Store, Env: slot.Env}, nil
}

// portSlots collects the ports every slot uses (see portcheck) from the options in the store. Options which can't be
// loaded are left out, so that a broken store does not stop an event from starting.
func (r *Resolver) portSlots() []portcheck.Slot {
	slots := make([]portcheck.Slot, 0, r.registry.Len())

	for _, slot := range r.registry.All() {
		ports := portcheck.Slot{ID: string(slot.ID()), Name: slot.Name}

		if slot.Process != nil {
			ports.Running = slot.Process.IsRunning()
		}

		if slot.Store != nil {
			var err error

			if ports.Server, err = slot.Store.LoadServerOptions(); err != nil {
				logrus.WithError(err).Errorf("Could not load the server options of server %q to check its ports", slot.Name)
			}

			if ports.Stracker, err = slot.Store.LoadStrackerOptions(); err != nil {
				logrus.WithError(err).Errorf("Could not load the stracker options of server %q to check its ports", slot.Name)
			}

			if ports.KissMyRank, err = slot.Store.LoadKissMyRankOptions(); err != nil {
				logrus.WithError(err).Errorf("Could not load the KissMyRank options of server %q to check its ports", slot.Name)
			}

			if ports.RealPenalty, err = slot.Store.LoadRealPenaltyOptions(); err != nil {
				logrus.WithError(err).Errorf("Could not load the Real Penalty options of server %q to check its ports", slot.Name)
			}
		}

		slots = append(slots, ports)
	}

	return slots
}

// portWarnings returns the warnings about the ports the server with the id shares with other servers, for the options
// pages.
func (r *Resolver) portWarnings(id smmodel.ServerID) []string {
	if r.registry.Len() < 2 {
		return nil
	}

	var warnings []string

	for _, conflict := range portcheck.ConflictsFor(r.portSlots(), string(id)) {
		warnings = append(warnings, conflict.Message(string(id)))
	}

	return warnings
}

// checkPorts is asked before an event is started on a slot. It logs the ports the slot shares with other slots, and
// refuses to start if an acServer port collides with a server which is running: the acServer could not bind it.
func (r *Resolver) checkPorts(slot *servers.Slot) error {
	if r.registry.Len() < 2 {
		return nil
	}

	id := string(slot.ID())
	slots := r.portSlots()

	for _, conflict := range portcheck.ConflictsFor(slots, id) {
		logrus.Warnf("Port conflict of server %q: %s", slot.Name, conflict.Message(id))
	}

	if conflicts := portcheck.RunningServerConflicts(slots, id); len(conflicts) > 0 {
		messages := make([]string, 0, len(conflicts))

		for _, conflict := range conflicts {
			messages = append(messages, fmt.Sprintf("port %d (%s) is used by the %s", conflict.Port, conflict.Protocol, conflict.Other(id)))
		}

		return fmt.Errorf("servermanager: server %q can't be started, its ports collide with a running server: %s. Change the ports in the server options or stop the other server", slot.Name, strings.Join(messages, "; "))
	}

	return nil
}
