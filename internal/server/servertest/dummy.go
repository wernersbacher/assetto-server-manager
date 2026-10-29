// Package servertest provides a stand-in for the server process, for the tests of the packages which use it.
package servertest

import (
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	"github.com/wernersbacher/assetto-server-manager/pkg/udp"
)

// Dummy is a server process which is always running, an active championship, and does nothing.
type Dummy struct{}

func (Dummy) Start(event smmodel.RaceEvent, udpPluginAddress string, udpPluginLocalPort int, forwardingAddress string, forwardListenPort int) error {
	return nil
}

func (Dummy) Logs() string {
	return ""
}

func (Dummy) Stop() error {
	return nil
}

func (Dummy) Restart() error {
	return nil
}

func (Dummy) IsRunning() bool {
	return true
}

func (Dummy) Event() smmodel.RaceEvent {
	return &smmodel.ActiveChampionship{}
}

func (Dummy) UDPCallback(message udp.Message) {
}

func (Dummy) SendUDPMessage(message udp.Message) error {
	return nil
}

func (Dummy) NotifyDone(chan struct{}) {
}
