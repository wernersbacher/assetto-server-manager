package store

import (
	"github.com/cj123/formulate"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

// ServerOverrides are server options which are fixed by the configuration (config.yml, servers[].ports) and override
// the stored options of a slot. 0 = not set.
type ServerOverrides struct {
	// Game is used for UDPPort and TCPPort.
	Game int
	// HTTP is used for HTTPPort.
	HTTP int
	// ContentManagerWrapper is used for ContentManagerWrapperPort, and also enables the wrapper.
	ContentManagerWrapper int
}

// Any returns true if at least one option is overridden.
func (o ServerOverrides) Any() bool {
	return o.Game != 0 || o.HTTP != 0 || o.ContentManagerWrapper != 0
}

// LockedFields says which server option fields are fixed by the configuration, by their field names in
// serverconfig.GlobalServerConfig. The UI shows them read-only.
type LockedFields struct {
	// UDPPort and TCPPort are locked together (ports.game).
	UDPPort  bool
	TCPPort  bool
	HTTPPort bool
	// ContentManagerWrapperPort and EnableContentManagerWrapper are locked together (ports.content_manager_wrapper).
	ContentManagerWrapperPort   bool
	EnableContentManagerWrapper bool
}

// Any returns true if at least one field is locked.
func (l LockedFields) Any() bool {
	return l.UDPPort || l.TCPPort || l.HTTPPort || l.ContentManagerWrapperPort || l.EnableContentManagerWrapper
}

// Has returns true if the server option field with the name (e.g. "HTTPPort") is locked.
func (l LockedFields) Has(field string) bool {
	switch field {
	case "UDPPort":
		return l.UDPPort
	case "TCPPort":
		return l.TCPPort
	case "HTTPPort":
		return l.HTTPPort
	case "ContentManagerWrapperPort":
		return l.ContentManagerWrapperPort
	case "EnableContentManagerWrapper":
		return l.EnableContentManagerWrapper
	}

	return false
}

// Locked returns the fields the overrides fix.
func (o ServerOverrides) Locked() LockedFields {
	return LockedFields{
		UDPPort:                     o.Game != 0,
		TCPPort:                     o.Game != 0,
		HTTPPort:                    o.HTTP != 0,
		ContentManagerWrapperPort:   o.ContentManagerWrapper != 0,
		EnableContentManagerWrapper: o.ContentManagerWrapper != 0,
	}
}

// Apply writes the overridden values into so.
func (o ServerOverrides) Apply(so *serverconfig.GlobalServerConfig) {
	if o.Game != 0 {
		so.UDPPort = o.Game
		so.TCPPort = o.Game
	}

	if o.HTTP != 0 {
		so.HTTPPort = o.HTTP
	}

	if o.ContentManagerWrapper != 0 {
		so.ContentManagerWrapperPort = o.ContentManagerWrapper
		so.EnableContentManagerWrapper = formulate.BoolNumber(1)
	}
}

// ForServerWithOverrides is ForServer, but loading the server options returns them with the overrides applied, and
// saving them keeps the stored values of the overridden fields (so the effective values shown in the UI are never
// written over what the user stored). Without overrides it is ForServer.
func ForServerWithOverrides(store Store, id smmodel.ServerID, overrides ServerOverrides) Store {
	if !overrides.Any() {
		return ForServer(store, id)
	}

	return &overriddenStore{Store: ForServer(store, id), overrides: overrides}
}

// LockedFieldsOf returns the fields of the server options the store view fixes by configuration (none for stores
// which are not made by ForServerWithOverrides).
func LockedFieldsOf(store Store) LockedFields {
	if o, ok := store.(*overriddenStore); ok {
		return o.overrides.Locked()
	}

	return LockedFields{}
}

type overriddenStore struct {
	Store

	overrides ServerOverrides
}

func (s *overriddenStore) LoadServerOptions() (*serverconfig.GlobalServerConfig, error) {
	so, err := s.Store.LoadServerOptions()

	if err != nil || so == nil {
		return so, err
	}

	s.overrides.Apply(so)

	return so, nil
}

func (s *overriddenStore) UpsertServerOptions(so *serverconfig.GlobalServerConfig) error {
	stored, err := s.Store.LoadServerOptions()

	if err != nil {
		return err
	}

	toSave := *so
	locked := s.overrides.Locked()

	if locked.UDPPort {
		toSave.UDPPort, toSave.TCPPort = stored.UDPPort, stored.TCPPort
	}

	if locked.HTTPPort {
		toSave.HTTPPort = stored.HTTPPort
	}

	if locked.ContentManagerWrapperPort {
		toSave.ContentManagerWrapperPort = stored.ContentManagerWrapperPort
		toSave.EnableContentManagerWrapper = stored.EnableContentManagerWrapper
	}

	return s.Store.UpsertServerOptions(&toSave)
}
