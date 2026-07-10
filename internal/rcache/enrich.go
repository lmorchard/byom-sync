package rcache

import (
	"database/sql"
	"time"
)

const enrichSchema = `
CREATE TABLE IF NOT EXISTS enrichment_cache (
  key         TEXT PRIMARY KEY,
  spotify_id  TEXT NOT NULL,
  isrc        TEXT,
  spotify_url TEXT,
  album       TEXT,
  title       TEXT,
  artist      TEXT,
  image       TEXT,
  duration_ms INTEGER,
  checked_at  TEXT NOT NULL
);`

// EnrichEntry is one enrichment-cache row. SpotifyID == "" means a known miss
// (negative entry). CheckedAt is the last attempt time and drives the miss TTL.
type EnrichEntry struct {
	SpotifyID  string
	ISRC       string
	SpotifyURL string
	Album      string
	Title      string
	Artist     string
	Image      string
	DurationMS int
	CheckedAt  time.Time
}

// GetEnrich returns the enrichment entry for key. ok is false when there is no
// row (or on a read error — a miss degrades gracefully to a live lookup).
func (d *DB) GetEnrich(key string) (EnrichEntry, bool) {
	row := d.db.QueryRow(
		`SELECT spotify_id, isrc, spotify_url, album, title, artist, image, duration_ms, checked_at
		   FROM enrichment_cache WHERE key = ?`, key,
	)
	var (
		e       EnrichEntry
		isrc    sql.NullString
		url     sql.NullString
		album   sql.NullString
		title   sql.NullString
		artist  sql.NullString
		image   sql.NullString
		dur     sql.NullInt64
		checked sql.NullString
	)
	if err := row.Scan(&e.SpotifyID, &isrc, &url, &album, &title, &artist, &image, &dur, &checked); err != nil {
		return EnrichEntry{}, false
	}
	e.ISRC = isrc.String
	e.SpotifyURL = url.String
	e.Album = album.String
	e.Title = title.String
	e.Artist = artist.String
	e.Image = image.String
	e.DurationMS = int(dur.Int64)
	if checked.Valid {
		e.CheckedAt, _ = time.Parse(time.RFC3339, checked.String)
	}
	return e, true
}

// PutEnrich upserts an enrichment entry.
func (d *DB) PutEnrich(key string, e EnrichEntry) error {
	_, err := d.db.Exec(
		`INSERT INTO enrichment_cache (key, spotify_id, isrc, spotify_url, album, title, artist, image, duration_ms, checked_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET
		   spotify_id=excluded.spotify_id, isrc=excluded.isrc, spotify_url=excluded.spotify_url,
		   album=excluded.album, title=excluded.title, artist=excluded.artist, image=excluded.image,
		   duration_ms=excluded.duration_ms, checked_at=excluded.checked_at`,
		key, e.SpotifyID, e.ISRC, e.SpotifyURL, e.Album, e.Title, e.Artist, e.Image, e.DurationMS,
		e.CheckedAt.UTC().Format(time.RFC3339),
	)
	return err
}

// EnrichStats reports enrichment-cache coverage. Positive = has a spotify_id;
// Negative = miss; ExpiredNegative = misses older than missCutoff.
func (d *DB) EnrichStats(missCutoff time.Time) (Stats, error) {
	rows, err := d.db.Query(`SELECT spotify_id, checked_at FROM enrichment_cache`)
	if err != nil {
		return Stats{}, err
	}
	defer func() { _ = rows.Close() }()

	var s Stats
	cutoff := missCutoff.UTC()
	for rows.Next() {
		var sid string
		var checked sql.NullString
		if err := rows.Scan(&sid, &checked); err != nil {
			return Stats{}, err
		}
		s.Total++
		if sid != "" {
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
