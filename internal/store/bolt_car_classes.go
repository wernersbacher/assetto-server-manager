package store

import (
	"time"

	"go.etcd.io/bbolt"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

var carClassesBucketName = []byte("car-classes")

func (rs *BoltStore) UpsertCarClass(class *smmodel.CarClass) error {
	return rs.db.Update(func(tx *bbolt.Tx) error {
		bkt, err := tx.CreateBucketIfNotExists(carClassesBucketName)

		if err != nil {
			return err
		}

		class.Updated = time.Now()

		encoded, err := rs.encode(class)

		if err != nil {
			return err
		}

		return bkt.Put([]byte(class.ID.String()), encoded)
	})
}

func (rs *BoltStore) FindCarClassByID(id string) (*smmodel.CarClass, error) {
	id, err := canonicalCarClassID(id)

	if err != nil {
		return nil, err
	}

	var class *smmodel.CarClass

	err = rs.db.View(func(tx *bbolt.Tx) error {
		bkt := tx.Bucket(carClassesBucketName)

		if bkt == nil {
			return smmodel.ErrCarClassNotFound
		}

		data := bkt.Get([]byte(id))

		if data == nil {
			return smmodel.ErrCarClassNotFound
		}

		return rs.decode(data, &class)
	})

	return class, err
}

func (rs *BoltStore) ListCarClasses() ([]*smmodel.CarClass, error) {
	var classes []*smmodel.CarClass

	err := rs.db.View(func(tx *bbolt.Tx) error {
		bkt := tx.Bucket(carClassesBucketName)

		if bkt == nil {
			return nil
		}

		return bkt.ForEach(func(k, v []byte) error {
			var class *smmodel.CarClass

			if err := rs.decode(v, &class); err != nil {
				return err
			}

			classes = append(classes, class)

			return nil
		})
	})

	sortCarClasses(classes)

	return classes, err
}

func (rs *BoltStore) DeleteCarClass(id string) error {
	id, err := canonicalCarClassID(id)

	if err != nil {
		return err
	}

	return rs.db.Update(func(tx *bbolt.Tx) error {
		bkt := tx.Bucket(carClassesBucketName)

		if bkt == nil || bkt.Get([]byte(id)) == nil {
			return smmodel.ErrCarClassNotFound
		}

		return bkt.Delete([]byte(id))
	})
}
