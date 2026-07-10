package rcache

import (
	"testing"
	"time"
)

func TestEnrichPutGetPositive(t *testing.T) {
	db := openTemp(t)
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	in := EnrichEntry{SpotifyID: "sid", ISRC: "FR123", SpotifyURL: "https://sp/track/sid", Album: "Nightcall", Title: "Nightcall", Artist: "Kavinsky", Image: "https://img", DurationMS: 258000, CheckedAt: now}
	if err := db.PutEnrich("at:kavinsky\tnightcall", in); err != nil {
		t.Fatalf("PutEnrich: %v", err)
	}
	got, ok := db.GetEnrich("at:kavinsky\tnightcall")
	if !ok {
		t.Fatal("GetEnrich: want ok")
	}
	if got.SpotifyID != "sid" || got.ISRC != "FR123" || got.DurationMS != 258000 || got.Image != "https://img" {
		t.Fatalf("GetEnrich: %+v", got)
	}
	if !got.CheckedAt.Equal(now) {
		t.Fatalf("CheckedAt: got %v want %v", got.CheckedAt, now)
	}
}

func TestEnrichPutZeroCheckedAt(t *testing.T) {
	db := openTemp(t)
	in := EnrichEntry{SpotifyID: ""}
	if err := db.PutEnrich("miss", in); err != nil {
		t.Fatalf("PutEnrich: %v", err)
	}
	got, ok := db.GetEnrich("miss")
	if !ok {
		t.Fatal("GetEnrich: want ok")
	}
	if !got.CheckedAt.IsZero() {
		t.Fatalf("CheckedAt: want zero, got %v", got.CheckedAt)
	}
}

func TestEnrichGetMissingIsFalse(t *testing.T) {
	db := openTemp(t)
	if _, ok := db.GetEnrich("nope"); ok {
		t.Fatal("GetEnrich: want !ok for missing key")
	}
}

func TestEnrichStatsAndClearBothTables(t *testing.T) {
	db := openTemp(t)
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	// one positive youtube entry, one enrichment positive + one fresh enrichment miss
	_ = db.Put("yt", Entry{VideoID: "v", CheckedAt: recent})
	_ = db.PutEnrich("epos", EnrichEntry{SpotifyID: "sid", CheckedAt: recent})
	_ = db.PutEnrich("emiss", EnrichEntry{SpotifyID: "", CheckedAt: recent})

	cutoff := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	es, err := db.EnrichStats(cutoff)
	if err != nil {
		t.Fatalf("EnrichStats: %v", err)
	}
	if es.Total != 2 || es.Positive != 1 || es.Negative != 1 {
		t.Fatalf("EnrichStats: %+v", es)
	}

	// Clear (all) empties both tables.
	if _, err := db.Clear(false); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, ok := db.Get("yt"); ok {
		t.Error("youtube entry should be gone after Clear(false)")
	}
	if _, ok := db.GetEnrich("epos"); ok {
		t.Error("enrichment entry should be gone after Clear(false)")
	}
	_ = old
}
