package spotifyenrich

import (
	"testing"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/lmorchard/byom-sync/internal/rcache"
	"github.com/zmb3/spotify/v2"
)

func TestBuildQuery(t *testing.T) {
	got := buildQuery(playlist.Track{Title: "Nightcall", Artist: "Kavinsky", Album: "Nightcall"})
	want := `track:"Nightcall" artist:"Kavinsky" album:"Nightcall"`
	if got != want {
		t.Errorf("buildQuery with album:\n got %q\nwant %q", got, want)
	}
	got = buildQuery(playlist.Track{Title: "Nightcall", Artist: "Kavinsky"})
	want = `track:"Nightcall" artist:"Kavinsky"`
	if got != want {
		t.Errorf("buildQuery no album:\n got %q\nwant %q", got, want)
	}
}

func TestToCandidate(t *testing.T) {
	ft := spotify.FullTrack{
		SimpleTrack: spotify.SimpleTrack{
			Name:         "Nightcall",
			ID:           spotify.ID("sid"),
			Duration:     258000,
			Artists:      []spotify.SimpleArtist{{Name: "Kavinsky"}, {Name: "Lovefoxxx"}},
			ExternalURLs: map[string]string{"spotify": "https://sp/track/sid"},
		},
		Album:       spotify.SimpleAlbum{Name: "Nightcall", Images: []spotify.Image{{URL: "big", Width: 640}, {URL: "small", Width: 64}}},
		ExternalIDs: map[string]string{"isrc": "FR123"},
	}
	c := toCandidate(ft)
	if c.SpotifyID != "sid" || c.ISRC != "FR123" || c.SpotifyURL != "https://sp/track/sid" {
		t.Errorf("ids: %+v", c)
	}
	if c.Artist != "Kavinsky, Lovefoxxx" || c.Album != "Nightcall" || c.DurationMS != 258000 {
		t.Errorf("fields: %+v", c)
	}
	if c.Image != "big" {
		t.Errorf("image: got %q want largest<=640", c.Image)
	}
}

func TestPickImage(t *testing.T) {
	imgs := []spotify.Image{{URL: "xl", Width: 1000}, {URL: "l", Width: 640}, {URL: "s", Width: 64}}
	if got := pickImage(imgs, 640); got != "l" {
		t.Errorf("pickImage largest<=640: got %q", got)
	}
	// none within cap -> smallest above cap (fallback to something)
	if got := pickImage([]spotify.Image{{URL: "xl", Width: 1000}}, 640); got != "xl" {
		t.Errorf("pickImage fallback: got %q", got)
	}
	if got := pickImage(nil, 640); got != "" {
		t.Errorf("pickImage empty: got %q", got)
	}
}

func TestEntryCandidateRoundTrip(t *testing.T) {
	now := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	c := Candidate{SpotifyID: "sid", ISRC: "FR123", Title: "Nightcall", Artist: "Kavinsky", Album: "Nightcall", SpotifyURL: "url", Image: "img", DurationMS: 258000}
	e := candidateToEntry(c, now)
	if e.SpotifyID != "sid" || e.CheckedAt != now || e.DurationMS != 258000 {
		t.Errorf("candidateToEntry: %+v", e)
	}
	back := entryToCandidate(rcache.EnrichEntry(e))
	if back != c {
		t.Errorf("round trip:\n got %+v\nwant %+v", back, c)
	}
}
