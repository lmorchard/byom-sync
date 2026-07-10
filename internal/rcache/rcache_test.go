package rcache

import (
	"path/filepath"
	"testing"
	"time"
)

func openTemp(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func boolPtr(b bool) *bool { return &b }

func TestPutGetPositive(t *testing.T) {
	db := openTemp(t)
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	in := Entry{VideoID: "abc", Source: "yt-dlp", Embeddable: boolPtr(true), ResolvedAt: now, CheckedAt: now}
	if err := db.Put("isrc:X", in); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ok := db.Get("isrc:X")
	if !ok {
		t.Fatal("Get: want ok")
	}
	if got.VideoID != "abc" || got.Source != "yt-dlp" || got.Embeddable == nil || !*got.Embeddable {
		t.Fatalf("Get: %+v", got)
	}
	if !got.CheckedAt.Equal(now) {
		t.Fatalf("CheckedAt: got %v want %v", got.CheckedAt, now)
	}
}

func TestGetMissingIsFalse(t *testing.T) {
	db := openTemp(t)
	if _, ok := db.Get("nope"); ok {
		t.Fatal("Get: want !ok for missing key")
	}
}

func TestPutUpsertAndNullEmbeddable(t *testing.T) {
	db := openTemp(t)
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	_ = db.Put("k", Entry{VideoID: "v1", CheckedAt: now})                             // embeddable nil
	_ = db.Put("k", Entry{VideoID: "v2", Embeddable: boolPtr(false), CheckedAt: now}) // upsert
	got, _ := db.Get("k")
	if got.VideoID != "v2" {
		t.Fatalf("upsert VideoID: got %q", got.VideoID)
	}
	if got.Embeddable == nil || *got.Embeddable {
		t.Fatalf("Embeddable: want non-nil false, got %v", got.Embeddable)
	}
	// A never-set embeddable stays nil:
	_ = db.Put("k2", Entry{VideoID: "", CheckedAt: now})
	g2, _ := db.Get("k2")
	if g2.Embeddable != nil {
		t.Fatalf("Embeddable: want nil, got %v", *g2.Embeddable)
	}
}

func TestStatsAndClear(t *testing.T) {
	db := openTemp(t)
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 7, 9, 0, 0, 0, 0, time.UTC)
	_ = db.Put("pos", Entry{VideoID: "v", CheckedAt: recent})
	_ = db.Put("missOld", Entry{VideoID: "", CheckedAt: old})
	_ = db.Put("missNew", Entry{VideoID: "", CheckedAt: recent})

	cutoff := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	s, err := db.Stats(cutoff)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if s.Total != 3 || s.Positive != 1 || s.Negative != 2 || s.ExpiredNegative != 1 {
		t.Fatalf("Stats: %+v", s)
	}

	n, err := db.Clear(true) // misses only
	if err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if n != 2 {
		t.Fatalf("Clear misses: deleted %d want 2", n)
	}
	if _, ok := db.Get("pos"); !ok {
		t.Fatal("positive should survive misses-only clear")
	}
}
