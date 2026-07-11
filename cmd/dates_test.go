package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

func TestRefreshFileDates_MigratesAndDerives(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.yaml")

	// A pre-migration file: date_created holds the import stamp, no date_imported.
	raw := `spotify_id: PID
title: Test
creator: Les
date_created: 2026-07-08T07:05:24Z
tracks:
    - title: Old
      artist: A
      added_at: "2020-01-01T00:00:00Z"
    - title: New
      artist: B
      added_at: "2025-06-15T12:00:00Z"
`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := refreshFileDates(path); err != nil {
		t.Fatalf("refreshFileDates: %v", err)
	}

	p, err := playlist.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// imported = old date_created; created/updated = min/max added_at.
	if got := p.DateImported.UTC().Format("2006-01-02T15:04:05Z"); got != "2026-07-08T07:05:24Z" {
		t.Errorf("DateImported: got %s", got)
	}
	if got := p.DateCreated.UTC().Format("2006-01-02"); got != "2020-01-01" {
		t.Errorf("DateCreated: got %s", got)
	}
	if got := p.DateUpdated.UTC().Format("2006-01-02"); got != "2025-06-15" {
		t.Errorf("DateUpdated: got %s", got)
	}
}
