package spotifyfetch

import (
	"testing"

	"github.com/zmb3/spotify/v2"
)

func TestParseID(t *testing.T) {
	cases := map[string]spotify.ID{
		"37i9dQZF1DXcBWIGoYBM5M":                                          "37i9dQZF1DXcBWIGoYBM5M",
		"spotify:playlist:37i9dQZF1DX0XUsuxWHRQd":                         "37i9dQZF1DX0XUsuxWHRQd",
		"https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M":        "37i9dQZF1DXcBWIGoYBM5M",
		"https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M?si=abc": "37i9dQZF1DXcBWIGoYBM5M",
	}
	for in, want := range cases {
		got, err := ParseID(in)
		if err != nil {
			t.Errorf("ParseID(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseID(%q) = %q, want %q", in, got, want)
		}
	}

	if _, err := ParseID("  "); err == nil {
		t.Error("expected error for empty reference")
	}
	if _, err := ParseID("https://open.spotify.com/album/xyz"); err == nil {
		t.Error("expected error for non-playlist URL")
	}
}

func TestConvert(t *testing.T) {
	ft := &spotify.FullTrack{}
	ft.Name = "My Song"
	ft.Artists = []spotify.SimpleArtist{{Name: "First"}, {Name: "Second"}}
	ft.Album = spotify.SimpleAlbum{Name: "The Album"}
	ft.Duration = 240000
	ft.ExternalIDs = map[string]string{"isrc": "USABC1234567"}
	ft.ID = "abc123trackid"
	ft.ExternalURLs = map[string]string{"spotify": "https://open.spotify.com/track/abc123trackid"}

	item := spotify.PlaylistItem{
		AddedAt: "2025-01-15T12:00:00Z",
		Track:   spotify.PlaylistItemTrack{Track: ft},
	}

	got := convert(item)

	if got.Title != "My Song" {
		t.Errorf("title: %q", got.Title)
	}
	if got.Artist != "First, Second" {
		t.Errorf("artists not joined: %q", got.Artist)
	}
	if got.Album != "The Album" {
		t.Errorf("album: %q", got.Album)
	}
	if got.ISRC != "USABC1234567" {
		t.Errorf("isrc: %q", got.ISRC)
	}
	if got.SpotifyID != "abc123trackid" {
		t.Errorf("spotify_id: %q", got.SpotifyID)
	}
	if got.SpotifyURL != "https://open.spotify.com/track/abc123trackid" {
		t.Errorf("spotify_url: %q", got.SpotifyURL)
	}
	if got.DurationMS != 240000 {
		t.Errorf("duration: %d", got.DurationMS)
	}
	if got.AddedAt != "2025-01-15T12:00:00Z" {
		t.Errorf("added_at: %q", got.AddedAt)
	}
	if !got.SyncState.SpotifyPresent {
		t.Errorf("should be marked present")
	}
}
