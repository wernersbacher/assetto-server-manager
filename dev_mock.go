package servermanager

import (
	"os"

	"github.com/sirupsen/logrus"
	"github.com/wernersbacher/ACPitwall/internal/racecontrol"
	"github.com/wernersbacher/ACPitwall/internal/servers"
)

// devMockLiveTimingEnv switches on the mock live timing session for UI development (dev/run.ps1 -Mock):
// "practice", "qualifying" or "race" (any other non-empty value is a race).
const devMockLiveTimingEnv = "DEV_MOCK_LIVE_TIMING"

// startDevMockLiveTiming replays a made-up session into the live timing of the default slot, if DEV_MOCK_LIVE_TIMING
// is set. It never runs in normal operation.
func startDevMockLiveTiming(slot *servers.Slot) {
	value := os.Getenv(devMockLiveTimingEnv)

	if value == "" || value == "false" {
		return
	}

	logrus.Warnf("%s is set: showing a mock live timing session on server %q", devMockLiveTimingEnv, slot.ID())

	slot.RaceControl.ReplayMockSession(racecontrol.MockSessionType(value))
}
