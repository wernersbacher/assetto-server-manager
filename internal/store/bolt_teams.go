package store

import (
	"time"

	"go.etcd.io/bbolt"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

var teamsBucketName = []byte("teams")

func (rs *BoltStore) teamsBucket(tx *bbolt.Tx) (*bbolt.Bucket, error) {
	if !tx.Writable() {
		bkt := tx.Bucket(teamsBucketName)

		if bkt == nil {
			return nil, bbolt.ErrBucketNotFound
		}

		return bkt, nil
	}

	return tx.CreateBucketIfNotExists(teamsBucketName)
}

func (rs *BoltStore) UpsertTeam(team *smmodel.Team) error {
	return rs.db.Update(func(tx *bbolt.Tx) error {
		bkt, err := rs.teamsBucket(tx)

		if err != nil {
			return err
		}

		team.Updated = time.Now()

		encoded, err := rs.encode(team)

		if err != nil {
			return err
		}

		return bkt.Put([]byte(team.ID.String()), encoded)
	})
}

func (rs *BoltStore) FindTeamByID(id string) (*smmodel.Team, error) {
	var team *smmodel.Team

	err := rs.db.View(func(tx *bbolt.Tx) error {
		bkt, err := rs.teamsBucket(tx)

		if err == bbolt.ErrBucketNotFound {
			return smmodel.ErrTeamNotFound
		} else if err != nil {
			return err
		}

		data := bkt.Get([]byte(id))

		if data == nil {
			return smmodel.ErrTeamNotFound
		}

		return rs.decode(data, &team)
	})

	return team, err
}

func (rs *BoltStore) ListTeams() ([]*smmodel.Team, error) {
	var teams []*smmodel.Team

	err := rs.db.View(func(tx *bbolt.Tx) error {
		bkt, err := rs.teamsBucket(tx)

		if err == bbolt.ErrBucketNotFound {
			return nil
		} else if err != nil {
			return err
		}

		return bkt.ForEach(func(k, v []byte) error {
			var team *smmodel.Team

			if err := rs.decode(v, &team); err != nil {
				return err
			}

			if team.IsDeleted() {
				return nil
			}

			teams = append(teams, team)

			return nil
		})
	})

	sortTeams(teams)

	return teams, err
}

func (rs *BoltStore) DeleteTeam(team *smmodel.Team) error {
	team.Deleted = time.Now()

	return rs.UpsertTeam(team)
}
