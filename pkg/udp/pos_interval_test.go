package udp

import "testing"

// Every connection (one per server slot) has its own real time pos interval state.
func TestAssettoServerUDPPosIntervalIsPerConnection(t *testing.T) {
	a, b := &AssettoServerUDP{}, &AssettoServerUDP{}

	if a.posIntervalModifierEnabled() || b.posIntervalModifierEnabled() {
		t.Fatal("expected the modifier to be disabled until it is set")
	}

	a.SetPosIntervalModifierEnabled(true)

	if !a.posIntervalModifierEnabled() || b.posIntervalModifierEnabled() {
		t.Error("enabling the modifier of one connection must not enable it for the other")
	}

	a.setCurrentRealtimePosInterval(120)
	b.setCurrentRealtimePosInterval(30)

	if a.CurrentRealtimePosInterval() != 120 || b.CurrentRealtimePosInterval() != 30 {
		t.Errorf("expected independent intervals, got %d and %d", a.CurrentRealtimePosInterval(), b.CurrentRealtimePosInterval())
	}

	a.SetPosIntervalModifierEnabled(false)

	if a.posIntervalModifierEnabled() {
		t.Error("expected the modifier to be disabled again")
	}
}
