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

// Test that source names containing % and _ are escaped properly, so they match
// exactly and do not act as SQL LIKE wildcards.
func TestClearPurchaseSourceWithWildcards(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()

	// Store rows under a source with both % and _ in the name, plus a plain neighbour
	_ = db.PutPurchase("we%ird_source\ta", PurchaseEntry{URL: "u1", Source: "we%ird_source", CheckedAt: now})
	_ = db.PutPurchase("plain_neighbour\ta", PurchaseEntry{URL: "u2", Source: "plain_neighbour", CheckedAt: now})

	// Clear the source with wildcards: should delete exactly 1 row
	n, err := db.ClearPurchaseSource("we%ird_source")
	if err != nil {
		t.Fatalf("ClearPurchaseSource: %v", err)
	}
	if n != 1 {
		t.Errorf("deleted %d rows, want 1", n)
	}

	// Verify the wildcard-source row is gone
	if _, ok := db.GetPurchase("we%ird_source\ta"); ok {
		t.Error("we%ird_source row should be deleted")
	}

	// Verify the plain neighbour row survives
	if _, ok := db.GetPurchase("plain_neighbour\ta"); !ok {
		t.Error("plain_neighbour row should survive")
	}
}

// Test that a source name containing % does not act as a wildcard, matching
// other sources. Clearing a source with embedded % should not match other sources.
func TestClearPurchaseSourcePercentNotWildcard(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()

	sourceWithPercent := "a%b"

	// Store rows under "ab" and "a%b"
	_ = db.PutPurchase("ab\ta", PurchaseEntry{URL: "u1", Source: "ab", CheckedAt: now})
	_ = db.PutPurchase(sourceWithPercent+"\ta", PurchaseEntry{URL: "u2", Source: sourceWithPercent, CheckedAt: now})

	// Clear the source with %: should delete exactly 1 row (the literal "a%b", not "ab")
	n, err := db.ClearPurchaseSource(sourceWithPercent)
	if err != nil {
		t.Fatalf("ClearPurchaseSource: %v", err)
	}
	if n != 1 {
		t.Errorf("deleted %d rows, want 1", n)
	}

	// Verify "ab" survives (% was not treated as a wildcard)
	if _, ok := db.GetPurchase("ab\ta"); !ok {
		t.Error("ab row should survive")
	}

	// Verify the row with % in source is gone
	if _, ok := db.GetPurchase(sourceWithPercent + "\ta"); ok {
		t.Error("row with percent in source should be deleted")
	}
}
