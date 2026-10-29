package model

import (
	"strings"
	"time"
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

	Name      string
	GUID      string
	Team      string
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
