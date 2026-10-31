package serverenv

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

// DefaultInstallPath is the install path before State.SetInstallPath was called.
const DefaultInstallPath = "assetto"

// State is the state behind an Env that is only known after the managers have been created: the install path (set
// from the configuration once the store has been built and migrated), the server id (set once the store has been
// initialised) and the fallback sorting server option (changed when the server options are saved). The Env built by
// State.Env reads the values whenever it is asked, so they can be changed after the managers were created.
type State struct {
	mu              sync.RWMutex
	installPath     string
	serverID        smmodel.ServerID
	fallBackSorting bool
}

// NewState returns a State with the DefaultInstallPath.
func NewState() *State {
	return &State{installPath: DefaultInstallPath}
}

// InstallPath is the directory in which the Assetto Corsa server is installed.
func (s *State) InstallPath() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.installPath
}

// SetInstallPath sets the install path. A relative path is made absolute by joining it to the working directory.
func (s *State) SetInstallPath(installPath string) error {
	if !filepath.IsAbs(installPath) {
		wd, err := os.Getwd()

		if err != nil {
			return fmt.Errorf("serverenv: unable to get working directory: %v", err)
		}

		installPath = filepath.Join(wd, installPath)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.installPath = installPath

	return nil
}

// ServerID identifies this ACPitwall instance.
func (s *State) ServerID() smmodel.ServerID {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.serverID
}

// SetServerID sets the id of this ACPitwall instance.
func (s *State) SetServerID(id smmodel.ServerID) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.serverID = id
}

// FallBackSorting is true if loaded results are to use SessionResults.FallBackSort (a server option).
func (s *State) FallBackSorting() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.fallBackSorting
}

// SetFallBackSorting sets the fallback sorting server option.
func (s *State) SetFallBackSorting(fallBackSorting bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.fallBackSorting = fallBackSorting
}

// EnvFor builds the Env of one server slot: the install path and the server id (read whenever it is asked, as it
// may only be known once the store has been initialised) are those of the slot, resultsDirs returns the results
// directories of all slots. The fallback sorting option is read from this State, so it is shared by all slots.
func (s *State) EnvFor(installPath string, serverID func() smmodel.ServerID, resultsDirs func() []string, panicCapture func(fn func()), debug bool) *Env {
	return &Env{
		ServerID:        serverID,
		InstallPath:     func() string { return installPath },
		ResultsDirs:     resultsDirs,
		FallBackSorting: s.FallBackSorting,
		PanicCapture:    panicCapture,
		Debug:           debug,
	}
}

// Env builds the Env which reads this State.
func (s *State) Env(panicCapture func(fn func()), debug bool) *Env {
	return &Env{
		ServerID:        s.ServerID,
		InstallPath:     s.InstallPath,
		FallBackSorting: s.FallBackSorting,
		PanicCapture:    panicCapture,
		Debug:           debug,
	}
}
