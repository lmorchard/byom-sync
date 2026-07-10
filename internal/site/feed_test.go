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
		{Name: "new", Title: "New", Path: "new", Playlist: &playlist.Playlist{Title: "New", DateCreated: newer}},
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
}
