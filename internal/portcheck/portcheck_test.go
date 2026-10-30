package portcheck

import (
	"strings"
	"testing"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smplugins "github.com/wernersbacher/assetto-server-manager/internal/plugins"
)

func serverOptions(udp, tcp, http int) *serverconfig.GlobalServerConfig {
	so := serverconfig.ConfigIniDefault().GlobalServerConfig
	so.UDPPort, so.TCPPort, so.HTTPPort = udp, tcp, http
	so.UDPPluginLocalPort = 0
	so.UDPPluginAddress = ""

	return &so
}

func TestConflicts_None(t *testing.T) {
	slots := []Slot{
		{ID: "a", Name: "Alpha", Server: serverOptions(9600, 9600, 8081)},
		{ID: "b", Name: "Beta", Server: serverOptions(9601, 9601, 8082)},
	}

	if conflicts := Conflicts(slots); len(conflicts) != 0 {
		t.Errorf("expected no conflicts, got %+v", conflicts)
	}
}

func TestConflicts_ServerPorts(t *testing.T) {
	slots := []Slot{
		{ID: "a", Name: "Alpha", Server: serverOptions(9600, 9600, 8081)},
		{ID: "b", Name: "Beta", Server: serverOptions(9600, 9601, 8081)},
	}

	conflicts := Conflicts(slots)

	if len(conflicts) != 2 {
		t.Fatalf("expected 2 conflicts, got %+v", conflicts)
	}

	// ordered by port
	if conflicts[0].Port != 8081 || conflicts[0].Protocol != TCP || conflicts[1].Port != 9600 || conflicts[1].Protocol != UDP {
		t.Errorf("unexpected conflicts: %+v", conflicts)
	}

	msg := conflicts[1].Message("a")

	for _, want := range []string{"9600", "udp", "acServer UDP port", `"Beta"`} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not contain %q", msg, want)
		}
	}
}

func TestConflicts_ProtocolsAreSeparate(t *testing.T) {
	// the HTTP (tcp) port of b is the UDP and TCP port of a
	slots := []Slot{
		{ID: "a", Name: "Alpha", Server: serverOptions(9600, 9600, 8081)},
		{ID: "b", Name: "Beta", Server: serverOptions(9700, 9701, 9600)},
	}

	conflicts := Conflicts(slots)

	// 9600/tcp of a is the HTTP port 9600/tcp of b, 9600/udp is only used by a
	if len(conflicts) != 1 || conflicts[0].Protocol != TCP || conflicts[0].Port != 9600 {
		t.Errorf("expected one tcp conflict on 9600, got %+v", conflicts)
	}
}

func TestConflicts_SameSlotIsNoConflict(t *testing.T) {
	slots := []Slot{{ID: "a", Name: "Alpha", Server: serverOptions(9600, 9600, 9600)}}

	if conflicts := Conflicts(slots); len(conflicts) != 0 {
		t.Errorf("expected no conflicts within a slot, got %+v", conflicts)
	}
}

func TestConflicts_UDPPluginPorts(t *testing.T) {
	a := serverOptions(9600, 9600, 8081)
	a.UDPPluginLocalPort = 12000
	a.UDPPluginAddress = "127.0.0.1:12001"

	b := serverOptions(9601, 9601, 8082)
	b.UDPPluginLocalPort = 12000
	b.UDPPluginAddress = "127.0.0.1:12001"

	// a plugin on another machine does not use a port here
	c := serverOptions(9602, 9602, 8083)
	c.UDPPluginAddress = "10.0.0.5:12001"

	conflicts := Conflicts([]Slot{{ID: "a", Name: "A", Server: a}, {ID: "b", Name: "B", Server: b}, {ID: "c", Name: "C", Server: c}})

	if len(conflicts) != 2 {
		t.Fatalf("expected the two UDP plugin ports to conflict, got %+v", conflicts)
	}

	for _, conflict := range conflicts {
		if conflict.Involves("c") {
			t.Errorf("a remote plugin address must not conflict: %+v", conflict)
		}
	}
}

func TestConflicts_PluginsOnlyWhenEnabled(t *testing.T) {
	newSlot := func(id string, udp, tcp, http int, enabled bool) Slot {
		stracker := smplugins.DefaultStrackerIni("")
		stracker.EnableStracker = enabled

		kmr := smplugins.DefaultKissMyRankConfig()
		kmr.EnableKissMyRank = enabled

		rp := smplugins.DefaultRealPenaltyConfig()
		rp.RealPenaltyAppConfig.General.EnableRealPenalty = enabled

		return Slot{ID: id, Name: id, Server: serverOptions(udp, tcp, http), Stracker: stracker, KissMyRank: kmr, RealPenalty: rp}
	}

	// the plugins of both slots have their default ports, but are disabled in one of them
	if conflicts := Conflicts([]Slot{newSlot("a", 9600, 9600, 8081, true), newSlot("b", 9601, 9601, 8082, false)}); len(conflicts) != 0 {
		t.Errorf("a disabled plugin must not conflict, got %+v", conflicts)
	}

	conflicts := Conflicts([]Slot{newSlot("a", 9600, 9600, 8081, true), newSlot("b", 9601, 9601, 8082, true)})

	found := make(map[string]bool)

	for _, conflict := range conflicts {
		if conflict.A.Kind != KindPlugin || conflict.B.Kind != KindPlugin {
			t.Errorf("expected only plugin conflicts, got %+v", conflict)
		}

		found[conflict.A.What] = true
	}

	for _, what := range []string{"stracker listening port", "stracker HTTP port", "KissMyRank web stats port", "KissMyRank web auth port", "Real Penalty app TCP port", "Real Penalty app UDP port"} {
		if !found[what] {
			t.Errorf("expected a conflict of %q, got %+v", what, conflicts)
		}
	}
}

func TestRunningServerConflicts(t *testing.T) {
	slots := []Slot{
		{ID: "a", Name: "Alpha", Running: true, Server: serverOptions(9600, 9600, 8081)},
		{ID: "b", Name: "Beta", Server: serverOptions(9600, 9601, 8082)},
		{ID: "c", Name: "Gamma", Server: serverOptions(9600, 9602, 8083)},
	}

	conflicts := RunningServerConflicts(slots, "b")

	if len(conflicts) != 1 || conflicts[0].Other("b").SlotID != "a" {
		t.Errorf("expected b to conflict with the running a only, got %+v", conflicts)
	}

	// c conflicts with a and b, but only a runs
	if conflicts := RunningServerConflicts(slots, "c"); len(conflicts) != 1 || conflicts[0].Other("c").SlotID != "a" {
		t.Errorf("expected c to conflict with the running a only, got %+v", conflicts)
	}

	// a stopped slot can start whatever its neighbours use
	slots[0].Running = false

	if conflicts := RunningServerConflicts(slots, "b"); len(conflicts) != 0 {
		t.Errorf("expected no conflict with stopped servers, got %+v", conflicts)
	}

	// plugin ports of running servers don't prevent the start
	stracker := smplugins.DefaultStrackerIni("")
	stracker.EnableStracker = true
	slots[0].Running = true
	slots[0].Server = serverOptions(9700, 9700, 8091)
	slots[0].Stracker = stracker
	slots[1].Server = serverOptions(9701, 9701, 8092)
	slots[1].Stracker = stracker

	if len(ConflictsFor(slots, "b")) == 0 {
		t.Fatal("expected the stracker ports to conflict")
	}

	if conflicts := RunningServerConflicts(slots, "b"); len(conflicts) != 0 {
		t.Errorf("plugin conflicts must not be reported as server conflicts, got %+v", conflicts)
	}
}
