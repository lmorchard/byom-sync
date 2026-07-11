package playlist

import (
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestPlaylist_YAMLRoundTrip(t *testing.T) {
	orig := Playlist{
		SpotifyID:    "37i9dQZF1DXcBWIGoYBM5M",
		Title:        "Test Playlist",
		Creator:      "Les",
		DateImported: time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC),
		DateCreated:  time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC),
		DateUpdated:  time.Date(2026, 7, 9, 0, 0, 0, 0, time.UTC),
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
	if !got.DateImported.Equal(orig.DateImported) {
		t.Errorf("date_imported mismatch: got %v want %v", got.DateImported, orig.DateImported)
	}
	if !got.DateUpdated.Equal(orig.DateUpdated) {
		t.Errorf("date_updated mismatch: got %v want %v", got.DateUpdated, orig.DateUpdated)
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

	// Fallback now includes album, so same artist+title on different albums
	// no longer collide.
	noISRC := Track{Title: "  Hello World ", Artist: "The Band", Album: "Debut"}
	if got := noISRC.Key(); got != "at:the band\thello world\tdebut" {
		t.Errorf("artist+title+album key: got %q", got)
	}
	noAlbum := Track{Title: "Hello World", Artist: "The Band"}
	if got := noAlbum.Key(); got != "at:the band\thello world\t" {
		t.Errorf("empty-album key: got %q", got)
	}
	live := Track{Title: "Hello World", Artist: "The Band", Album: "Live 1999"}
	if noAlbum.Key() == live.Key() {
		t.Errorf("different albums must not share a key: %q", live.Key())
	}
}

func TestTrack_ContentKey(t *testing.T) {
	// ContentKey is the normalized artist+title+album composite shared by the
	// Key() fallback and the synthesized JSPF identifier — independent of ISRC.
	a := Track{Artist: "The Band", Title: "Hello World", Album: "Debut", ISRC: "GBX1"}
	b := Track{Artist: "the band", Title: "  hello world ", Album: "DEBUT"}
	if a.ContentKey() != b.ContentKey() {
		t.Errorf("ContentKey should normalize identically: %q vs %q", a.ContentKey(), b.ContentKey())
	}
	if a.ContentKey() != "the band\thello world\tdebut" {
		t.Errorf("ContentKey: got %q", a.ContentKey())
	}
}

func TestTrack_SpotifyMarkerRoundTrip(t *testing.T) {
	no := false
	yes := true
	cases := map[string]struct {
		in      Track
		wantYML string // substring that must (or must not) appear
		present bool
	}{
		"unset omits field": {in: Track{Title: "T", Artist: "A"}, wantYML: "spotify:", present: false},
		"false serializes":  {in: Track{Title: "T", Artist: "A", Spotify: &no}, wantYML: "spotify: false", present: true},
		"true serializes":   {in: Track{Title: "T", Artist: "A", Spotify: &yes}, wantYML: "spotify: true", present: true},
	}
	for name, tc := range cases {
		data, err := yaml.Marshal(tc.in)
		if err != nil {
			t.Fatalf("%s: marshal: %v", name, err)
		}
		got := strings.Contains(string(data), tc.wantYML)
		if got != tc.present {
			t.Errorf("%s: contains(%q)=%v want %v\n%s", name, tc.wantYML, got, tc.present, data)
		}
		var back Track
		if err := yaml.Unmarshal(data, &back); err != nil {
			t.Fatalf("%s: unmarshal: %v", name, err)
		}
		if (back.Spotify == nil) != (tc.in.Spotify == nil) {
			t.Errorf("%s: nil-ness not preserved", name)
		}
		if back.Spotify != nil && *back.Spotify != *tc.in.Spotify {
			t.Errorf("%s: value not preserved: got %v", name, *back.Spotify)
		}
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
