package servermanager

import (
	"fmt"

	"github.com/etcd-io/bbolt"
	smconfig "github.com/wernersbacher/assetto-server-manager/internal/config"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	smstore "github.com/wernersbacher/assetto-server-manager/internal/store"
)

// BuildStore opens the Store configured in StoreConfig and runs migrations.
func BuildStore(s *smconfig.StoreConfig) (smstore.Store, error) {
	var rs smstore.Store

	// the install path is set after the store was built, so it is looked up when it is needed
	installPath := func() string { return ServerInstallPath }

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

type ThemeDetails struct {
	Theme smmodel.Theme
	Name  string
}

var ThemeOptions = []ThemeDetails{
	{
		Theme: smmodel.ThemeDefault,
		Name:  "Use Default",
	},
	{
		Theme: smmodel.ThemeLight,
		Name:  "Light",
	},
	{
		Theme: smmodel.ThemeDark,
		Name:  "Dark",
	},
}
