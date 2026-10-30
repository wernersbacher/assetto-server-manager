package steam

import (
	"reflect"
	"testing"
)

func TestSteamCmdArgsOrder(t *testing.T) {
	got := steamCmdArgs("/base", "user", "pw")
	want := []string{
		"+@sSteamCmdForcePlatformType windows",
		"+force_install_dir /base",
		"+login user pw",
		"+app_update 302550",
		"+quit",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}
