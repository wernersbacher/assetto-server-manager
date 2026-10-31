package model

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestUserSkinUsableBy(t *testing.T) {
	owner, other, team := uuid.New(), uuid.New(), uuid.New()
	skin := NewUserSkin("ks_mazda_mx5_cup", "blue", owner, 3, 100, time.Now())

	if !skin.UsableBy(owner, nil) || skin.UsableBy(other, []uuid.UUID{team}) || skin.UsableBy(uuid.Nil, nil) {
		t.Error("a new skin must be private to its owner")
	}

	if err := skin.SetVisibility(SkinTeam, uuid.Nil, time.Now()); err != ErrSkinVisibility {
		t.Errorf("a team skin needs a team, got %v", err)
	}

	if err := skin.SetVisibility(SkinTeam, team, time.Now()); err != nil {
		t.Fatal(err)
	}

	if !skin.UsableBy(other, []uuid.UUID{team}) || skin.UsableBy(other, []uuid.UUID{uuid.New()}) || !skin.UsableBy(owner, nil) {
		t.Error("a team skin is usable by team members and the owner only")
	}

	if err := skin.SetVisibility(SkinGlobal, team, time.Now()); err != nil || skin.TeamID != uuid.Nil {
		t.Errorf("global drops the team: %v %v", err, skin.TeamID)
	}

	if !skin.UsableBy(other, nil) || !skin.UsableBy(uuid.Nil, nil) {
		t.Error("a global skin is usable by everybody")
	}

	if _, err := ParseSkinVisibility("everyone"); err != ErrSkinVisibility {
		t.Errorf("unknown visibility must fail, got %v", err)
	}
}
