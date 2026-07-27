package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// hubPaths must see playlists filed in subdirectories — the mixtapes hub keeps
// every playlist under playlists/<section>/, and a shallow glob silently found
// none of them.
func TestHubPaths_FindsNestedPlaylists(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "00-conceptual", "drones.yaml")
	if err := os.MkdirAll(filepath.Dir(nested), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nested, []byte("title: Drones\ntracks: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := hubPaths(dir)
	if err != nil {
		t.Fatalf("hubPaths: %v", err)
	}
	if len(got) != 1 || got[0] != nested {
		t.Errorf("got %v, want [%s]", got, nested)
	}
}
