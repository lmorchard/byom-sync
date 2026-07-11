package rcache

import (
	"testing"
	"time"
)

func TestArtPutGetPositive(t *testing.T) {
	db := openTemp(t)
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	in := ArtEntry{ImageURL: "https://caa/front-500.jpg", Source: "musicbrainz-release-group", CheckedAt: now}
	if err := db.PutArt("at:kavinsky\tnightcall\tnightcall", in); err != nil {
		t.Fatalf("PutArt: %v", err)
	}
	got, ok := db.GetArt("at:kavinsky\tnightcall\tnightcall")
	if !ok || got.ImageURL != in.ImageURL || got.Source != in.Source {
		t.Fatalf("GetArt: %+v ok=%v", got, ok)
	}
	if !got.CheckedAt.Equal(now) {
		t.Fatalf("CheckedAt: got %v want %v", got.CheckedAt, now)
	}
}

func TestArtGetMissingIsFalse(t *testing.T) {
	db := openTemp(t)
	if _, ok := db.GetArt("nope"); ok {
		t.Fatal("GetArt: want !ok for missing key")
	}
}

func TestArtPutZeroCheckedAt(t *testing.T) {
	db := openTemp(t)
	// A miss with a zero CheckedAt must insert cleanly (checked_at is NOT NULL).
	if err := db.PutArt("miss", ArtEntry{ImageURL: ""}); err != nil {
		t.Fatalf("PutArt zero CheckedAt: %v", err)
	}
	if _, ok := db.GetArt("miss"); !ok {
		t.Fatal("miss entry should be retrievable")
	}
}

func TestArtStatsAndClearAllTables(t *testing.T) {
	db := openTemp(t)
	recent := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	_ = db.Put("yt", Entry{VideoID: "v", CheckedAt: recent})
	_ = db.PutEnrich("e", EnrichEntry{SpotifyID: "sid", CheckedAt: recent})
	_ = db.PutArt("apos", ArtEntry{ImageURL: "u", CheckedAt: recent})
	_ = db.PutArt("amiss", ArtEntry{ImageURL: "", CheckedAt: recent})

	s, err := db.ArtStats(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ArtStats: %v", err)
	}
	if s.Total != 2 || s.Positive != 1 || s.Negative != 1 {
		t.Fatalf("ArtStats: %+v", s)
	}

	if _, err := db.Clear(false); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	for _, check := range []func() bool{
		func() bool { _, ok := db.Get("yt"); return ok },
		func() bool { _, ok := db.GetEnrich("e"); return ok },
		func() bool { _, ok := db.GetArt("apos"); return ok },
	} {
		if check() {
			t.Error("Clear(false) should empty all three tables")
		}
	}
}
