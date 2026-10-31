package store

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

const teamsDir = "teams"

// sortTeams orders teams by name, case insensitive, so both stores list them identically.
func sortTeams(teams []*smmodel.Team) {
	sort.SliceStable(teams, func(i, j int) bool {
		a, b := strings.ToLower(teams[i].Name), strings.ToLower(teams[j].Name)

		if a != b {
			return a < b
		}

		return teams[i].ID.String() < teams[j].ID.String()
	})
}

func (rs *JSONStore) UpsertTeam(team *smmodel.Team) error {
	team.Updated = time.Now()

	return rs.encodeFile(rs.shared, filepath.Join(teamsDir, team.ID.String()+".json"), team)
}

func (rs *JSONStore) FindTeamByID(id string) (*smmodel.Team, error) {
	var team *smmodel.Team

	err := rs.decodeFile(rs.shared, filepath.Join(teamsDir, id+".json"), &team)

	if os.IsNotExist(err) {
		return nil, smmodel.ErrTeamNotFound
	} else if err != nil {
		return nil, err
	}

	return team, nil
}

func (rs *JSONStore) ListTeams() ([]*smmodel.Team, error) {
	files, err := rs.listFiles(filepath.Join(rs.shared, teamsDir))

	if err != nil {
		return nil, err
	}

	var teams []*smmodel.Team

	for _, file := range files {
		team, err := rs.FindTeamByID(file)

		if err != nil || team.IsDeleted() {
			continue
		}

		teams = append(teams, team)
	}

	sortTeams(teams)

	return teams, nil
}

func (rs *JSONStore) DeleteTeam(team *smmodel.Team) error {
	team.Deleted = time.Now()

	return rs.UpsertTeam(team)
}
