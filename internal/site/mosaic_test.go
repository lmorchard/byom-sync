package site

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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
	mustWrite(t, filepath.Join(hub, "needs.yaml"),
		"title: Needs\ntracks:\n  - {title: '1', artist: A, image_file: art/aa/a.jpg}\n  - {title: '2', artist: B, image_file: art/bb/b.jpg}\n")
	mustWrite(t, filepath.Join(hub, "explicit.yaml"),
		"title: Explicit\nimage: https://x/hero.jpg\ntracks:\n  - {title: '1', artist: A, image_file: art/aa/a.jpg}\n")
	mustWrite(t, filepath.Join(hub, "bare.yaml"),
		"title: Bare\ntracks:\n  - {title: '1', artist: A}\n")

	root, err := BuildTree(hub)
	if err != nil {
		t.Fatal(err)
	}
	if err := GenerateMosaics(hub, out, root); err != nil {
		t.Fatal(err)
	}
	byName := map[string]*Node{}
	for _, c := range root.Children {
		byName[c.Name] = c
	}
	// Covered playlist → predictable slug-named mosaic that exists on disk.
	if got := byName["needs"].Playlist.ImageFile; got != "art/mosaic/needs.jpg" {
		t.Errorf("mosaic ImageFile = %q, want art/mosaic/needs.jpg", got)
	}
	if _, err := os.Stat(filepath.Join(out, "art", "mosaic", "needs.jpg")); err != nil {
		t.Errorf("mosaic file not written: %v", err)
	}
	if byName["explicit"].Playlist.ImageFile != "" {
		t.Errorf("explicit-hero playlist must not get a mosaic: %q", byName["explicit"].Playlist.ImageFile)
	}
	if byName["bare"].Playlist.ImageFile != "" {
		t.Errorf("cover-less playlist must not get a mosaic: %q", byName["bare"].Playlist.ImageFile)
	}
}

func TestGenerateMosaics_SkipsWhenFresh(t *testing.T) {
	hub, out := t.TempDir(), t.TempDir()
	writePNG(t, hub, "art/aa/a.jpg")
	src := filepath.Join(hub, "m.yaml")
	mustWrite(t, src, "title: M\ntracks:\n  - {title: '1', artist: A, image_file: art/aa/a.jpg}\n")
	root, err := BuildTree(hub)
	if err != nil {
		t.Fatal(err)
	}
	if err := GenerateMosaics(hub, out, root); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(out, "art", "mosaic", "m.jpg")
	// Sentinel + force src OLDER than the mosaic → fresh → must skip.
	if err := os.WriteFile(dst, []byte("SENTINEL"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(src, past, past); err != nil {
		t.Fatal(err)
	}
	root2, _ := BuildTree(hub)
	if err := GenerateMosaics(hub, out, root2); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); string(got) != "SENTINEL" {
		t.Errorf("fresh mosaic should be skipped, but it was regenerated (%d bytes)", len(got))
	}
}

func TestGenerateMosaics_RegeneratesWhenStale(t *testing.T) {
	hub, out := t.TempDir(), t.TempDir()
	writePNG(t, hub, "art/aa/a.jpg")
	src := filepath.Join(hub, "m.yaml")
	mustWrite(t, src, "title: M\ntracks:\n  - {title: '1', artist: A, image_file: art/aa/a.jpg}\n")
	root, err := BuildTree(hub)
	if err != nil {
		t.Fatal(err)
	}
	if err := GenerateMosaics(hub, out, root); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(out, "art", "mosaic", "m.jpg")
	if err := os.WriteFile(dst, []byte("SENTINEL"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Force src NEWER than the mosaic → stale → must regenerate.
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(src, future, future); err != nil {
		t.Fatal(err)
	}
	root2, _ := BuildTree(hub)
	if err := GenerateMosaics(hub, out, root2); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); string(got) == "SENTINEL" {
		t.Error("stale mosaic (source YAML newer) should be regenerated")
	}
}
