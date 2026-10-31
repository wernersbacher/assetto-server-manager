package servertest

import (
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

// Interruptible behaves like the AssettoServerProcess when it comes to starting events: starting an event while
// another one is running stops the running one first. It records which events were started and how often it was
// stopped.
type Interruptible struct {
	Dummy

	event smmodel.RaceEvent

	// Started are the events which were started, in order.
	Started []smmodel.RaceEvent

	// Stopped is the number of times the process was stopped.
	Stopped int
}

func (p *Interruptible) Start(event smmodel.RaceEvent, udpPluginAddress string, udpPluginLocalPort int, forwardingAddress string, forwardListenPort int) error {
	if p.IsRunning() {
		_ = p.Stop()
	}

	p.event = event
	p.Started = append(p.Started, event)

	return nil
}

func (p *Interruptible) Stop() error {
	p.Stopped++
	p.event = nil

	return nil
}

func (p *Interruptible) IsRunning() bool {
	return p.event != nil
}

func (p *Interruptible) Event() smmodel.RaceEvent {
	if p.event == nil {
		return smmodel.QuickRace{}
	}

	return p.event
}

// ClearEvent forgets the running event without counting a stop.
func (p *Interruptible) ClearEvent() {
	p.event = nil
}
