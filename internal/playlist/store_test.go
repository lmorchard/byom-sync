package playlist

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Chill Vibes":           "chill-vibes",
		"  Trailing / Spaces  ": "trailing-spaces",
		"Rock & Roll!!!":        "rock-roll",
		"90s   Throwbacks":      "90s-throwbacks",
		"":                      "playlist",
		"---":                   "playlist",
		"Café Del Mar":          "caf-del-mar",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSaveLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := Playlist{
		SpotifyID:   "PID1",
		Title:       "My Playlist",
		Creator:     "Les",
		DateCreated: time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC),
		Tracks:      []Track{{Title: "T", Artist: "A", SyncState: SyncState{SpotifyPresent: true}}},
	}

	path, err := Save(dir, p)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if filepath.Base(path) != "my-playlist.yaml" {
		t.Errorf("filename: got %q", filepath.Base(path))
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 1 || loaded[0].SpotifyID != "PID1" || loaded[0].Title != "My Playlist" {
		t.Errorf("loaded mismatch: %+v", loaded)
	}
}

func TestSave_MatchesExistingByID_PreservingFilename(t *testing.T) {
	dir := t.TempDir()
	// First save under original title.
	p := Playlist{SpotifyID: "PID1", Title: "Original Title"}
	path1, err := Save(dir, p)
	if err != nil {
		t.Fatalf("save1: %v", err)
	}

	// Re-save same SpotifyID with a changed title — must overwrite the same file,
	// NOT create a new "new-title.yaml".
	p.Title = "New Title"
	path2, err := Save(dir, p)
	if err != nil {
		t.Fatalf("save2: %v", err)
	}
	if path1 != path2 {
		t.Errorf("re-save created a new file: %q -> %q", path1, path2)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("expected 1 file, got %d: %v", len(entries), entries)
	}
}

func TestSave_CollisionAppendsIDSuffix(t *testing.T) {
	dir := t.TempDir()
	// Two different playlists with the same title → same slug → collision.
	a := Playlist{SpotifyID: "AAAAAAAAAAAA", Title: "Favorites"}
	b := Playlist{SpotifyID: "BBBBBBBBBBBB", Title: "Favorites"}

	pathA, err := Save(dir, a)
	if err != nil {
		t.Fatalf("saveA: %v", err)
	}
	pathB, err := Save(dir, b)
	if err != nil {
		t.Fatalf("saveB: %v", err)
	}

	if pathA == pathB {
		t.Fatalf("collision not resolved: both %q", pathA)
	}
	if filepath.Base(pathA) != "favorites.yaml" {
		t.Errorf("first file should be favorites.yaml, got %q", filepath.Base(pathA))
	}
	if filepath.Base(pathB) != "favorites-bbbbbb.yaml" {
		t.Errorf("second file should be favorites-bbbbbb.yaml, got %q", filepath.Base(pathB))
	}
}

func TestFindFileByID(t *testing.T) {
	dir := t.TempDir()
	if _, err := Save(dir, Playlist{SpotifyID: "PID1", Title: "One"}); err != nil {
		t.Fatal(err)
	}

	path, ok, err := FindFileByID(dir, "PID1")
	if err != nil || !ok {
		t.Fatalf("expected to find PID1: ok=%v err=%v", ok, err)
	}
	if filepath.Base(path) != "one.yaml" {
		t.Errorf("wrong path: %q", path)
	}

	_, ok, err = FindFileByID(dir, "MISSING")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ok {
		t.Errorf("should not find MISSING")
	}
}
