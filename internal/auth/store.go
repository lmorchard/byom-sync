// Package auth handles the Spotify OAuth2 (authorization-code + PKCE) flow and
// caches tokens on disk so later commands can silently refresh.
package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/oauth2"
)

// ErrNoToken is returned by LoadToken when no cached token exists.
var ErrNoToken = errors.New("no spotify token stored; run `byom-sync auth` first")

// configDir returns the byom-sync config directory: $XDG_CONFIG_HOME/byom-sync,
// falling back to ~/.config/byom-sync.
func configDir() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "byom-sync")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "byom-sync")
	}
	return filepath.Join(home, ".config", "byom-sync")
}

// tokenPath is the location of the cached OAuth token.
func tokenPath() string {
	return filepath.Join(configDir(), "token.json")
}

// SaveToken persists tok as JSON at the token path (0o600, dir 0o700).
func SaveToken(tok *oauth2.Token) error {
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tokenPath(), data, 0o600); err != nil {
		return fmt.Errorf("write token: %w", err)
	}
	return nil
}

// LoadToken reads the cached token, returning ErrNoToken if none is stored.
func LoadToken() (*oauth2.Token, error) {
	data, err := os.ReadFile(tokenPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoToken
	}
	if err != nil {
		return nil, err
	}
	var tok oauth2.Token
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}
	return &tok, nil
}
