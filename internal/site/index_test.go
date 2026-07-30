package site

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteIndexJSON(t *testing.T) {
	root, err := BuildTree(writeFixtureHub(t))
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := WriteIndexJSON(out, root); err != nil {
		t.Fatalf("WriteIndexJSON: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(out, "site-index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var idx SiteIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	nodes := idx.Children
	// The fixture hub features nothing, so the key is absent entirely.
	if len(idx.Featured) != 0 {
		t.Errorf("Featured = %+v, want empty for a hub with nothing featured", idx.Featured)
	}
	if strings.Contains(string(data), `"featured"`) {
		t.Errorf("unfeatured hub should omit the featured key:\n%s", data)
	}
	if len(nodes) != 2 || !nodes[0].IsDir || nodes[0].Name != "synthpop" {
		t.Fatalf("top-level nodes = %+v", nodes)
	}
	child := nodes[0].Children[0]
	if child.Path != "/synthpop/bleep-bloop-bop/" {
		t.Errorf("nested path = %q, want /synthpop/bleep-bloop-bop/", child.Path)
	}
	if nodes[1].Path != "/2014-top-songs/" {
		t.Errorf("leaf path = %q", nodes[1].Path)
	}
	// Playlist leaves carry a summary Meta line for the sidebar; directories don't.
	if nodes[1].Meta != "1 track" {
		t.Errorf("leaf Meta = %q, want %q", nodes[1].Meta, "1 track")
	}
	if nodes[0].Meta != "" {
		t.Errorf("directory Meta = %q, want empty", nodes[0].Meta)
	}
}

func TestIndexNodeImage(t *testing.T) {
	root, err := BuildTree(writeFixtureHub(t))
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := WriteIndexJSON(out, root); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(out, "site-index.json"))
	var idx SiteIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatal(err)
	}
	nodes := idx.Children
	if nodes[1].Name != "2014-top-songs" {
		t.Fatalf("expected 2014-top-songs leaf, got %q", nodes[1].Name)
	}
	if nodes[1].Image != "http://img/1.jpg" {
		t.Errorf("leaf Image = %q, want http://img/1.jpg", nodes[1].Image)
	}
	if nodes[0].Image != "" {
		t.Errorf("directory Image = %q, want empty", nodes[0].Image)
	}
}

func TestIndexNodeYear(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.yaml"), "title: A\ndate_updated: 2019-04-01T00:00:00Z\ntracks:\n  - {title: T, artist: X}\n")
	mustWrite(t, filepath.Join(dir, "b.yaml"), "title: B\ntracks:\n  - {title: T, artist: X}\n") // undated
	root, err := BuildTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := WriteIndexJSON(out, root); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(out, "site-index.json"))
	var idx SiteIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatal(err)
	}
	nodes := idx.Children
	byName := map[string]IndexNode{}
	for _, n := range nodes {
		byName[n.Name] = n
	}
	if byName["a"].Year != 2019 {
		t.Errorf("a.Year = %d, want 2019", byName["a"].Year)
	}
	if byName["b"].Year != 0 {
		t.Errorf("undated b.Year = %d, want 0", byName["b"].Year)
	}
}

func TestWriteIndexJSON_Featured(t *testing.T) {
	dir := t.TempDir()
	// Two featured playlists — one nested, one at the root — plus one unfeatured.
	mustWrite(t, filepath.Join(dir, "newer.yaml"),
		"title: Newer\nfeatured: true\ndate_updated: 2026-02-01T00:00:00Z\ntracks:\n  - {title: T, artist: X, image: 'http://img/n.jpg'}\n")
	mustWrite(t, filepath.Join(dir, "plain.yaml"),
		"title: Plain\ndate_updated: 2026-03-01T00:00:00Z\ntracks:\n  - {title: T, artist: X}\n")
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(sub, "older.yaml"),
		"title: Older\nfeatured: true\ndate_updated: 2025-01-01T00:00:00Z\ntracks:\n  - {title: T, artist: X}\n")

	root, err := BuildTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := WriteIndexJSON(out, root); err != nil {
		t.Fatalf("WriteIndexJSON: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(out, "site-index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var idx SiteIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Flat, newest-first, absolute paths, and the nav fields the sidebar needs.
	if len(idx.Featured) != 2 {
		t.Fatalf("Featured = %+v, want 2 entries", idx.Featured)
	}
	if idx.Featured[0].Title != "Newer" || idx.Featured[1].Title != "Older" {
		t.Errorf("Featured order = %q, %q; want Newer, Older", idx.Featured[0].Title, idx.Featured[1].Title)
	}
	if idx.Featured[1].Path != "/sub/older/" {
		t.Errorf("nested featured Path = %q, want /sub/older/", idx.Featured[1].Path)
	}
	// Featured entries carry the same nav fields as tree entries (dated playlists
	// get a "1 track · Feb 2026"-style summary).
	if !strings.HasPrefix(idx.Featured[0].Meta, "1 track") || idx.Featured[0].Image != "http://img/n.jpg" {
		t.Errorf("featured entry missing nav fields: %+v", idx.Featured[0])
	}
	if idx.Featured[0].Year != 2026 {
		t.Errorf("featured Year = %d, want 2026", idx.Featured[0].Year)
	}

	// Featuring is additive: the tree still holds everything — the "sub"
	// directory plus the two root leaves.
	if len(idx.Children) != 3 {
		t.Errorf("Children = %d entries, want 3 (sub dir + 2 root leaves)", len(idx.Children))
	}
}
