package store

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

const userSkinsDir = "user-skins"

// sortUserSkins orders skins by car, name (case insensitive) and id, so both stores list them identically.
func sortUserSkins(skins []*smmodel.UserSkin) {
	sort.SliceStable(skins, func(i, j int) bool {
		a, b := skins[i], skins[j]

		if a.Car != b.Car {
			return a.Car < b.Car
		}

		if an, bn := strings.ToLower(a.Name), strings.ToLower(b.Name); an != bn {
			return an < bn
		}

		return a.ID.String() < b.ID.String()
	})
}

func (rs *JSONStore) UpsertUserSkin(skin *smmodel.UserSkin) error {
	skin.Updated = time.Now()

	return rs.encodeFile(rs.shared, filepath.Join(userSkinsDir, skin.ID.String()+".json"), skin)
}

func (rs *JSONStore) FindUserSkinByID(id string) (*smmodel.UserSkin, error) {
	var skin *smmodel.UserSkin

	err := rs.decodeFile(rs.shared, filepath.Join(userSkinsDir, id+".json"), &skin)

	if os.IsNotExist(err) {
		return nil, smmodel.ErrUserSkinNotFound
	} else if err != nil {
		return nil, err
	}

	return skin, nil
}

func (rs *JSONStore) ListUserSkins() ([]*smmodel.UserSkin, error) {
	files, err := rs.listFiles(filepath.Join(rs.shared, userSkinsDir))

	if err != nil {
		return nil, err
	}

	var skins []*smmodel.UserSkin

	for _, file := range files {
		if skin, err := rs.FindUserSkinByID(file); err == nil {
			skins = append(skins, skin)
		}
	}

	sortUserSkins(skins)

	return skins, nil
}

func (rs *JSONStore) DeleteUserSkin(id string) error {
	if _, err := rs.FindUserSkinByID(id); err != nil {
		return err
	}

	return os.Remove(filepath.Join(rs.shared, userSkinsDir, id+".json"))
}
