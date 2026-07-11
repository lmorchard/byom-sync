package site

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	var nodes []IndexNode
	if err := json.Unmarshal(data, &nodes); err != nil {
		t.Fatalf("unmarshal: %v", err)
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
	var nodes []IndexNode
	if err := json.Unmarshal(data, &nodes); err != nil {
		t.Fatal(err)
	}
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
	var nodes []IndexNode
	if err := json.Unmarshal(data, &nodes); err != nil {
		t.Fatal(err)
	}
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
