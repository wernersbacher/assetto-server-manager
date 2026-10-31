// Package teams holds the business logic for teams: accounts create teams, join them (public ones freely, private
// ones with an invite), invite others and leave again.
package teams

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/google/uuid"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
)

const (
	// DefaultMaxOwnedTeams is how many teams one account may own if the option is not set.
	DefaultMaxOwnedTeams = 3

	// DefaultInviteTTL is how long an invite can be used.
	DefaultInviteTTL = 7 * 24 * time.Hour
)

// Options are the server wide team settings.
type Options struct {
	// DisableCreation stops normal accounts from creating teams. Moderators can always create teams.
	DisableCreation bool

	// MaxOwnedTeams limits how many teams an account can own. 0 means DefaultMaxOwnedTeams, negative is unlimited.
	// The limit does not apply to moderators.
	MaxOwnedTeams int
}

func (o Options) maxOwned() int {
	if o.MaxOwnedTeams == 0 {
		return DefaultMaxOwnedTeams
	}

	return o.MaxOwnedTeams
}

// OptionsFunc returns the current options. It is a function so changes in the settings apply immediately.
type OptionsFunc func() Options

// Actor is who does something with a team. Moderators (decided by the caller from the account group) may manage every
// team, whether they are a member or not.
type Actor struct {
	AccountID uuid.UUID
	Moderator bool
}

// Manager changes teams. All changes are serialised, because they read the team, modify it and write it back.
type Manager struct {
	store   smstore.Store
	options OptionsFunc

	mutex sync.Mutex

	now      func() time.Time
	newToken func() string

	// onRename is called after a team was renamed, with the lock held.
	onRename func(team *smmodel.Team) error
}

func NewManager(store smstore.Store, options OptionsFunc) *Manager {
	if options == nil {
		options = func() Options { return Options{} }
	}

	return &Manager{
		store:    store,
		options:  options,
		now:      time.Now,
		newToken: randomToken,
	}
}

// SetRenameHook registers a function that is called after a team was renamed, e.g. to update the registrations of
// championships that are still running. An error is logged by the caller of Rename and returned to it, but the rename
// itself is not rolled back.
func (m *Manager) SetRenameHook(hook func(team *smmodel.Team) error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.onRename = hook
}

func randomToken() string {
	b := make([]byte, 16)

	if _, err := rand.Read(b); err != nil {
		panic(err)
	}

	return hex.EncodeToString(b)
}

// ErrRenameHook is returned if a team was renamed, but the follow up work failed.
type ErrRenameHook struct{ Err error }

func (e ErrRenameHook) Error() string {
	return "team was renamed, but updating the registrations failed: " + e.Err.Error()
}

func (m *Manager) requireAccount(id uuid.UUID) error {
	if id == uuid.Nil {
		return smmodel.ErrTeamPermission
	}

	if _, err := m.store.FindAccountByID(id.String()); err != nil {
		return smmodel.ErrTeamPermission
	}

	return nil
}

// canonicalID turns an id from outside (URL, form) into the form the store expects. The stores build file names from
// it, so anything that is not a UUID must never reach them.
func canonicalID(id string) (string, error) {
	parsed, err := uuid.Parse(id)

	if err != nil {
		return "", smmodel.ErrTeamNotFound
	}

	return parsed.String(), nil
}

func (m *Manager) load(id string) (*smmodel.Team, error) {
	id, err := canonicalID(id)

	if err != nil {
		return nil, err
	}

	team, err := m.store.FindTeamByID(id)

	if err != nil {
		return nil, err
	}

	if team.IsDeleted() {
		return nil, smmodel.ErrTeamNotFound
	}

	return team, nil
}

// loadVisible loads a team the actor is allowed to know about. For everybody else a private team does not exist.
func (m *Manager) loadVisible(actor Actor, id string) (*smmodel.Team, error) {
	team, err := m.load(id)

	if err != nil {
		return nil, err
	}

	if !actor.Moderator && !team.CanSee(actor.AccountID, m.now()) {
		return nil, smmodel.ErrTeamNotFound
	}

	return team, nil
}

// loadManaged loads a team the actor may change.
func (m *Manager) loadManaged(actor Actor, id string) (*smmodel.Team, error) {
	team, err := m.loadVisible(actor, id)

	if err != nil {
		return nil, err
	}

	if !actor.Moderator && !team.CanManage(actor.AccountID) {
		return nil, smmodel.ErrTeamPermission
	}

	return team, nil
}

func (m *Manager) nameTaken(name string, exceptID uuid.UUID) (bool, error) {
	teams, err := m.store.ListTeams()

	if err != nil {
		return false, err
	}

	for _, team := range teams {
		if team.ID != exceptID && smmodel.SameTeamName(team.Name, name) {
			return true, nil
		}
	}

	return false, nil
}

// NameTaken reports whether a live team already uses the name.
func (m *Manager) NameTaken(name string) (bool, error) {
	return m.nameTaken(name, uuid.Nil)
}

// Create creates a team that is owned by the actor.
func (m *Manager) Create(actor Actor, name string, visibility smmodel.TeamVisibility) (*smmodel.Team, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if err := m.requireAccount(actor.AccountID); err != nil {
		return nil, err
	}

	options := m.options()

	if !actor.Moderator && options.DisableCreation {
		return nil, smmodel.ErrTeamCreationBlocked
	}

	team, err := smmodel.NewTeam(name, visibility, actor.AccountID, m.now())

	if err != nil {
		return nil, err
	}

	teams, err := m.store.ListTeams()

	if err != nil {
		return nil, err
	}

	owned := 0

	for _, existing := range teams {
		if smmodel.SameTeamName(existing.Name, team.Name) {
			return nil, smmodel.ErrTeamNameTaken
		}

		if existing.IsOwner(actor.AccountID) {
			owned++
		}
	}

	if limit := options.maxOwned(); !actor.Moderator && limit > 0 && owned >= limit {
		return nil, smmodel.ErrTeamLimitReached
	}

	return team, m.store.UpsertTeam(team)
}

// CanCreate reports whether the actor may create teams right now (the actor has to be logged in, which is up to the
// caller to find out).
func (m *Manager) CanCreate(actor Actor) bool {
	return actor.Moderator || !m.options().DisableCreation
}

// Get returns a team if the actor may see it.
func (m *Manager) Get(actor Actor, id string) (*smmodel.Team, error) {
	return m.loadVisible(actor, id)
}

// Lookup returns a team including deleted ones, for registrations that refer to a team. It performs no access check.
func (m *Manager) Lookup(id string) (*smmodel.Team, error) {
	id, err := canonicalID(id)

	if err != nil {
		return nil, err
	}

	return m.store.FindTeamByID(id)
}

// FindByInvite returns the team an open invite token belongs to, so the invited person can see where they are going
// to join. The token is the secret, anybody who has it may know the team.
func (m *Manager) FindByInvite(token string) (*smmodel.Team, error) {
	if token == "" {
		return nil, smmodel.ErrTeamInviteInvalid
	}

	teams, err := m.store.ListTeams()

	if err != nil {
		return nil, err
	}

	now := m.now()

	for _, team := range teams {
		for _, invite := range team.Invites {
			if invite.Token == token && !invite.Expired(now) {
				return team, nil
			}
		}
	}

	return nil, smmodel.ErrTeamInviteInvalid
}

// List returns the teams the actor may see: public teams, their own teams and the private teams that invited them.
// Moderators see all teams.
func (m *Manager) List(actor Actor) ([]*smmodel.Team, error) {
	teams, err := m.store.ListTeams()

	if err != nil {
		return nil, err
	}

	now := m.now()

	var visible []*smmodel.Team

	for _, team := range teams {
		if actor.Moderator || team.CanSee(actor.AccountID, now) {
			visible = append(visible, team)
		}
	}

	return visible, nil
}

// MemberOf returns the teams the account is a member of.
func (m *Manager) MemberOf(accountID uuid.UUID) ([]*smmodel.Team, error) {
	teams, err := m.store.ListTeams()

	if err != nil {
		return nil, err
	}

	var member []*smmodel.Team

	for _, team := range teams {
		if team.IsMember(accountID) {
			member = append(member, team)
		}
	}

	return member, nil
}

// InvitedTo returns the private teams that have invited the account and that it has not joined yet.
func (m *Manager) InvitedTo(accountID uuid.UUID) ([]*smmodel.Team, error) {
	teams, err := m.store.ListTeams()

	if err != nil {
		return nil, err
	}

	now := m.now()

	var invited []*smmodel.Team

	for _, team := range teams {
		if accountID == uuid.Nil || team.IsMember(accountID) {
			continue
		}

		for _, invite := range team.Invites {
			if invite.AccountID == accountID && !invite.Expired(now) {
				invited = append(invited, team)
				break
			}
		}
	}

	return invited, nil
}

// Rename changes the name of a team.
func (m *Manager) Rename(actor Actor, id, name string) (*smmodel.Team, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	team, err := m.loadManaged(actor, id)

	if err != nil {
		return nil, err
	}

	oldName := team.Name

	if err := team.Rename(name, m.now()); err != nil {
		return nil, err
	}

	if taken, err := m.nameTaken(team.Name, team.ID); err != nil {
		return nil, err
	} else if taken {
		return nil, smmodel.ErrTeamNameTaken
	}

	if err := m.store.UpsertTeam(team); err != nil {
		return nil, err
	}

	if m.onRename != nil && team.Name != oldName {
		if err := m.onRename(team); err != nil {
			return team, ErrRenameHook{Err: err}
		}
	}

	return team, nil
}

// SetVisibility makes a team public or private. Members stay members.
func (m *Manager) SetVisibility(actor Actor, id string, visibility smmodel.TeamVisibility) (*smmodel.Team, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	team, err := m.loadManaged(actor, id)

	if err != nil {
		return nil, err
	}

	team.SetVisibility(visibility, m.now())

	return team, m.store.UpsertTeam(team)
}

// Join lets the actor join a team. token is the token of an invite and is only needed for private teams without an
// invite for this account.
func (m *Manager) Join(actor Actor, id, token string) (*smmodel.Team, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if err := m.requireAccount(actor.AccountID); err != nil {
		return nil, err
	}

	team, err := m.load(id)

	if err != nil {
		return nil, err
	}

	// the visibility is decided by Join itself: a private team can only be joined with a valid invite
	if err := team.Join(actor.AccountID, token, m.now()); err != nil {
		return nil, err
	}

	return team, m.store.UpsertTeam(team)
}

// JoinByToken lets the actor join the team an invite token belongs to.
func (m *Manager) JoinByToken(actor Actor, token string) (*smmodel.Team, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if err := m.requireAccount(actor.AccountID); err != nil {
		return nil, err
	}

	if token == "" {
		return nil, smmodel.ErrTeamInviteInvalid
	}

	teams, err := m.store.ListTeams()

	if err != nil {
		return nil, err
	}

	now := m.now()

	for _, team := range teams {
		for _, invite := range team.Invites {
			if invite.Token != token || invite.Expired(now) {
				continue
			}

			if err := team.Join(actor.AccountID, token, now); err != nil {
				return nil, err
			}

			return team, m.store.UpsertTeam(team)
		}
	}

	return nil, smmodel.ErrTeamInviteInvalid
}

// Leave removes the actor from a team. If they were the owner, the ownership moves on. The last member leaving
// deletes the team.
func (m *Manager) Leave(actor Actor, id string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	team, err := m.load(id)

	if err != nil {
		return err
	}

	if !team.IsMember(actor.AccountID) {
		return smmodel.ErrNotTeamMember
	}

	return m.removeMember(team, actor.AccountID)
}

func (m *Manager) removeMember(team *smmodel.Team, accountID uuid.UUID) error {
	_, empty, err := team.RemoveMember(accountID, m.now())

	if err != nil {
		return err
	}

	if empty {
		return m.store.DeleteTeam(team)
	}

	return m.store.UpsertTeam(team)
}

// Kick removes another member. The owner can remove everybody, admins only normal members, moderators everybody.
func (m *Manager) Kick(actor Actor, id string, target uuid.UUID) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	team, err := m.loadManaged(actor, id)

	if err != nil {
		return err
	}

	member := team.Member(target)

	if member == nil {
		return smmodel.ErrNotTeamMember
	}

	if !actor.Moderator && target != actor.AccountID {
		if member.Role == smmodel.TeamRoleOwner || (member.Role == smmodel.TeamRoleAdmin && !team.IsOwner(actor.AccountID)) {
			return smmodel.ErrTeamPermission
		}
	}

	return m.removeMember(team, target)
}

// SetAdmin promotes or demotes a member. Only the owner (or a moderator) can do that.
func (m *Manager) SetAdmin(actor Actor, id string, target uuid.UUID, admin bool) (*smmodel.Team, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	team, err := m.loadManaged(actor, id)

	if err != nil {
		return nil, err
	}

	if !actor.Moderator && !team.IsOwner(actor.AccountID) {
		return nil, smmodel.ErrTeamPermission
	}

	if err := team.SetAdmin(target, admin, m.now()); err != nil {
		return nil, err
	}

	return team, m.store.UpsertTeam(team)
}

// TransferOwnership makes a member the owner. Only the owner (or a moderator, for a team without an active owner)
// can do that.
func (m *Manager) TransferOwnership(actor Actor, id string, to uuid.UUID) (*smmodel.Team, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	team, err := m.loadManaged(actor, id)

	if err != nil {
		return nil, err
	}

	owner := team.Owner()

	if owner == nil {
		return nil, smmodel.ErrTeamPermission
	}

	if !actor.Moderator && owner.AccountID != actor.AccountID {
		return nil, smmodel.ErrTeamPermission
	}

	if err := team.TransferOwnership(owner.AccountID, to, m.now()); err != nil {
		return nil, err
	}

	return team, m.store.UpsertTeam(team)
}

// Invite creates an invite. With a target account only that account can use it, without one everybody with the
// returned token can.
func (m *Manager) Invite(actor Actor, id string, target uuid.UUID) (smmodel.TeamInvite, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	team, err := m.loadManaged(actor, id)

	if err != nil {
		return smmodel.TeamInvite{}, err
	}

	if target != uuid.Nil {
		if _, err := m.store.FindAccountByID(target.String()); err != nil {
			return smmodel.TeamInvite{}, err
		}
	}

	invite, err := team.AddInvite(m.newToken(), actor.AccountID, target, DefaultInviteTTL, m.now())

	if err != nil {
		return smmodel.TeamInvite{}, err
	}

	return invite, m.store.UpsertTeam(team)
}

// RevokeInvite withdraws an invite.
func (m *Manager) RevokeInvite(actor Actor, id, token string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	team, err := m.loadManaged(actor, id)

	if err != nil {
		return err
	}

	if err := team.RevokeInvite(token, m.now()); err != nil {
		return err
	}

	return m.store.UpsertTeam(team)
}

// DeclineInvite lets an account refuse the invite addressed to it.
func (m *Manager) DeclineInvite(actor Actor, id string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	team, err := m.load(id)

	if err != nil {
		return err
	}

	for _, invite := range team.Invites {
		if invite.AccountID == actor.AccountID && actor.AccountID != uuid.Nil {
			if err := team.RevokeInvite(invite.Token, m.now()); err != nil {
				return err
			}

			return m.store.UpsertTeam(team)
		}
	}

	return smmodel.ErrTeamInviteInvalid
}

// Delete deletes a team (soft delete): it disappears from all lists, registrations keep its name. Only the owner or
// a moderator can do that.
func (m *Manager) Delete(actor Actor, id string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	team, err := m.loadManaged(actor, id)

	if err != nil {
		return err
	}

	if !actor.Moderator && !team.IsOwner(actor.AccountID) {
		return smmodel.ErrTeamPermission
	}

	return m.store.DeleteTeam(team)
}

// RemoveAccount takes a deleted account out of all its teams (ownership moves on, empty teams are deleted) and
// withdraws the invites addressed to it.
func (m *Manager) RemoveAccount(accountID uuid.UUID) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	teams, err := m.store.ListTeams()

	if err != nil {
		return err
	}

	for _, team := range teams {
		if team.IsMember(accountID) {
			if err := m.removeMember(team, accountID); err != nil {
				return err
			}

			continue
		}

		before := len(team.Invites)

		for _, invite := range team.Invites {
			if invite.AccountID == accountID {
				_ = team.RevokeInvite(invite.Token, m.now())
			}
		}

		if len(team.Invites) != before {
			if err := m.store.UpsertTeam(team); err != nil {
				return err
			}
		}
	}

	return nil
}
