package store

import (
	"time"

	"go.etcd.io/bbolt"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

var userSkinsBucketName = []byte("user-skins")

func (rs *BoltStore) UpsertUserSkin(skin *smmodel.UserSkin) error {
	return rs.db.Update(func(tx *bbolt.Tx) error {
		bkt, err := tx.CreateBucketIfNotExists(userSkinsBucketName)

		if err != nil {
			return err
		}

		skin.Updated = time.Now()

		encoded, err := rs.encode(skin)

		if err != nil {
			return err
		}

		return bkt.Put([]byte(skin.ID.String()), encoded)
	})
}

func (rs *BoltStore) FindUserSkinByID(id string) (*smmodel.UserSkin, error) {
	var skin *smmodel.UserSkin

	err := rs.db.View(func(tx *bbolt.Tx) error {
		bkt := tx.Bucket(userSkinsBucketName)

		if bkt == nil {
			return smmodel.ErrUserSkinNotFound
		}

		data := bkt.Get([]byte(id))

		if data == nil {
			return smmodel.ErrUserSkinNotFound
		}

		return rs.decode(data, &skin)
	})

	return skin, err
}

func (rs *BoltStore) ListUserSkins() ([]*smmodel.UserSkin, error) {
	var skins []*smmodel.UserSkin

	err := rs.db.View(func(tx *bbolt.Tx) error {
		bkt := tx.Bucket(userSkinsBucketName)

		if bkt == nil {
			return nil
		}

		return bkt.ForEach(func(k, v []byte) error {
			var skin *smmodel.UserSkin

			if err := rs.decode(v, &skin); err != nil {
				return err
			}

			skins = append(skins, skin)

			return nil
		})
	})

	sortUserSkins(skins)

	return skins, err
}

func (rs *BoltStore) DeleteUserSkin(id string) error {
	return rs.db.Update(func(tx *bbolt.Tx) error {
		bkt := tx.Bucket(userSkinsBucketName)

		if bkt == nil || bkt.Get([]byte(id)) == nil {
			return smmodel.ErrUserSkinNotFound
		}

		return bkt.Delete([]byte(id))
	})
}
