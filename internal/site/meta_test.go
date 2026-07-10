package site

import (
	"testing"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

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
