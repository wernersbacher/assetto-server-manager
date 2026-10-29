package servermanager

import (
	"fmt"

	"github.com/etcd-io/bbolt"
	smconfig "github.com/wernersbacher/assetto-server-manager/internal/config"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
)

// BuildStore opens the Store configured in StoreConfig and runs migrations.
func BuildStore(s *smconfig.StoreConfig) (Store, error) {
	var rs Store

	if s.SharedPath == "" {
		s.SharedPath = s.Path
	}

	switch s.Type {
	case "boltdb":
		bbdb, err := bbolt.Open(s.Path, 0644, nil)

		if err != nil {
			return nil, err
		}

		rs = NewBoltStore(bbdb)
	case "json":
		rs = NewJSONStore(s.Path, s.SharedPath)
	default:
		return nil, fmt.Errorf("invalid store type (%s), must be either boltdb/json", s.Type)
	}

	if err := Migrate(rs); err != nil {
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
