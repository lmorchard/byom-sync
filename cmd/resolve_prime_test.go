package cmd

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/lmorchard/byom-sync/internal/rcache"
)

func TestPrimeCacheSeedsAndCountsDupes(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, p playlist.Playlist) string {
		path := filepath.Join(dir, name)
		if err := playlist.SaveFile(path, p); err != nil {
			t.Fatal(err)
		}
		return path
	}
	shared := playlist.Track{Artist: "A", Title: "T", ISRC: "US1", YouTubeID: "yt1"}
	p1 := write("a.yaml", playlist.Playlist{SpotifyID: "a", Tracks: []playlist.Track{
		shared,
		{Artist: "B", Title: "U", ISRC: "US2", YouTubeID: "yt2"},
		{Artist: "C", Title: "V", ISRC: "US3"}, // no id — skipped
	}})
	p2 := write("b.yaml", playlist.Playlist{SpotifyID: "b", Tracks: []playlist.Track{shared}}) // dup

	db, err := rcache.Open(filepath.Join(dir, "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	now := time.Date(2026, 7, 9, 0, 0, 0, 0, time.UTC)
	seeded, dupes, err := primeCache([]string{p1, p2}, db, true, now)
	if err != nil {
		t.Fatal(err)
	}
	if seeded != 2 { // US1 and US2 (US3 has no id)
		t.Fatalf("seeded=%d want 2", seeded)
	}
	if dupes != 1 { // US1 seen twice
		t.Fatalf("dupes=%d want 1", dupes)
	}
	e, ok := db.Get(playlist.Track{ISRC: "US1"}.Key())
	if !ok || e.VideoID != "yt1" || e.Source != "prime" {
		t.Fatalf("US1 entry: ok=%v %+v", ok, e)
	}
	if e.Embeddable == nil || !*e.Embeddable {
		t.Fatalf("assume-embeddable: want true, got %v", e.Embeddable)
	}
}

func TestPrimeCacheNoAssumeLeavesEmbeddableNil(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.yaml")
	if err := playlist.SaveFile(path, playlist.Playlist{SpotifyID: "a", Tracks: []playlist.Track{
		{Artist: "A", Title: "T", ISRC: "US1", YouTubeID: "yt1"},
	}}); err != nil {
		t.Fatal(err)
	}
	db, err := rcache.Open(filepath.Join(dir, "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, _, err := primeCache([]string{path}, db, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	e, _ := db.Get(playlist.Track{ISRC: "US1"}.Key())
	if e.Embeddable != nil {
		t.Fatalf("no-assume: want nil embeddable, got %v", *e.Embeddable)
	}
}
