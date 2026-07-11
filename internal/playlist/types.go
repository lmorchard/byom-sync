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
	SpotifyID   string `yaml:"spotify_id"`
	Title       string `yaml:"title"`
	Creator     string `yaml:"creator"`
	Description string `yaml:"description,omitempty"`
	// Image is playlist-level cover art (a URL). When unset, exporters fall back
	// to the first track's image.
	Image string `yaml:"image,omitempty"`
	// DateCreated is when byom-sync first synced this playlist. Spotify's API
	// exposes no true playlist creation date, so this is "first seen" time; the
	// per-track added_at fields carry the real curation history.
	DateCreated time.Time `yaml:"date_created"`
	Tracks      []Track   `yaml:"tracks"`
}

// Source identifies where a playlist came from. It is derived from which
// source-ID field is populated, never stored as an explicit label — so adding a
// new ingestion source later (e.g. YouTube playlists) means adding one field and
// one case here, not migrating data.
type Source string

const (
	SourceSpotify Source = "spotify"
	SourceNative  Source = "native"
)

// Source returns the playlist's provenance: SourceSpotify when it carries a
// Spotify playlist ID, otherwise SourceNative (hand-authored).
func (p Playlist) Source() Source {
	if p.SpotifyID != "" {
		return SourceSpotify
	}
	return SourceNative
}

// IsNative reports whether the playlist has no upstream source (hand-authored).
func (p Playlist) IsNative() bool {
	return p.Source() == SourceNative
}

// Track is a single entry in a playlist.
type Track struct {
	Title      string `yaml:"title"`
	Artist     string `yaml:"artist"`
	Album      string `yaml:"album,omitempty"`
	ISRC       string `yaml:"isrc,omitempty"`
	SpotifyID  string `yaml:"spotify_id,omitempty"`
	SpotifyURL string `yaml:"spotify_url,omitempty"`
	DurationMS int    `yaml:"duration_ms,omitempty"`
	YouTubeID  string `yaml:"youtube_id,omitempty"`
	Image      string `yaml:"image,omitempty"`
	AddedAt    string `yaml:"added_at,omitempty"`
	// Spotify is a tri-state opt-out for enrichment. nil (field absent) or true
	// means "enrich normally"; false ("spotify: false") asserts the track has no
	// Spotify equivalent, so `resolve spotify` skips it. A pointer so an explicit
	// false is distinguishable from an unset default and serialized as such.
	Spotify   *bool     `yaml:"spotify,omitempty"`
	SyncState SyncState `yaml:"sync_state"`
	// EnrichCandidates holds the top Spotify search matches for a track the
	// enricher could not confidently resolve. To accept one, copy its SpotifyID
	// up to the track's own spotify_id and re-run `resolve spotify`; the enricher
	// then fills the remaining fields and clears this list.
	EnrichCandidates []EnrichCandidate `yaml:"enrich_candidates,omitempty"`
}

// SyncState records whether a track is still present in the remote playlist and,
// if not, when it was first observed missing.
type SyncState struct {
	SpotifyPresent bool   `yaml:"spotify_present"`
	DateOrphaned   string `yaml:"date_orphaned,omitempty"`
}

// EnrichCandidate is one Spotify search match recorded for an ambiguous track,
// with a 0..1 similarity Score. It carries enough metadata for a human to
// eyeball the choice; SpotifyID is what you copy up to accept it.
type EnrichCandidate struct {
	SpotifyID  string  `yaml:"spotify_id"`
	Title      string  `yaml:"title,omitempty"`
	Artist     string  `yaml:"artist,omitempty"`
	Album      string  `yaml:"album,omitempty"`
	ISRC       string  `yaml:"isrc,omitempty"`
	DurationMS int     `yaml:"duration_ms,omitempty"`
	Score      float64 `yaml:"score"`
}

// Key returns the merge identity for a track: its ISRC when present, otherwise
// the normalized artist+title+album composite (ContentKey). Used to match tracks
// across syncs and to key the resolution caches.
func (t Track) Key() string {
	if t.ISRC != "" {
		return "isrc:" + t.ISRC
	}
	return "at:" + t.ContentKey()
}

// ContentKey is the normalized "artist\ttitle\talbum" composite. It is the basis
// for the ISRC-less Key() fallback and for the synthesized JSPF identifier, so
// including album keeps two same-artist+title recordings on different albums
// distinct. Independent of ISRC.
func (t Track) ContentKey() string {
	return normalize(t.Artist) + "\t" + normalize(t.Title) + "\t" + normalize(t.Album)
}

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
