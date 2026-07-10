package cmd

import (
	"testing"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

func TestCountNeedingEnrich(t *testing.T) {
	no := false
	yes := true
	cand := []playlist.EnrichCandidate{{SpotifyID: "x"}}

	tracks := []playlist.Track{
		{Title: "unresolved"},                                           // needs: no id, not opted out
		{Title: "resolved", SpotifyID: "sid"},                           // skip: has id, no candidates
		{Title: "pick", SpotifyID: "sid", EnrichCandidates: cand},       // needs: pending pick
		{Title: "optedout clean", Spotify: &no},                         // skip: opted out, no candidates
		{Title: "optedout stale", Spotify: &no, EnrichCandidates: cand}, // needs: cleanup
		{Title: "explicit true", Spotify: &yes},                         // needs: not opted out, no id
	}
	got := countNeedingEnrich(playlist.Playlist{Tracks: tracks})
	if got != 4 {
		t.Errorf("countNeedingEnrich = %d, want 4 (unresolved, pick, optedout-stale, explicit-true)", got)
	}
}
