package playlist

import (
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
