package servermanager

import (
	"fmt"

	"github.com/etcd-io/bbolt"
	smconfig "github.com/wernersbacher/ACPitwall/internal/config"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
)

// BuildStore opens the Store configured in StoreConfig and runs migrations. installPath returns the install path of
// the Assetto Corsa server; it is called when the path is needed, because it is only set after the store was built.
func BuildStore(s *smconfig.StoreConfig, installPath func() string) (smstore.Store, error) {
	var rs smstore.Store

	if s.SharedPath == "" {
		s.SharedPath = s.Path
	}

	switch s.Type {
	case "boltdb":
		bbdb, err := bbolt.Open(s.Path, 0644, nil)

		if err != nil {
			return nil, err
		}

		rs = smstore.NewBoltStore(bbdb, smstore.WithInstallPath(installPath))
	case "json":
		rs = smstore.NewJSONStore(s.Path, s.SharedPath, smstore.WithInstallPath(installPath))
	default:
		return nil, fmt.Errorf("invalid store type (%s), must be either boltdb/json", s.Type)
	}

	if err := smstore.Migrate(rs, smstore.MigrationEnv{BuildVersion: BuildVersion, InstallPath: installPath}); err != nil {
		return nil, err
	}

	return rs, nil
}
