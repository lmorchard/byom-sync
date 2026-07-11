package site

import (
	"testing"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

func TestPlaylistMeta(t *testing.T) {
	p := &playlist.Playlist{
		DateCreated: time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC),
		Tracks:      make([]playlist.Track, 16),
	}
	for i := range p.Tracks {
		p.Tracks[i].DurationMS = 255_000 // 16 × 255s = 4080s = 68 min
	}
	if got, want := playlistMeta(p), "16 tracks · 1 hr 8 min · Jul 2026"; got != want {
		t.Errorf("playlistMeta = %q, want %q", got, want)
	}
	// Singular, no durations, no date → just the count.
	if got := playlistMeta(&playlist.Playlist{Tracks: []playlist.Track{{}}}); got != "1 track" {
		t.Errorf("minimal playlistMeta = %q, want %q", got, "1 track")
	}
}

func TestMetaHelpers(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{
		{Title: "A"}, {Title: "B", Image: "http://img/b.jpg"},
	}}
	if got := playlistImage(p); got != "http://img/b.jpg" {
		t.Errorf("playlistImage = %q", got)
	}
	if got := playlistImage(&playlist.Playlist{}); got != "" {
		t.Errorf("empty playlistImage = %q, want empty", got)
	}
	if got := firstParagraph("# Heading\n\nBody text here.\n"); got != "Heading" {
		t.Errorf("firstParagraph = %q", got)
	}
	if got := canonical("https://x.test", "a/b"); got != "https://x.test/a/b/" {
		t.Errorf("canonical = %q", got)
	}
	if got := canonical("https://x.test/", ""); got != "https://x.test/" {
		t.Errorf("root canonical = %q", got)
	}
}

func TestDateRange(t *testing.T) {
	feb23 := time.Date(2023, 2, 1, 0, 0, 0, 0, time.UTC)
	jun26 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	feb23b := time.Date(2023, 2, 15, 0, 0, 0, 0, time.UTC) // same month as feb23
	var zero time.Time
	cases := []struct {
		c, u time.Time
		want string
	}{
		{feb23, jun26, "Feb 2023 – Jun 2026"},
		{feb23, feb23b, "Feb 2023"}, // same month-year collapses
		{feb23, zero, "Feb 2023"},
		{zero, jun26, "Jun 2026"},
		{zero, zero, ""},
	}
	for _, tc := range cases {
		if got := dateRange(tc.c, tc.u); got != tc.want {
			t.Errorf("dateRange(%v,%v) = %q, want %q", tc.c, tc.u, got, tc.want)
		}
	}
}
