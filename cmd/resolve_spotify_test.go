package cmd

import (
	"testing"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

func TestApplyTrackArt(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{
		{Title: "A", SpotifyID: "s1"},                // gets art
		{Title: "B", SpotifyID: "s2", Image: "keep"}, // already has image -> untouched
		{Title: "C", SpotifyID: "s3"},                // no art in map -> untouched
		{Title: "D"},                                 // no spotify_id -> untouched
	}}
	n := applyTrackArt(p, map[string]string{"s1": "art1", "s2": "art2"})
	if n != 1 {
		t.Fatalf("filled = %d, want 1", n)
	}
	if p.Tracks[0].Image != "art1" {
		t.Errorf("track A should get art1: %q", p.Tracks[0].Image)
	}
	if p.Tracks[1].Image != "keep" {
		t.Errorf("track B image must not be overwritten: %q", p.Tracks[1].Image)
	}
	if p.Tracks[2].Image != "" || p.Tracks[3].Image != "" {
		t.Errorf("tracks C/D should stay imageless")
	}
}

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
