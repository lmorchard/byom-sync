// Package playlist defines the local YAML "hub" schema for byom-sync and the
// operations for loading, saving, and merging playlists on disk.
package playlist

import (
	"strings"
	"time"
)

// Playlist is one curated playlist, stored as a single YAML file. SpotifyID is
// the authoritative key used to match a local file to its remote counterpart on
// re-sync (the filename is cosmetic).
type Playlist struct {
	SpotifyID   string    `yaml:"spotify_id"`
	Title       string    `yaml:"title"`
	Creator     string    `yaml:"creator"`
	DateCreated time.Time `yaml:"date_created"`
	Tracks      []Track   `yaml:"tracks"`
}

// Track is a single entry in a playlist.
type Track struct {
	Title      string    `yaml:"title"`
	Artist     string    `yaml:"artist"`
	Album      string    `yaml:"album,omitempty"`
	ISRC       string    `yaml:"isrc,omitempty"`
	DurationMS int       `yaml:"duration_ms,omitempty"`
	SyncState  SyncState `yaml:"sync_state"`
}

// SyncState records whether a track is still present in the remote playlist and,
// if not, when it was first observed missing.
type SyncState struct {
	SpotifyPresent bool   `yaml:"spotify_present"`
	DateOrphaned   string `yaml:"date_orphaned,omitempty"`
}

// Key returns the merge identity for a track: its ISRC when present, otherwise a
// normalized "artist\ttitle" composite. Used to match tracks across syncs.
func (t Track) Key() string {
	if t.ISRC != "" {
		return "isrc:" + t.ISRC
	}
	return "at:" + normalize(t.Artist) + "\t" + normalize(t.Title)
}

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
