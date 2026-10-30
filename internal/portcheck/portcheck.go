// Package portcheck finds ports which several server slots would use on the host: every slot is an acServer with its
// own plugins, and two slots must not listen on the same port. The comparison is a pure function on the options of the
// slots, so the options pages, the start of an event and the tests can all use it.
package portcheck

import (
	"fmt"
	"net"
	"sort"
	"strconv"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smplugins "github.com/wernersbacher/assetto-server-manager/internal/plugins"
)

// Protocol is the transport protocol of a port. A TCP and a UDP port with the same number do not conflict.
type Protocol string

const (
	TCP Protocol = "tcp"
	UDP Protocol = "udp"
)

// Kind tells what a port belongs to.
type Kind int

const (
	// KindServer is a port of the acServer itself (and of the UDP plugin ports Server Manager sets up for it). Two
	// running servers can't share it.
	KindServer Kind = iota

	// KindPlugin is a port of a plugin (stracker, KissMyRank, Real Penalty) or of the Content Manager wrapper.
	KindPlugin
)

// Slot is what is known about the ports of one server slot. The plugin options may be nil, a plugin is only taken
// into account if it is enabled.
type Slot struct {
	ID   string
	Name string

	// Running is true while the server of the slot runs an event.
	Running bool

	Server      *serverconfig.GlobalServerConfig
	Stracker    *smplugins.StrackerConfiguration
	KissMyRank  *smplugins.KissMyRankConfig
	RealPenalty *smplugins.RealPenaltyConfig
}

// Use is one port of a slot.
type Use struct {
	SlotID   string
	SlotName string

	// What is the setting the port comes from, e.g. "acServer UDP port" or "stracker HTTP port".
	What     string
	Port     int
	Protocol Protocol
	Kind     Kind
}

func (u Use) String() string {
	return fmt.Sprintf("%s of server %q", u.What, u.SlotName)
}

// Conflict is a port which two slots use.
type Conflict struct {
	Port     int
	Protocol Protocol
	A, B     Use
}

// Involves returns true if one of the two uses belongs to the slot.
func (c Conflict) Involves(slotID string) bool {
	return c.A.SlotID == slotID || c.B.SlotID == slotID
}

// Other returns the use of the other slot, as seen from the slot with the id.
func (c Conflict) Other(slotID string) Use {
	if c.A.SlotID == slotID {
		return c.B
	}

	return c.A
}

// Own returns the use of the slot with the id.
func (c Conflict) Own(slotID string) Use {
	if c.A.SlotID == slotID {
		return c.A
	}

	return c.B
}

// Message describes the conflict for the slot with the id, for the warning shown to the user.
func (c Conflict) Message(slotID string) string {
	own, other := c.Own(slotID), c.Other(slotID)

	return fmt.Sprintf("Port %d (%s) is used by the %s and by the %s. Change one of them.", c.Port, c.Protocol, own.What, other)
}

// Uses lists the host ports a slot uses. Ports which are 0 or negative (unset, chosen by the plugin) are left out, and
// so are plugins which are not enabled.
func Uses(slot Slot) []Use {
	var uses []Use

	add := func(kind Kind, what string, protocol Protocol, port int) {
		if port <= 0 || port > 65535 {
			return
		}

		uses = append(uses, Use{SlotID: slot.ID, SlotName: slot.Name, What: what, Port: port, Protocol: protocol, Kind: kind})
	}

	if so := slot.Server; so != nil {
		add(KindServer, "acServer UDP port", UDP, so.UDPPort)
		add(KindServer, "acServer TCP port", TCP, so.TCPPort)
		add(KindServer, "acServer HTTP port", TCP, so.HTTPPort)
		add(KindServer, "UDP plugin local port", UDP, so.UDPPluginLocalPort)

		if port, ok := localPort(so.UDPPluginAddress); ok {
			add(KindServer, "UDP plugin address", UDP, port)
		}

		if so.EnableContentManagerWrapper == 1 {
			add(KindPlugin, "Content Manager wrapper port", TCP, so.ContentManagerWrapperPort)
		}
	}

	if st := slot.Stracker; st != nil && st.EnableStracker {
		add(KindPlugin, "stracker listening port", TCP, st.InstanceConfiguration.ListeningPort)

		if st.HTTPConfiguration.Enabled {
			add(KindPlugin, "stracker HTTP port", TCP, st.HTTPConfiguration.ListenPort)
		}

		add(KindPlugin, "stracker proxy plugin local port", UDP, st.ACPlugin.ProxyPluginLocalPort)
		add(KindPlugin, "stracker proxy plugin port", UDP, st.ACPlugin.ProxyPluginPort)
	}

	if kmr := slot.KissMyRank; kmr != nil && kmr.EnableKissMyRank {
		add(KindPlugin, "KissMyRank web stats port", TCP, kmr.WebStatsServerPort)
		add(KindPlugin, "KissMyRank web auth port", TCP, kmr.WebAuthServerPort)
	}

	if rp := slot.RealPenalty; rp != nil && rp.RealPenaltyAppConfig.General.EnableRealPenalty {
		add(KindPlugin, "Real Penalty app TCP port", TCP, rp.RealPenaltyAppConfig.General.AppTCPPort)
		add(KindPlugin, "Real Penalty app UDP port", UDP, rp.RealPenaltyAppConfig.General.AppUDPPort)
	}

	return uses
}

// localPort returns the port of a "host:port" address if the host is this machine (or not given). The plugin address
// of the server options can also point to a plugin on another machine, which doesn't use a port here.
func localPort(address string) (int, bool) {
	host, portStr, err := net.SplitHostPort(address)

	if err != nil {
		return 0, false
	}

	switch host {
	case "", "localhost", "0.0.0.0", "::", "::1", "127.0.0.1":
	default:
		return 0, false
	}

	port, err := strconv.Atoi(portStr)

	return port, err == nil
}

// Conflicts returns the ports which are used by more than one slot, ordered by port. The same port twice within one
// slot is not a conflict between slots and not reported.
func Conflicts(slots []Slot) []Conflict {
	var conflicts []Conflict

	for i, a := range slots {
		usesA := Uses(a)

		for _, b := range slots[i+1:] {
			if a.ID == b.ID {
				continue
			}

			for _, useB := range Uses(b) {
				for _, useA := range usesA {
					if useA.Port == useB.Port && useA.Protocol == useB.Protocol {
						conflicts = append(conflicts, Conflict{Port: useA.Port, Protocol: useA.Protocol, A: useA, B: useB})
					}
				}
			}
		}
	}

	sort.SliceStable(conflicts, func(i, j int) bool {
		if conflicts[i].Port != conflicts[j].Port {
			return conflicts[i].Port < conflicts[j].Port
		}

		return conflicts[i].Protocol < conflicts[j].Protocol
	})

	return conflicts
}

// ConflictsFor returns the conflicts the slot with the id is part of.
func ConflictsFor(slots []Slot, slotID string) []Conflict {
	var conflicts []Conflict

	for _, conflict := range Conflicts(slots) {
		if conflict.Involves(slotID) {
			conflicts = append(conflicts, conflict)
		}
	}

	return conflicts
}

// RunningServerConflicts returns the conflicts between acServer ports of the slot with the id and a slot whose server
// is running: the event of the slot can't be started, as the acServer could not bind the port.
func RunningServerConflicts(slots []Slot, slotID string) []Conflict {
	running := make(map[string]bool)

	for _, slot := range slots {
		if slot.Running {
			running[slot.ID] = true
		}
	}

	var conflicts []Conflict

	for _, conflict := range ConflictsFor(slots, slotID) {
		if conflict.A.Kind == KindServer && conflict.B.Kind == KindServer && running[conflict.Other(slotID).SlotID] {
			conflicts = append(conflicts, conflict)
		}
	}

	return conflicts
}
