package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

func TestWriteFeed(t *testing.T) {
	older := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	root := &Node{IsDir: true, Children: []*Node{
		{Name: "old", Title: "Old", Path: "old", Playlist: &playlist.Playlist{Title: "Old", DateCreated: older}},
		{Name: "new", Title: "New", Path: "new", Playlist: &playlist.Playlist{
			Title:       "New",
			DateCreated: newer,
			Tracks:      []playlist.Track{{Artist: "Tycho", Title: "A Walk", YouTubeID: "walk1"}},
		}},
	}}
	out := t.TempDir()
	if err := WriteFeed(out, testSite(), root); err != nil {
		t.Fatalf("WriteFeed: %v", err)
	}
	xml, err := os.ReadFile(filepath.Join(out, "feed.xml"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(xml)
	if !strings.Contains(s, "https://mix.test/new/") {
		t.Error("feed missing absolute item link")
	}
	// Newest first: "New" item appears before "Old".
	if strings.Index(s, "<title>New</title>") > strings.Index(s, "<title>Old</title>") {
		t.Error("feed items not newest-first")
	}
	// The tracklist reaches <content:encoded>, which is emitted as CDATA and so
	// carries raw markup.
	if !strings.Contains(s, "<![CDATA[") || !strings.Contains(s, "<ol>") {
		t.Error("feed missing raw tracklist markup in content:encoded")
	}
	// It also reaches <description>, which the XML marshaller escapes.
	if !strings.Contains(s, "&lt;ol&gt;") {
		t.Error("feed missing escaped tracklist markup in description")
	}
	if !strings.Contains(s, "youtube.com/watch?v=walk1") {
		t.Error("feed missing track link")
	}
}

func TestWriteFeedEnclosure(t *testing.T) {
	out := t.TempDir()
	rel := writeArt(t, out, "art/ab/cover.jpg", 4096)

	root := &Node{IsDir: true, Children: []*Node{
		{Name: "local", Title: "Local", Path: "local", Playlist: &playlist.Playlist{
			Title: "Local", ImageFile: rel,
		}},
		{Name: "remote", Title: "Remote", Path: "remote", Playlist: &playlist.Playlist{
			Title: "Remote", Image: "https://i.scdn.co/image/xyz",
		}},
	}}
	if err := WriteFeed(out, testSite(), root); err != nil {
		t.Fatalf("WriteFeed: %v", err)
	}
	xml, err := os.ReadFile(filepath.Join(out, "feed.xml"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(xml)

	if !strings.Contains(s, `url="https://mix.test/art/ab/cover.jpg"`) {
		t.Error("feed missing enclosure for local cover")
	}
	if !strings.Contains(s, `length="4096"`) {
		t.Error("enclosure missing byte length")
	}
	// Exactly one item has local art, so exactly one enclosure.
	if got := strings.Count(s, "<enclosure"); got != 1 {
		t.Errorf("expected 1 enclosure, got %d", got)
	}
}
