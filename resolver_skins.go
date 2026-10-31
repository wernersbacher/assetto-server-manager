package servermanager

import (
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/skins"
)

// resolveSkinManager returns the manager of the skins that accounts upload. Like the cars it is shared by all slots.
func (r *Resolver) resolveSkinManager() *skins.Manager {
	if r.skinManager != nil {
		return r.skinManager
	}

	r.skinManager = skins.NewManager(
		r.store,
		func() string { return r.resolveEnv().InstallPath() },
		func() smmodel.SkinSettings { return r.resolveAccountHandler().SkinSettings() },
		func(account uuid.UUID) []uuid.UUID {
			teams, err := r.resolveTeamManager().MemberOf(account)

			if err != nil {
				logrus.WithError(err).Error("could not load the teams of an account for its skins")
				return nil
			}

			ids := make([]uuid.UUID, 0, len(teams))

			for _, team := range teams {
				ids = append(ids, team.ID)
			}

			return ids
		},
	)

	return r.skinManager
}

// userSkinDir is the skin directory of a private or team skin for the CM wrapper, or "".
func (r *Resolver) userSkinDir(car, skin string) string {
	record, dir, err := r.resolveSkinManager().Resolve(car, skin)

	if err != nil || record == nil {
		return ""
	}

	return dir
}

// skinInfo gives the skin pages the names of teams and accounts.
type skinInfo struct {
	r *Resolver
}

func (s skinInfo) TeamsOf(account uuid.UUID) []*smmodel.Team {
	teams, err := s.r.resolveTeamManager().MemberOf(account)

	if err != nil {
		logrus.WithError(err).Error("could not load the teams of an account for its skins")
		return nil
	}

	return teams
}

func (s skinInfo) TeamName(id uuid.UUID) string {
	team, err := s.r.resolveTeamManager().Lookup(id.String())

	if err != nil {
		return ""
	}

	return team.Name
}

func (s skinInfo) AccountName(id uuid.UUID) string {
	account, err := s.r.store.FindAccountByID(id.String())

	if err != nil {
		return ""
	}

	if account.DriverName != "" {
		return account.DriverName
	}

	return account.Name
}
