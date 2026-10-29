// Package envtest builds serverenv.Env values for tests.
package envtest

import (
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	"github.com/wernersbacher/assetto-server-manager/internal/serverenv"
)

// ServerID is the id of the server manager instance in tests.
const ServerID smmodel.ServerID = "test-server"

// New returns an Env for the Assetto Corsa installation in installPath, with the ServerID, no fallback sorting and a
// panic handler which does not recover.
func New(installPath string) *serverenv.Env {
	return &serverenv.Env{
		ServerID:        func() smmodel.ServerID { return ServerID },
		InstallPath:     func() string { return installPath },
		FallBackSorting: func() bool { return false },
		PanicCapture:    func(fn func()) { fn() },
	}
}
