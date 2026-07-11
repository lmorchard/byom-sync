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
	if got := playlistImage(p, "https://site.example"); got != "http://img/b.jpg" {
		t.Errorf("playlistImage = %q", got)
	}
	if got := playlistImage(&playlist.Playlist{}, "https://site.example"); got != "" {
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

func TestPlaylistImage_PrefersDeployedLocalArt(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{
		{Title: "A", Image: "https://x/c.jpg", ImageFile: "art/ab/abcd.jpg"},
	}}
	if got := playlistImage(p, "https://site.example"); got != "https://site.example/art/ab/abcd.jpg" {
		t.Errorf("OG image should use base+image_file: %q", got)
	}
	// no local copy → source URL
	q := &playlist.Playlist{Tracks: []playlist.Track{{Title: "B", Image: "https://x/d.jpg"}}}
	if got := playlistImage(q, "https://site.example"); got != "https://x/d.jpg" {
		t.Errorf("no local copy → source URL: %q", got)
	}
}

func TestPlaylistImage_ExplicitHeroWins(t *testing.T) {
	// First track has a deployed local copy, but an explicit playlist hero must
	// win the og:image.
	tracks := []playlist.Track{{Title: "A", Image: "https://x/c.jpg", ImageFile: "art/ff/first.jpg"}}

	// Source hero URL, not yet downloaded → used as-is.
	p := &playlist.Playlist{Image: "https://x/hero.jpg", Tracks: tracks}
	if got := playlistImage(p, "https://site.example"); got != "https://x/hero.jpg" {
		t.Errorf("explicit hero URL should win: %q", got)
	}

	// Downloaded hero → self-hosted base+image_file.
	q := &playlist.Playlist{Image: "https://x/hero.jpg", ImageFile: "art/ee/hero.jpg", Tracks: tracks}
	if got := playlistImage(q, "https://site.example"); got != "https://site.example/art/ee/hero.jpg" {
		t.Errorf("downloaded hero should self-host: %q", got)
	}
}

func TestCoverHref(t *testing.T) {
	cases := []struct {
		name string
		p    *playlist.Playlist
		want string
	}{
		{"playlist hero file → root-relative", &playlist.Playlist{ImageFile: "art/aa/x.jpg"}, "/art/aa/x.jpg"},
		{"playlist remote image passthrough", &playlist.Playlist{Image: "http://img/pl.jpg"}, "http://img/pl.jpg"},
		{"first track local beats earlier remote", &playlist.Playlist{Tracks: []playlist.Track{{Image: "http://img/0.jpg"}, {ImageFile: "art/bb/y.jpg"}}}, "/art/bb/y.jpg"},
		{"first track remote fallback", &playlist.Playlist{Tracks: []playlist.Track{{}, {Image: "http://img/2.jpg"}}}, "http://img/2.jpg"},
		{"nothing", &playlist.Playlist{Tracks: []playlist.Track{{}}}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := coverHref(tc.p); got != tc.want {
				t.Errorf("coverHref = %q, want %q", got, tc.want)
			}
		})
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
