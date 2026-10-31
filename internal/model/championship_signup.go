package model

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

func (c *Championship) SignUpAvailable() bool {
	numTotalEntrants := 0
	numFilledSlots := 0

	for _, class := range c.Classes {
		for _, entrant := range class.Entrants {
			if entrant.GUID != "" {
				numFilledSlots++
			}

			numTotalEntrants++
		}
	}

	return c.SignUpForm.Enabled && c.Progress() < 100.0 && numFilledSlots < numTotalEntrants
}

type ChampionshipSignUpForm struct {
	Enabled          bool
	AskForEmail      bool
	AskForTeam       bool
	HideCarChoice    bool
	ExtraFields      []string
	RequiresApproval bool

	// TeamMode decides how the team is asked for if AskForTeam is set, see SignUpTeamMode. The zero value keeps the
	// free text field of championships created before teams existed.
	TeamMode SignUpTeamMode

	// MaxDriversPerTeam limits the registrations (pending or accepted) per team, for teams that are chosen from the
	// team list. 0 means DefaultMaxDriversPerTeam, a negative number means unlimited.
	MaxDriversPerTeam int

	Responses []*ChampionshipSignUpResponse
}

func (c ChampionshipSignUpForm) EmailList(group string) string {
	var filteredStatus ChampionshipEntrantStatus

	switch group {
	case "accepted":
		filteredStatus = ChampionshipEntrantAccepted
	case "rejected":
		filteredStatus = ChampionshipEntrantRejected
	case "pending":
		filteredStatus = ChampionshipEntrantPending
	case "all":
		filteredStatus = ChampionshipEntrantAll
	default:
		panic("unknown entrant status: " + group)
	}

	var filteredEmails []string

	for _, entrant := range c.Responses {
		if entrant.Email == "" {
			continue
		}

		if filteredStatus == ChampionshipEntrantAll || entrant.Status == filteredStatus {
			filteredEmails = append(filteredEmails, entrant.Email)
		}
	}

	return strings.Join(filteredEmails, ",")
}

type ChampionshipEntrantStatus string

const (
	ChampionshipEntrantAll      = "All"
	ChampionshipEntrantAccepted = "Accepted"
	ChampionshipEntrantRejected = "Rejected"
	ChampionshipEntrantPending  = "Pending Approval"
)

type ChampionshipSignUpResponse struct {
	Created time.Time

	Name string
	GUID string

	// Team is the name of the team. For a team chosen from the team list (TeamID is set) it is the name at the time
	// of the registration (updated when the team is renamed while the championship runs), otherwise free text.
	Team string
	// TeamID refers to the team the driver chose from the team list. It is uuid.Nil for free text teams and for
	// registrations from before teams existed.
	TeamID uuid.UUID

	Email     string
	Car       string
	Skin      string
	Questions map[string]string

	Status ChampionshipEntrantStatus
}

func (csr ChampionshipSignUpResponse) GetName() string {
	return csr.Name
}

func (csr ChampionshipSignUpResponse) GetTeam() string {
	return csr.Team
}

func (csr ChampionshipSignUpResponse) GetCar() string {
	return csr.Car
}

func (csr ChampionshipSignUpResponse) GetSkin() string {
	return csr.Skin
}

func (csr ChampionshipSignUpResponse) GetGUID() string {
	return csr.GUID
}
