package model

import (
	"encoding/gob"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

func init() {
	gob.RegisterName("github.com/wernersbacher/ACPitwall.UserSkin", UserSkin{})
}

var (
	ErrUserSkinNotFound = errors.New("servermanager: skin not found")
	ErrSkinVisibility   = errors.New("servermanager: invalid skin visibility")
)

// SkinVisibility decides who can pick and download an uploaded skin.
type SkinVisibility string

const (
	// SkinPrivate skins are only usable by their owner (and visible to admins). It is the zero value, the safe default.
	SkinPrivate SkinVisibility = "private"
	// SkinTeam skins are usable by the members of the team named in UserSkin.TeamID.
	SkinTeam SkinVisibility = "team"
	// SkinGlobal skins are usable by everybody; they live in the regular skins directory of the car.
	SkinGlobal SkinVisibility = "global"
)

// ParseSkinVisibility converts a form value ("private", "team", "global"). An empty value is private.
func ParseSkinVisibility(s string) (SkinVisibility, error) {
	switch SkinVisibility(s) {
	case "", SkinPrivate:
		return SkinPrivate, nil
	case SkinTeam:
		return SkinTeam, nil
	case SkinGlobal:
		return SkinGlobal, nil
	}

	return SkinPrivate, ErrSkinVisibility
}

// UserSkin is the metadata of an uploaded car skin. The skin files themselves live on disk, in the skins directory
// of the car (global) or in the user skins directory (private, team); see the skins manager.
type UserSkin struct {
	ID    uuid.UUID
	Car   string
	Name  string // name of the skin as uploaded, shown to users
	// Folder is the name of the skin directory (and the skin name in entry lists). It never changes. For skins of
	// users it carries a prefix of the owner id, so that names of different users cannot collide.
	Folder string
	Owner uuid.UUID

	Visibility SkinVisibility
	// TeamID is only set for SkinTeam skins.
	TeamID uuid.UUID

	Files int
	Size  int64

	Created time.Time
	Updated time.Time
}

// NewUserSkin creates the record of a freshly uploaded, private skin.
func NewUserSkin(car, name string, owner uuid.UUID, files int, size int64, now time.Time) *UserSkin {
	return &UserSkin{
		ID:         uuid.New(),
		Car:        car,
		Name:       name,
		Folder:     name,
		Owner:      owner,
		Visibility: SkinPrivate,
		Files:      files,
		Size:       size,
		Created:    now,
		Updated:    now,
	}
}

// UserSkinFolder is the directory name of a skin that a user uploaded: the name with a prefix of the owner id.
func UserSkinFolder(owner uuid.UUID, name string) string {
	return fmt.Sprintf("u%x_%s", owner[:4], name)
}

// Normalized returns the visibility with the zero value resolved to SkinPrivate.
func (s *UserSkin) Normalized() SkinVisibility {
	if s.Visibility == "" {
		return SkinPrivate
	}

	return s.Visibility
}

// SetVisibility changes who may use the skin. A team skin needs a team, other visibilities drop it.
func (s *UserSkin) SetVisibility(v SkinVisibility, team uuid.UUID, now time.Time) error {
	switch v {
	case SkinPrivate, SkinGlobal:
		team = uuid.Nil
	case SkinTeam:
		if team == uuid.Nil {
			return ErrSkinVisibility
		}
	default:
		return ErrSkinVisibility
	}

	s.Visibility, s.TeamID, s.Updated = v, team, now

	return nil
}

// Defaults for SkinSettings fields that are 0 (not set).
const (
	DefaultSkinMaxFiles    = 500
	DefaultSkinMaxMB       = 100
	DefaultSkinUserQuotaMB = 500
	DefaultSkinMinFreeMB   = 2048
)

// SkinSettings are the server options for skin uploads. The zero value allows only what was possible before user
// skins: nobody but accounts with write access uploads, and only globally.
type SkinSettings struct {
	// UserUploads lets every logged in account upload skins.
	UserUploads bool
	// GlobalShare lets users make their skins global themselves. Accounts with write access always can.
	GlobalShare bool

	// The limits count 0 as "use the default"; there is no unlimited setting, so the disk cannot be filled.
	MaxFiles    int // files in one skin
	MaxSkinMB   int // size of one skin
	UserQuotaMB int // all skins of one user together
	MinFreeMB   int // free disk space that must remain after an upload
}

// SkinLimits are the resolved limits in bytes and files.
type SkinLimits struct {
	MaxFiles       int
	MaxSkinBytes   int64
	UserQuotaBytes int64
	MinFreeBytes   int64
}

// Limits resolves the defaults.
func (s SkinSettings) Limits() SkinLimits {
	pick := func(v, def int) int64 {
		if v <= 0 {
			v = def
		}

		return int64(v)
	}

	return SkinLimits{
		MaxFiles:       int(pick(s.MaxFiles, DefaultSkinMaxFiles)),
		MaxSkinBytes:   pick(s.MaxSkinMB, DefaultSkinMaxMB) << 20,
		UserQuotaBytes: pick(s.UserQuotaMB, DefaultSkinUserQuotaMB) << 20,
		MinFreeBytes:   pick(s.MinFreeMB, DefaultSkinMinFreeMB) << 20,
	}
}

// UsableBy reports whether the account may pick the skin, given the ids of the teams it is a member of.
func (s *UserSkin) UsableBy(account uuid.UUID, teams []uuid.UUID) bool {
	if s.Normalized() == SkinGlobal {
		return true
	}

	if account != uuid.Nil && account == s.Owner {
		return true
	}

	if s.Normalized() == SkinTeam {
		for _, team := range teams {
			if team == s.TeamID {
				return true
			}
		}
	}

	return false
}
