package site

import (
	"strconv"
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

// feedNode builds a playlist leaf node for body tests.
func feedNode(p *playlist.Playlist) *Node {
	return &Node{Name: "mix", Title: p.Title, Path: "mix", Playlist: p}
}

// manyTracks returns n distinct linkable tracks.
func manyTracks(n int) []playlist.Track {
	out := make([]playlist.Track, 0, n)
	for i := range n {
		out = append(out, playlist.Track{
			Artist:    "Artist" + strconv.Itoa(i),
			Title:     "Title" + strconv.Itoa(i),
			YouTubeID: "yt" + strconv.Itoa(i),
		})
	}
	return out
}

func TestItemHTMLStructure(t *testing.T) {
	site := testSite()
	site.FeedTrackLimit = 20
	body := itemHTML(feedNode(&playlist.Playlist{
		Title:       "Night Drive",
		Description: "Mostly instrumental.",
		ImageFile:   "art/cover.jpg",
		Tracks: []playlist.Track{
			{Artist: "Tycho", Title: "A Walk", YouTubeID: "walk1"},
		},
	}), site)

	for _, want := range []string{
		`<img src="https://mix.test/art/cover.jpg"`,
		`width="300"`,
		`Mostly instrumental.`,
		`1 track`,
		`<ol>`,
		`href="https://www.youtube.com/watch?v=walk1"`,
		`</ol>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\ngot: %s", want, body)
		}
	}
	// A single-page playlist has no overflow line.
	if strings.Contains(body, "more") {
		t.Errorf("unexpected overflow line: %s", body)
	}
}

func TestItemHTMLTruncatesAtLimit(t *testing.T) {
	site := testSite()
	site.FeedTrackLimit = 3
	body := itemHTML(feedNode(&playlist.Playlist{Title: "Long", Tracks: manyTracks(25)}), site)

	if got := strings.Count(body, "<li>"); got != 3 {
		t.Errorf("expected 3 track rows, got %d\n%s", got, body)
	}
	if !strings.Contains(body, "and 22 more") {
		t.Errorf("expected overflow line for 22 remaining tracks\ngot: %s", body)
	}
	// The overflow line links to the playlist's own page.
	if !strings.Contains(body, `href="https://mix.test/mix/"`) {
		t.Errorf("overflow line should link to the playlist page\ngot: %s", body)
	}
	// Track 4 and beyond must not appear.
	if strings.Contains(body, "Title3") {
		t.Errorf("body leaked a track past the limit: %s", body)
	}
}

func TestItemHTMLLimitZeroListsEverything(t *testing.T) {
	site := testSite()
	site.FeedTrackLimit = 0
	body := itemHTML(feedNode(&playlist.Playlist{Title: "All", Tracks: manyTracks(25)}), site)

	if got := strings.Count(body, "<li>"); got != 25 {
		t.Errorf("expected all 25 track rows, got %d", got)
	}
	if strings.Contains(body, "more") {
		t.Errorf("limit 0 should produce no overflow line: %s", body)
	}
}

// Spotify serves descriptions HTML-encoded. They must be decoded once, then
// escaped for output — not passed through doubly encoded.
func TestItemHTMLDecodesEncodedDescription(t *testing.T) {
	body := itemHTML(feedNode(&playlist.Playlist{
		Title:       "Encoded",
		Description: "what&#x27;s next &amp; why",
	}), testSite())

	if strings.Contains(body, "&amp;#x27;") {
		t.Errorf("description is double-encoded: %s", body)
	}
	if !strings.Contains(body, "what&#39;s next &amp; why") {
		t.Errorf("description not decoded-then-escaped as expected: %s", body)
	}
}

func TestItemHTMLOmitsAbsentPieces(t *testing.T) {
	body := itemHTML(feedNode(&playlist.Playlist{Title: "Bare"}), testSite())

	if strings.Contains(body, "<img") {
		t.Errorf("no cover should mean no img: %s", body)
	}
	if strings.Contains(body, "<ol>") {
		t.Errorf("no tracks should mean no list: %s", body)
	}
}
