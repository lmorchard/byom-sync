// Package spotifyenrich looks up Spotify metadata for hub tracks that lack it
// (the reverse of spotifyfetch), filling technical fields on confident matches
// and recording candidates for ambiguous ones.
package spotifyenrich

import (
	"strings"
	"unicode"

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

// norm lowercases and reduces a string to space-separated alphanumeric tokens,
// so punctuation and casing don't distort similarity.
func norm(s string) string {
	var b strings.Builder
	prevSpace := false
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
			prevSpace = false
		} else if !prevSpace {
			b.WriteRune(' ')
			prevSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

// sim is a 0..1 similarity that rewards the shorter of the two strings matching
// a contiguous run of the longer one — a "partial ratio". This keeps loosely
// authored strings scoring high against fuller catalog strings ("come together"
// vs "come together remastered 2019"; "beatles" vs "the beatles") while wrong
// matches stay low. Two empty strings are identical (1.0); one empty and one not
// is 0.0. Inputs are expected already normalized (see norm).
func sim(a, b string) float64 {
	if a == b {
		return 1.0
	}
	if a == "" || b == "" {
		return 0.0
	}
	ra, rb := []rune(a), []rune(b)
	if len(ra) > len(rb) {
		ra, rb = rb, ra // ra is the shorter string — the pattern
	}
	// Best edit-ratio of the pattern against any equal-length window of the
	// longer string. An exact substring yields 1.0.
	best := 0.0
	for i := 0; i+len(ra) <= len(rb); i++ {
		d := levenshtein(ra, rb[i:i+len(ra)])
		r := 1.0 - float64(d)/float64(len(ra))
		if r > best {
			best = r
			if best == 1.0 {
				break
			}
		}
	}
	return best
}

// levenshtein computes edit distance between two rune slices.
func levenshtein(a, b []rune) int {
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
