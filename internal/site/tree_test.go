package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFixtureHub(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "README.md"), "# mixtapes\n\nWelcome.\n")
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

func TestBuildTree_SkipsArtStore(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "mix.yaml"),
		"title: Mix\ncreator: les\ntracks:\n  - {title: T, artist: A}\n")
	// A content-addressed art store at <hub>/art (as `resolve art --download` writes)
	// must NOT be treated as a playlist folder.
	if err := os.MkdirAll(filepath.Join(dir, "art", "ab"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "art", "ab", "abcd.jpg"), "IMG")

	root, err := BuildTree(dir)
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}
	for _, c := range root.Children {
		if c.Name == "art" {
			t.Fatalf("the art store must be skipped, but a %q node was created", c.Name)
		}
	}
	if len(root.Children) != 1 || root.Children[0].Name != "mix" {
		t.Errorf("expected only the mix playlist, got %d children", len(root.Children))
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
		t.Error("root IntroMD should come from README.md")
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

func TestBuildTree_SkipsDotfiles(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "real.yaml"), "title: Real\ntracks:\n  - {title: T, artist: A}\n")
	// macOS AppleDouble sidecar: binary junk that would crash the YAML parser.
	mustWrite(t, filepath.Join(dir, "._real.yaml"), "\x00\x05\x16\x07Mac OS X junk")
	mustWrite(t, filepath.Join(dir, ".DS_Store"), "\x00\x01\x02")
	if err := os.MkdirAll(filepath.Join(dir, ".hidden"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, ".hidden", "x.yaml"), "title: Nope\n")
	root, err := BuildTree(dir)
	if err != nil {
		t.Fatalf("BuildTree should skip dotfiles, got error: %v", err)
	}
	if len(root.Children) != 1 || root.Children[0].Name != "real" {
		t.Errorf("expected only 'real', got %+v", root.Children)
	}
}

func TestBuildTreeReverseChron(t *testing.T) {
	dir := t.TempDir()
	write := func(name, updated string) {
		body := "title: " + name + "\ntracks:\n  - {title: T, artist: A}\n"
		if updated != "" {
			body = "title: " + name + "\ndate_updated: " + updated + "\ntracks:\n  - {title: T, artist: A}\n"
		}
		mustWrite(t, filepath.Join(dir, name+".yaml"), body)
	}
	write("old", "2015-03-01T00:00:00Z")
	write("newest", "2020-06-01T00:00:00Z")
	write("mid", "2018-01-01T00:00:00Z")
	write("undated", "") // no date_updated

	root, err := BuildTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, c := range root.Children {
		order = append(order, c.Name)
	}
	want := []string{"newest", "mid", "old", "undated"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v, want %v", order, want)
	}
}
