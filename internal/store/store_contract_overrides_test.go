package store

import (
	"testing"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

func TestStoreContract_ServerOverrides(t *testing.T) {
	runStoreContract(t, func(t *testing.T, store Store) {
		for _, id := range []string{"", "slot-a"} {
			plain := ForServer(store, "")
			if id != "" {
				plain = ForServer(store, "slot-a")
			}

			opts, err := plain.LoadServerOptions()

			if err != nil {
				t.Fatal(err)
			}

			opts.UDPPort, opts.TCPPort, opts.HTTPPort, opts.ContentManagerWrapperPort = 9600, 9600, 8081, 0
			opts.EnableContentManagerWrapper = 0

			if err := plain.UpsertServerOptions(opts); err != nil {
				t.Fatal(err)
			}

			view := ForServerWithOverrides(store, smmodel.ServerID(id), ServerOverrides{Game: 9700, ContentManagerWrapper: 8090})

			got, err := view.LoadServerOptions()

			if err != nil {
				t.Fatal(err)
			}

			if got.UDPPort != 9700 || got.TCPPort != 9700 || got.HTTPPort != 8081 || got.ContentManagerWrapperPort != 8090 || got.EnableContentManagerWrapper != 0 {
				t.Errorf("slot %q: overrides not applied: %d %d %d %d %d", id, got.UDPPort, got.TCPPort, got.HTTPPort, got.ContentManagerWrapperPort, got.EnableContentManagerWrapper)
			}

			// saving from the UI (effective values) and changing an unlocked field keeps the stored locked values
			got.HTTPPort = 8082
			got.Name = "changed"
			got.EnableContentManagerWrapper = 1

			if err := view.UpsertServerOptions(got); err != nil {
				t.Fatal(err)
			}

			stored, err := plain.LoadServerOptions()

			if err != nil {
				t.Fatal(err)
			}

			if stored.UDPPort != 9600 || stored.TCPPort != 9600 || stored.ContentManagerWrapperPort != 0 {
				t.Errorf("slot %q: locked values were persisted: %+v", id, stored)
			}

			if stored.HTTPPort != 8082 || stored.Name != "changed" || stored.EnableContentManagerWrapper != 1 {
				t.Errorf("slot %q: unlocked fields not saved: %d %q", id, stored.HTTPPort, stored.Name)
			}

			if l := LockedFieldsOf(view); !l.UDPPort || !l.TCPPort || l.HTTPPort || !l.Has("ContentManagerWrapperPort") {
				t.Errorf("unexpected locks: %+v", l)
			}

			if LockedFieldsOf(plain).Any() {
				t.Error("plain view must not lock anything")
			}
		}
	})
}
