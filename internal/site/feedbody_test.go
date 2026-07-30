package site

import (
	"strings"
	"testing"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

func TestTrackLink(t *testing.T) {
	tests := []struct {
		name  string
		track playlist.Track
		want  string
	}{
		{
			name:  "youtube id wins over spotify url",
			track: playlist.Track{YouTubeID: "abc123", SpotifyURL: "https://open.spotify.com/track/xyz"},
			want:  "https://www.youtube.com/watch?v=abc123",
		},
		{
			name:  "falls back to spotify url",
			track: playlist.Track{SpotifyURL: "https://open.spotify.com/track/xyz"},
			want:  "https://open.spotify.com/track/xyz",
		},
		{
			name:  "no link when neither is present",
			track: playlist.Track{Title: "Untitled"},
			want:  "",
		},
		{
			name:  "non-https spotify url is refused",
			track: playlist.Track{SpotifyURL: "javascript:alert(1)"},
			want:  "",
		},
		{
			name:  "plain http spotify url is refused",
			track: playlist.Track{SpotifyURL: "http://open.spotify.com/track/xyz"},
			want:  "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := trackLink(tc.track); got != tc.want {
				t.Errorf("trackLink() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTrackThumb(t *testing.T) {
	tests := []struct {
		name  string
		track playlist.Track
		want  string
	}{
		{
			name:  "local image file becomes an absolute url",
			track: playlist.Track{ImageFile: "art/ab/cdef.jpg"},
			want:  "https://mix.test/art/ab/cdef.jpg",
		},
		{
			name:  "remote-only image gets no thumbnail",
			track: playlist.Track{Image: "https://i.scdn.co/image/xyz"},
			want:  "",
		},
		{
			name:  "no art at all gets no thumbnail",
			track: playlist.Track{Title: "Untitled"},
			want:  "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := trackThumb(tc.track, "https://mix.test"); got != tc.want {
				t.Errorf("trackThumb() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTrackRowLinksAndEscapes(t *testing.T) {
	row := trackRow(playlist.Track{
		Artist:    "Simon & Garfunkel",
		Title:     "<Untitled>",
		YouTubeID: "abc123",
		ImageFile: "art/ab/cdef.jpg",
	}, "https://mix.test")

	for _, want := range []string{
		`<li>`,
		`href="https://www.youtube.com/watch?v=abc123"`,
		`src="https://mix.test/art/ab/cdef.jpg"`,
		`width="48"`,
		`Simon &amp; Garfunkel`,
		`&lt;Untitled&gt;`,
	} {
		if !strings.Contains(row, want) {
			t.Errorf("row missing %q\ngot: %s", want, row)
		}
	}
	// Raw angle brackets from the title must not survive into the markup.
	if strings.Contains(row, "<Untitled>") {
		t.Errorf("row contains unescaped title: %s", row)
	}
}

func TestTrackRowWithoutLinkOrThumb(t *testing.T) {
	row := trackRow(playlist.Track{Artist: "Bibio", Title: "Lovers' Carvings"}, "https://mix.test")

	if strings.Contains(row, "<a ") {
		t.Errorf("unlinkable track should not be wrapped in an anchor: %s", row)
	}
	if strings.Contains(row, "<img") {
		t.Errorf("track without local art should have no thumbnail: %s", row)
	}
	if !strings.Contains(row, "Bibio") || !strings.Contains(row, "Carvings") {
		t.Errorf("row should still name the track: %s", row)
	}
}
