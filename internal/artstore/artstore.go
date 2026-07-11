// Package artstore downloads cover-art images into a local, content-addressed
// store so playlists survive source-URL rot. Files are named by the sha256 of
// their bytes, so byte-identical covers dedup automatically.
package artstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Store writes downloaded art under Root/art/. Root is the hub directory.
type Store struct {
	Root string
	HTTP *http.Client
}

// Save fetches url, writes the bytes to art/<hh>/<sha256>.<ext> under Root
// (skipping the write if that file already exists), and returns the hub-relative
// slash path. A non-200 response is an error.
func (s Store) Save(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch %s: status %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	ext := extFor(resp.Header.Get("Content-Type"))
	rel := filepath.ToSlash(filepath.Join("art", hash[:2], hash+"."+ext))
	abs := filepath.Join(s.Root, filepath.FromSlash(rel))

	if _, err := os.Stat(abs); err == nil {
		return rel, nil // already stored (dedup) — skip the write
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(abs, data, 0o644); err != nil {
		return "", err
	}
	return rel, nil
}

// extFor maps a Content-Type to a file extension, defaulting to jpg.
func extFor(contentType string) string {
	switch {
	case strings.Contains(contentType, "png"):
		return "png"
	case strings.Contains(contentType, "webp"):
		return "webp"
	case strings.Contains(contentType, "gif"):
		return "gif"
	default:
		return "jpg"
	}
}
