package model

import "sort"

// Area is a part of the site whose visibility the admin sets separately.
type Area string

const (
	AreaStats         Area = "stats"         // driver profiles, leaderboards, lap records
	AreaDownloads     Area = "downloads"     // content directory, setups and skin zips
	AreaTeams         Area = "teams"         // teams
	AreaCustomRaces   Area = "custom_races"  // custom race list
	AreaResults       Area = "results"       // results
	AreaChampionships Area = "championships" // championships
	AreaRaceWeekends  Area = "race_weekends" // race weekends
	AreaCalendar      Area = "calendar"      // calendar, upcoming races, iCal feeds
	AreaLiveTiming    Area = "live_timing"   // live timing and race control views
	AreaContent       Area = "content"       // cars, tracks and weather
)

// AllAreas lists every area in display order.
var AllAreas = []Area{
	AreaStats, AreaDownloads, AreaTeams, AreaCustomRaces, AreaResults, AreaChampionships,
	AreaRaceWeekends, AreaCalendar, AreaLiveTiming, AreaContent,
}

// ParseArea is the inverse of Area's string value.
func ParseArea(s string) (Area, bool) {
	for _, a := range AllAreas {
		if string(a) == s {
			return a, true
		}
	}

	return "", false
}

// Level says who may see an area. The levels are ordered: every level includes the ones above it in the list.
type Level int

const (
	// LevelPublic: everybody, also visitors without an account.
	LevelPublic Level = iota
	// LevelLoggedIn: accounts with read access.
	LevelLoggedIn
	// LevelWrite: accounts with write access.
	LevelWrite
	// LevelAdmin: admins only.
	LevelAdmin
)

// AllLevels lists every level from open to restricted.
var AllLevels = []Level{LevelPublic, LevelLoggedIn, LevelWrite, LevelAdmin}

func (l Level) String() string {
	switch l {
	case LevelLoggedIn:
		return "login"
	case LevelWrite:
		return "write"
	case LevelAdmin:
		return "admin"
	default:
		return "public"
	}
}

// ParseLevel is the inverse of Level.String.
func ParseLevel(s string) (Level, bool) {
	for _, l := range AllLevels {
		if l.String() == s {
			return l, true
		}
	}

	return LevelPublic, false
}

// DefaultLevel is the level of an area without an entry in Visibility: public, except for teams (logged in) and
// the custom races (write access).
func DefaultLevel(a Area) Level {
	switch a {
	case AreaTeams:
		return LevelLoggedIn
	case AreaCustomRaces:
		return LevelWrite
	}

	return LevelPublic
}

// Visibility maps an area to its level. A missing entry means DefaultLevel.
type Visibility map[Area]Level

// Level returns the level of the area.
func (v Visibility) Level(a Area) Level {
	if l, ok := v[a]; ok {
		return l
	}

	return DefaultLevel(a)
}

// Copy returns an independent copy.
func (v Visibility) Copy() Visibility {
	c := make(Visibility, len(v))

	for a, l := range v {
		c[a] = l
	}

	return c
}

// AnyPublic tells whether at least one area is visible to anonymous visitors.
func (v Visibility) AnyPublic() bool {
	for _, a := range AllAreas {
		if v.Level(a) == LevelPublic {
			return true
		}
	}

	return false
}

// IsAnonymous tells whether the account is no real account: nil, empty or the dummy account of open areas.
func IsAnonymous(account *Account) bool {
	return account == nil || account.Name == "" || account.Name == OpenAccountName
}

// Allowed tells whether the account (nil or the dummy account for anonymous visitors) may see the area.
func (v Visibility) Allowed(area Area, account *Account, serverID ServerID) bool {
	level := v.Level(area)

	if level == LevelPublic {
		return true
	}

	if IsAnonymous(account) {
		return false
	}

	switch level {
	case LevelLoggedIn:
		return account.HasGroupPrivilege(serverID, GroupRead)
	case LevelWrite:
		return account.HasGroupPrivilege(serverID, GroupWrite)
	default:
		return account.HasGroupPrivilege(serverID, GroupAdmin)
	}
}

// SortedAreas returns the areas with an explicit entry, for stable output.
func (v Visibility) SortedAreas() []Area {
	areas := make([]Area, 0, len(v))

	for a := range v {
		areas = append(areas, a)
	}

	sort.Slice(areas, func(i, j int) bool { return areas[i] < areas[j] })

	return areas
}

// ServerAccountOptions are the server wide settings of the account system. They are stored as meta value.
type ServerAccountOptions struct {
	// Visibility sets who may see which area. Missing areas have their DefaultLevel.
	Visibility Visibility

	// DisableTeamCreation stops accounts without write access from creating teams. The zero value allows it, as
	// before the option existed.
	DisableTeamCreation bool

	// MaxOwnedTeams is how many teams one account may own. 0 means the default of the teams package, a negative
	// number is unlimited.
	MaxOwnedTeams int

	// Skins are the settings for skin uploads of users.
	Skins SkinSettings

	// SteamSignupDisabled turns off the creation of accounts by a Steam login (the zero value, so a fresh or older
	// stored option set allows it). Visitors without an account can create one by logging in with Steam; Steam
	// logins of existing accounts (matched by their GUID) always work.
	SteamSignupDisabled bool
}
