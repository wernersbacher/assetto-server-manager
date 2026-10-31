package model

import (
	"encoding/gob"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

func init() {
	gob.RegisterName("github.com/wernersbacher/ACPitwall.Team", Team{})
}

const (
	// TeamNameMinLength and TeamNameMaxLength limit the length of a team name in characters.
	TeamNameMinLength = 2
	TeamNameMaxLength = 40
)

var (
	ErrTeamNameInvalid     = errors.New("servermanager: invalid team name")
	ErrTeamNotFound        = errors.New("servermanager: team not found")
	ErrTeamNameTaken       = errors.New("servermanager: team name is already taken")
	ErrNotTeamMember       = errors.New("servermanager: account is not a member of the team")
	ErrAlreadyTeamMember   = errors.New("servermanager: account is already a member of the team")
	ErrTeamInviteInvalid   = errors.New("servermanager: team invite is invalid or expired")
	ErrTeamPermission      = errors.New("servermanager: not allowed to do this in the team")
	ErrTeamOwnerRole       = errors.New("servermanager: the role of the team owner cannot be changed, transfer the ownership instead")
	ErrTeamTransferToSelf  = errors.New("servermanager: the ownership cannot be transferred to its owner")
	ErrTeamLimitReached    = errors.New("servermanager: team limit reached")
	ErrTeamCreationBlocked = errors.New("servermanager: creating teams is disabled")
)

// TeamVisibility decides who can see and join a team.
type TeamVisibility string

const (
	// TeamPublic teams are listed for everybody and can be joined freely. It is the zero value.
	TeamPublic TeamVisibility = "public"
	// TeamPrivate teams are only visible to their members (and moderators) and can only be joined with an invite.
	TeamPrivate TeamVisibility = "private"
)

// TeamRole is the role of a member inside a team.
type TeamRole string

const (
	TeamRoleOwner  TeamRole = "owner"
	TeamRoleAdmin  TeamRole = "admin"
	TeamRoleMember TeamRole = "member"
)

type TeamMember struct {
	AccountID uuid.UUID
	Role      TeamRole
	Joined    time.Time
}

// TeamInvite lets an account join a team. An invite with an AccountID can only be used by that account, otherwise
// everybody who knows the Token can use it. Every invite can only be used once.
type TeamInvite struct {
	Token     string
	AccountID uuid.UUID
	CreatedBy uuid.UUID
	Created   time.Time
	Expires   time.Time
}

func (i TeamInvite) Expired(now time.Time) bool {
	return !i.Expires.IsZero() && !now.Before(i.Expires)
}

// Team is a group of accounts. Teams are shared by all server slots. Deleting a team is a soft delete (Deleted is set),
// so registrations which refer to a deleted team keep working.
type Team struct {
	ID uuid.UUID

	Created time.Time
	Updated time.Time
	Deleted time.Time

	Name       string
	Visibility TeamVisibility

	Members []TeamMember
	Invites []TeamInvite
}

// NormalizeTeamName trims the name and collapses runs of whitespace into a single space.
func NormalizeTeamName(name string) string {
	return strings.Join(strings.Fields(name), " ")
}

// ValidateTeamName checks a (normalized) team name.
func ValidateTeamName(name string) error {
	length := utf8.RuneCountInString(name)

	if length < TeamNameMinLength || length > TeamNameMaxLength {
		return ErrTeamNameInvalid
	}

	for _, r := range name {
		if unicode.IsControl(r) {
			return ErrTeamNameInvalid
		}
	}

	return nil
}

// SameTeamName reports whether two team names are the same for the uniqueness check (case and extra whitespace
// are ignored).
func SameTeamName(a, b string) bool {
	return strings.EqualFold(NormalizeTeamName(a), NormalizeTeamName(b))
}

// NewTeam creates a team with the given account as its owner.
func NewTeam(name string, visibility TeamVisibility, owner uuid.UUID, now time.Time) (*Team, error) {
	name = NormalizeTeamName(name)

	if err := ValidateTeamName(name); err != nil {
		return nil, err
	}

	if visibility != TeamPrivate {
		visibility = TeamPublic
	}

	return &Team{
		ID:         uuid.New(),
		Created:    now,
		Updated:    now,
		Name:       name,
		Visibility: visibility,
		Members:    []TeamMember{{AccountID: owner, Role: TeamRoleOwner, Joined: now}},
	}, nil
}

// GetURL is the address of the team page, or an empty string if the base URL of the server is not known.
func (t *Team) GetURL(baseURL string) string {
	if baseURL != "" {
		return baseURL + "/teams/" + t.ID.String()
	}

	return ""
}

func (t *Team) IsDeleted() bool {
	return !t.Deleted.IsZero()
}

func (t *Team) IsPrivate() bool {
	return t.Visibility == TeamPrivate
}

func (t *Team) Member(accountID uuid.UUID) *TeamMember {
	for index := range t.Members {
		if t.Members[index].AccountID == accountID {
			return &t.Members[index]
		}
	}

	return nil
}

func (t *Team) IsMember(accountID uuid.UUID) bool {
	return t.Member(accountID) != nil
}

func (t *Team) Owner() *TeamMember {
	for index := range t.Members {
		if t.Members[index].Role == TeamRoleOwner {
			return &t.Members[index]
		}
	}

	return nil
}

func (t *Team) IsOwner(accountID uuid.UUID) bool {
	member := t.Member(accountID)

	return member != nil && member.Role == TeamRoleOwner
}

// CanManage reports whether the account may change the team (members, invites, name, visibility).
func (t *Team) CanManage(accountID uuid.UUID) bool {
	member := t.Member(accountID)

	return member != nil && (member.Role == TeamRoleOwner || member.Role == TeamRoleAdmin)
}

// CanSee reports whether the account may see the team: public teams for everybody, private teams for their members
// and for the accounts the team has invited.
func (t *Team) CanSee(accountID uuid.UUID, now time.Time) bool {
	if t.IsDeleted() {
		return false
	}

	if !t.IsPrivate() || t.IsMember(accountID) {
		return true
	}

	return t.hasInviteFor(accountID, now)
}

func (t *Team) hasInviteFor(accountID uuid.UUID, now time.Time) bool {
	if accountID == uuid.Nil {
		return false
	}

	for _, invite := range t.Invites {
		if invite.AccountID == accountID && !invite.Expired(now) {
			return true
		}
	}

	return false
}

// Join adds the account to the team. Public teams can be joined without an invite, private teams need an invite,
// either one for the account or one identified by its token. A used invite is consumed.
func (t *Team) Join(accountID uuid.UUID, token string, now time.Time) error {
	if t.IsDeleted() {
		return ErrTeamNotFound
	}

	if t.IsMember(accountID) {
		return ErrAlreadyTeamMember
	}

	if t.IsPrivate() {
		if !t.consumeInvite(accountID, token, now) {
			return ErrTeamInviteInvalid
		}
	} else {
		// an invite for this account became pointless, link invites stay untouched
		t.consumeInvite(accountID, "", now)
	}

	t.Members = append(t.Members, TeamMember{AccountID: accountID, Role: TeamRoleMember, Joined: now})
	t.Updated = now

	return nil
}

// RemoveMember removes a member (leaving or being kicked). If the owner is removed the ownership moves on to the
// oldest admin or, without admins, the oldest member (newOwner). If nobody is left empty is true and the caller
// should delete the team.
func (t *Team) RemoveMember(accountID uuid.UUID, now time.Time) (newOwner uuid.UUID, empty bool, err error) {
	index := -1

	for i := range t.Members {
		if t.Members[i].AccountID == accountID {
			index = i
			break
		}
	}

	if index < 0 {
		return uuid.Nil, false, ErrNotTeamMember
	}

	wasOwner := t.Members[index].Role == TeamRoleOwner

	t.Members = append(t.Members[:index:index], t.Members[index+1:]...)
	t.Updated = now

	if len(t.Members) == 0 {
		return uuid.Nil, true, nil
	}

	if wasOwner {
		successor := t.successor()
		successor.Role = TeamRoleOwner

		return successor.AccountID, false, nil
	}

	return uuid.Nil, false, nil
}

// successor finds the member who takes over a team whose owner left: the member who joined first, admins first.
func (t *Team) successor() *TeamMember {
	var best *TeamMember

	for index := range t.Members {
		candidate := &t.Members[index]

		if best == nil {
			best = candidate
			continue
		}

		if candidate.Role == TeamRoleAdmin && best.Role != TeamRoleAdmin {
			best = candidate
		} else if candidate.Role == best.Role && candidate.Joined.Before(best.Joined) {
			best = candidate
		}
	}

	return best
}

// SetAdmin promotes a member to admin or demotes an admin to a normal member. The owner keeps their role.
func (t *Team) SetAdmin(accountID uuid.UUID, admin bool, now time.Time) error {
	member := t.Member(accountID)

	if member == nil {
		return ErrNotTeamMember
	}

	if member.Role == TeamRoleOwner {
		return ErrTeamOwnerRole
	}

	if admin {
		member.Role = TeamRoleAdmin
	} else {
		member.Role = TeamRoleMember
	}

	t.Updated = now

	return nil
}

// TransferOwnership makes another member the owner. The previous owner becomes an admin.
func (t *Team) TransferOwnership(from, to uuid.UUID, now time.Time) error {
	if from == to {
		return ErrTeamTransferToSelf
	}

	if !t.IsOwner(from) {
		return ErrTeamPermission
	}

	target := t.Member(to)

	if target == nil {
		return ErrNotTeamMember
	}

	t.Member(from).Role = TeamRoleAdmin
	target.Role = TeamRoleOwner
	t.Updated = now

	return nil
}

// AddInvite adds an invite. A target of uuid.Nil creates an invite everybody with the token can use.
func (t *Team) AddInvite(token string, createdBy, target uuid.UUID, ttl time.Duration, now time.Time) (TeamInvite, error) {
	if token == "" {
		return TeamInvite{}, ErrTeamInviteInvalid
	}

	if target != uuid.Nil && t.IsMember(target) {
		return TeamInvite{}, ErrAlreadyTeamMember
	}

	invite := TeamInvite{Token: token, AccountID: target, CreatedBy: createdBy, Created: now}

	if ttl > 0 {
		invite.Expires = now.Add(ttl)
	}

	t.PruneInvites(now)

	// an account has one open invite, a new one replaces the old one
	if target != uuid.Nil {
		t.removeInvites(func(i TeamInvite) bool { return i.AccountID == target })
	}

	t.Invites = append(t.Invites, invite)
	t.Updated = now

	return invite, nil
}

// RevokeInvite removes an invite by its token.
func (t *Team) RevokeInvite(token string, now time.Time) error {
	before := len(t.Invites)

	t.removeInvites(func(i TeamInvite) bool { return i.Token == token })

	if len(t.Invites) == before {
		return ErrTeamInviteInvalid
	}

	t.Updated = now

	return nil
}

// PruneInvites drops expired invites.
func (t *Team) PruneInvites(now time.Time) {
	t.removeInvites(func(i TeamInvite) bool { return i.Expired(now) })
}

func (t *Team) removeInvites(match func(TeamInvite) bool) {
	var kept []TeamInvite

	for _, invite := range t.Invites {
		if !match(invite) {
			kept = append(kept, invite)
		}
	}

	t.Invites = kept
}

// consumeInvite uses up the invite that lets the account in and reports whether there was one.
func (t *Team) consumeInvite(accountID uuid.UUID, token string, now time.Time) bool {
	for index, invite := range t.Invites {
		if invite.Expired(now) {
			continue
		}

		byToken := token != "" && invite.Token == token && (invite.AccountID == uuid.Nil || invite.AccountID == accountID)
		byAccount := token == "" && accountID != uuid.Nil && invite.AccountID == accountID

		if byToken || byAccount {
			t.Invites = append(t.Invites[:index:index], t.Invites[index+1:]...)

			return true
		}
	}

	return false
}

// Rename changes the name, which must be valid. The uniqueness is checked by the caller, who knows all teams.
func (t *Team) Rename(name string, now time.Time) error {
	name = NormalizeTeamName(name)

	if err := ValidateTeamName(name); err != nil {
		return err
	}

	t.Name = name
	t.Updated = now

	return nil
}

// SetVisibility changes the visibility. Existing members stay members when a team becomes private.
func (t *Team) SetVisibility(visibility TeamVisibility, now time.Time) {
	if visibility != TeamPrivate {
		visibility = TeamPublic
	}

	t.Visibility = visibility
	t.Updated = now
}
