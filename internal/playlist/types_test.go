package playlist

import (
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestPlaylist_YAMLRoundTrip(t *testing.T) {
	orig := Playlist{
		SpotifyID:   "37i9dQZF1DXcBWIGoYBM5M",
		Title:       "Test Playlist",
		Creator:     "Les",
		DateCreated: time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC),
		Tracks: []Track{
			{
				Title:      "Track One",
				Artist:     "Artist A",
				Album:      "Album X",
				ISRC:       "GBA098000010",
				DurationMS: 354000,
				SyncState:  SyncState{SpotifyPresent: true},
			},
			{
				Title:     "Orphaned Track",
				Artist:    "Artist B",
				SyncState: SyncState{SpotifyPresent: false, DateOrphaned: "2026-07-01T00:00:00Z"},
			},
		},
	}

	data, err := yaml.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Playlist
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.SpotifyID != orig.SpotifyID || got.Title != orig.Title || got.Creator != orig.Creator {
		t.Errorf("header mismatch: got %+v", got)
	}
	if !got.DateCreated.Equal(orig.DateCreated) {
		t.Errorf("date mismatch: got %v want %v", got.DateCreated, orig.DateCreated)
	}
	if len(got.Tracks) != 2 {
		t.Fatalf("track count: got %d want 2", len(got.Tracks))
	}
	if got.Tracks[0].ISRC != "GBA098000010" || got.Tracks[0].DurationMS != 354000 {
		t.Errorf("track 0 mismatch: got %+v", got.Tracks[0])
	}
	if got.Tracks[1].SyncState.DateOrphaned != "2026-07-01T00:00:00Z" {
		t.Errorf("track 1 orphan date mismatch: got %+v", got.Tracks[1].SyncState)
	}
}

func TestPlaylist_Source(t *testing.T) {
	spotify := Playlist{SpotifyID: "37i9dQZF1DXcBWIGoYBM5M", Title: "Synced"}
	if got := spotify.Source(); got != SourceSpotify {
		t.Errorf("Source() with spotify_id: got %q want %q", got, SourceSpotify)
	}
	if spotify.IsNative() {
		t.Error("IsNative() with spotify_id: got true want false")
	}

	native := Playlist{Title: "Hand Authored"}
	if got := native.Source(); got != SourceNative {
		t.Errorf("Source() without spotify_id: got %q want %q", got, SourceNative)
	}
	if !native.IsNative() {
		t.Error("IsNative() without spotify_id: got false want true")
	}
}

func TestTrack_Key(t *testing.T) {
	withISRC := Track{Title: "T", Artist: "A", ISRC: "GBX123"}
	if got := withISRC.Key(); got != "isrc:GBX123" {
		t.Errorf("ISRC key: got %q", got)
	}

	noISRC := Track{Title: "  Hello World ", Artist: "The Band"}
	if got := noISRC.Key(); got != "at:the band\thello world" {
		t.Errorf("artist+title key: got %q", got)
	}
}

func TestTrack_EnrichFieldsRoundTrip(t *testing.T) {
	orig := Track{
		Title:  "Nightcall",
		Artist: "Kavinsky",
		Image:  "https://img/cover.jpg",
		EnrichCandidates: []EnrichCandidate{
			{SpotifyID: "0lVo", Title: "Nightcall", Artist: "Kavinsky, Lovefoxxx", Album: "Nightcall", ISRC: "FR123", DurationMS: 258000, Score: 0.74},
		},
	}
	data, err := yaml.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Track
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Image != "https://img/cover.jpg" {
		t.Errorf("image: got %q", got.Image)
	}
	if len(got.EnrichCandidates) != 1 || got.EnrichCandidates[0].SpotifyID != "0lVo" || got.EnrichCandidates[0].Score != 0.74 {
		t.Errorf("candidates: got %+v", got.EnrichCandidates)
	}

	// omitempty: a plain track emits neither field.
	bare, _ := yaml.Marshal(Track{Title: "T", Artist: "A"})
	if s := string(bare); strings.Contains(s, "image:") || strings.Contains(s, "enrich_candidates:") {
		t.Errorf("bare track should omit image/enrich_candidates:\n%s", s)
	}
}
