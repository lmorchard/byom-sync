// Package spotifyenrich looks up Spotify metadata for hub tracks that lack it
// (the reverse of spotifyfetch), filling technical fields on confident matches
// and recording candidates for ambiguous ones.
package spotifyenrich

import (
	"github.com/lmorchard/byom-sync/internal/match"
	"github.com/lmorchard/byom-sync/internal/playlist"
)

// Candidate is a Spotify track match, mapped from the Search/GetTrack response.
type Candidate struct {
	SpotifyID  string
	ISRC       string
	Title      string
	Artist     string
	Album      string
	SpotifyURL string
	Image      string
	DurationMS int
}

// DefaultThreshold is the minimum Score for an auto-accepted match. Below it, a
// track is left unenriched and its candidates are recorded instead. Tunable.
const DefaultThreshold = 0.8

// Scoring weights (must sum to 1.0 for the base title+artist score). Tunable.
const (
	titleWeight  = 0.55
	artistWeight = 0.45
	albumWeight  = 0.10 // blended in only when both albums are present
)

// Score rates how well a Spotify Candidate matches an authored Track, 0..1.
// Title and artist similarity dominate; album is a tiebreaker; a large duration
// mismatch (only when the authored track carries a duration) applies a mild
// penalty. Pure and deterministic.
func Score(t playlist.Track, c Candidate) float64 {
	base := titleWeight*sim(norm(t.Title), norm(c.Title)) + artistWeight*sim(norm(t.Artist), norm(c.Artist))

	score := base
	if t.Album != "" && c.Album != "" {
		score = (1-albumWeight)*base + albumWeight*sim(norm(t.Album), norm(c.Album))
	}

	if t.DurationMS > 0 && c.DurationMS > 0 {
		diff := t.DurationMS - c.DurationMS
		if diff < 0 {
			diff = -diff
		}
		if diff > 15000 { // >15s apart: probably a different edit/version
			score *= 0.9
		}
	}
	return score
}

// Thin delegating aliases to match package implementations.
func norm(s string) string    { return match.Norm(s) }
func sim(a, b string) float64 { return match.Sim(a, b) }
