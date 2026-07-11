package site

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyArt(t *testing.T) {
	hub := t.TempDir()
	if err := os.MkdirAll(filepath.Join(hub, "art", "ab"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hub, "art", "ab", "abcd.jpg"), []byte("IMG"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := CopyArt(hub, out); err != nil {
		t.Fatalf("CopyArt: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(out, "art", "ab", "abcd.jpg"))
	if err != nil || string(got) != "IMG" {
		t.Errorf("art not copied: %v / %q", err, got)
	}
}

func TestCopyArt_NoArtDirIsNoop(t *testing.T) {
	if err := CopyArt(t.TempDir(), t.TempDir()); err != nil {
		t.Errorf("missing art dir should be a no-op, got %v", err)
	}
}
