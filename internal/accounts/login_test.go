package accounts

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

func tryLogin(t *testing.T, am *AccountManager, username, password string) error {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(url.Values{"Username": {username}, "Password": {password}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = smweb.WithScope(req, &smweb.Scope{SessionStore: am.sessionStore})

	return am.login(req, httptest.NewRecorder())
}

func TestLogin_PasswordlessAccountsNeverLogIn(t *testing.T) {
	ah, _, cleanup := newTestHandler(t)
	defer cleanup()

	steam := smmodel.NewAccount(testServerID, "test")
	steam.Name = "steam-76561198000000001"
	steam.GUID = "76561198000000001"

	if err := ah.store.UpsertAccount(steam); err != nil {
		t.Fatal(err)
	}

	if !steam.IsPasswordless() || steam.NeedsPasswordReset("") {
		t.Fatal("a steam account is passwordless and must not need a password reset")
	}

	for _, password := range []string{"", " ", "steam", steam.GUID, steam.Name} {
		if err := tryLogin(t, ah.accountManager, steam.Name, password); err != ErrInvalidUsernameOrPassword {
			t.Errorf("password %q: expected ErrInvalidUsernameOrPassword, got %v", password, err)
		}
	}

	// the empty salt/hash must not match either (an existing account without any password data)
	for _, name := range []string{"read", "write"} {
		if err := tryLogin(t, ah.accountManager, name, ""); err != ErrInvalidUsernameOrPassword {
			t.Errorf("account %s: expected ErrInvalidUsernameOrPassword, got %v", name, err)
		}
	}
}

func TestLogin_PasswordAccountsStillWork(t *testing.T) {
	ah, accounts, cleanup := newTestHandler(t)
	defer cleanup()

	account := accounts[smmodel.GroupRead]

	if err := ah.accountManager.ChangePassword(account, "secret"); err != nil {
		t.Fatal(err)
	}

	if account.IsPasswordless() {
		t.Fatal("an account with a password is not passwordless")
	}

	if err := tryLogin(t, ah.accountManager, account.Name, "secret"); err != nil {
		t.Fatalf("login with the right password: %v", err)
	}

	if err := tryLogin(t, ah.accountManager, account.Name, ""); err != ErrInvalidUsernameOrPassword {
		t.Fatalf("empty password: %v", err)
	}

	// first log in with the default password still leads to the reset form
	account.PasswordHash, account.PasswordSalt, account.DefaultPassword = "", "", "one-two"

	if err := ah.store.UpsertAccount(account); err != nil {
		t.Fatal(err)
	}

	if account.IsPasswordless() {
		t.Fatal("an account with a default password is not passwordless")
	}

	if err := tryLogin(t, ah.accountManager, account.Name, "one-two"); err != ErrAccountNeedsPassword {
		t.Fatalf("expected ErrAccountNeedsPassword, got %v", err)
	}

	if err := tryLogin(t, ah.accountManager, account.Name, ""); err != ErrInvalidUsernameOrPassword {
		t.Fatalf("empty password with default password: %v", err)
	}
}

func TestNeedsPasswordReset_Passwordless(t *testing.T) {
	a := smmodel.Account{Name: "steam-1", GUID: "1"}

	if a.NeedsPasswordReset("") || a.NeedsPasswordReset("override") {
		t.Fatal("a passwordless account with a GUID must not need a password reset")
	}
}
