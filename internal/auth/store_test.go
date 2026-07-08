package auth

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestTokenPath_HonorsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-test")
	got := tokenPath()
	want := filepath.Join("/tmp/xdg-test", "byom-sync", "token.json")
	if got != want {
		t.Errorf("tokenPath() = %q, want %q", got, want)
	}
}

func TestTokenStore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	tok := &oauth2.Token{
		AccessToken:  "access-123",
		RefreshToken: "refresh-456",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(time.Hour).Round(time.Second),
	}
	if err := SaveToken(tok); err != nil {
		t.Fatalf("save: %v", err)
	}

	// File written with 0o600 perms.
	info, err := os.Stat(tokenPath())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("token file perms = %o, want 600", perm)
	}

	got, err := LoadToken()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.AccessToken != tok.AccessToken || got.RefreshToken != tok.RefreshToken {
		t.Errorf("round-trip mismatch: got %+v", got)
	}
}

func TestLoadToken_ErrNoToken(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_, err := LoadToken()
	if !errors.Is(err, ErrNoToken) {
		t.Errorf("expected ErrNoToken, got %v", err)
	}
}
