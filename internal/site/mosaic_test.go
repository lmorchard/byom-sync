package site

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

// writePNG writes a tiny solid PNG to <hub>/<rel> so Select/Render have bytes.
func writePNG(t *testing.T, hub, rel string) {
	t.Helper()
	p := filepath.Join(hub, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	// 1x1 PNG (valid, decodable).
	png := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xde, 0x00, 0x00, 0x00,
		0x0c, 0x49, 0x44, 0x41, 0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x00, 0x03, 0x00, 0x01, 0x00, 0x18, 0xdd, 0x8d, 0xb0, 0x00, 0x00,
		0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
	}
	if err := os.WriteFile(p, png, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateMosaics(t *testing.T) {
	hub, out := t.TempDir(), t.TempDir()
	writePNG(t, hub, "art/aa/a.jpg")
	writePNG(t, hub, "art/bb/b.jpg")

	needs := &playlist.Playlist{Title: "Needs", Tracks: []playlist.Track{
		{Title: "1", ImageFile: "art/aa/a.jpg"},
		{Title: "2", ImageFile: "art/bb/b.jpg"},
	}}
	explicit := &playlist.Playlist{Title: "Explicit", Image: "https://x/hero.jpg",
		Tracks: []playlist.Track{{Title: "1", ImageFile: "art/aa/a.jpg"}}}
	bare := &playlist.Playlist{Title: "Bare", Tracks: []playlist.Track{{Title: "1"}}}

	root := &Node{IsDir: true, Children: []*Node{
		{Playlist: needs}, {Playlist: explicit}, {Playlist: bare},
	}}

	if err := GenerateMosaics(hub, out, root); err != nil {
		t.Fatal(err)
	}

	// Playlist with covers → ImageFile set to an art/mosaic/*.jpg that exists.
	if needs.ImageFile == "" {
		t.Fatal("expected a mosaic ImageFile for the covered playlist")
	}
	if filepath.Dir(needs.ImageFile) != "art/mosaic" {
		t.Errorf("mosaic path = %q, want under art/mosaic/", needs.ImageFile)
	}
	if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(needs.ImageFile))); err != nil {
		t.Errorf("mosaic file not written: %v", err)
	}
	// Explicit hero untouched.
	if explicit.ImageFile != "" {
		t.Errorf("explicit-hero playlist must not get a mosaic: %q", explicit.ImageFile)
	}
	// No downloaded covers → untouched.
	if bare.ImageFile != "" {
		t.Errorf("cover-less playlist must not get a mosaic: %q", bare.ImageFile)
	}
}
