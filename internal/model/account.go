package model

import (
	"encoding/gob"
	"time"

	"github.com/Masterminds/semver"
	"github.com/google/uuid"
)

// AdminUserName is the name of the default admin account.
const AdminUserName = "admin"

// OpenAccountName is the name of the dummy account used for visitors if read access is open.
const OpenAccountName = "Free Access"

func NewAccount(serverID ServerID, version string) *Account {
	return &Account{
		ID:              uuid.New(),
		Created:         time.Now(),
		LastSeenVersion: version,
		Theme:           ThemeDefault,
		Groups:          map[ServerID]Group{serverID: GroupRead},
	}
}

type Account struct {
	ID uuid.UUID

	Created time.Time
	Updated time.Time
	Deleted time.Time

	Name   string
	Groups map[ServerID]Group

	DriverName, GUID, Team string

	PasswordHash string
	PasswordSalt string

	DefaultPassword string
	LastSeenVersion string

	Theme Theme

	// Deprecated: Use Groups instead.
	DeprecatedGroup Group `json:"Group"`
}

func (a Account) GroupFor(serverID ServerID) Group {
	if a.Groups == nil {
		return GroupNoAccess
	}

	if group, ok := a.Groups[serverID]; ok {
		return group
	}

	// in the case where a user has not got any group permissions at all for this server instance
	// give them the first permission we find from other server instances (if any)
	for _, group := range a.Groups {
		return group
	}

	return GroupNoAccess
}

func (a Account) ShowDarkTheme(serverManagerDarkThemeEnabled bool) bool {
	if (a.Theme == "" || a.Theme == ThemeDefault) && serverManagerDarkThemeEnabled {
		return true
	}

	return a.Theme == ThemeDark
}

func (a Account) ShouldSeeUpgradePopup(currentVersion, adminPasswordOverride string) bool {
	return !a.HasSeenVersion(currentVersion) && !a.NeedsPasswordReset(adminPasswordOverride)
}

func (a Account) HasSeenVersion(version string) bool {
	if a.Name == OpenAccountName {
		return true // open accounts don't see version releases
	}

	newVersion, err := semver.NewVersion(version)

	if err != nil {
		return true
	}

	currentVersion, err := semver.NewVersion(a.LastSeenVersion)

	if err != nil {
		return true
	}

	return newVersion.Equal(currentVersion) || newVersion.LessThan(currentVersion)
}

func (a Account) NeedsPasswordReset(adminPasswordOverride string) bool {
	return a.DefaultPassword != "" || (a.Name == AdminUserName && adminPasswordOverride != "")
}

func (a Account) HasGroupPrivilege(serverID ServerID, g Group) bool {
	userGroup := a.GroupFor(serverID)

	if g == userGroup {
		return true
	}

	if userGroup == GroupAdmin {
		return true
	}

	if g == GroupWrite && userGroup == GroupDelete {
		return true
	}

	if g == GroupRead && (userGroup == GroupWrite || userGroup == GroupDelete) {
		return true
	}

	return false
}

type Group string

const (
	GroupNoAccess Group = "no_access"
	GroupRead     Group = "read"
	GroupWrite    Group = "write"
	GroupDelete   Group = "delete"
	GroupAdmin    Group = "admin"
)

func init() {
	// Register the Account struct with gob so that it can be stored in a session. The name is the one the type had
	// when it lived in the root package, so that already stored sessions can still be decoded.
	gob.RegisterName("github.com/wernersbacher/assetto-server-manager.Account", Account{})
}
