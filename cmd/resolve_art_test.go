package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// artStoreRoot must anchor the cover-art store at the configured hub root
// whenever --input narrows a run to a single playlist file or a subdirectory
// of that hub — image_file values are hub-relative, so any other root writes
// a store the site can never find (see cmd/resolve.go's artStoreRoot doc
// comment). It must fall back to today's behavior (input, or its parent when
// input is a file) whenever hubDir is empty or input isn't actually under it.
func TestArtStoreRoot(t *testing.T) {
	hub := t.TempDir()
	sub := filepath.Join(hub, "00-conceptual")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	fileInSub := filepath.Join(sub, "drones.yaml")
	if err := os.WriteFile(fileInSub, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "playlist.yaml")
	if err := os.WriteFile(outsideFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A sibling directory that shares a textual prefix with the hub must not
	// be mistaken for a descendant of it (naive strings.HasPrefix would get
	// this wrong).
	hubSibling := hub + "2"
	if err := os.Mkdir(hubSibling, 0o755); err != nil {
		t.Fatal(err)
	}
	siblingFile := filepath.Join(hubSibling, "x.yaml")
	if err := os.WriteFile(siblingFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		input  string
		hubDir string
		want   string
	}{
		{
			name:   "input is the hub root",
			input:  hub,
			hubDir: hub,
			want:   hub,
		},
		{
			name:   "input is a file inside a subdirectory of the hub",
			input:  fileInSub,
			hubDir: hub,
			want:   hub,
		},
		{
			name:   "input is a subdirectory of the hub",
			input:  sub,
			hubDir: hub,
			want:   hub,
		},
		{
			name:   "input is a directory entirely outside hubDir",
			input:  outside,
			hubDir: hub,
			want:   outside,
		},
		{
			name:   "input is a file outside hubDir",
			input:  outsideFile,
			hubDir: hub,
			want:   outside,
		},
		{
			name:   "hubDir empty falls back to today's behavior (directory input)",
			input:  sub,
			hubDir: "",
			want:   sub,
		},
		{
			name:   "hubDir empty falls back to today's behavior (file input)",
			input:  fileInSub,
			hubDir: "",
			want:   sub,
		},
		{
			name:   "sibling path sharing a textual prefix with hubDir is not contained",
			input:  siblingFile,
			hubDir: hub,
			want:   hubSibling,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := artStoreRoot(tt.input, tt.hubDir)
			if got != tt.want {
				t.Errorf("artStoreRoot(%q, %q) = %q, want %q", tt.input, tt.hubDir, got, tt.want)
			}
		})
	}
}
