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

func TestSaveFileRoundTripsYouTubeID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pl.yaml")
	p := Playlist{
		SpotifyID: "PID", Title: "T",
		Tracks: []Track{{Title: "S", Artist: "A", YouTubeID: "vid123"}},
	}
	if err := SaveFile(path, p); err != nil {
		t.Fatalf("SaveFile: %v", err)
	}
	got, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if got.Tracks[0].YouTubeID != "vid123" {
		t.Errorf("youtube_id did not round-trip: %q", got.Tracks[0].YouTubeID)
	}
}

func TestSaveFileOverwritesAtomicallyLeavingNoTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pl.yaml")
	p := Playlist{SpotifyID: "PID", Title: "T", Tracks: []Track{{Title: "S", Artist: "A"}}}
	if err := SaveFile(path, p); err != nil {
		t.Fatalf("first save: %v", err)
	}
	p.Tracks[0].YouTubeID = "vid1"
	if err := SaveFile(path, p); err != nil {
		t.Fatalf("overwrite: %v", err)
	}

	got, err := LoadFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Tracks[0].YouTubeID != "vid1" {
		t.Errorf("overwrite lost: %q", got.Tracks[0].YouTubeID)
	}

	// No temp files left behind — only the target should remain.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "pl.yaml" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("expected only pl.yaml, got %v", names)
	}
}

func TestSave_DoesNotClobberNativeFileOnSlugCollision(t *testing.T) {
	dir := t.TempDir()

	// A hand-authored native playlist (no SpotifyID).
	nativePath, err := Save(dir, Playlist{
		Title:  "Late Night Drives",
		Tracks: []Track{{Title: "Nightcall", Artist: "Kavinsky"}},
	})
	if err != nil {
		t.Fatalf("save native: %v", err)
	}

	// A Spotify playlist that slugifies to the same base name, synced for the
	// first time.
	spotifyPath, err := Save(dir, Playlist{
		SpotifyID: "PID123",
		Title:     "Late Night Drives",
		Tracks:    []Track{{Title: "Something Else", Artist: "Someone", SyncState: SyncState{SpotifyPresent: true}}},
	})
	if err != nil {
		t.Fatalf("save spotify: %v", err)
	}

	if spotifyPath == nativePath {
		t.Fatalf("spotify playlist overwrote the native file at %q", nativePath)
	}

	// The native file must still be native (no SpotifyID) and unchanged.
	got, err := LoadFile(nativePath)
	if err != nil {
		t.Fatalf("reload native: %v", err)
	}
	if got.SpotifyID != "" {
		t.Errorf("native file gained a SpotifyID: %+v", got)
	}
	if !got.IsNative() {
		t.Errorf("native file is no longer native: %+v", got)
	}
	if len(got.Tracks) != 1 || got.Tracks[0].Title != "Nightcall" {
		t.Errorf("native file tracks changed: %+v", got.Tracks)
	}
}
