package store

import (
	"testing"
	"time"

	"github.com/google/uuid"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

func teamNames(teams []*smmodel.Team) []string {
	var names []string

	for _, team := range teams {
		names = append(names, team.Name)
	}

	return names
}

func TestStoreContract_Teams(t *testing.T) {
	runStoreContract(t, func(t *testing.T, store Store) {
		teams, err := store.ListTeams()

		if err != nil || len(teams) != 0 {
			t.Fatalf("list on empty store: %v, %d teams", err, len(teams))
		}

		if _, err := store.FindTeamByID(uuid.New().String()); err != smmodel.ErrTeamNotFound {
			t.Errorf("expected ErrTeamNotFound on a fresh store, got %v", err)
		}

		now := time.Now()
		owner, member := uuid.New(), uuid.New()

		zebra, err := smmodel.NewTeam("zebra Racing", smmodel.TeamPublic, owner, now)

		if err != nil {
			t.Fatal(err)
		}

		alpha, err := smmodel.NewTeam("Alpha Team", smmodel.TeamPrivate, owner, now)

		if err != nil {
			t.Fatal(err)
		}

		if err := alpha.Join(member, "", now); err != smmodel.ErrTeamInviteInvalid {
			t.Fatal(err)
		}

		if _, err := alpha.AddInvite("tok", owner, member, time.Hour, now); err != nil {
			t.Fatal(err)
		}

		if err := alpha.Join(member, "", now); err != nil {
			t.Fatal(err)
		}

		if _, err := alpha.AddInvite("open-link", owner, uuid.Nil, time.Hour, now); err != nil {
			t.Fatal(err)
		}

		for _, team := range []*smmodel.Team{zebra, alpha} {
			if err := store.UpsertTeam(team); err != nil {
				t.Fatalf("upsert team: %s", err)
			}

			if team.Updated.IsZero() {
				t.Errorf("expected UpsertTeam to set Updated on %s", team.Name)
			}
		}

		// everything round-trips: members, roles, invites, visibility
		found, err := store.FindTeamByID(alpha.ID.String())

		if err != nil {
			t.Fatal(err)
		}

		if found.ID != alpha.ID || found.Name != "Alpha Team" || !found.IsPrivate() {
			t.Errorf("team did not round-trip: %+v", found)
		}

		if len(found.Members) != 2 || !found.IsOwner(owner) || found.Member(member).Role != smmodel.TeamRoleMember {
			t.Errorf("members did not round-trip: %+v", found.Members)
		}

		if len(found.Invites) != 1 || found.Invites[0].Token != "open-link" || found.Invites[0].AccountID != uuid.Nil {
			t.Errorf("invites did not round-trip: %+v", found.Invites)
		}

		if found.Invites[0].Expires.IsZero() || found.Invites[0].CreatedBy != owner {
			t.Errorf("invite details did not round-trip: %+v", found.Invites[0])
		}

		// listed by name, case insensitive and identical in both stores
		teams, err = store.ListTeams()

		if err != nil {
			t.Fatal(err)
		}

		if got := teamNames(teams); len(got) != 2 || got[0] != "Alpha Team" || got[1] != "zebra Racing" {
			t.Errorf("expected teams ordered by name, got %v", got)
		}

		// updating overwrites
		alpha.Name = "Alpha Team (renamed)"
		alpha.SetVisibility(smmodel.TeamPublic, now)

		if err := store.UpsertTeam(alpha); err != nil {
			t.Fatal(err)
		}

		teams, _ = store.ListTeams()

		if got := teamNames(teams); len(got) != 2 || got[0] != "Alpha Team (renamed)" {
			t.Errorf("expected the update to replace the team, got %v", got)
		}

		if reloaded, _ := store.FindTeamByID(alpha.ID.String()); reloaded.IsPrivate() {
			t.Error("expected the visibility change to be stored")
		}

		// delete is a soft delete: hidden from the list, but registrations can still resolve it
		if err := store.DeleteTeam(zebra); err != nil {
			t.Fatalf("delete team: %s", err)
		}

		teams, _ = store.ListTeams()

		if got := teamNames(teams); len(got) != 1 || got[0] != "Alpha Team (renamed)" {
			t.Errorf("expected only the remaining team, got %v", got)
		}

		deleted, err := store.FindTeamByID(zebra.ID.String())

		if err != nil {
			t.Fatalf("expected a soft-deleted team to still be findable, got %s", err)
		}

		if !deleted.IsDeleted() || deleted.Name != "zebra Racing" {
			t.Errorf("expected the deleted team with its data, got %+v", deleted)
		}

		// the slot view shares the teams
		view := ForServer(store, "slot-a")

		if viewTeams, err := view.ListTeams(); err != nil || len(viewTeams) != 1 {
			t.Errorf("slot view must see the shared teams: %v %d", err, len(viewTeams))
		}
	})
}
