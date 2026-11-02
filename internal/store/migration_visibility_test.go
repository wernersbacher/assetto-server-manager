package store

import (
	"testing"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

type legacyAccountOptions struct {
	IsOpen         bool
	PublicStats    bool
	DownloadAccess int
	MaxOwnedTeams  int
}

func TestConvertOpenOptionsToVisibility(t *testing.T) {
	allLoggedIn := smmodel.Visibility{}

	for _, a := range smmodel.AllAreas {
		allLoggedIn[a] = smmodel.LevelLoggedIn
	}

	cases := []struct {
		name   string
		legacy legacyAccountOptions
		want   map[smmodel.Area]smmodel.Level // levels per area after the migration; others are the defaults
	}{
		{"closed", legacyAccountOptions{}, allLoggedIn},
		{"closed ignores stats and downloads", legacyAccountOptions{PublicStats: true, DownloadAccess: 2}, allLoggedIn},
		{"open", legacyAccountOptions{IsOpen: true, PublicStats: true}, nil},
		{"open, private stats", legacyAccountOptions{IsOpen: true}, map[smmodel.Area]smmodel.Level{smmodel.AreaStats: smmodel.LevelLoggedIn}},
		{"open, login downloads", legacyAccountOptions{IsOpen: true, PublicStats: true, DownloadAccess: 1}, map[smmodel.Area]smmodel.Level{smmodel.AreaDownloads: smmodel.LevelLoggedIn}},
		{"open, downloads off", legacyAccountOptions{IsOpen: true, PublicStats: true, DownloadAccess: 2}, map[smmodel.Area]smmodel.Level{smmodel.AreaDownloads: smmodel.LevelAdmin}},
	}

	for _, c := range cases {
		c := c

		runStoreContract(t, func(t *testing.T, store Store) {
			if err := store.SetMeta(versionMetaKey, 5); err != nil {
				t.Fatal(err)
			}

			c.legacy.MaxOwnedTeams = 3

			if err := store.SetMeta(serverAccountOptionsMetaKey, c.legacy); err != nil {
				t.Fatal(err)
			}

			if err := convertOpenOptionsToVisibility(store); err != nil {
				t.Fatalf("%s: %s", c.name, err)
			}

			var got smmodel.ServerAccountOptions

			if err := store.GetMeta(serverAccountOptionsMetaKey, &got); err != nil {
				t.Fatal(err)
			}

			if got.MaxOwnedTeams != 3 {
				t.Errorf("%s: other options must be kept, got %+v", c.name, got)
			}

			for _, area := range smmodel.AllAreas {
				want, ok := c.want[area]

				if !ok {
					want = smmodel.DefaultLevel(area)
				}

				if level := got.Visibility.Level(area); level != want {
					t.Errorf("%s: area %s: expected %s, got %s", c.name, area, want, level)
				}
			}
		})
	}
}

func TestConvertOpenOptionsToVisibility_NoStoredOptions(t *testing.T) {
	// an existing installation which never saved the options was closed
	runStoreContract(t, func(t *testing.T, store Store) {
		if err := store.SetMeta(versionMetaKey, 5); err != nil {
			t.Fatal(err)
		}

		if err := convertOpenOptionsToVisibility(store); err != nil {
			t.Fatal(err)
		}

		var got smmodel.ServerAccountOptions

		if err := store.GetMeta(serverAccountOptionsMetaKey, &got); err != nil {
			t.Fatal(err)
		}

		for _, area := range smmodel.AllAreas {
			if got.Visibility.Level(area) != smmodel.LevelLoggedIn {
				t.Errorf("area %s: expected a closed server to need a login, got %s", area, got.Visibility.Level(area))
			}
		}
	})

	// a new installation (no version yet) keeps the defaults
	runStoreContract(t, func(t *testing.T, store Store) {
		if err := convertOpenOptionsToVisibility(store); err != nil {
			t.Fatal(err)
		}

		var got smmodel.ServerAccountOptions

		if err := store.GetMeta(serverAccountOptionsMetaKey, &got); err != ErrValueNotSet {
			t.Errorf("expected nothing to be stored for a new installation, got %+v (%v)", got, err)
		}
	})
}
