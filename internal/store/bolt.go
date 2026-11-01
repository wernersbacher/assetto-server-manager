package store

import (
	"encoding/json"
	"errors"
	"time"

	"go.etcd.io/bbolt"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smplugins "github.com/wernersbacher/ACPitwall/internal/plugins"
)

type BoltStore struct {
	db   *bbolt.DB
	opts storeOptions
}

func NewBoltStore(db *bbolt.DB, opts ...Option) Store {
	return &BoltStore{db: db, opts: newStoreOptions(opts)}
}

var (
	customRaceBucketName    = []byte("customRaces")
	serverOptionsBucketName = []byte("serverOptions")
	entrantsBucketName      = []byte("entrants")
	championshipsBucketName = []byte("championships")
	accountsBucketName      = []byte("accounts")
	frameLinksBucketName    = []byte("frameLinks")
	raceWeekendsBucketName  = []byte("raceWeekends")
	liveTimingsBucketName   = []byte("liveTimings")

	serverOptionsKey      = []byte("serverOptions")
	strackerOptionsKey    = []byte("strackerOptions")
	kissMyRankOptionsKey  = []byte("kissMyRankOptions")
	realPenaltyOptionsKey = []byte("realPenaltyOptions")
	liveTimingsKey        = []byte("liveTimings")
	lastRaceEventKey      = []byte("lastRaceEvent")
)

func (rs *BoltStore) customRaceBucket(tx *bbolt.Tx) (*bbolt.Bucket, error) {
	if !tx.Writable() {
		bkt := tx.Bucket(customRaceBucketName)

		if bkt == nil {
			return nil, bbolt.ErrBucketNotFound
		}

		return bkt, nil
	}

	return tx.CreateBucketIfNotExists(customRaceBucketName)
}

func (rs *BoltStore) encode(data interface{}) ([]byte, error) {
	return json.Marshal(data)
}

func (rs *BoltStore) decode(data []byte, out interface{}) error {
	return json.Unmarshal(data, out)
}

func (rs *BoltStore) UpsertCustomRace(race *smmodel.CustomRace) error {
	return rs.db.Update(func(tx *bbolt.Tx) error {
		bkt, err := rs.customRaceBucket(tx)

		if err != nil {
			return err
		}

		race.Updated = time.Now()

		encoded, err := rs.encode(race)

		if err != nil {
			return err
		}

		return bkt.Put([]byte(race.UUID.String()), encoded)
	})
}

func (rs *BoltStore) FindCustomRaceByID(uuid string) (*smmodel.CustomRace, error) {
	var customRace *smmodel.CustomRace

	err := rs.db.View(func(tx *bbolt.Tx) error {
		bkt, err := rs.customRaceBucket(tx)

		if err == bbolt.ErrBucketNotFound {
			return ErrCustomRaceNotFound
		} else if err != nil {
			return err
		}

		data := bkt.Get([]byte(uuid))

		if data == nil {
			return ErrCustomRaceNotFound
		}

		return rs.decode(data, &customRace)
	})

	return customRace, err
}

func (rs *BoltStore) ListCustomRaces() ([]*smmodel.CustomRace, error) {
	var customRaces []*smmodel.CustomRace

	err := rs.db.View(func(tx *bbolt.Tx) error {
		bkt, err := rs.customRaceBucket(tx)

		if err == bbolt.ErrBucketNotFound {
			return nil
		} else if err != nil {
			return err
		}

		return bkt.ForEach(func(k, v []byte) error {
			var race *smmodel.CustomRace

			err := rs.decode(v, &race)

			if err != nil {
				return err
			}

			if !race.Deleted.IsZero() {
				// soft deleted race, move on
				return nil
			}

			customRaces = append(customRaces, race)

			return nil
		})
	})

	return customRaces, err
}

func (rs *BoltStore) DeleteCustomRace(race *smmodel.CustomRace) error {
	race.Deleted = time.Now()

	return rs.UpsertCustomRace(race)
}

func (rs *BoltStore) entrantsBucket(tx *bbolt.Tx) (*bbolt.Bucket, error) {
	if !tx.Writable() {
		bkt := tx.Bucket(entrantsBucketName)

		if bkt == nil {
			return nil, bbolt.ErrBucketNotFound
		}

		return bkt, nil
	}

	return tx.CreateBucketIfNotExists(entrantsBucketName)
}

func (rs *BoltStore) UpsertEntrant(entrant serverconfig.Entrant) error {
	return rs.db.Update(func(tx *bbolt.Tx) error {
		bkt, err := rs.entrantsBucket(tx)

		if err != nil {
			return err
		}

		entrant = autoFillEntrant(entrant)

		encoded, err := rs.encode(entrant)

		if err != nil {
			return err
		}

		return bkt.Put([]byte(entrant.ID()), encoded)
	})
}

func (rs *BoltStore) DeleteEntrant(id string) error {
	return rs.db.Update(func(tx *bbolt.Tx) error {
		bkt, err := rs.entrantsBucket(tx)

		if err != nil {
			return err
		}

		return bkt.Delete([]byte(id))
	})
}

func (rs *BoltStore) ListEntrants() ([]*serverconfig.Entrant, error) {
	var entrants []*serverconfig.Entrant

	err := rs.db.View(func(tx *bbolt.Tx) error {
		bkt, err := rs.entrantsBucket(tx)

		if err == bbolt.ErrBucketNotFound {
			return nil
		} else if err != nil {
			return err
		}

		return bkt.ForEach(func(k, v []byte) error {
			var entrant *serverconfig.Entrant

			err := rs.decode(v, &entrant)

			if err != nil {
				return err
			}

			entrants = append(entrants, entrant)

			return nil
		})
	})

	return entrants, err
}

func (rs *BoltStore) frameLinksBucket(tx *bbolt.Tx) (*bbolt.Bucket, error) {
	if !tx.Writable() {
		bkt := tx.Bucket(frameLinksBucketName)

		if bkt == nil {
			return nil, bbolt.ErrBucketNotFound
		}

		return bkt, nil
	}

	return tx.CreateBucketIfNotExists(frameLinksBucketName)
}

func (rs *BoltStore) UpsertLiveFrames(frameLinks []string) error {
	return rs.db.Update(func(tx *bbolt.Tx) error {
		bkt, err := rs.frameLinksBucket(tx)

		if err != nil {
			return err
		}

		encoded, err := rs.encode(frameLinks)

		if err != nil {
			return err
		}

		return bkt.Put([]byte("frameLinks"), encoded)
	})
}

func (rs *BoltStore) ListPrevFrames() ([]string, error) {
	var links []string

	err := rs.db.View(func(tx *bbolt.Tx) error {
		bkt, err := rs.frameLinksBucket(tx)

		if err == bbolt.ErrBucketNotFound {
			return nil
		} else if err != nil {
			return err
		}

		linksByte := bkt.Get([]byte("frameLinks"))

		err = rs.decode(linksByte, &links)

		if err != nil {
			return err
		}

		return nil
	})

	return links, err
}

func (rs *BoltStore) serverOptionsBucket(tx *bbolt.Tx) (*bbolt.Bucket, error) {
	if !tx.Writable() {
		bkt := tx.Bucket(serverOptionsBucketName)

		if bkt == nil {
			return nil, bbolt.ErrBucketNotFound
		}

		return bkt, nil
	}

	return tx.CreateBucketIfNotExists(serverOptionsBucketName)
}

// serverOptionsKeyFor returns the key of the server options of a slot. The default slot keeps the original key.
func serverOptionsKeyFor(id smmodel.ServerID) []byte {
	if id == "" {
		return serverOptionsKey
	}

	return []byte(string(serverOptionsKey) + "/" + string(id))
}

// slotKey returns the key of a per-slot value. The default slot keeps the original key.
func slotKey(key []byte, id smmodel.ServerID) []byte {
	if id == "" {
		return key
	}

	return []byte(string(key) + "/" + string(id))
}

func (rs *BoltStore) UpsertServerOptions(so *serverconfig.GlobalServerConfig) error {
	return rs.UpsertServerOptionsFor("", so)
}

func (rs *BoltStore) UpsertServerOptionsFor(id smmodel.ServerID, so *serverconfig.GlobalServerConfig) error {
	return rs.db.Update(func(tx *bbolt.Tx) error {
		bkt, err := rs.serverOptionsBucket(tx)

		if err != nil {
			return err
		}

		encoded, err := rs.encode(so)

		if err != nil {
			return err
		}

		return bkt.Put(serverOptionsKeyFor(id), encoded)
	})
}

func (rs *BoltStore) LoadServerOptions() (*serverconfig.GlobalServerConfig, error) {
	return rs.LoadServerOptionsFor("")
}

func (rs *BoltStore) LoadServerOptionsFor(id smmodel.ServerID) (*serverconfig.GlobalServerConfig, error) {
	// start with defaults
	defaultConfig := serverconfig.ConfigIniDefault()

	so := &defaultConfig.GlobalServerConfig

	err := rs.db.View(func(tx *bbolt.Tx) error {
		bkt, err := rs.serverOptionsBucket(tx)

		if err != nil {
			return err
		}

		data := bkt.Get(serverOptionsKeyFor(id))

		if data == nil {
			return nil
		}

		return rs.decode(data, &so)
	})

	if err == bbolt.ErrBucketNotFound {
		// no server options created yet, apply defaults
		return so, rs.UpsertServerOptionsFor(id, so)
	}

	return so, err
}

func (rs *BoltStore) championshipsBucket(tx *bbolt.Tx) (*bbolt.Bucket, error) {
	if !tx.Writable() {
		bkt := tx.Bucket(championshipsBucketName)

		if bkt == nil {
			return nil, bbolt.ErrBucketNotFound
		}

		return bkt, nil
	}

	return tx.CreateBucketIfNotExists(championshipsBucketName)
}

func (rs *BoltStore) UpsertChampionship(c *smmodel.Championship) error {
	c.Updated = time.Now()

	return rs.db.Update(func(tx *bbolt.Tx) error {
		b, err := rs.championshipsBucket(tx)

		if err != nil {
			return err
		}

		data, err := rs.encode(c)

		if err != nil {
			return err
		}

		return b.Put([]byte(c.ID.String()), data)
	})
}

func (rs *BoltStore) ListChampionships() ([]*smmodel.Championship, error) {
	var championships []*smmodel.Championship

	err := rs.db.View(func(tx *bbolt.Tx) error {
		b, err := rs.championshipsBucket(tx)

		if err == bbolt.ErrBucketNotFound {
			return nil
		} else if err != nil {
			return err
		}

		return b.ForEach(func(k, v []byte) error {
			var championship *smmodel.Championship

			err := rs.decode(v, &championship)

			if err != nil {
				return err
			}

			if !championship.Deleted.IsZero() {
				// championship deleted
				return nil // continue
			}

			championships = append(championships, championship)

			return nil
		})
	})

	return championships, err
}

var ErrChampionshipNotFound = errors.New("servermanager: championship not found")

func (rs *BoltStore) LoadChampionship(id string) (*smmodel.Championship, error) {
	var championship *smmodel.Championship

	err := rs.db.View(func(tx *bbolt.Tx) error {
		b, err := rs.championshipsBucket(tx)

		if err == bbolt.ErrBucketNotFound {
			return ErrChampionshipNotFound
		} else if err != nil {
			return err
		}

		data := b.Get([]byte(id))

		if data == nil {
			return ErrChampionshipNotFound
		}

		return rs.decode(data, &championship)
	})

	if err != nil {
		return nil, err
	}

	if err := loadChampionshipRaceWeekends(championship, rs); err != nil {
		return nil, err
	}

	return championship, err
}

func (rs *BoltStore) DeleteChampionship(id string) error {
	championship, err := rs.LoadChampionship(id)

	if err != nil {
		return err
	}

	championship.Deleted = time.Now()

	return rs.UpsertChampionship(championship)
}

func (rs *BoltStore) accountsBucket(tx *bbolt.Tx) (*bbolt.Bucket, error) {
	if !tx.Writable() {
		bkt := tx.Bucket(accountsBucketName)

		if bkt == nil {
			return nil, bbolt.ErrBucketNotFound
		}

		return bkt, nil
	}

	return tx.CreateBucketIfNotExists(accountsBucketName)
}

func (rs *BoltStore) ListAccounts() ([]*smmodel.Account, error) {
	var accounts []*smmodel.Account

	err := rs.db.View(func(tx *bbolt.Tx) error {
		b, err := rs.accountsBucket(tx)

		if err == bbolt.ErrBucketNotFound {
			return nil
		} else if err != nil {
			return err
		}

		return b.ForEach(func(k, v []byte) error {
			var account *smmodel.Account

			err := rs.decode(v, &account)

			if err != nil {
				return err
			}

			if !account.Deleted.IsZero() {
				// account deleted
				return nil // continue
			}

			accounts = append(accounts, account)

			return nil
		})
	})

	return accounts, err
}

func (rs *BoltStore) UpsertAccount(a *smmodel.Account) error {
	a.Updated = time.Now()

	return rs.db.Update(func(tx *bbolt.Tx) error {
		b, err := rs.accountsBucket(tx)

		if err != nil {
			return err
		}

		data, err := rs.encode(a)

		if err != nil {
			return err
		}

		return b.Put([]byte(a.Name), data)
	})
}

var ErrAccountNotFound = errors.New("servermanager: account not found")

func (rs *BoltStore) FindAccountByName(name string) (*smmodel.Account, error) {
	var account *smmodel.Account

	err := rs.db.View(func(tx *bbolt.Tx) error {
		b, err := rs.accountsBucket(tx)

		if err == bbolt.ErrBucketNotFound {
			return ErrAccountNotFound
		} else if err != nil {
			return err
		}

		data := b.Get([]byte(name))

		if data == nil {
			return ErrAccountNotFound
		}

		return rs.decode(data, &account)
	})

	if err != nil {
		return nil, err
	}

	return account, err
}

func (rs *BoltStore) FindAccountByID(id string) (*smmodel.Account, error) {
	accounts, err := rs.ListAccounts()

	if err != nil {
		return nil, err
	}

	for _, a := range accounts {
		if a.ID.String() == id {
			return a, nil
		}
	}

	return nil, ErrAccountNotFound
}

func (rs *BoltStore) DeleteAccount(id string) error {
	account, err := rs.FindAccountByID(id)

	if err != nil {
		return err
	}

	account.Deleted = time.Now()

	return rs.UpsertAccount(account)
}

var metaBucketName = []byte("meta")

func (rs *BoltStore) metaBucket(tx *bbolt.Tx) (*bbolt.Bucket, error) {
	if !tx.Writable() {
		bkt := tx.Bucket(metaBucketName)

		if bkt == nil {
			return nil, bbolt.ErrBucketNotFound
		}

		return bkt, nil
	}

	return tx.CreateBucketIfNotExists(metaBucketName)
}

func (rs *BoltStore) SetMeta(key string, value interface{}) error {
	return rs.db.Update(func(tx *bbolt.Tx) error {
		bkt, err := rs.metaBucket(tx)

		if err != nil {
			return err
		}

		enc, err := rs.encode(value)

		if err != nil {
			return err
		}

		return bkt.Put([]byte(key), enc)
	})
}

var ErrValueNotSet = errors.New("servermanager: value not set")

func (rs *BoltStore) GetMeta(key string, out interface{}) error {
	err := rs.db.View(func(tx *bbolt.Tx) error {
		bkt, err := rs.metaBucket(tx)

		if err == bbolt.ErrBucketNotFound {
			return ErrValueNotSet
		} else if err != nil {
			return err
		}

		val := bkt.Get([]byte(key))

		if val == nil {
			return ErrValueNotSet
		}

		err = rs.decode(val, &out)

		return err
	})

	return err
}

var auditBucketName = []byte("audit")

func (rs *BoltStore) auditBucket(tx *bbolt.Tx) (*bbolt.Bucket, error) {
	if !tx.Writable() {
		bkt := tx.Bucket(auditBucketName)

		if bkt == nil {
			return nil, bbolt.ErrBucketNotFound
		}

		return bkt, nil
	}

	return tx.CreateBucketIfNotExists(auditBucketName)
}

func (rs *BoltStore) GetAuditEntries() ([]*smmodel.AuditEntry, error) {
	var audits []*smmodel.AuditEntry

	err := rs.db.View(func(tx *bbolt.Tx) error {
		bkt, err := rs.auditBucket(tx)

		if err == bbolt.ErrBucketNotFound {
			// no audit entries recorded yet
			return nil
		} else if err != nil {
			return err
		}

		val := bkt.Get([]byte("audit"))

		if val == nil {
			return nil
		}

		err = rs.decode(val, &audits)

		return err
	})

	return audits, err
}

func (rs *BoltStore) AddAuditEntry(entry *smmodel.AuditEntry) error {
	entries, err := rs.GetAuditEntries()

	if err != nil {
		return err
	}

	entries = append(entries, entry)

	if len(entries) > maxAuditEntries {
		entries = entries[20:]
	}

	return rs.db.Update(func(tx *bbolt.Tx) error {
		bkt, err := rs.auditBucket(tx)

		if err != nil {
			return err
		}

		enc, err := rs.encode(entries)

		if err != nil {
			return err
		}

		return bkt.Put([]byte("audit"), enc)
	})
}

func (rs *BoltStore) raceWeekendsBucket(tx *bbolt.Tx) (*bbolt.Bucket, error) {
	if !tx.Writable() {
		bkt := tx.Bucket(raceWeekendsBucketName)

		if bkt == nil {
			return nil, bbolt.ErrBucketNotFound
		}

		return bkt, nil
	}

	return tx.CreateBucketIfNotExists(raceWeekendsBucketName)
}

func (rs *BoltStore) UpsertRaceWeekend(rw *smmodel.RaceWeekend) error {
	rw.Updated = time.Now()

	return rs.db.Update(func(tx *bbolt.Tx) error {
		b, err := rs.raceWeekendsBucket(tx)

		if err != nil {
			return err
		}

		data, err := rs.encode(rw)

		if err != nil {
			return err
		}

		return b.Put([]byte(rw.ID.String()), data)
	})
}

func (rs *BoltStore) ListRaceWeekends() ([]*smmodel.RaceWeekend, error) {
	var raceWeekends []*smmodel.RaceWeekend

	err := rs.db.View(func(tx *bbolt.Tx) error {
		b, err := rs.raceWeekendsBucket(tx)

		if err == bbolt.ErrBucketNotFound {
			return nil
		} else if err != nil {
			return err
		}

		return b.ForEach(func(k, v []byte) error {
			var raceWeekend *smmodel.RaceWeekend

			err := rs.decode(v, &raceWeekend)

			if err != nil {
				return err
			}

			if !raceWeekend.Deleted.IsZero() {
				// race weekend deleted
				return nil // continue
			}

			raceWeekends = append(raceWeekends, raceWeekend)

			return nil
		})
	})

	return raceWeekends, err
}

func (rs *BoltStore) LoadRaceWeekend(id string) (*smmodel.RaceWeekend, error) {
	var raceWeekend *smmodel.RaceWeekend

	err := rs.db.View(func(tx *bbolt.Tx) error {
		b, err := rs.raceWeekendsBucket(tx)

		if err == bbolt.ErrBucketNotFound {
			return smmodel.ErrRaceWeekendNotFound
		} else if err != nil {
			return err
		}

		data := b.Get([]byte(id))

		if data == nil {
			return smmodel.ErrRaceWeekendNotFound
		}

		return rs.decode(data, &raceWeekend)
	})

	if err != nil {
		return nil, err
	}

	return raceWeekend, err
}

func (rs *BoltStore) DeleteRaceWeekend(id string) error {
	raceWeekend, err := rs.LoadRaceWeekend(id)

	if err != nil {
		return err
	}

	raceWeekend.Deleted = time.Now()

	return rs.UpsertRaceWeekend(raceWeekend)
}

func (rs *BoltStore) UpsertStrackerOptions(sto *smplugins.StrackerConfiguration) error {
	return rs.UpsertStrackerOptionsFor("", sto)
}

func (rs *BoltStore) UpsertStrackerOptionsFor(id smmodel.ServerID, sto *smplugins.StrackerConfiguration) error {
	return rs.db.Update(func(tx *bbolt.Tx) error {
		bkt, err := rs.serverOptionsBucket(tx)

		if err != nil {
			return err
		}

		encoded, err := rs.encode(sto)

		if err != nil {
			return err
		}

		return bkt.Put(slotKey(strackerOptionsKey, id), encoded)
	})
}

func (rs *BoltStore) LoadStrackerOptions() (*smplugins.StrackerConfiguration, error) {
	return rs.LoadStrackerOptionsFor("")
}

func (rs *BoltStore) LoadStrackerOptionsFor(id smmodel.ServerID) (*smplugins.StrackerConfiguration, error) {
	// start with defaults
	sto := smplugins.DefaultStrackerIni(rs.opts.serverInstallPath())

	err := rs.db.View(func(tx *bbolt.Tx) error {
		bkt, err := rs.serverOptionsBucket(tx)

		if err == bbolt.ErrBucketNotFound {
			// nothing stored yet
			return nil
		} else if err != nil {
			return err
		}

		data := bkt.Get(slotKey(strackerOptionsKey, id))

		if data == nil {
			return nil
		}

		return rs.decode(data, &sto)
	})

	return sto, err
}

func (rs *BoltStore) UpsertKissMyRankOptions(kmr *smplugins.KissMyRankConfig) error {
	return rs.UpsertKissMyRankOptionsFor("", kmr)
}

func (rs *BoltStore) UpsertKissMyRankOptionsFor(id smmodel.ServerID, kmr *smplugins.KissMyRankConfig) error {
	return rs.db.Update(func(tx *bbolt.Tx) error {
		bkt, err := rs.serverOptionsBucket(tx)

		if err != nil {
			return err
		}

		encoded, err := rs.encode(kmr)

		if err != nil {
			return err
		}

		return bkt.Put(slotKey(kissMyRankOptionsKey, id), encoded)
	})
}

func (rs *BoltStore) LoadKissMyRankOptions() (*smplugins.KissMyRankConfig, error) {
	return rs.LoadKissMyRankOptionsFor("")
}

func (rs *BoltStore) LoadKissMyRankOptionsFor(id smmodel.ServerID) (*smplugins.KissMyRankConfig, error) {
	// start with defaults
	kmr := smplugins.DefaultKissMyRankConfig()

	err := rs.db.View(func(tx *bbolt.Tx) error {
		bkt, err := rs.serverOptionsBucket(tx)

		if err == bbolt.ErrBucketNotFound {
			// nothing stored yet
			return nil
		} else if err != nil {
			return err
		}

		data := bkt.Get(slotKey(kissMyRankOptionsKey, id))

		if data == nil {
			return nil
		}

		return rs.decode(data, &kmr)
	})

	return kmr, err
}

func (rs *BoltStore) UpsertRealPenaltyOptions(rpc *smplugins.RealPenaltyConfig) error {
	return rs.UpsertRealPenaltyOptionsFor("", rpc)
}

func (rs *BoltStore) UpsertRealPenaltyOptionsFor(id smmodel.ServerID, rpc *smplugins.RealPenaltyConfig) error {
	return rs.db.Update(func(tx *bbolt.Tx) error {
		bkt, err := rs.serverOptionsBucket(tx)

		if err != nil {
			return err
		}

		encoded, err := rs.encode(rpc)

		if err != nil {
			return err
		}

		return bkt.Put(slotKey(realPenaltyOptionsKey, id), encoded)
	})
}

func (rs *BoltStore) LoadRealPenaltyOptions() (*smplugins.RealPenaltyConfig, error) {
	return rs.LoadRealPenaltyOptionsFor("")
}

func (rs *BoltStore) LoadRealPenaltyOptionsFor(id smmodel.ServerID) (*smplugins.RealPenaltyConfig, error) {
	// start with defaults
	rpc := smplugins.DefaultRealPenaltyConfig()

	err := rs.db.View(func(tx *bbolt.Tx) error {
		bkt, err := rs.serverOptionsBucket(tx)

		if err == bbolt.ErrBucketNotFound {
			// nothing stored yet
			return nil
		} else if err != nil {
			return err
		}

		data := bkt.Get(slotKey(realPenaltyOptionsKey, id))

		if data == nil {
			return nil
		}

		return rs.decode(data, &rpc)
	})

	return rpc, err
}

func (rs *BoltStore) liveTimingsDataBucket(tx *bbolt.Tx) (*bbolt.Bucket, error) {
	if !tx.Writable() {
		bkt := tx.Bucket(liveTimingsBucketName)

		if bkt == nil {
			return nil, bbolt.ErrBucketNotFound
		}

		return bkt, nil
	}

	return tx.CreateBucketIfNotExists(liveTimingsBucketName)
}

func (rs *BoltStore) UpsertLiveTimingsData(lt *smmodel.LiveTimingsPersistedData) error {
	return rs.UpsertLiveTimingsDataFor("", lt)
}

func (rs *BoltStore) UpsertLiveTimingsDataFor(id smmodel.ServerID, lt *smmodel.LiveTimingsPersistedData) error {
	return rs.db.Update(func(tx *bbolt.Tx) error {
		bkt, err := rs.liveTimingsDataBucket(tx)

		if err != nil {
			return err
		}

		encoded, err := rs.encode(lt)

		if err != nil {
			return err
		}

		return bkt.Put(slotKey(liveTimingsKey, id), encoded)
	})
}

func (rs *BoltStore) LoadLiveTimingsData() (*smmodel.LiveTimingsPersistedData, error) {
	return rs.LoadLiveTimingsDataFor("")
}

func (rs *BoltStore) LoadLiveTimingsDataFor(id smmodel.ServerID) (*smmodel.LiveTimingsPersistedData, error) {
	var lt *smmodel.LiveTimingsPersistedData

	err := rs.db.View(func(tx *bbolt.Tx) error {
		bkt, err := rs.liveTimingsDataBucket(tx)

		if err == bbolt.ErrBucketNotFound {
			// nothing stored yet
			return nil
		} else if err != nil {
			return err
		}

		data := bkt.Get(slotKey(liveTimingsKey, id))

		if data == nil {
			return nil
		}

		return rs.decode(data, &lt)
	})

	return lt, err
}

func (rs *BoltStore) UpsertLastRaceEvent(r smmodel.RaceEvent) error {
	return rs.UpsertLastRaceEventFor("", r)
}

func (rs *BoltStore) UpsertLastRaceEventFor(id smmodel.ServerID, r smmodel.RaceEvent) error {
	return rs.db.Update(func(tx *bbolt.Tx) error {
		bkt, err := rs.liveTimingsDataBucket(tx)

		if err != nil {
			return err
		}

		encoded, err := smmodel.MarshalRaceEvent(r)

		if err != nil {
			return err
		}

		return bkt.Put(slotKey(lastRaceEventKey, id), encoded)
	})
}

func (rs *BoltStore) LoadLastRaceEvent() (smmodel.RaceEvent, error) {
	return rs.LoadLastRaceEventFor("")
}

func (rs *BoltStore) LoadLastRaceEventFor(id smmodel.ServerID) (smmodel.RaceEvent, error) {
	var re smmodel.RaceEvent

	err := rs.db.View(func(tx *bbolt.Tx) error {
		bkt, err := rs.liveTimingsDataBucket(tx)

		if err == bbolt.ErrBucketNotFound {
			// nothing stored yet
			return nil
		} else if err != nil {
			return err
		}

		data := bkt.Get(slotKey(lastRaceEventKey, id))

		if data == nil {
			return nil
		}

		re, err = smmodel.UnmarshalRaceEvent(data)

		return err
	})

	return re, err
}

func (rs *BoltStore) ClearLastRaceEvent() error {
	return rs.ClearLastRaceEventFor("")
}

func (rs *BoltStore) ClearLastRaceEventFor(id smmodel.ServerID) error {
	return rs.db.Update(func(tx *bbolt.Tx) error {
		bkt, err := rs.liveTimingsDataBucket(tx)

		if err != nil {
			return err
		}

		return bkt.Delete(slotKey(lastRaceEventKey, id))
	})
}
