package rcache

import (
	"path/filepath"
	"testing"
	"time"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestPurchaseRoundTrip(t *testing.T) {
	db := openTestDB(t)
	now := time.Now().UTC().Truncate(time.Second)
	want := PurchaseEntry{
		URL:       "https://amandapalmer.bandcamp.com/album/theatre-is-evil-2",
		Source:    "bandcamp",
		Score:     1.0,
		CheckedAt: now,
	}
	if err := db.PutPurchase("bandcamp\tamanda palmer\ttheatre is evil", want); err != nil {
		t.Fatalf("PutPurchase: %v", err)
	}
	got, ok := db.GetPurchase("bandcamp\tamanda palmer\ttheatre is evil")
	if !ok {
		t.Fatal("GetPurchase: not found")
	}
	if got.URL != want.URL || got.Source != want.Source || got.Score != want.Score {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if !got.CheckedAt.Equal(now) {
		t.Errorf("CheckedAt = %v, want %v", got.CheckedAt, now)
	}
}

func TestPurchaseNegativeEntry(t *testing.T) {
	db := openTestDB(t)
	if err := db.PutPurchase("itunes\tk", PurchaseEntry{CheckedAt: time.Now()}); err != nil {
		t.Fatalf("PutPurchase: %v", err)
	}
	got, ok := db.GetPurchase("itunes\tk")
	if !ok {
		t.Fatal("negative entry should be found")
	}
	if got.URL != "" {
		t.Errorf("URL = %q, want empty (miss marker)", got.URL)
	}
}

func TestGetPurchaseMissing(t *testing.T) {
	db := openTestDB(t)
	if _, ok := db.GetPurchase("nope"); ok {
		t.Error("expected not found")
	}
}

// Each tier owns its own key space, so clearing one leaves the others intact.
func TestClearPurchaseSource(t *testing.T) {
	db := openTestDB(t)
	old := time.Now().Add(-72 * time.Hour)
	_ = db.PutPurchase("bandcamp\ta", PurchaseEntry{URL: "u1", Source: "bandcamp", CheckedAt: old})
	_ = db.PutPurchase("itunes\ta", PurchaseEntry{URL: "u2", Source: "itunes", CheckedAt: old})

	n, err := db.ClearPurchaseSource("bandcamp")
	if err != nil {
		t.Fatalf("ClearPurchaseSource: %v", err)
	}
	if n != 1 {
		t.Errorf("deleted %d rows, want 1", n)
	}
	if _, ok := db.GetPurchase("bandcamp\ta"); ok {
		t.Error("bandcamp row should be gone")
	}
	if _, ok := db.GetPurchase("itunes\ta"); !ok {
		t.Error("itunes row should survive")
	}
}

func TestPurchaseStats(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()
	_ = db.PutPurchase("bandcamp\thit", PurchaseEntry{URL: "u", Source: "bandcamp", CheckedAt: now})
	_ = db.PutPurchase("bandcamp\tfresh", PurchaseEntry{CheckedAt: now})
	_ = db.PutPurchase("bandcamp\tstale", PurchaseEntry{CheckedAt: now.Add(-72 * time.Hour)})

	s, err := db.PurchaseStats(now.Add(-24 * time.Hour))
	if err != nil {
		t.Fatalf("PurchaseStats: %v", err)
	}
	if s.Total != 3 || s.Positive != 1 || s.Negative != 2 || s.ExpiredNegative != 1 {
		t.Errorf("got %+v, want Total=3 Positive=1 Negative=2 ExpiredNegative=1", s)
	}
}

// Clear(true) must not remove positive rows from purchase_cache.
func TestClearMissesOnlySparesPurchaseHits(t *testing.T) {
	db := openTestDB(t)
	_ = db.PutPurchase("bandcamp\thit", PurchaseEntry{URL: "u", Source: "bandcamp", CheckedAt: time.Now()})
	_ = db.PutPurchase("bandcamp\tmiss", PurchaseEntry{CheckedAt: time.Now()})

	if _, err := db.Clear(true); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, ok := db.GetPurchase("bandcamp\thit"); !ok {
		t.Error("positive row should survive Clear(missesOnly)")
	}
	if _, ok := db.GetPurchase("bandcamp\tmiss"); ok {
		t.Error("negative row should be cleared")
	}
}
