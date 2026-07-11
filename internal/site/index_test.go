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
