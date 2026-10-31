package model

import (
	"errors"

	"github.com/google/uuid"
)

// SignUpTeamMode decides how a championship sign up form asks for the team (if AskForTeam is set).
type SignUpTeamMode string

const (
	// SignUpTeamFreeText is a free text field. It is the zero value, so championships from before teams existed
	// keep working.
	SignUpTeamFreeText SignUpTeamMode = ""
	// SignUpTeamSelect lets drivers pick one of the teams they are a member of (or none).
	SignUpTeamSelect SignUpTeamMode = "select"
	// SignUpTeamBoth offers the team list and a free text field for drivers without a team. A free text name must not
	// be the name of an existing team.
	SignUpTeamBoth SignUpTeamMode = "both"
)

// DefaultMaxDriversPerTeam is the number of drivers a team can register in a championship if the championship does
// not say otherwise.
const DefaultMaxDriversPerTeam = 2

var (
	// ErrSignUpTeamFull is returned if a team already has as many registered drivers as the championship allows.
	ErrSignUpTeamFull = errors.New("servermanager: the team already has the maximum number of drivers in this championship")

	// ErrSignUpTeamLocked is returned if a driver who is registered tries to change their team.
	ErrSignUpTeamLocked = errors.New("servermanager: the team of a registered driver cannot be changed")
)

// UsesTeamSelection reports whether drivers can choose from the team list.
func (f ChampionshipSignUpForm) UsesTeamSelection() bool {
	return f.AskForTeam && (f.TeamMode == SignUpTeamSelect || f.TeamMode == SignUpTeamBoth)
}

// AllowsFreeTextTeam reports whether drivers can type a team name.
func (f ChampionshipSignUpForm) AllowsFreeTextTeam() bool {
	return f.AskForTeam && (f.TeamMode == SignUpTeamFreeText || f.TeamMode == SignUpTeamBoth)
}

// TeamLimit returns the maximum number of drivers per team. If limited is false there is no limit.
func (f ChampionshipSignUpForm) TeamLimit() (limit int, limited bool) {
	switch {
	case f.MaxDriversPerTeam < 0:
		return 0, false
	case f.MaxDriversPerTeam == 0:
		return DefaultMaxDriversPerTeam, true
	default:
		return f.MaxDriversPerTeam, true
	}
}

// Response returns the registration of a driver, or nil.
func (f ChampionshipSignUpForm) Response(guid string) *ChampionshipSignUpResponse {
	for _, response := range f.Responses {
		if response.GUID == guid {
			return response
		}
	}

	return nil
}

// TakesTeamSlot reports whether the registration counts for the driver limit of its team: everything but a
// rejected registration.
func (csr ChampionshipSignUpResponse) TakesTeamSlot() bool {
	return csr.Status != ChampionshipEntrantRejected
}

// SameTeam reports whether two registrations are for the same team: the same team of the team list or, for free
// text, the same name (ignoring case). No team is the same as no team.
func (csr ChampionshipSignUpResponse) SameTeam(other ChampionshipSignUpResponse) bool {
	if csr.TeamID != uuid.Nil || other.TeamID != uuid.Nil {
		return csr.TeamID == other.TeamID
	}

	return SameTeamName(csr.Team, other.Team)
}

// TeamDriverCount counts the registrations that take a slot of the team, not counting the one of exceptGUID.
func (f ChampionshipSignUpForm) TeamDriverCount(teamID uuid.UUID, exceptGUID string) int {
	if teamID == uuid.Nil {
		return 0
	}

	count := 0

	for _, response := range f.Responses {
		if response.TeamID == teamID && response.GUID != exceptGUID && response.TakesTeamSlot() {
			count++
		}
	}

	return count
}

// CheckTeamLock refuses a registration that would change the team of a driver who is already registered.
// A rejected registration does not hold a team, so the driver can register again with another one.
func (f ChampionshipSignUpForm) CheckTeamLock(response *ChampionshipSignUpResponse) error {
	existing := f.Response(response.GUID)

	if existing == nil || !existing.TakesTeamSlot() {
		return nil
	}

	if !existing.SameTeam(*response) {
		return ErrSignUpTeamLocked
	}

	return nil
}

// CheckTeamLimit refuses a registration for a team of the team list which already has the maximum number of drivers.
func (f ChampionshipSignUpForm) CheckTeamLimit(response *ChampionshipSignUpResponse) error {
	limit, limited := f.TeamLimit()

	if !limited || response.TeamID == uuid.Nil {
		return nil
	}

	if f.TeamDriverCount(response.TeamID, response.GUID) >= limit {
		return ErrSignUpTeamFull
	}

	return nil
}

// CheckTeam applies the rules for the team of a registration: a registered driver cannot change the team and a team
// has a limited number of drivers. Moderators can override both.
func (f ChampionshipSignUpForm) CheckTeam(response *ChampionshipSignUpResponse, override bool) error {
	if override {
		return nil
	}

	if err := f.CheckTeamLock(response); err != nil {
		return err
	}

	return f.CheckTeamLimit(response)
}

// SetEntrantTeam changes the team of the registration of a driver and of their entry in the entry list. It does not
// check any rules, this is the way moderators move a driver. It reports false if the driver is not registered.
func (c *Championship) SetEntrantTeam(guid string, teamID uuid.UUID, team string) bool {
	response := c.SignUpForm.Response(guid)

	if response == nil {
		return false
	}

	response.TeamID = teamID
	response.Team = team

	entryListMutex.Lock()
	defer entryListMutex.Unlock()

	for _, class := range c.Classes {
		for _, entrant := range class.Entrants {
			if entrant.GUID == guid {
				entrant.Team = team
			}
		}
	}

	return true
}

// RenameTeam updates the name of a team in the registrations (and entry list entries) that refer to it. Finished
// championships keep the name from back then. It reports how many registrations changed.
func (c *Championship) RenameTeam(teamID uuid.UUID, name string) int {
	if teamID == uuid.Nil || c.Progress() >= 100.0 {
		return 0
	}

	changed := 0

	for _, response := range c.SignUpForm.Responses {
		if response.TeamID == teamID && response.Team != name {
			c.SetEntrantTeam(response.GUID, teamID, name)
			changed++
		}
	}

	return changed
}
