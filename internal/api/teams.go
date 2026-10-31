package api

import (
	"sort"
	"time"

	"github.com/google/uuid"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

// The teams API exposes what the team pages show (the teams a visitor may see, their owner and members) in a stable
// JSON format. Account ids, invites and their tokens are intentionally left out.

// Team is a team of drivers.
type Team struct {
	ID   uuid.UUID
	Name string
	URL  string `json:",omitempty"`

	// Visibility is "public" or "private". Private teams are only listed for their members and for moderators.
	Visibility string

	MemberCount int

	// Owner is the display name of the owner of the team.
	Owner string `json:",omitempty"`
}

// TeamMember is a member of a team.
type TeamMember struct {
	// Name is the driver name of the account, or its account name if it has none.
	Name string
	// Role is "owner", "admin" or "member".
	Role   string
	Joined time.Time
}

// TeamDetail is a team with its members, owner first.
type TeamDetail struct {
	Team

	Members []*TeamMember
}

// NewTeam returns the API representation of a team. names gives the display names of the accounts.
func NewTeam(team *smmodel.Team, names map[uuid.UUID]string, baseURL string) *Team {
	visibility := "public"

	if team.IsPrivate() {
		visibility = "private"
	}

	out := &Team{
		ID:          team.ID,
		Name:        team.Name,
		URL:         team.GetURL(baseURL),
		Visibility:  visibility,
		MemberCount: len(team.Members),
	}

	if owner := team.Owner(); owner != nil {
		out.Owner = names[owner.AccountID]
	}

	return out
}

// NewTeamDetail returns the API representation of a team including its members. The owner comes first, then the
// admins, then the members, each in the order they joined.
func NewTeamDetail(team *smmodel.Team, names map[uuid.UUID]string, baseURL string) *TeamDetail {
	out := &TeamDetail{Team: *NewTeam(team, names, baseURL), Members: []*TeamMember{}}

	members := append([]smmodel.TeamMember(nil), team.Members...)

	sort.SliceStable(members, func(i, j int) bool {
		if a, b := roleOrder(members[i].Role), roleOrder(members[j].Role); a != b {
			return a < b
		}

		return members[i].Joined.Before(members[j].Joined)
	})

	for _, member := range members {
		out.Members = append(out.Members, &TeamMember{Name: names[member.AccountID], Role: string(member.Role), Joined: member.Joined})
	}

	return out
}

func roleOrder(role smmodel.TeamRole) int {
	switch role {
	case smmodel.TeamRoleOwner:
		return 0
	case smmodel.TeamRoleAdmin:
		return 1
	}

	return 2
}
