package site

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteJSPF(t *testing.T) {
	root, err := BuildTree(writeFixtureHub(t))
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := WriteJSPF(out, root); err != nil {
		t.Fatalf("WriteJSPF: %v", err)
	}
	for _, rel := range []string{
		"2014-top-songs/playlist.jspf.json",
		"synthpop/bleep-bloop-bop/playlist.jspf.json",
	} {
		data, err := os.ReadFile(filepath.Join(out, rel))
		if err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Errorf("%s not valid JSON: %v", rel, err)
		}
		if _, ok := doc["playlist"]; !ok {
			t.Errorf("%s missing playlist key", rel)
		}
	}
}
