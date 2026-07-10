package rcache

import (
	"database/sql"
	"time"
)

const artSchema = `
CREATE TABLE IF NOT EXISTS art_cache (
  key        TEXT PRIMARY KEY,
  image_url  TEXT NOT NULL,
  source     TEXT,
  checked_at TEXT NOT NULL
);`

// ArtEntry is one art-cache row. ImageURL == "" means a known miss (negative
// entry). CheckedAt is the last attempt time and drives the miss TTL.
type ArtEntry struct {
	ImageURL  string
	Source    string
	CheckedAt time.Time
}

// GetArt returns the art entry for key. ok is false when there is no row (or on
// a read error — a miss degrades gracefully to a live lookup).
func (d *DB) GetArt(key string) (ArtEntry, bool) {
	row := d.db.QueryRow(
		`SELECT image_url, source, checked_at FROM art_cache WHERE key = ?`, key,
	)
	var (
		e       ArtEntry
		source  sql.NullString
		checked sql.NullString
	)
	if err := row.Scan(&e.ImageURL, &source, &checked); err != nil {
		return ArtEntry{}, false
	}
	e.Source = source.String
	if checked.Valid {
		e.CheckedAt, _ = time.Parse(time.RFC3339, checked.String)
	}
	return e, true
}

// PutArt upserts an art entry.
func (d *DB) PutArt(key string, e ArtEntry) error {
	_, err := d.db.Exec(
		`INSERT INTO art_cache (key, image_url, source, checked_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET
		   image_url=excluded.image_url, source=excluded.source, checked_at=excluded.checked_at`,
		key, e.ImageURL, e.Source, e.CheckedAt.UTC().Format(time.RFC3339),
	)
	return err
}

// ArtStats reports art-cache coverage. Positive = has an image_url; Negative =
// miss; ExpiredNegative = misses older than missCutoff.
func (d *DB) ArtStats(missCutoff time.Time) (Stats, error) {
	rows, err := d.db.Query(`SELECT image_url, checked_at FROM art_cache`)
	if err != nil {
		return Stats{}, err
	}
	defer func() { _ = rows.Close() }()

	var s Stats
	cutoff := missCutoff.UTC()
	for rows.Next() {
		var url string
		var checked sql.NullString
		if err := rows.Scan(&url, &checked); err != nil {
			return Stats{}, err
		}
		s.Total++
		if url != "" {
			s.Positive++
			continue
		}
		s.Negative++
		if checked.Valid {
			if ts, perr := time.Parse(time.RFC3339, checked.String); perr == nil && ts.Before(cutoff) {
				s.ExpiredNegative++
			}
		}
	}
	return s, rows.Err()
}
