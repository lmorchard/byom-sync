// Package rcache is an optional SQLite index in front of the YouTube resolver.
// It caches resolved video ids, misses, and embeddability verdicts keyed by a
// track's merge identity (playlist.Track.Key()) so a track resolved in one
// playlist is reused everywhere and across runs. It is an accelerator, not a
// source of truth — the YAML hub remains authoritative and disposable-safe:
// deleting the DB only costs re-resolution.
package rcache

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS resolution_cache (
  key         TEXT PRIMARY KEY,
  video_id    TEXT NOT NULL,
  source      TEXT,
  embeddable  INTEGER,
  resolved_at TEXT,
  checked_at  TEXT NOT NULL
);`

// Entry is one cache row. VideoID == "" means a known miss (negative entry).
// Embeddable is tri-state: nil = unknown/unverified. ResolvedAt is zero when
// there is no positive id. CheckedAt is the last attempt/verify time and drives
// both TTLs in the resolve layer.
type Entry struct {
	VideoID    string
	Source     string
	Embeddable *bool
	ResolvedAt time.Time
	CheckedAt  time.Time
}

// DB is a handle to the cache database.
type DB struct{ db *sql.DB }

// Open opens (creating if needed) the cache DB at path, ensuring the parent
// directory and schema exist.
func Open(path string) (*DB, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &DB{db: db}, nil
}

// Close closes the underlying database.
func (d *DB) Close() error { return d.db.Close() }

// Get returns the entry for key. ok is false when there is no row (or on a read
// error — a cache miss degrades gracefully to a network resolution).
func (d *DB) Get(key string) (Entry, bool) {
	row := d.db.QueryRow(
		`SELECT video_id, source, embeddable, resolved_at, checked_at FROM resolution_cache WHERE key = ?`, key,
	)
	var (
		e        Entry
		source   sql.NullString
		emb      sql.NullInt64
		resolved sql.NullString
		checked  sql.NullString
	)
	if err := row.Scan(&e.VideoID, &source, &emb, &resolved, &checked); err != nil {
		return Entry{}, false
	}
	e.Source = source.String
	if emb.Valid {
		b := emb.Int64 != 0
		e.Embeddable = &b
	}
	if resolved.Valid {
		e.ResolvedAt, _ = time.Parse(time.RFC3339, resolved.String)
	}
	if checked.Valid {
		e.CheckedAt, _ = time.Parse(time.RFC3339, checked.String)
	}
	return e, true
}

// Put upserts an entry.
func (d *DB) Put(key string, e Entry) error {
	var emb sql.NullInt64
	if e.Embeddable != nil {
		emb.Valid = true
		if *e.Embeddable {
			emb.Int64 = 1
		}
	}
	var resolved sql.NullString
	if !e.ResolvedAt.IsZero() {
		resolved = sql.NullString{String: e.ResolvedAt.UTC().Format(time.RFC3339), Valid: true}
	}
	var source sql.NullString
	if e.Source != "" {
		source = sql.NullString{String: e.Source, Valid: true}
	}
	_, err := d.db.Exec(
		`INSERT INTO resolution_cache (key, video_id, source, embeddable, resolved_at, checked_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET
		   video_id=excluded.video_id, source=excluded.source, embeddable=excluded.embeddable,
		   resolved_at=excluded.resolved_at, checked_at=excluded.checked_at`,
		key, e.VideoID, source, emb, resolved, e.CheckedAt.UTC().Format(time.RFC3339),
	)
	return err
}

// Stats summarizes cache contents. ExpiredNegative counts negative entries whose
// checked_at is before missCutoff (i.e. would be re-attempted).
type Stats struct{ Total, Positive, Negative, ExpiredNegative int }

// Stats returns cache coverage counts. missCutoff is the boundary below which a
// negative entry is considered expired (typically now - miss TTL).
func (d *DB) Stats(missCutoff time.Time) (Stats, error) {
	var s Stats
	row := d.db.QueryRow(`
		SELECT
		  COUNT(*),
		  COALESCE(SUM(CASE WHEN video_id <> '' THEN 1 ELSE 0 END), 0),
		  COALESCE(SUM(CASE WHEN video_id  = '' THEN 1 ELSE 0 END), 0),
		  COALESCE(SUM(CASE WHEN video_id = '' AND checked_at < ? THEN 1 ELSE 0 END), 0)
		FROM resolution_cache`, missCutoff.UTC().Format(time.RFC3339))
	if err := row.Scan(&s.Total, &s.Positive, &s.Negative, &s.ExpiredNegative); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Stats{}, nil
		}
		return Stats{}, err
	}
	return s, nil
}

// Clear deletes cache rows. When missesOnly is true, only negative entries are
// removed. Returns the number of rows deleted.
func (d *DB) Clear(missesOnly bool) (int64, error) {
	q := `DELETE FROM resolution_cache`
	if missesOnly {
		q += ` WHERE video_id = ''`
	}
	res, err := d.db.Exec(q)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
