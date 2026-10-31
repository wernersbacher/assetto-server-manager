package store

import (
	"testing"
	"time"

	"github.com/google/uuid"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

func TestStoreContract_UserSkins(t *testing.T) {
	runStoreContract(t, func(t *testing.T, store Store) {
		if skins, err := store.ListUserSkins(); err != nil || len(skins) != 0 {
			t.Fatalf("list on empty store: %v, %d skins", err, len(skins))
		}

		if _, err := store.FindUserSkinByID(uuid.New().String()); err != smmodel.ErrUserSkinNotFound {
			t.Errorf("expected ErrUserSkinNotFound, got %v", err)
		}

		if err := store.DeleteUserSkin(uuid.New().String()); err != smmodel.ErrUserSkinNotFound {
			t.Errorf("deleting a missing skin: expected ErrUserSkinNotFound, got %v", err)
		}

		now := time.Now()
		owner, team := uuid.New(), uuid.New()

		zed := smmodel.NewUserSkin("ferrari", "Zed", owner, 4, 2048, now)
		alpha := smmodel.NewUserSkin("ferrari", "alpha", owner, 2, 1024, now)
		bmw := smmodel.NewUserSkin("bmw", "red", owner, 1, 10, now)

		if err := alpha.SetVisibility(smmodel.SkinTeam, team, now); err != nil {
			t.Fatal(err)
		}

		for _, skin := range []*smmodel.UserSkin{zed, alpha, bmw} {
			if err := store.UpsertUserSkin(skin); err != nil {
				t.Fatal(err)
			}

			if skin.Updated.IsZero() {
				t.Error("expected UpsertUserSkin to set Updated")
			}
		}

		found, err := store.FindUserSkinByID(alpha.ID.String())

		if err != nil {
			t.Fatal(err)
		}

		if found.Car != "ferrari" || found.Name != "alpha" || found.Owner != owner || found.Visibility != smmodel.SkinTeam ||
			found.TeamID != team || found.Files != 2 || found.Size != 1024 {
			t.Errorf("skin did not round-trip: %+v", found)
		}

		// listed by car, then by name (case insensitive), identical in both stores
		skins, err := store.ListUserSkins()

		if err != nil || len(skins) != 3 {
			t.Fatalf("list: %v, %d skins", err, len(skins))
		}

		if skins[0].Car != "bmw" || skins[1].Name != "alpha" || skins[2].Name != "Zed" {
			t.Errorf("unexpected order: %s/%s, %s/%s, %s/%s", skins[0].Car, skins[0].Name, skins[1].Car, skins[1].Name, skins[2].Car, skins[2].Name)
		}

		// updating overwrites
		if err := alpha.SetVisibility(smmodel.SkinGlobal, uuid.Nil, now); err != nil {
			t.Fatal(err)
		}

		if err := store.UpsertUserSkin(alpha); err != nil {
			t.Fatal(err)
		}

		if reloaded, _ := store.FindUserSkinByID(alpha.ID.String()); reloaded.Visibility != smmodel.SkinGlobal || reloaded.TeamID != uuid.Nil {
			t.Errorf("expected the update to be stored: %+v", reloaded)
		}

		// delete removes the record
		if err := store.DeleteUserSkin(zed.ID.String()); err != nil {
			t.Fatal(err)
		}

		if _, err := store.FindUserSkinByID(zed.ID.String()); err != smmodel.ErrUserSkinNotFound {
			t.Errorf("expected the skin to be gone, got %v", err)
		}

		// the slot view shares the skins
		if viewSkins, err := ForServer(store, "slot-a").ListUserSkins(); err != nil || len(viewSkins) != 2 {
			t.Errorf("slot view must see the shared skins: %v %d", err, len(viewSkins))
		}
	})
}
