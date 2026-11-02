package servermanager

import (
	"os"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/racecontrol"
	smserver "github.com/wernersbacher/ACPitwall/internal/server"
	"github.com/wernersbacher/ACPitwall/internal/servers"
	"github.com/wernersbacher/ACPitwall/pkg/udp"
)

// devMockLiveTimingEnv switches on the mock session for UI development (dev/run.ps1 -Mock): "practice",
// "qualifying" or "race" (any other non-empty value is a race). The default server gets that session, every other
// server a practice.
const devMockLiveTimingEnv = "DEV_MOCK_LIVE_TIMING"

func devMockValue() string {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(devMockLiveTimingEnv)))

	if value == "false" {
		return ""
	}

	return value
}

// devMockProcess is the process of a slot in mock mode: it starts no acServer, it only remembers the event, so the
// slot counts as running (status, live timing page, server overview).
type devMockProcess struct {
	smserver.ServerProcess

	mutex sync.Mutex
	event smmodel.RaceEvent
}

func (p *devMockProcess) Start(event smmodel.RaceEvent, _ string, _ int, _ string, _ int) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.event = event

	return nil
}

func (p *devMockProcess) Stop() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.event = nil

	return nil
}

func (p *devMockProcess) Restart() error { return nil }

func (p *devMockProcess) IsRunning() bool {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	return p.event != nil
}

func (p *devMockProcess) Event() smmodel.RaceEvent {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.event == nil {
		return smmodel.QuickRace{}
	}

	return p.event
}

func (p *devMockProcess) SendUDPMessage(udp.Message) error { return nil }

// wrapDevMockProcess replaces the process of a slot by a devMockProcess if the mock mode is on.
func wrapDevMockProcess(process smserver.ServerProcess) smserver.ServerProcess {
	if devMockValue() == "" {
		return process
	}

	return &devMockProcess{ServerProcess: process}
}

// startDevMockLiveTiming starts a custom race on the slot (through the race manager, like a real start) and replays
// a made-up session into its live timing, if the mock mode is on. It never runs in normal operation.
func startDevMockLiveTiming(slot *servers.Slot, isDefault bool) {
	value := devMockValue()

	if value == "" {
		return
	}

	sessionType := racecontrol.MockSessionType(value)

	if !isDefault {
		sessionType = udp.SessionTypePractice
	}

	logrus.Warnf("%s is set: running a mock %s on server %q", devMockLiveTimingEnv, sessionType, slot.ID())

	if err := slot.RaceManager.ApplyConfigAndStart(racecontrol.MockEvent(sessionType)); err != nil {
		logrus.WithError(err).Errorf("Could not start the mock event on server %q", slot.ID())
		return
	}

	slot.RaceControl.ReplayMockSession(sessionType)
}
