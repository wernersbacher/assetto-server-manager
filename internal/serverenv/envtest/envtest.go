// Package envtest builds serverenv.Env values for tests.
package envtest

import (
	"io/ioutil"
	"os"
	"path/filepath"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/serverenv"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
)

// ServerID is the id of ACPitwall instance in tests.
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

// TempJSONStore returns a JSON store which keeps its files in new temporary directories, and a function which removes
// them. Every test package needs its own store, as the packages' tests run in parallel processes.
func TempJSONStore() (smstore.Store, func()) {
	dir, err := ioutil.TempDir("", "asm-test-store")

	if err != nil {
		panic(err)
	}

	return smstore.NewJSONStore(filepath.Join(dir, "store"), filepath.Join(dir, "shared")), func() { _ = os.RemoveAll(dir) }
}
