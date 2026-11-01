package championship

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/wernersbacher/ACPitwall/internal/i18n"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

// TeamDirectory is what the championship needs to know about teams. It is implemented by teams.Manager.
type TeamDirectory interface {
	// Lookup finds a team by id, including deleted ones.
	Lookup(id string) (*smmodel.Team, error)
	// NameTaken reports whether a team uses the name.
	NameTaken(name string) (bool, error)
	// MemberOf returns the teams an account is a member of.
	MemberOf(accountID uuid.UUID) ([]*smmodel.Team, error)
	// ListAll returns all teams that were not deleted, whoever can see them.
	ListAll() ([]*smmodel.Team, error)
}

// SignUpTeamOption is a team a driver can choose on the sign up form.
type SignUpTeamOption struct {
	Team *smmodel.Team

	// Drivers is how many drivers are registered with the team, Full tells that the championship does not take
	// another one.
	Drivers int
	Full    bool
}

// SignUpTeamOptions returns the teams the account can choose on the sign up form of the championship: the teams
// they are a member of, with how many drivers each one has registered. It is empty if the form has no team
// selection, the account is not logged in or the manager does not know the teams.
func (cm *ChampionshipManager) SignUpTeamOptions(championship *smmodel.Championship, account *smmodel.Account) ([]*SignUpTeamOption, error) {
	if !championship.SignUpForm.UsesTeamSelection() || cm.teams == nil || account == nil || account.ID == uuid.Nil {
		return nil, nil
	}

	teams, err := cm.teams.MemberOf(account.ID)

	if err != nil {
		return nil, err
	}

	limit, limited := championship.SignUpForm.TeamLimit()

	var options []*SignUpTeamOption

	for _, team := range teams {
		drivers := championship.SignUpForm.TeamDriverCount(team.ID, "")

		options = append(options, &SignUpTeamOption{Team: team, Drivers: drivers, Full: limited && drivers >= limit})
	}

	sort.SliceStable(options, func(i, j int) bool {
		return strings.ToLower(options[i].Team.Name) < strings.ToLower(options[j].Team.Name)
	})

	return options, nil
}

// SignUpTeamLock returns the registration of a driver if it fixes their team, which is the case for every
// registration that is not rejected. It is nil if the driver can still choose.
func SignUpTeamLock(championship *smmodel.Championship, guid string) *smmodel.ChampionshipSignUpResponse {
	response := championship.SignUpForm.Response(guid)

	if response == nil || !response.TakesTeamSlot() {
		return nil
	}

	return response
}

// AllTeams returns all teams for the moderators who move drivers between teams.
func (cm *ChampionshipManager) AllTeams() ([]*smmodel.Team, error) {
	if cm.teams == nil {
		return nil, nil
	}

	return cm.teams.ListAll()
}

// parseSignUpTeamSettings reads the team settings of the championship form: how the team is asked for and how many
// drivers a team can register. Values that make no sense fall back to the defaults (free text, the default limit)
// instead of failing the whole form; every negative limit means unlimited.
func parseSignUpTeamSettings(mode, maxDrivers string) (smmodel.SignUpTeamMode, int) {
	teamMode := smmodel.SignUpTeamFreeText

	switch smmodel.SignUpTeamMode(strings.TrimSpace(mode)) {
	case smmodel.SignUpTeamSelect:
		teamMode = smmodel.SignUpTeamSelect
	case smmodel.SignUpTeamBoth:
		teamMode = smmodel.SignUpTeamBoth
	}

	limit, err := strconv.Atoi(strings.TrimSpace(maxDrivers))

	switch {
	case err != nil || limit > maxDriversPerTeamLimit:
		limit = 0
	case limit < 0:
		limit = -1
	}

	return teamMode, limit
}

// maxDriversPerTeamLimit is the highest number of drivers per team the form accepts.
const maxDriversPerTeamLimit = 1000

// SetTeamDirectory tells the manager where the teams are. Without it sign up forms cannot offer a team selection.
func (cm *ChampionshipManager) SetTeamDirectory(teams TeamDirectory) {
	cm.teams = teams
}

// signUpMutex serialises everything that changes the registrations of championships. Championships are shared by
// all server slots and every slot has its own ChampionshipManager, so a lock inside the manager would not stop two
// drivers on different slots from taking the last place of a team at the same time.
var signUpMutex sync.Mutex

// LockSignUps locks the registrations of all championships and returns the function that unlocks them again.
func LockSignUps() func() {
	signUpMutex.Lock()

	return signUpMutex.Unlock
}

// carClassMutex orders changes to the global car classes against the saving of championships: every save through
// ChampionshipManager.UpsertChampionship holds it for reading, LockCarClassChanges holds it for writing.
var carClassMutex sync.RWMutex

// LockCarClassChanges is taken by whoever creates, changes or deletes a global car class. It locks the registrations
// of all championships (LockSignUps) first, which is the order of a sign up (registrations, then save), and then stops
// all saves of championships until the returned function is called, so that the check of the championships that refer
// to the car class and the change of the car class are one step.
func LockCarClassChanges() func() {
	unlockSignUps := LockSignUps()

	carClassMutex.Lock()

	return func() {
		carClassMutex.Unlock()
		unlockSignUps()
	}
}

// resolveSignUpTeam sets the team of a registration from the sign up form: the chosen team of the team list or the
// typed name, depending on the mode of the championship.
func (cm *ChampionshipManager) resolveSignUpTeam(championship *smmodel.Championship, response *smmodel.ChampionshipSignUpResponse, r *http.Request) error {
	form := championship.SignUpForm

	response.Team, response.TeamID = "", uuid.Nil

	if !form.AskForTeam {
		return nil
	}

	if rawID := strings.TrimSpace(r.FormValue("TeamID")); rawID != "" {
		if !form.UsesTeamSelection() {
			return ValidationError{i18n.Msg("validation.championship.no_team_selection", "This championship does not offer a team selection.")}
		}

		team, err := cm.memberTeam(r, rawID, form.Response(response.GUID))

		if err != nil {
			return err
		}

		response.TeamID, response.Team = team.ID, team.Name

		return nil
	}

	if !form.AllowsFreeTextTeam() {
		// the driver does not take part with a team
		return nil
	}

	if form.TeamMode == smmodel.SignUpTeamFreeText {
		response.Team = strings.TrimSpace(r.FormValue("Team"))

		return nil
	}

	name := smmodel.NormalizeTeamName(r.FormValue("Team"))

	if name != "" && cm.teams != nil {
		taken, err := cm.teams.NameTaken(name)

		if err != nil {
			return err
		}

		if taken {
			return ValidationError{i18n.Msg("validation.championship.team_name_taken", "\"{team}\" is the name of a team. Please choose it from the list of your teams.", "team", name)}
		}
	}

	response.Team = name

	return nil
}

// memberTeam loads the team a logged in driver chose, which they have to be a member of. A driver who is registered
// with the team already can keep it when they update their registration, even if they have left the team or the
// team was deleted since: they were a member when they registered and the team of a registration is fixed.
func (cm *ChampionshipManager) memberTeam(r *http.Request, rawID string, existing *smmodel.ChampionshipSignUpResponse) (*smmodel.Team, error) {
	account := smweb.AccountFromRequest(r)

	if !smweb.LoggedIn(r)() || account.ID == uuid.Nil {
		return nil, ValidationError{i18n.Msg("validation.championship.team_login", "Please log in to register with a team.")}
	}

	if cm.teams == nil {
		return nil, ValidationError{i18n.Msg("validation.championship.teams_unavailable", "Teams are not available.")}
	}

	id, err := uuid.Parse(rawID)

	if err != nil {
		return nil, ValidationError{i18n.Msg("validation.championship.team_choose", "Please choose a team from the list.")}
	}

	team, err := cm.teams.Lookup(id.String())

	if err != nil && err != smmodel.ErrTeamNotFound {
		return nil, err
	}

	if existing != nil && existing.TakesTeamSlot() && existing.TeamID == id {
		if err == smmodel.ErrTeamNotFound {
			// the team is gone from the store altogether, the registration still knows its name
			return &smmodel.Team{ID: id, Name: existing.Team}, nil
		}

		return team, nil
	}

	if err == smmodel.ErrTeamNotFound || team.IsDeleted() {
		return nil, ValidationError{i18n.Msg("validation.championship.team_gone", "This team does not exist anymore.")}
	}

	if !team.IsMember(account.ID) {
		return nil, ValidationError{i18n.Msg("validation.championship.team_not_member", "You are not a member of this team.")}
	}

	return team, nil
}

// checkSignUpTeam applies the team rules (maximum number of drivers, no change of the team) to a registration and
// turns a violation into a message for the driver.
func (cm *ChampionshipManager) checkSignUpTeam(championship *smmodel.Championship, response *smmodel.ChampionshipSignUpResponse) error {
	if !championship.SignUpForm.AskForTeam {
		return nil
	}

	switch err := championship.SignUpForm.CheckTeam(response, false); err {
	case nil:
		return nil
	case smmodel.ErrSignUpTeamLocked:
		return ValidationError{i18n.Msg("validation.championship.team_locked", "You are already registered for this championship with a different team. The team cannot be changed, please ask an administrator.")}
	case smmodel.ErrSignUpTeamFull:
		return ValidationError{teamFullMessage(championship, response)}
	default:
		return err
	}
}

func teamFullMessage(championship *smmodel.Championship, response *smmodel.ChampionshipSignUpResponse) i18n.Message {
	limit, _ := championship.SignUpForm.TeamLimit()

	return i18n.Msg("validation.championship.team_full", "The team \"{team}\" already has the maximum of {limit} drivers in this championship. Please choose another team.", "team", response.Team, "limit", limit)
}

// CheckAcceptTeamLimit tells whether an administrator can (re)accept a rejected registration without putting more
// drivers into the team than the championship allows. Registrations that are pending are already counted.
func CheckAcceptTeamLimit(championship *smmodel.Championship, response *smmodel.ChampionshipSignUpResponse) error {
	if response.Status != smmodel.ChampionshipEntrantRejected {
		return nil
	}

	if err := championship.SignUpForm.CheckTeamLimit(response); err != nil {
		return ValidationError{teamFullMessage(championship, response)}
	}

	return nil
}

// ChangeEntrantTeam moves a registered driver to another team, which is what moderators do because drivers cannot
// change their team themselves. The team rules do not apply, but if the team ends up with more drivers than the
// championship allows, warning (nil otherwise) says so. teamID is a team of the team list, otherwise freeText is used
// as the team name (empty for no team).
func (cm *ChampionshipManager) ChangeEntrantTeam(championshipID, guid, teamID, freeText string) (warning *i18n.Message, err error) {
	defer LockSignUps()()

	championship, err := cm.LoadChampionship(championshipID)

	if err != nil {
		return nil, err
	}

	response := championship.SignUpForm.Response(guid)

	if response == nil {
		return nil, ValidationError{i18n.Msg("validation.championship.driver_not_registered", "This driver is not registered for the championship.")}
	}

	id, name := uuid.Nil, smmodel.NormalizeTeamName(freeText)

	if teamID = strings.TrimSpace(teamID); teamID != "" {
		if cm.teams == nil {
			return nil, ValidationError{i18n.Msg("validation.championship.teams_unavailable", "Teams are not available.")}
		}

		parsed, err := uuid.Parse(teamID)

		if err != nil {
			return nil, ValidationError{i18n.Msg("validation.championship.team_unknown", "Unknown team.")}
		}

		team, err := cm.teams.Lookup(parsed.String())

		if err == smmodel.ErrTeamNotFound || (err == nil && team.IsDeleted()) {
			return nil, ValidationError{i18n.Msg("validation.championship.team_gone", "This team does not exist anymore.")}
		} else if err != nil {
			return nil, err
		}

		id, name = team.ID, team.Name
	}

	moved := &smmodel.ChampionshipSignUpResponse{GUID: guid, TeamID: id, Team: name}

	if championship.SignUpForm.CheckTeamLimit(moved) != nil {
		limit, _ := championship.SignUpForm.TeamLimit()

		msg := i18n.Msg("validation.championship.team_over_limit", "The team \"{team}\" now has more than the {limit} drivers this championship allows.", "team", name, "limit", limit)
		warning = &msg
	}

	championship.SetEntrantTeam(guid, id, name)

	return warning, cm.UpsertChampionship(championship)
}

// RenameTeam updates the team name in the registrations of all championships that are not finished yet. The
// championships of finished seasons keep the name the team had back then.
func (cm *ChampionshipManager) RenameTeam(team *smmodel.Team) error {
	defer LockSignUps()()

	championships, err := cm.ListChampionships()

	if err != nil {
		return err
	}

	for _, championship := range championships {
		if championship.RenameTeam(team.ID, team.Name) == 0 {
			continue
		}

		if err := cm.UpsertChampionship(championship); err != nil {
			return err
		}
	}

	return nil
}
