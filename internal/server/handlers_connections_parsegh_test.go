package server

import (
	"testing"
)

func TestParseGHAuthStatus_Empty(t *testing.T) {
	accounts, active := parseGHAuthStatus("")
	if accounts != nil {
		t.Errorf("expected nil accounts, got %v", accounts)
	}
	if active != "" {
		t.Errorf("expected empty active, got %q", active)
	}
}

func TestParseGHAuthStatus_SingleAccount(t *testing.T) {
	output := `  ✓ Logged in to github.com account mjbonanno (keychain)`
	accounts, active := parseGHAuthStatus(output)
	if accounts == nil || len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %v", accounts)
	}
	if accounts[0] != "@mjbonanno" {
		t.Errorf("expected @mjbonanno, got %q", accounts[0])
	}
	if active != "" {
		t.Errorf("expected empty active (no active flag), got %q", active)
	}
}

func TestParseGHAuthStatus_SingleAccountActive(t *testing.T) {
	output := `  ✓ Logged in to github.com account mjbonanno (keychain)
    - Active account: true`
	accounts, active := parseGHAuthStatus(output)
	if accounts == nil || len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %v", accounts)
	}
	if accounts[0] != "@mjbonanno" {
		t.Errorf("expected @mjbonanno, got %q", accounts[0])
	}
	if active != "@mjbonanno" {
		t.Errorf("expected active=@mjbonanno, got %q", active)
	}
}

func TestParseGHAuthStatus_MultipleAccounts(t *testing.T) {
	output := `  ✓ Logged in to github.com account mjbonanno (keychain)
    - Active account: false
  ✓ Logged in to github.com account workuser (oauth_token)
    - Active account: false`
	accounts, active := parseGHAuthStatus(output)
	if accounts == nil || len(accounts) != 2 {
		t.Fatalf("expected 2 accounts, got %v", accounts)
	}
	if accounts[0] != "@mjbonanno" {
		t.Errorf("expected first account @mjbonanno, got %q", accounts[0])
	}
	if accounts[1] != "@workuser" {
		t.Errorf("expected second account @workuser, got %q", accounts[1])
	}
	if active != "" {
		t.Errorf("expected empty active, got %q", active)
	}
}

func TestParseGHAuthStatus_ActiveSecondAccount(t *testing.T) {
	output := `  ✓ Logged in to github.com account mjbonanno (keychain)
    - Active account: false
  ✓ Logged in to github.com account workuser (oauth_token)
    - Active account: true`
	accounts, active := parseGHAuthStatus(output)
	if accounts == nil || len(accounts) != 2 {
		t.Fatalf("expected 2 accounts, got %v", accounts)
	}
	if accounts[0] != "@mjbonanno" {
		t.Errorf("expected first account @mjbonanno, got %q", accounts[0])
	}
	if accounts[1] != "@workuser" {
		t.Errorf("expected second account @workuser, got %q", accounts[1])
	}
	if active != "@workuser" {
		t.Errorf("expected active=@workuser, got %q", active)
	}
}

func TestParseGHAuthStatus_StripsParen(t *testing.T) {
	output := `  ✓ Logged in to github.com account USERNAME(keychain)
    - Active account: true`
	accounts, active := parseGHAuthStatus(output)
	if accounts == nil || len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %v", accounts)
	}
	if accounts[0] != "@USERNAME" {
		t.Errorf("expected @USERNAME (parens stripped), got %q", accounts[0])
	}
	if active != "@USERNAME" {
		t.Errorf("expected active=@USERNAME, got %q", active)
	}
}

func TestParseGHAuthStatus_AtPrefix(t *testing.T) {
	output := `  ✓ Logged in to github.com account alice (oauth_token)`
	accounts, _ := parseGHAuthStatus(output)
	if accounts == nil || len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %v", accounts)
	}
	// Should have @ prefix added
	if accounts[0] != "@alice" {
		t.Errorf("expected @alice, got %q", accounts[0])
	}
}
