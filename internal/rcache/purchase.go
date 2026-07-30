package rcache

import (
	"database/sql"
	"strings"
	"time"
)

const purchaseSchema = `
CREATE TABLE IF NOT EXISTS purchase_cache (
  key        TEXT PRIMARY KEY,
  url        TEXT NOT NULL,
  source     TEXT,
  score      REAL,
  checked_at TEXT NOT NULL
);`

// PurchaseEntry is one purchase-cache row. URL == "" means a known miss for that
// (source, album) pair — misses expire via CheckedAt so an album that later goes
// on sale is retried. Score is the match confidence that admitted the URL, kept
// for debugging a bad threshold after the fact.
type PurchaseEntry struct {
	URL       string
	Source    string
	Score     float64
	CheckedAt time.Time
}

// GetPurchase returns the entry for key. ok is false when there is no row (or on
// a read error — a miss degrades gracefully to a live lookup).
func (d *DB) GetPurchase(key string) (PurchaseEntry, bool) {
	row := d.db.QueryRow(
		`SELECT url, source, score, checked_at FROM purchase_cache WHERE key = ?`, key,
	)
	var (
		e       PurchaseEntry
		source  sql.NullString
		score   sql.NullFloat64
		checked sql.NullString
	)
	if err := row.Scan(&e.URL, &source, &score, &checked); err != nil {
		return PurchaseEntry{}, false
	}
	e.Source = source.String
	e.Score = score.Float64
	if checked.Valid {
		e.CheckedAt, _ = time.Parse(time.RFC3339, checked.String)
	}
	return e, true
}

// PutPurchase upserts a purchase entry.
func (d *DB) PutPurchase(key string, e PurchaseEntry) error {
	var source sql.NullString
	if e.Source != "" {
		source = sql.NullString{String: e.Source, Valid: true}
	}
	_, err := d.db.Exec(
		`INSERT INTO purchase_cache (key, url, source, score, checked_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET
		   url=excluded.url, source=excluded.source, score=excluded.score,
		   checked_at=excluded.checked_at`,
		key, e.URL, source, e.Score, e.CheckedAt.UTC().Format(time.RFC3339),
	)
	return err
}

// ClearPurchaseSource deletes every row belonging to one tier, so it can be
// re-run without discarding the other tiers' work. Keys are "<source>\t<album>".
func (d *DB) ClearPurchaseSource(source string) (int64, error) {
	res, err := d.db.Exec(
		`DELETE FROM purchase_cache WHERE key LIKE ?`,
		strings.ReplaceAll(source, "%", `\%`)+"\t%",
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// PurchaseStats reports purchase-cache coverage, mirroring ArtStats.
func (d *DB) PurchaseStats(missCutoff time.Time) (Stats, error) {
	var s Stats
	row := d.db.QueryRow(`
		SELECT
		  COUNT(*),
		  COALESCE(SUM(CASE WHEN url <> '' THEN 1 ELSE 0 END), 0),
		  COALESCE(SUM(CASE WHEN url  = '' THEN 1 ELSE 0 END), 0),
		  COALESCE(SUM(CASE WHEN url  = '' AND checked_at < ? THEN 1 ELSE 0 END), 0)
		FROM purchase_cache`, missCutoff.UTC().Format(time.RFC3339))
	if err := row.Scan(&s.Total, &s.Positive, &s.Negative, &s.ExpiredNegative); err != nil {
		return Stats{}, err
	}
	return s, nil
}
