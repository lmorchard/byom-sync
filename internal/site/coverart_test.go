package site

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

func TestWriteCoverArt(t *testing.T) {
	hub := t.TempDir()
	// A real local art file that a playlist references via image_file.
	srcRel := filepath.FromSlash("art/aa/hash.jpg")
	if err := os.MkdirAll(filepath.Join(hub, "art", "aa"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hub, srcRel), []byte("JPEGDATA"), 0o644); err != nil {
		t.Fatal(err)
	}

	root := &Node{IsDir: true, Children: []*Node{
		{Playlist: &playlist.Playlist{Tracks: []playlist.Track{{ImageFile: "art/aa/hash.jpg"}}}},                                   // local → copied
		{Playlist: &playlist.Playlist{Tracks: []playlist.Track{{Image: "http://img/x.jpg"}}}},                                      // remote → skipped
		{Playlist: &playlist.Playlist{Tracks: []playlist.Track{{ImageFile: "art/zz/missing.jpg"}}}},                                // missing source → skipped
		{IsDir: true, Children: []*Node{{Playlist: &playlist.Playlist{Tracks: []playlist.Track{{ImageFile: "art/aa/hash.jpg"}}}}}}, // dup in subdir → copied once
	}}

	out := t.TempDir()
	if err := WriteCoverArt(hub, out, root); err != nil {
		t.Fatalf("WriteCoverArt: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(out, srcRel))
	if err != nil {
		t.Fatalf("copied art missing: %v", err)
	}
	if string(got) != "JPEGDATA" {
		t.Errorf("copied bytes = %q", got)
	}
	if _, err := os.Stat(filepath.Join(out, "art", "zz", "missing.jpg")); !os.IsNotExist(err) {
		t.Errorf("missing source should not be created, err=%v", err)
	}
}
