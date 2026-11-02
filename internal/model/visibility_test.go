package model

import "testing"

func TestVisibilityDefaults(t *testing.T) {
	var v Visibility

	for _, a := range AllAreas {
		want := LevelPublic

		switch a {
		case AreaTeams:
			want = LevelLoggedIn
		case AreaCustomRaces:
			want = LevelWrite
		}

		if got := v.Level(a); got != want {
			t.Errorf("default of %s: expected %s, got %s", a, want, got)
		}
	}

	if !v.AnyPublic() {
		t.Error("the defaults have public areas")
	}

	closed := Visibility{}

	for _, a := range AllAreas {
		closed[a] = LevelLoggedIn
	}

	if closed.AnyPublic() {
		t.Error("no area is public")
	}
}

func TestVisibilityAllowed(t *testing.T) {
	const server ServerID = "srv"

	account := func(g Group) *Account {
		return &Account{Name: string(g), Groups: map[ServerID]Group{server: g}}
	}

	principals := []struct {
		name    string
		account *Account
		// allowed per level: public, login, write, admin
		allowed [4]bool
	}{
		{"nil", nil, [4]bool{true, false, false, false}},
		{"open account", &Account{Name: OpenAccountName, Groups: map[ServerID]Group{server: GroupRead}}, [4]bool{true, false, false, false}},
		{"no access", account(GroupNoAccess), [4]bool{true, false, false, false}},
		{"read", account(GroupRead), [4]bool{true, true, false, false}},
		{"write", account(GroupWrite), [4]bool{true, true, true, false}},
		{"delete", account(GroupDelete), [4]bool{true, true, true, false}},
		{"admin", account(GroupAdmin), [4]bool{true, true, true, true}},
	}

	for _, area := range AllAreas {
		for i, level := range AllLevels {
			for _, p := range principals {
				v := Visibility{area: level}

				if got := v.Allowed(area, p.account, server); got != p.allowed[i] {
					t.Errorf("area %s level %s %s: expected %t, got %t", area, level, p.name, p.allowed[i], got)
				}
			}
		}
	}
}

func TestParse(t *testing.T) {
	for _, a := range AllAreas {
		if got, ok := ParseArea(string(a)); !ok || got != a {
			t.Errorf("area %s did not round trip", a)
		}
	}

	for _, l := range AllLevels {
		if got, ok := ParseLevel(l.String()); !ok || got != l {
			t.Errorf("level %s did not round trip", l)
		}
	}

	if _, ok := ParseArea("nonsense"); ok {
		t.Error("expected an unknown area to be refused")
	}

	if _, ok := ParseLevel("nonsense"); ok {
		t.Error("expected an unknown level to be refused")
	}
}
