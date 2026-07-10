package site

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFixtureHub(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "index.md"), "# mixtapes\n\nWelcome.\n")
	mustWrite(t, filepath.Join(dir, "2014-top-songs.yaml"),
		"spotify_id: abc\ntitle: 2014 Top Songs\ncreator: les\ntracks:\n  - {title: T1, artist: A1, image: 'http://img/1.jpg'}\n")
	sp := filepath.Join(dir, "synthpop")
	if err := os.MkdirAll(sp, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(sp, "README.md"), "Synthpop picks.\n")
	mustWrite(t, filepath.Join(sp, "bleep-bloop-bop.yaml"),
		"title: Bleep Bloop Bop\ncreator: les\ntracks:\n  - {title: T2, artist: A2}\n")
	return dir
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildTree(t *testing.T) {
	root, err := BuildTree(writeFixtureHub(t))
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}
	if !root.IsDir || root.Path != "" {
		t.Fatalf("root: IsDir=%v Path=%q", root.IsDir, root.Path)
	}
	if root.IntroMD == "" {
		t.Error("root IntroMD should come from index.md")
	}
	// Directories sort before playlists.
	if len(root.Children) != 2 {
		t.Fatalf("root children = %d, want 2", len(root.Children))
	}
	if root.Children[0].Name != "synthpop" || !root.Children[0].IsDir {
		t.Errorf("first child = %q (dir=%v), want synthpop dir", root.Children[0].Name, root.Children[0].IsDir)
	}
	leaf := root.Children[1]
	if leaf.Name != "2014-top-songs" || leaf.Path != "2014-top-songs" || leaf.Title != "2014 Top Songs" {
		t.Errorf("leaf = %+v", leaf)
	}
	if leaf.Playlist == nil || len(leaf.Playlist.Tracks) != 1 {
		t.Error("leaf should carry loaded playlist")
	}
	nested := root.Children[0].Children[0]
	if nested.Path != "synthpop/bleep-bloop-bop" {
		t.Errorf("nested path = %q", nested.Path)
	}
	if root.Children[0].IntroMD == "" {
		t.Error("synthpop IntroMD should come from README.md")
	}
}
