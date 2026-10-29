// Package serverenv carries the process wide state which the managers need but which is not owned by them: the id of
// this server manager instance, the installation path of the Assetto Corsa server and the panic handler. The values
// are functions because they are only known after the managers have been created (the install path is set after the
// resolver, the server id once the store has been initialised).
package serverenv

import (
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
)

// Env is the environment of the managers. All fields have to be set.
type Env struct {
	// ServerID identifies this server manager instance.
	ServerID func() smmodel.ServerID

	// InstallPath is the directory in which the Assetto Corsa server is installed.
	InstallPath func() string

	// PanicCapture runs fn and recovers from (and logs) a panic. It wraps background goroutines.
	PanicCapture func(fn func())

	// Debug is true if the server manager runs in debug mode.
	Debug bool
}
