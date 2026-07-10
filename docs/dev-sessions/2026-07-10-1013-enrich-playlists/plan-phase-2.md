# Phase 2 — Spotify Enrichment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `resolve spotify` command that looks up Spotify metadata for hand-authored ("native") tracks and fills their technical fields — reverse of today's Spotify→hub flow — auto-accepting only confident matches and flagging ambiguous ones for the user to resolve by editing.

**Architecture:** A new `internal/spotifyenrich` package holds an `Enrich` loop that parallels `youtube.Resolve` in structure (per-track, budget/pace, event narration, incremental persist, cache short-circuit) but fills a *field set* rather than a single ID. Matching integrity is protected by a scoring gate: only a top candidate scoring above a threshold is written; below-threshold tracks get an `enrich_candidates` list written to their YAML so the user can pick one by copying its `spotify_id` up and re-running. The enrichment cache is a second table in the existing `cache.db`, owned by the `rcache` package (one connection, so the single `cache stats`/`clear` commands cover both tables). The Spotify client (from `auth.Client`) already auto-retries HTTP 429, so no bespoke rate-limit handling is needed.

**Tech Stack:** Go 1.25 · `github.com/zmb3/spotify/v2` v2.4.3 · `modernc.org/sqlite` · Cobra/Viper · `gopkg.in/yaml.v3`. No new dependencies.

## Global Constraints

- Go 1.25; no cgo (`modernc.org/sqlite` only).
- Formatting via `gofumpt` (`make format`); lint via golangci-lint v2 (`make lint`).
- **errcheck is strict** — assign intentionally-ignored returns to `_ =` (e.g. `_ = cache.PutEnrich(...)`, `_ = fmt.Fprintln(...)`).
- **zmb3/spotify v2.4.3 quirk:** `FullTrack.ExternalIDs` is a `map[string]string`; ISRC is `ft.ExternalIDs["isrc"]`, Spotify URL is `ft.ExternalURLs["spotify"]`. Mirror `internal/spotifyfetch/convert()`.
- Run `make lint && make test && make build` before claiming done; read the output.
- Commit trailer on every commit: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
- Work on branch `feat/spotify-enrich`; PR into `main` when the phase is complete. No direct pushes to `main`.
- Make the smallest reasonable change. No unrelated refactoring.
- **Field-fill policy:** enrichment fills only *empty* technical fields (`isrc`, `spotify_id`, `spotify_url`, `duration_ms`, `album`, `image`). Authored `title`/`artist`/`album` text is preserved unless `--canonicalize` is set. Never overwrite a non-empty technical field.
- **Match trust:** only a top candidate with `score >= threshold` (default 0.8) is auto-accepted. Below-threshold → write `enrich_candidates`, do not touch the track's technical fields.
- **Live Spotify behavior is manual to verify** (needs a real account + registered app). All automated tests use fakes/fixtures; `ClientSearcher` (the real Spotify wrapper) is exercised only via its pure helpers.

## File Structure

- `internal/playlist/types.go` — **modify.** Add `Track.Image`, `Track.EnrichCandidates`, and the `EnrichCandidate` struct.
- `internal/playlist/types_test.go` — **modify.** Round-trip test for the new fields (omitempty behavior).
- `internal/rcache/enrich.go` — **create.** `EnrichEntry`, `enrichment_cache` schema, `GetEnrich`/`PutEnrich`, `EnrichStats`; extend `Clear` to cover both tables.
- `internal/rcache/enrich_test.go` — **create.** Round-trip, miss, stats, clear-both.
- `internal/rcache/rcache.go` — **modify.** `Open` also creates the enrichment schema; `Clear` also clears the enrichment table.
- `internal/spotifyenrich/score.go` — **create.** `norm`, `sim` (Levenshtein ratio), `Score`.
- `internal/spotifyenrich/score_test.go` — **create.** Similarity + scoring truth table.
- `internal/spotifyenrich/search.go` — **create.** `Candidate`, `Searcher` interface, `buildQuery`, `toCandidate`, `pickImage`, `ClientSearcher`, `candidateToEntry`/`entryToCandidate`.
- `internal/spotifyenrich/search_test.go` — **create.** Query construction, image selection, FullTrack→Candidate mapping, entry round-trip.
- `internal/spotifyenrich/enrich.go` — **create.** `Enrich` loop, `Options`, `Event`, `EventKind`, `Cache` interface, `applyCandidate`.
- `internal/spotifyenrich/enrich_test.go` — **create.** Confident/ambiguous/miss/pick-by-editing/canonicalize/field-preserve/budget/cache, all with fakes.
- `cmd/resolve.go` — **modify.** Add `resolve spotify` subcommand + flags; extend `cache stats` to report enrichment coverage.
- `AGENTS.md` — **modify.** Document `resolve spotify`, the pipeline order, and enrichment.

---

### Task 1: Playlist schema — Image + enrich_candidates

**Files:**
- Modify: `internal/playlist/types.go`
- Test: `internal/playlist/types_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `Track.Image string` (yaml `image,omitempty`)
  - `Track.EnrichCandidates []EnrichCandidate` (yaml `enrich_candidates,omitempty`)
  - `type EnrichCandidate struct { SpotifyID, Title, Artist, Album, ISRC string; DurationMS int; Score float64 }`

- [ ] **Step 1: Write the failing test**

Add to `internal/playlist/types_test.go`:

```go
func TestTrack_EnrichFieldsRoundTrip(t *testing.T) {
	orig := Track{
		Title:  "Nightcall",
		Artist: "Kavinsky",
		Image:  "https://img/cover.jpg",
		EnrichCandidates: []EnrichCandidate{
			{SpotifyID: "0lVo", Title: "Nightcall", Artist: "Kavinsky, Lovefoxxx", Album: "Nightcall", ISRC: "FR123", DurationMS: 258000, Score: 0.74},
		},
	}
	data, err := yaml.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Track
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Image != "https://img/cover.jpg" {
		t.Errorf("image: got %q", got.Image)
	}
	if len(got.EnrichCandidates) != 1 || got.EnrichCandidates[0].SpotifyID != "0lVo" || got.EnrichCandidates[0].Score != 0.74 {
		t.Errorf("candidates: got %+v", got.EnrichCandidates)
	}

	// omitempty: a plain track emits neither field.
	bare, _ := yaml.Marshal(Track{Title: "T", Artist: "A"})
	if s := string(bare); strings.Contains(s, "image:") || strings.Contains(s, "enrich_candidates:") {
		t.Errorf("bare track should omit image/enrich_candidates:\n%s", s)
	}
}
```

Add `"strings"` to the test file's imports if not already present.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/playlist/ -run TestTrack_EnrichFieldsRoundTrip -v`
Expected: FAIL — `unknown field` / `Track has no field Image` (build error).

- [ ] **Step 3: Write minimal implementation**

In `internal/playlist/types.go`, add two fields to the `Track` struct (after `YouTubeID`, keeping the existing fields):

```go
	YouTubeID  string    `yaml:"youtube_id,omitempty"`
	Image      string    `yaml:"image,omitempty"`
	AddedAt    string    `yaml:"added_at,omitempty"`
	SyncState  SyncState `yaml:"sync_state"`
	// EnrichCandidates holds the top Spotify search matches for a track the
	// enricher could not confidently resolve. To accept one, copy its SpotifyID
	// up to the track's own spotify_id and re-run `resolve spotify`; the enricher
	// then fills the remaining fields and clears this list.
	EnrichCandidates []EnrichCandidate `yaml:"enrich_candidates,omitempty"`
```

And add the `EnrichCandidate` type after the `Track` struct:

```go
// EnrichCandidate is one Spotify search match recorded for an ambiguous track,
// with a 0..1 similarity Score. It carries enough metadata for a human to
// eyeball the choice; SpotifyID is what you copy up to accept it.
type EnrichCandidate struct {
	SpotifyID  string  `yaml:"spotify_id"`
	Title      string  `yaml:"title,omitempty"`
	Artist     string  `yaml:"artist,omitempty"`
	Album      string  `yaml:"album,omitempty"`
	ISRC       string  `yaml:"isrc,omitempty"`
	DurationMS int     `yaml:"duration_ms,omitempty"`
	Score      float64 `yaml:"score"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/playlist/ -run TestTrack_EnrichFieldsRoundTrip -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/playlist/types.go internal/playlist/types_test.go
git commit -m "feat(playlist): add Track.Image and enrich_candidates schema

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: rcache enrichment table

**Files:**
- Create: `internal/rcache/enrich.go`
- Modify: `internal/rcache/rcache.go` (Open creates enrichment schema; Clear clears both tables)
- Test: `internal/rcache/enrich_test.go`

**Interfaces:**
- Consumes: the existing `*DB` handle and its `*sql.DB` (`d.db`).
- Produces:
  - `type EnrichEntry struct { SpotifyID, ISRC, SpotifyURL, Album, Title, Artist, Image string; DurationMS int; CheckedAt time.Time }` — `SpotifyID == ""` means a known miss (negative entry).
  - `func (d *DB) GetEnrich(key string) (EnrichEntry, bool)`
  - `func (d *DB) PutEnrich(key string, e EnrichEntry) error`
  - `func (d *DB) EnrichStats(missCutoff time.Time) (Stats, error)` (reuses the existing `Stats` struct shape).
  - `Clear(missesOnly bool)` now also deletes from `enrichment_cache`.

**Context:** The existing `Stats` struct is `{ Total, Positive, Negative, ExpiredNegative int }` (defined in `rcache.go`). Reuse it for `EnrichStats`. The enrichment schema is created in `Open` alongside the existing `resolution_cache` schema.

- [ ] **Step 1: Write the failing test**

Create `internal/rcache/enrich_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/rcache/ -run TestEnrich -v`
Expected: FAIL — `undefined: EnrichEntry` / `db.PutEnrich undefined` (build error).

- [ ] **Step 3: Write the enrichment schema + methods**

Create `internal/rcache/enrich.go`:

```go
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
	var checked sql.NullString
	if !e.CheckedAt.IsZero() {
		checked = sql.NullString{String: e.CheckedAt.UTC().Format(time.RFC3339), Valid: true}
	}
	_, err := d.db.Exec(
		`INSERT INTO enrichment_cache (key, spotify_id, isrc, spotify_url, album, title, artist, image, duration_ms, checked_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET
		   spotify_id=excluded.spotify_id, isrc=excluded.isrc, spotify_url=excluded.spotify_url,
		   album=excluded.album, title=excluded.title, artist=excluded.artist, image=excluded.image,
		   duration_ms=excluded.duration_ms, checked_at=excluded.checked_at`,
		key, e.SpotifyID, e.ISRC, e.SpotifyURL, e.Album, e.Title, e.Artist, e.Image, e.DurationMS, checked,
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
```

- [ ] **Step 4: Wire the schema and Clear in rcache.go**

In `internal/rcache/rcache.go`, in `Open`, after the existing `db.Exec(schema)` block, also create the enrichment schema:

```go
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(enrichSchema); err != nil {
		_ = db.Close()
		return nil, err
	}
```

Then find the `Clear` method. It currently deletes from `resolution_cache` and returns the row count. Change it to also delete from `enrichment_cache`, summing both counts. Locate the existing body (it builds a `WHERE` clause for `missesOnly`) and apply the same delete to `enrichment_cache`. The miss predicate differs per table: `resolution_cache` uses `video_id = ''`, `enrichment_cache` uses `spotify_id = ''`. Concretely, replace the `Clear` body with:

```go
// Clear deletes cache entries across both the resolution and enrichment tables.
// With missesOnly, only negative entries (empty id) are removed. Returns the
// total number of rows deleted.
func (d *DB) Clear(missesOnly bool) (int64, error) {
	var total int64
	del := func(table, idCol string) (int64, error) {
		q := "DELETE FROM " + table
		if missesOnly {
			q += " WHERE " + idCol + " = ''"
		}
		res, err := d.db.Exec(q)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	}
	n1, err := del("resolution_cache", "video_id")
	if err != nil {
		return total, err
	}
	total += n1
	n2, err := del("enrichment_cache", "spotify_id")
	if err != nil {
		return total, err
	}
	total += n2
	return total, nil
}
```

Note: if the existing `Clear` signature differs (e.g. returns `(int, error)`), keep the existing signature and adapt — check the current definition in `rcache.go` before editing and preserve its return type. The `cmd` caller and `rcache_test.go` expectations must still compile; run the existing rcache tests after this change.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/rcache/ -v`
Expected: PASS — the new `TestEnrich*` tests and all pre-existing rcache tests (including the existing `TestStatsAndClear`, which after this change sees only the resolution table populated, so its counts are unchanged).

If `TestStatsAndClear` now fails because `Clear` returns a different total, that is a real signal the existing test encoded single-table behavior — STOP and report it as DONE_WITH_CONCERNS describing the conflict; do not weaken the existing assertion.

- [ ] **Step 6: Commit**

```bash
git add internal/rcache/enrich.go internal/rcache/enrich_test.go internal/rcache/rcache.go
git commit -m "feat(rcache): add enrichment cache table alongside resolution cache

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Enrichment scoring

**Files:**
- Create: `internal/spotifyenrich/score.go`
- Test: `internal/spotifyenrich/score_test.go`

**Interfaces:**
- Consumes: `playlist.Track`, and the `Candidate` type — which is DEFINED IN TASK 4. To avoid an ordering dependency, Task 3 defines `Score` against the fields it needs via a small local interface is NOT used; instead **Task 3 defines the `Candidate` struct** (moved here) and Task 4 consumes it. Correction to file layout: put the `Candidate` struct in `score.go` so scoring compiles standalone; Task 4's `search.go` uses it.
- Produces:
  - `type Candidate struct { SpotifyID, ISRC, Title, Artist, Album, SpotifyURL, Image string; DurationMS int }`
  - `func Score(t playlist.Track, c Candidate) float64` — 0..1.
  - `DefaultThreshold = 0.8` (exported const).

**Context:** Scoring must be a pure, deterministic function so it can be unit-tested without any network. Weights and threshold are the feel-tuning surface and must live as named constants in one place.

- [ ] **Step 1: Write the failing test**

Create `internal/spotifyenrich/score_test.go`:

```go
package spotifyenrich

import (
	"testing"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

func TestScore_ExactMatchIsHigh(t *testing.T) {
	tr := playlist.Track{Title: "Nightcall", Artist: "Kavinsky", Album: "Nightcall"}
	c := Candidate{Title: "Nightcall", Artist: "Kavinsky", Album: "Nightcall"}
	if s := Score(tr, c); s < 0.99 {
		t.Errorf("exact match should score ~1.0, got %v", s)
	}
}

func TestScore_MinorVariationClearsThreshold(t *testing.T) {
	// authored loosely; Spotify has fuller strings
	tr := playlist.Track{Title: "Come Together", Artist: "Beatles"}
	c := Candidate{Title: "Come Together - Remastered 2019", Artist: "The Beatles", Album: "Abbey Road"}
	if s := Score(tr, c); s < 0.6 {
		t.Errorf("close match scored too low: %v", s)
	}
}

func TestScore_WrongTrackIsLow(t *testing.T) {
	tr := playlist.Track{Title: "Nightcall", Artist: "Kavinsky"}
	c := Candidate{Title: "Bohemian Rhapsody", Artist: "Queen"}
	if s := Score(tr, c); s >= DefaultThreshold {
		t.Errorf("wrong track should be below threshold, got %v", s)
	}
}

func TestSim(t *testing.T) {
	if got := sim("nightcall", "nightcall"); got != 1.0 {
		t.Errorf("identical sim: got %v want 1.0", got)
	}
	if got := sim("", ""); got != 1.0 {
		t.Errorf("empty/empty sim: got %v want 1.0", got)
	}
	if got := sim("abc", ""); got != 0.0 {
		t.Errorf("something vs empty: got %v want 0.0", got)
	}
	if got := sim("kitten", "sitting"); got < 0.5 || got > 0.6 {
		t.Errorf("kitten/sitting ratio out of expected band: got %v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/spotifyenrich/ -run 'TestScore|TestSim' -v`
Expected: FAIL — package/functions undefined (build error).

- [ ] **Step 3: Write the implementation**

Create `internal/spotifyenrich/score.go`:

```go
// Package spotifyenrich looks up Spotify metadata for hub tracks that lack it
// (the reverse of spotifyfetch), filling technical fields on confident matches
// and recording candidates for ambiguous ones.
package spotifyenrich

import (
	"strings"
	"unicode"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

// Candidate is a Spotify track match, mapped from the Search/GetTrack response.
type Candidate struct {
	SpotifyID  string
	ISRC       string
	Title      string
	Artist     string
	Album      string
	SpotifyURL string
	Image      string
	DurationMS int
}

// DefaultThreshold is the minimum Score for an auto-accepted match. Below it, a
// track is left unenriched and its candidates are recorded instead. Tunable.
const DefaultThreshold = 0.8

// Scoring weights (must sum to 1.0 for the base title+artist score). Tunable.
const (
	titleWeight  = 0.55
	artistWeight = 0.45
	albumWeight  = 0.10 // blended in only when both albums are present
)

// Score rates how well a Spotify Candidate matches an authored Track, 0..1.
// Title and artist similarity dominate; album is a tiebreaker; a large duration
// mismatch (only when the authored track carries a duration) applies a mild
// penalty. Pure and deterministic.
func Score(t playlist.Track, c Candidate) float64 {
	base := titleWeight*sim(norm(t.Title), norm(c.Title)) + artistWeight*sim(norm(t.Artist), norm(c.Artist))

	score := base
	if t.Album != "" && c.Album != "" {
		score = (1-albumWeight)*base + albumWeight*sim(norm(t.Album), norm(c.Album))
	}

	if t.DurationMS > 0 && c.DurationMS > 0 {
		diff := t.DurationMS - c.DurationMS
		if diff < 0 {
			diff = -diff
		}
		if diff > 15000 { // >15s apart: probably a different edit/version
			score *= 0.9
		}
	}
	return score
}

// norm lowercases and reduces a string to space-separated alphanumeric tokens,
// so punctuation and casing don't distort similarity.
func norm(s string) string {
	var b strings.Builder
	prevSpace := false
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
			prevSpace = false
		} else if !prevSpace {
			b.WriteRune(' ')
			prevSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

// sim is a 0..1 similarity ratio based on Levenshtein edit distance:
// 1 - distance/maxLen. Two empty strings are identical (1.0); one empty and one
// not is 0.0.
func sim(a, b string) float64 {
	if a == b {
		return 1.0
	}
	if a == "" || b == "" {
		return 0.0
	}
	d := levenshtein(a, b)
	maxLen := len([]rune(a))
	if l := len([]rune(b)); l > maxLen {
		maxLen = l
	}
	return 1.0 - float64(d)/float64(maxLen)
}

// levenshtein computes edit distance between two strings (rune-aware).
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	curr := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		curr[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(rb)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/spotifyenrich/ -run 'TestScore|TestSim' -v`
Expected: PASS. (If `TestScore_MinorVariationClearsThreshold` is borderline, do NOT change the test threshold to fit; the 0.6 bar is deliberately lenient. If it genuinely fails, report the computed score as DONE_WITH_CONCERNS so the weights can be reviewed.)

- [ ] **Step 5: Commit**

```bash
git add internal/spotifyenrich/score.go internal/spotifyenrich/score_test.go
git commit -m "feat(spotifyenrich): candidate scoring against authored tracks

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Spotify search wrapper + candidate mapping

**Files:**
- Create: `internal/spotifyenrich/search.go`
- Test: `internal/spotifyenrich/search_test.go`

**Interfaces:**
- Consumes: `Candidate` (Task 3), `playlist.Track`, `rcache.EnrichEntry` (Task 2), `github.com/zmb3/spotify/v2`.
- Produces:
  - `type Searcher interface { Search(ctx, t playlist.Track) ([]Candidate, error); GetByID(ctx, id string) (Candidate, error) }`
  - `type ClientSearcher struct { Client *spotify.Client; Max int }` implementing `Searcher`.
  - `func buildQuery(t playlist.Track) string`
  - `func toCandidate(ft spotify.FullTrack) Candidate`
  - `func pickImage(images []spotify.Image, maxWidth int) string`
  - `func candidateToEntry(c Candidate, now time.Time) rcache.EnrichEntry`
  - `func entryToCandidate(e rcache.EnrichEntry) Candidate`

**Context:** `Client.Search(ctx, query, spotify.SearchTypeTrack, spotify.Limit(n))` returns `*spotify.SearchResult` whose `.Tracks.Tracks` is `[]spotify.FullTrack`. `Client.GetTrack(ctx, spotify.ID(id))` returns `*spotify.FullTrack`. Map fields exactly as `internal/spotifyfetch/convert()` does: ISRC via `ft.ExternalIDs["isrc"]`, URL via `ft.ExternalURLs["spotify"]`, artist via joined `ft.Artists[].Name`, album via `ft.Album.Name`, duration via `int(ft.Duration)`, album art via `ft.Album.Images`. `ClientSearcher.Search`/`GetByID` require a live Spotify client and are verified manually; the pure helpers (`buildQuery`, `toCandidate`, `pickImage`, entry mapping) are unit-tested.

- [ ] **Step 1: Write the failing test**

Create `internal/spotifyenrich/search_test.go`:

```go
package spotifyenrich

import (
	"testing"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/lmorchard/byom-sync/internal/rcache"
	"github.com/zmb3/spotify/v2"
)

func TestBuildQuery(t *testing.T) {
	got := buildQuery(playlist.Track{Title: "Nightcall", Artist: "Kavinsky", Album: "Nightcall"})
	want := `track:"Nightcall" artist:"Kavinsky" album:"Nightcall"`
	if got != want {
		t.Errorf("buildQuery with album:\n got %q\nwant %q", got, want)
	}
	got = buildQuery(playlist.Track{Title: "Nightcall", Artist: "Kavinsky"})
	want = `track:"Nightcall" artist:"Kavinsky"`
	if got != want {
		t.Errorf("buildQuery no album:\n got %q\nwant %q", got, want)
	}
}

func TestToCandidate(t *testing.T) {
	ft := spotify.FullTrack{
		SimpleTrack: spotify.SimpleTrack{
			Name:     "Nightcall",
			ID:       spotify.ID("sid"),
			Duration: 258000,
			Artists:  []spotify.SimpleArtist{{Name: "Kavinsky"}, {Name: "Lovefoxxx"}},
		},
		Album:        spotify.SimpleAlbum{Name: "Nightcall", Images: []spotify.Image{{URL: "big", Width: 640}, {URL: "small", Width: 64}}},
		ExternalIDs:  map[string]string{"isrc": "FR123"},
		ExternalURLs: map[string]string{"spotify": "https://sp/track/sid"},
	}
	c := toCandidate(ft)
	if c.SpotifyID != "sid" || c.ISRC != "FR123" || c.SpotifyURL != "https://sp/track/sid" {
		t.Errorf("ids: %+v", c)
	}
	if c.Artist != "Kavinsky, Lovefoxxx" || c.Album != "Nightcall" || c.DurationMS != 258000 {
		t.Errorf("fields: %+v", c)
	}
	if c.Image != "big" {
		t.Errorf("image: got %q want largest<=640", c.Image)
	}
}

func TestPickImage(t *testing.T) {
	imgs := []spotify.Image{{URL: "xl", Width: 1000}, {URL: "l", Width: 640}, {URL: "s", Width: 64}}
	if got := pickImage(imgs, 640); got != "l" {
		t.Errorf("pickImage largest<=640: got %q", got)
	}
	// none within cap -> smallest above cap (fallback to something)
	if got := pickImage([]spotify.Image{{URL: "xl", Width: 1000}}, 640); got != "xl" {
		t.Errorf("pickImage fallback: got %q", got)
	}
	if got := pickImage(nil, 640); got != "" {
		t.Errorf("pickImage empty: got %q", got)
	}
}

func TestEntryCandidateRoundTrip(t *testing.T) {
	now := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	c := Candidate{SpotifyID: "sid", ISRC: "FR123", Title: "Nightcall", Artist: "Kavinsky", Album: "Nightcall", SpotifyURL: "url", Image: "img", DurationMS: 258000}
	e := candidateToEntry(c, now)
	if e.SpotifyID != "sid" || e.CheckedAt != now || e.DurationMS != 258000 {
		t.Errorf("candidateToEntry: %+v", e)
	}
	back := entryToCandidate(rcache.EnrichEntry(e))
	if back != c {
		t.Errorf("round trip:\n got %+v\nwant %+v", back, c)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/spotifyenrich/ -run 'TestBuildQuery|TestToCandidate|TestPickImage|TestEntryCandidate' -v`
Expected: FAIL — undefined functions (build error).

**Note on the FullTrack literal:** if the struct-literal field names in the test do not compile against zmb3/spotify v2.4.3 (e.g. `SimpleTrack`/`Album` embedding differs), adjust the literal to match the actual struct — the field *values* asserted are what matter. Confirm the shape with `go doc github.com/zmb3/spotify/v2 FullTrack` and `SimpleTrack` before finalizing.

- [ ] **Step 3: Write the implementation**

Create `internal/spotifyenrich/search.go`:

```go
package spotifyenrich

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/lmorchard/byom-sync/internal/rcache"
	"github.com/zmb3/spotify/v2"
)

// defaultImageMaxWidth is the preferred upper bound for album art width.
const defaultImageMaxWidth = 640

// Searcher looks up Spotify candidates for a track and fetches a specific track
// by id (for the pick-by-editing flow). Abstracted for testability.
type Searcher interface {
	Search(ctx context.Context, t playlist.Track) ([]Candidate, error)
	GetByID(ctx context.Context, id string) (Candidate, error)
}

// ClientSearcher is the live Spotify-backed Searcher. Max caps how many search
// results are considered (0 → a sensible default of 5).
type ClientSearcher struct {
	Client *spotify.Client
	Max    int
}

func (s ClientSearcher) limit() int {
	if s.Max > 0 {
		return s.Max
	}
	return 5
}

func (s ClientSearcher) Search(ctx context.Context, t playlist.Track) ([]Candidate, error) {
	res, err := s.Client.Search(ctx, buildQuery(t), spotify.SearchTypeTrack, spotify.Limit(s.limit()))
	if err != nil {
		return nil, err
	}
	cands := fromResult(res)
	if len(cands) == 0 {
		// Fall back to an unfielded query — some tracks don't match the strict
		// field filters but do match a plain "artist title" search.
		res, err = s.Client.Search(ctx, strings.TrimSpace(t.Artist+" "+t.Title), spotify.SearchTypeTrack, spotify.Limit(s.limit()))
		if err != nil {
			return nil, err
		}
		cands = fromResult(res)
	}
	return cands, nil
}

func (s ClientSearcher) GetByID(ctx context.Context, id string) (Candidate, error) {
	ft, err := s.Client.GetTrack(ctx, spotify.ID(id))
	if err != nil {
		return Candidate{}, err
	}
	return toCandidate(*ft), nil
}

func fromResult(res *spotify.SearchResult) []Candidate {
	if res == nil || res.Tracks == nil {
		return nil
	}
	out := make([]Candidate, 0, len(res.Tracks.Tracks))
	for _, ft := range res.Tracks.Tracks {
		out = append(out, toCandidate(ft))
	}
	return out
}

// buildQuery constructs a fielded Spotify search query from a track.
func buildQuery(t playlist.Track) string {
	q := fmt.Sprintf(`track:%q artist:%q`, t.Title, t.Artist)
	if t.Album != "" {
		q += fmt.Sprintf(` album:%q`, t.Album)
	}
	return q
}

// toCandidate maps a Spotify FullTrack to a Candidate, mirroring
// spotifyfetch.convert()'s field handling for the v2.4.3 API shape.
func toCandidate(ft spotify.FullTrack) Candidate {
	names := make([]string, 0, len(ft.Artists))
	for _, a := range ft.Artists {
		names = append(names, a.Name)
	}
	return Candidate{
		SpotifyID:  string(ft.ID),
		ISRC:       ft.ExternalIDs["isrc"],
		Title:      ft.Name,
		Artist:     strings.Join(names, ", "),
		Album:      ft.Album.Name,
		SpotifyURL: ft.ExternalURLs["spotify"],
		Image:      pickImage(ft.Album.Images, defaultImageMaxWidth),
		DurationMS: int(ft.Duration),
	}
}

// pickImage returns the URL of the largest image no wider than maxWidth; if none
// qualify it returns the smallest available; empty when there are no images.
func pickImage(images []spotify.Image, maxWidth int) string {
	best := ""
	bestW := -1
	fallback := ""
	fallbackW := 1 << 30
	for _, img := range images {
		if img.Width <= maxWidth {
			if img.Width > bestW {
				bestW = img.Width
				best = img.URL
			}
		} else if img.Width < fallbackW {
			fallbackW = img.Width
			fallback = img.URL
		}
	}
	if best != "" {
		return best
	}
	return fallback
}

// candidateToEntry converts a Candidate to a positive enrichment-cache entry.
func candidateToEntry(c Candidate, now time.Time) rcache.EnrichEntry {
	return rcache.EnrichEntry{
		SpotifyID: c.SpotifyID, ISRC: c.ISRC, SpotifyURL: c.SpotifyURL,
		Album: c.Album, Title: c.Title, Artist: c.Artist, Image: c.Image,
		DurationMS: c.DurationMS, CheckedAt: now,
	}
}

// entryToCandidate converts a cached enrichment entry back to a Candidate.
func entryToCandidate(e rcache.EnrichEntry) Candidate {
	return Candidate{
		SpotifyID: e.SpotifyID, ISRC: e.ISRC, Title: e.Title, Artist: e.Artist,
		Album: e.Album, SpotifyURL: e.SpotifyURL, Image: e.Image, DurationMS: e.DurationMS,
	}
}
```

Note: `candidateToEntry` returns `rcache.EnrichEntry`; the test's `rcache.EnrichEntry(e)` conversion is an identity (e is already that type) — it compiles because the field sets match. If the test's cast is awkward, simplify the test to compare `entryToCandidate(candidateToEntry(c, now))` directly.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/spotifyenrich/ -v`
Expected: PASS (all Task 3 + Task 4 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/spotifyenrich/search.go internal/spotifyenrich/search_test.go
git commit -m "feat(spotifyenrich): Spotify search wrapper and candidate mapping

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Enrichment loop

**Files:**
- Create: `internal/spotifyenrich/enrich.go`
- Test: `internal/spotifyenrich/enrich_test.go`

**Interfaces:**
- Consumes: `Searcher`, `Candidate`, `Score`, `DefaultThreshold` (Tasks 3–4); `rcache.EnrichEntry` (Task 2); `playlist` types (Task 1).
- Produces:
  - `type EventKind string` with `KindEnriched`, `KindPicked`, `KindAmbiguous`, `KindMiss`, `KindError`.
  - `type Event struct { Kind EventKind; Artist, Title, SpotifyID string; Score float64; Err error }`
  - `type Cache interface { GetEnrich(key string) (rcache.EnrichEntry, bool); PutEnrich(key string, e rcache.EnrichEntry) error }`
  - `type Options struct { Budget *int; Pace time.Duration; Report func(Event); OnEnriched func() error; Canonicalize bool; Threshold float64; MaxCandidates int; Cache Cache; Now func() time.Time; MissTTL time.Duration }`
  - `func Enrich(ctx context.Context, s Searcher, p *playlist.Playlist, opts Options) (enriched int, err error)`
  - `func applyCandidate(t *playlist.Track, c Candidate, canonicalize bool)`

**Context:** Mirrors `youtube.Resolve`'s control flow but with no early-stop reason (the Spotify client auto-retries 429). Per track:
- **Already enriched** (`SpotifyID != ""` and no `EnrichCandidates`) → skip.
- **User picked** (`SpotifyID != ""` and `EnrichCandidates` present) → `GetByID`, fill remaining empty fields, clear candidates, report `Picked`.
- **Fresh** (no `SpotifyID`) → cache short-circuit, then `Search`+`Score`; confident → fill + clear candidates + report `Enriched`; else if any candidates → write top `MaxCandidates` as `EnrichCandidates` + report `Ambiguous`; else → report `Miss`.

`applyCandidate` fills only-empty technical fields (never overwriting a set one — so a user-picked `spotify_id` survives), and overwrites `title`/`artist`/`album` only when `canonicalize`. The command persists the whole playlist after `Enrich` returns, so ambiguous candidate writes survive without needing `OnEnriched`.

- [ ] **Step 1: Write the failing test**

Create `internal/spotifyenrich/enrich_test.go`:

```go
package spotifyenrich

import (
	"context"
	"testing"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/lmorchard/byom-sync/internal/rcache"
)

// fakeSearcher returns canned candidates per query and records GetByID calls.
type fakeSearcher struct {
	byTitle map[string][]Candidate
	byID    map[string]Candidate
	calls   int
}

func (f *fakeSearcher) Search(_ context.Context, t playlist.Track) ([]Candidate, error) {
	f.calls++
	return f.byTitle[t.Title], nil
}
func (f *fakeSearcher) GetByID(_ context.Context, id string) (Candidate, error) {
	f.calls++
	return f.byID[id], nil
}

func TestEnrich_ConfidentFillsOnlyEmpty(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{{Title: "Nightcall", Artist: "Kavinsky"}}}
	s := &fakeSearcher{byTitle: map[string][]Candidate{
		"Nightcall": {{SpotifyID: "sid", ISRC: "FR123", Title: "Nightcall", Artist: "Kavinsky", Album: "Nightcall", SpotifyURL: "url", Image: "img", DurationMS: 258000}},
	}}
	n, err := Enrich(context.Background(), s, p, Options{})
	if err != nil || n != 1 {
		t.Fatalf("Enrich: n=%d err=%v", n, err)
	}
	got := p.Tracks[0]
	if got.SpotifyID != "sid" || got.ISRC != "FR123" || got.DurationMS != 258000 || got.Image != "img" {
		t.Errorf("technical fields not filled: %+v", got)
	}
	if got.Title != "Nightcall" || got.Artist != "Kavinsky" {
		t.Errorf("authored text should be preserved: %+v", got)
	}
	if len(got.EnrichCandidates) != 0 {
		t.Errorf("candidates should be empty on confident match: %+v", got.EnrichCandidates)
	}
}

func TestEnrich_PreservesSetTechnicalField(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{{Title: "Nightcall", Artist: "Kavinsky", Album: "My Album"}}}
	s := &fakeSearcher{byTitle: map[string][]Candidate{
		"Nightcall": {{SpotifyID: "sid", Title: "Nightcall", Artist: "Kavinsky", Album: "Spotify Album"}},
	}}
	_, _ = Enrich(context.Background(), s, p, Options{})
	if p.Tracks[0].Album != "My Album" {
		t.Errorf("set album should be preserved: %q", p.Tracks[0].Album)
	}
}

func TestEnrich_Canonicalize(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{{Title: "come together", Artist: "beatles"}}}
	s := &fakeSearcher{byTitle: map[string][]Candidate{
		"come together": {{SpotifyID: "sid", Title: "Come Together", Artist: "The Beatles", Album: "Abbey Road"}},
	}}
	_, _ = Enrich(context.Background(), s, p, Options{Canonicalize: true})
	if p.Tracks[0].Title != "Come Together" || p.Tracks[0].Artist != "The Beatles" {
		t.Errorf("canonicalize should overwrite text: %+v", p.Tracks[0])
	}
}

func TestEnrich_AmbiguousWritesCandidates(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{{Title: "Song", Artist: "X"}}}
	s := &fakeSearcher{byTitle: map[string][]Candidate{
		"Song": {
			{SpotifyID: "a", Title: "Totally Different", Artist: "Nobody"},
			{SpotifyID: "b", Title: "Another", Artist: "Someone"},
		},
	}}
	n, _ := Enrich(context.Background(), s, p, Options{MaxCandidates: 5})
	if n != 0 {
		t.Fatalf("ambiguous should not count as enriched: n=%d", n)
	}
	got := p.Tracks[0]
	if got.SpotifyID != "" {
		t.Errorf("ambiguous track should not get a spotify_id: %q", got.SpotifyID)
	}
	if len(got.EnrichCandidates) != 2 || got.EnrichCandidates[0].SpotifyID == "" || got.EnrichCandidates[0].Score == 0 {
		t.Errorf("candidates should be written with scores: %+v", got.EnrichCandidates)
	}
}

func TestEnrich_Miss(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{{Title: "Nope", Artist: "X"}}}
	s := &fakeSearcher{byTitle: map[string][]Candidate{}}
	var kinds []EventKind
	n, _ := Enrich(context.Background(), s, p, Options{Report: func(e Event) { kinds = append(kinds, e.Kind) }})
	if n != 0 || len(kinds) != 1 || kinds[0] != KindMiss {
		t.Fatalf("expected one miss event: n=%d kinds=%v", n, kinds)
	}
}

func TestEnrich_PickByEditing(t *testing.T) {
	// user copied candidate 'b' up to spotify_id; candidates still present
	p := &playlist.Playlist{Tracks: []playlist.Track{{
		Title: "Song", Artist: "X", SpotifyID: "b",
		EnrichCandidates: []playlist.EnrichCandidate{{SpotifyID: "a"}, {SpotifyID: "b"}},
	}}}
	s := &fakeSearcher{byID: map[string]Candidate{
		"b": {SpotifyID: "b", ISRC: "FR9", Title: "Song", Artist: "X", Album: "Real", SpotifyURL: "u", DurationMS: 100000},
	}}
	var kinds []EventKind
	n, _ := Enrich(context.Background(), s, p, Options{Report: func(e Event) { kinds = append(kinds, e.Kind) }})
	got := p.Tracks[0]
	if n != 1 || len(kinds) != 1 || kinds[0] != KindPicked {
		t.Fatalf("expected one picked event: n=%d kinds=%v", n, kinds)
	}
	if got.SpotifyID != "b" || got.ISRC != "FR9" || got.Album != "Real" {
		t.Errorf("pick should fill from the chosen id: %+v", got)
	}
	if len(got.EnrichCandidates) != 0 {
		t.Errorf("candidates should be cleared after pick: %+v", got.EnrichCandidates)
	}
}

func TestEnrich_SkipsAlreadyEnriched(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{{Title: "T", Artist: "A", SpotifyID: "done"}}}
	s := &fakeSearcher{}
	_, _ = Enrich(context.Background(), s, p, Options{})
	if s.calls != 0 {
		t.Errorf("already-enriched track should not be searched: calls=%d", s.calls)
	}
}

func TestEnrich_BudgetCaps(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{
		{Title: "One", Artist: "A"}, {Title: "Two", Artist: "B"},
	}}
	s := &fakeSearcher{byTitle: map[string][]Candidate{
		"One": {{SpotifyID: "1", Title: "One", Artist: "A"}},
		"Two": {{SpotifyID: "2", Title: "Two", Artist: "B"}},
	}}
	budget := 1
	_, _ = Enrich(context.Background(), s, p, Options{Budget: &budget})
	if s.calls != 1 {
		t.Errorf("budget=1 should attempt one track: calls=%d", s.calls)
	}
}

// fakeCache implements the Cache interface.
type fakeCache struct {
	m map[string]rcache.EnrichEntry
}

func (c *fakeCache) GetEnrich(key string) (rcache.EnrichEntry, bool) { e, ok := c.m[key]; return e, ok }
func (c *fakeCache) PutEnrich(key string, e rcache.EnrichEntry) error { c.m[key] = e; return nil }

func TestEnrich_CachePositiveShortCircuits(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{{Title: "Nightcall", Artist: "Kavinsky"}}}
	key := p.Tracks[0].Key()
	cache := &fakeCache{m: map[string]rcache.EnrichEntry{
		key: {SpotifyID: "cached", ISRC: "FR1", Title: "Nightcall", Artist: "Kavinsky", CheckedAt: time.Now()},
	}}
	s := &fakeSearcher{}
	n, _ := Enrich(context.Background(), s, p, Options{Cache: cache})
	if n != 1 || s.calls != 0 {
		t.Fatalf("cache hit should avoid search: n=%d calls=%d", n, s.calls)
	}
	if p.Tracks[0].SpotifyID != "cached" {
		t.Errorf("cache positive should fill the track: %+v", p.Tracks[0])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/spotifyenrich/ -run TestEnrich -v`
Expected: FAIL — `Enrich`, `Options`, `Event`, `KindMiss`, etc. undefined (build error).

- [ ] **Step 3: Write the implementation**

Create `internal/spotifyenrich/enrich.go`:

```go
package spotifyenrich

import (
	"context"
	"sort"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/lmorchard/byom-sync/internal/rcache"
)

// EventKind classifies what happened to a track, for narration.
type EventKind string

const (
	KindEnriched  EventKind = "enriched"  // confident match filled the track
	KindPicked    EventKind = "picked"    // pick-by-editing applied a chosen id
	KindAmbiguous EventKind = "ambiguous" // no confident match; candidates recorded
	KindMiss      EventKind = "miss"      // no search results at all
	KindError     EventKind = "error"     // lookup error (track left unchanged)
)

// Event reports the outcome of one track so the caller can narrate progress.
type Event struct {
	Kind      EventKind
	Artist    string
	Title     string
	SpotifyID string  // the resulting id (enriched/picked)
	Score     float64 // best candidate score (enriched/ambiguous)
	Err       error
}

// Cache short-circuits enrichment: a positive hit fills the track without a
// network call; a fresh miss skips re-searching. Results are written back.
type Cache interface {
	GetEnrich(key string) (rcache.EnrichEntry, bool)
	PutEnrich(key string, e rcache.EnrichEntry) error
}

// Options configures an Enrich run.
type Options struct {
	Budget        *int          // if non-nil, caps tracks attempted this call
	Pace          time.Duration // if > 0, waits between network attempts
	Report        func(Event)   // if set, called once per track attempted
	OnEnriched    func() error  // if set, called after each fill (incremental persist)
	Canonicalize  bool          // overwrite authored title/artist/album with Spotify's
	Threshold     float64       // auto-accept score (0 → DefaultThreshold)
	MaxCandidates int           // ambiguous candidates recorded (0 → 5)
	Cache         Cache
	Now           func() time.Time // clock for TTL/timestamps (default time.Now)
	MissTTL       time.Duration    // negative-result freshness window
}

// Enrich fills Spotify metadata for every track in p that lacks it, mutating
// tracks in place. Confident matches fill technical fields; ambiguous tracks get
// an enrich_candidates list; misses are left untouched. Per-track lookup errors
// are reported and skipped. Only a returned error (from OnEnriched) aborts.
func Enrich(ctx context.Context, s Searcher, p *playlist.Playlist, opts Options) (enriched int, err error) {
	threshold := opts.Threshold
	if threshold == 0 {
		threshold = DefaultThreshold
	}
	maxCand := opts.MaxCandidates
	if maxCand == 0 {
		maxCand = 5
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	report := func(e Event) {
		if opts.Report != nil {
			opts.Report(e)
		}
	}
	persist := func() error {
		if opts.OnEnriched != nil {
			return opts.OnEnriched()
		}
		return nil
	}
	fresh := func(ts time.Time, ttl time.Duration) bool {
		return ttl > 0 && now().Sub(ts) < ttl
	}
	cachePut := func(key string, e rcache.EnrichEntry) {
		if opts.Cache != nil {
			_ = opts.Cache.PutEnrich(key, e)
		}
	}

	attempted := 0
	for i := range p.Tracks {
		t := &p.Tracks[i]
		picked := t.SpotifyID != "" && len(t.EnrichCandidates) > 0
		if t.SpotifyID != "" && !picked {
			continue // already enriched
		}
		key := t.Key()

		// Cache short-circuit (search path only; a pick always re-fetches by id).
		if !picked && opts.Cache != nil {
			if e, ok := opts.Cache.GetEnrich(key); ok {
				switch {
				case e.SpotifyID != "":
					applyCandidate(t, entryToCandidate(e), opts.Canonicalize)
					t.EnrichCandidates = nil
					enriched++
					report(Event{Kind: KindEnriched, Artist: t.Artist, Title: t.Title, SpotifyID: e.SpotifyID})
					if perr := persist(); perr != nil {
						return enriched, perr
					}
					continue
				case fresh(e.CheckedAt, opts.MissTTL):
					report(Event{Kind: KindMiss, Artist: t.Artist, Title: t.Title})
					continue
				}
				// expired miss → fall through to a live lookup
			}
		}

		if opts.Budget != nil && *opts.Budget <= 0 {
			return enriched, nil
		}
		if attempted > 0 && opts.Pace > 0 {
			if serr := sleep(ctx, opts.Pace); serr != nil {
				return enriched, nil
			}
		}
		attempted++
		if opts.Budget != nil {
			*opts.Budget--
		}

		if picked {
			c, gerr := s.GetByID(ctx, t.SpotifyID)
			if gerr != nil {
				report(Event{Kind: KindError, Artist: t.Artist, Title: t.Title, Err: gerr})
				continue
			}
			applyCandidate(t, c, opts.Canonicalize)
			t.EnrichCandidates = nil
			enriched++
			cachePut(key, candidateToEntry(c, now()))
			report(Event{Kind: KindPicked, Artist: t.Artist, Title: t.Title, SpotifyID: t.SpotifyID})
			if perr := persist(); perr != nil {
				return enriched, perr
			}
			continue
		}

		cands, serr := s.Search(ctx, *t)
		if serr != nil {
			report(Event{Kind: KindError, Artist: t.Artist, Title: t.Title, Err: serr})
			continue
		}
		if len(cands) == 0 {
			cachePut(key, rcache.EnrichEntry{CheckedAt: now()})
			report(Event{Kind: KindMiss, Artist: t.Artist, Title: t.Title})
			continue
		}

		// Score and rank.
		scored := make([]scoredCandidate, len(cands))
		for j, c := range cands {
			scored[j] = scoredCandidate{c: c, score: Score(*t, c)}
		}
		sort.SliceStable(scored, func(a, b int) bool { return scored[a].score > scored[b].score })
		best := scored[0]

		if best.score >= threshold {
			applyCandidate(t, best.c, opts.Canonicalize)
			t.EnrichCandidates = nil
			enriched++
			cachePut(key, candidateToEntry(best.c, now()))
			report(Event{Kind: KindEnriched, Artist: t.Artist, Title: t.Title, SpotifyID: best.c.SpotifyID, Score: best.score})
			if perr := persist(); perr != nil {
				return enriched, perr
			}
			continue
		}

		// Ambiguous: record the top candidates for the user to pick from.
		t.EnrichCandidates = topCandidates(scored, maxCand)
		report(Event{Kind: KindAmbiguous, Artist: t.Artist, Title: t.Title, Score: best.score})
	}
	return enriched, nil
}

type scoredCandidate struct {
	c     Candidate
	score float64
}

func topCandidates(scored []scoredCandidate, n int) []playlist.EnrichCandidate {
	if len(scored) < n {
		n = len(scored)
	}
	out := make([]playlist.EnrichCandidate, 0, n)
	for _, sc := range scored[:n] {
		out = append(out, playlist.EnrichCandidate{
			SpotifyID:  sc.c.SpotifyID,
			Title:      sc.c.Title,
			Artist:     sc.c.Artist,
			Album:      sc.c.Album,
			ISRC:       sc.c.ISRC,
			DurationMS: sc.c.DurationMS,
			Score:      round2(sc.score),
		})
	}
	return out
}

func round2(f float64) float64 { return float64(int(f*100+0.5)) / 100 }

// applyCandidate fills only-empty technical fields from c (never overwriting a
// value already set — so a user-picked spotify_id survives). Authored
// title/artist/album text is overwritten only when canonicalize is true.
func applyCandidate(t *playlist.Track, c Candidate, canonicalize bool) {
	if t.ISRC == "" {
		t.ISRC = c.ISRC
	}
	if t.SpotifyID == "" {
		t.SpotifyID = c.SpotifyID
	}
	if t.SpotifyURL == "" {
		t.SpotifyURL = c.SpotifyURL
	}
	if t.DurationMS == 0 {
		t.DurationMS = c.DurationMS
	}
	if t.Album == "" {
		t.Album = c.Album
	}
	if t.Image == "" {
		t.Image = c.Image
	}
	if canonicalize {
		if c.Title != "" {
			t.Title = c.Title
		}
		if c.Artist != "" {
			t.Artist = c.Artist
		}
		if c.Album != "" {
			t.Album = c.Album
		}
	}
}

// sleep waits d or until ctx is cancelled.
func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/spotifyenrich/ -v`
Expected: PASS (all spotifyenrich tests).

- [ ] **Step 5: Commit**

```bash
git add internal/spotifyenrich/enrich.go internal/spotifyenrich/enrich_test.go
git commit -m "feat(spotifyenrich): enrichment loop with confident/ambiguous/pick flows

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: `resolve spotify` command

**Files:**
- Modify: `cmd/resolve.go`

**Interfaces:**
- Consumes: `auth.Client`, `spotifyenrich.{ClientSearcher,Enrich,Options,Event,Kind*}`, `playlist.{LoadFile,SaveFile}`, the existing `hubPaths`, `openCache`, `defaultCachePath` helpers, and Viper config keys (`client_id`, `redirect_port`, `dir`, `cache_miss_ttl`).
- Produces: a `resolve spotify` cobra subcommand.

**Context:** Mirror `runResolveYouTube` (same file) for structure: resolve input dir, load each file, count tracks needing enrichment, run `Enrich`, persist. Two differences from YouTube: (1) needs an authenticated Spotify client (`auth.Client` + `defer auth.PersistRefreshed`); (2) **always `SaveFile` after `Enrich` for a playlist that had work**, because ambiguous runs write `enrich_candidates` that must persist even though nothing was "enriched." The enrichment cache is the same `*rcache.DB` from `openCache()` (it now carries both tables) — pass it as `opts.Cache`.

A track "needs enrichment" when `SpotifyID == ""` OR (`SpotifyID != ""` AND `len(EnrichCandidates) > 0`) — i.e. unresolved or a pending pick.

- [ ] **Step 1: Add the flag vars and command**

At the top var block of `cmd/resolve.go`, add enrichment flag vars (distinct from the YouTube ones):

```go
var (
	enrichInput        string
	enrichLimit        int
	enrichDelay        time.Duration
	enrichFlush        int
	enrichNoCache      bool
	enrichCanonicalize bool
)
```

Add the command definition (near `resolveYouTubeCmd`):

```go
var resolveSpotifyCmd = &cobra.Command{
	Use:   "spotify",
	Short: "Enrich hub tracks with Spotify metadata (ISRC, ids, duration, art)",
	Long: `Look up each hub track that lacks a spotify_id in Spotify and fill its
technical fields (isrc, spotify_id, spotify_url, duration_ms, album, image),
leaving your authored title/artist/album text intact. Only confident matches are
written; ambiguous tracks get an enrich_candidates list in their YAML — to accept
one, copy its spotify_id up to the track's own spotify_id and re-run.

--limit caps tracks attempted per run; --delay paces requests. --canonicalize
overwrites authored text with Spotify's official strings (off by default).
Typically run before 'resolve youtube' so downstream identity keys on ISRC.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runResolveSpotify(context.Background())
	},
}
```

- [ ] **Step 2: Add the run function**

Add `runResolveSpotify` to `cmd/resolve.go`:

```go
func runResolveSpotify(ctx context.Context) error {
	input := enrichInput
	if input == "" {
		input = viper.GetString("dir")
	}
	paths, err := hubPaths(input)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		log.Warnf("no playlist YAML files found under %s — nothing to enrich", input)
		return nil
	}

	client, tok, err := auth.Client(ctx, viper.GetString("client_id"), viper.GetInt("redirect_port"))
	if err != nil {
		return err
	}
	defer auth.PersistRefreshed(client, tok)
	searcher := spotifyenrich.ClientSearcher{Client: client}

	// Enrichment cache lives in the same cache.db (a second table). --no-cache
	// bypasses it. openCache honors the shared resolveNoCache flag, so set it.
	resolveNoCache = enrichNoCache
	cache, err := openCache()
	if err != nil {
		return fmt.Errorf("open cache: %w", err)
	}
	if cache != nil {
		defer func() { _ = cache.Close() }()
	}
	missTTL := viper.GetDuration("cache_miss_ttl")

	var budget *int
	if enrichLimit > 0 {
		budget = &enrichLimit
	}

	total := 0
	for _, path := range paths {
		p, lerr := playlist.LoadFile(path)
		if lerr != nil {
			return fmt.Errorf("load %s: %w", path, lerr)
		}
		need := countNeedingEnrich(p)
		base := filepath.Base(path)
		if need == 0 {
			log.Infof("%s: nothing to enrich (%d tracks)", base, len(p.Tracks))
			continue
		}
		log.Infof("%s: %d of %d tracks need enrichment", base, need, len(p.Tracks))

		var got, ambiguous, missed int
		report := func(e spotifyenrich.Event) {
			switch e.Kind {
			case spotifyenrich.KindEnriched:
				got++
				log.Debugf("  enriched: %s - %s -> %s (score %.2f)", e.Artist, e.Title, e.SpotifyID, e.Score)
			case spotifyenrich.KindPicked:
				got++
				log.Debugf("  picked: %s - %s -> %s", e.Artist, e.Title, e.SpotifyID)
			case spotifyenrich.KindAmbiguous:
				ambiguous++
				log.Debugf("  ambiguous: %s - %s (best %.2f) — candidates written", e.Artist, e.Title, e.Score)
			case spotifyenrich.KindMiss:
				missed++
				log.Debugf("  no match: %s - %s", e.Artist, e.Title)
			case spotifyenrich.KindError:
				log.Warnf("  error: %s - %s: %v", e.Artist, e.Title, e.Err)
			}
		}

		sinceSave := 0
		onEnriched := func() error {
			sinceSave++
			if sinceSave >= enrichFlush {
				sinceSave = 0
				return playlist.SaveFile(path, p)
			}
			return nil
		}

		opts := spotifyenrich.Options{
			Budget:       budget,
			Pace:         enrichDelay,
			Report:       report,
			OnEnriched:   onEnriched,
			Canonicalize: enrichCanonicalize,
			MissTTL:      missTTL,
		}
		if cache != nil {
			opts.Cache = cache
		}
		n, eerr := spotifyenrich.Enrich(ctx, searcher, &p, opts)
		// Always persist: ambiguous runs wrote enrich_candidates even when n==0.
		if serr := playlist.SaveFile(path, p); serr != nil {
			return fmt.Errorf("save %s: %w", path, serr)
		}
		if eerr != nil {
			return fmt.Errorf("enrich %s: %w", path, eerr)
		}
		log.Infof("%s: %d enriched, %d ambiguous (candidates written), %d no-match", base, got, ambiguous, missed)
		total += n
		if budget != nil && *budget <= 0 {
			log.Warnf("enrichment limit reached — stopping (progress saved)")
			break
		}
	}
	log.Warnf("Spotify enrich done: %d track(s) enriched", total)
	return nil
}

// countNeedingEnrich counts tracks that are unresolved or have a pending pick.
func countNeedingEnrich(p playlist.Playlist) int {
	n := 0
	for _, t := range p.Tracks {
		if t.SpotifyID == "" || len(t.EnrichCandidates) > 0 {
			n++
		}
	}
	return n
}
```

Add the imports `"github.com/lmorchard/byom-sync/internal/auth"` and `"github.com/lmorchard/byom-sync/internal/spotifyenrich"` to `cmd/resolve.go`.

- [ ] **Step 3: Register the command and flags**

In `func init()` of `cmd/resolve.go`, after the YouTube registration, add:

```go
	resolveCmd.AddCommand(resolveSpotifyCmd)
	resolveSpotifyCmd.Flags().StringVar(&enrichInput, "input", "", "hub YAML file or directory (default: config dir)")
	resolveSpotifyCmd.Flags().IntVar(&enrichLimit, "limit", 0, "max tracks attempted this run (0 = unlimited)")
	resolveSpotifyCmd.Flags().DurationVar(&enrichDelay, "delay", 200*time.Millisecond, "pause between Spotify lookups")
	resolveSpotifyCmd.Flags().IntVar(&enrichFlush, "flush", 20, "write enriched fields to disk every N fills (granular resume)")
	resolveSpotifyCmd.Flags().BoolVar(&enrichNoCache, "no-cache", false, "bypass the enrichment cache")
	resolveSpotifyCmd.Flags().BoolVar(&enrichCanonicalize, "canonicalize", false, "overwrite authored title/artist/album with Spotify's strings")
```

- [ ] **Step 4: Build and smoke-check the command wiring**

Run: `make build && ./byom-sync resolve spotify --help`
Expected: build succeeds; help shows the `spotify` subcommand with all six flags. (Actual enrichment against live Spotify is manual — not part of automated verification.)

Run: `go test ./cmd/ -v`
Expected: PASS (existing cmd tests unaffected).

- [ ] **Step 5: Commit**

```bash
git add cmd/resolve.go
git commit -m "feat(resolve): add 'resolve spotify' enrichment command

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Report enrichment coverage in `cache stats`

**Files:**
- Modify: `cmd/resolve.go` (the `resolveCacheStatsCmd` RunE)

**Interfaces:**
- Consumes: `db.EnrichStats` (Task 2).
- Produces: an extra log line in `cache stats` output.

**Context:** `cache clear` already covers both tables via Task 2's `Clear` change. Only `cache stats` needs a second line for the enrichment table. Add it after the existing resolution-cache line.

- [ ] **Step 1: Add the enrichment stats line**

In `resolveCacheStatsCmd`'s `RunE`, after the existing resolution `log.Infof(...)` line, add:

```go
	es, err := db.EnrichStats(time.Now().Add(-missTTL))
	if err != nil {
		return err
	}
	log.Infof("enrichment cache: %d entries — %d resolved, %d misses (%d expired)",
		es.Total, es.Positive, es.Negative, es.ExpiredNegative)
```

(The existing `missTTL` var in that function is reused.)

- [ ] **Step 2: Build and verify**

Run: `make build && make test`
Expected: build + all tests pass.

Run: `./byom-sync resolve cache stats 2>&1 | head` (against a throwaway/empty cache is fine)
Expected: two lines — the resolution-cache line and the new enrichment-cache line.

- [ ] **Step 3: Commit**

```bash
git add cmd/resolve.go
git commit -m "feat(resolve): report enrichment coverage in 'cache stats'

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Document enrichment in AGENTS.md

**Files:**
- Modify: `AGENTS.md`

**Interfaces:**
- Consumes: nothing.
- Produces: documentation only.

**Context:** Document the new `resolve spotify` command, the reverse/enrichment flow, the pick-by-editing mechanic, and the recommended pipeline order. Update the `internal/spotifyenrich/` and `internal/rcache/` descriptions in the Layout section, and the `resolve` subcommand list.

- [ ] **Step 1: Update the Layout section**

In `AGENTS.md`, in the `cmd/` bullet, extend the `resolve` subcommand list to include `spotify`:

Find `resolve` (subcommands `youtube`, `prime`, `cache stats`, `cache clear`) and change it to:
`resolve` (subcommands `youtube`, `spotify`, `prime`, `cache stats`, `cache clear`).

Add a new Layout bullet after the `internal/youtube/` bullet:

```markdown
- `internal/spotifyenrich/` — reverse enrichment: `score.go` (`Candidate`,
  `Score`, similarity), `search.go` (`Searcher`/`ClientSearcher`, `buildQuery`,
  `toCandidate`, image pick), `enrich.go` (`Enrich` loop, `Options`, `Event`,
  `Cache`, `applyCandidate`). Fills empty technical fields on confident matches;
  writes `enrich_candidates` for ambiguous ones.
```

Update the `internal/rcache/` bullet to note the second table:

```markdown
- `internal/rcache/` — SQLite cache with two tables in one `cache.db`:
  `resolution_cache` (YouTube: `Entry`, `Get`/`Put`) and `enrichment_cache`
  (Spotify: `EnrichEntry`, `GetEnrich`/`PutEnrich`). `Stats`/`EnrichStats`/`Clear`
  span both; keyed by `Track.Key()`; gitignored, disposable.
```

- [ ] **Step 2: Add an enrichment convention bullet**

Under "Conventions & gotchas", after the **Native playlists** bullet, add:

```markdown
- **Enrichment (reverse path):** `resolve spotify` searches Spotify per track and
  fills only *empty* technical fields (`isrc`, `spotify_id`, `spotify_url`,
  `duration_ms`, `album`, `image`), preserving authored `title`/`artist`/`album`
  unless `--canonicalize`. Only matches scoring ≥ threshold (0.8, in
  `spotifyenrich`) auto-fill; below that, the track's top matches are written as
  `enrich_candidates` — accept one by copying its `spotify_id` up to the track's
  own `spotify_id` and re-running. Recommended pipeline order:
  author/`sync` → `resolve spotify` → `resolve youtube` → `export`, so YouTube
  resolution and its cache key on the enriched ISRC (`Track.Key()` is ISRC-first).
```

- [ ] **Step 3: Verify the doc**

Run: `git diff AGENTS.md`
Expected: the Layout `resolve`/`spotifyenrich`/`rcache` edits and the new convention bullet, well-formed Markdown, no other changes.

- [ ] **Step 4: Commit**

```bash
git add AGENTS.md
git commit -m "docs(agents): document Spotify enrichment (resolve spotify)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Final verification (after all tasks)

- [ ] **Run the full suite and build:**

Run: `make lint && make test && make build`
Expected: lint clean, all tests pass, build succeeds. Read the output.

- [ ] **Manual live check (optional, needs a real Spotify app + Premium):**

Author a small native playlist YAML (title + a few loose title/artist entries), then:
`./byom-sync resolve spotify --input path/to/native.yaml`
Confirm confident tracks gain `isrc`/`spotify_id`/`image` while text is preserved, and at least one ambiguous track (try a deliberately vague entry) gets an `enrich_candidates` list. Then copy a candidate's `spotify_id` up and re-run to confirm the pick fills the track and clears candidates. This is manual and not gating.

---

## Self-Review

**Spec coverage (Phase 2 section of spec.md):**
- `resolve spotify` command mirroring `resolve youtube` → Task 6.
- New `internal/spotifyenrich` package with Resolve-style loop → Tasks 3–5.
- Query with field filters + plain fallback → Task 4 (`buildQuery`, `ClientSearcher.Search` fallback).
- Scoring (normalized similarity + album tiebreak + duration proximity), tunable/centralized → Task 3.
- Field-fill policy (only-empty; preserve text; `--canonicalize`) → Task 5 (`applyCandidate`), tested.
- Capture album art into `Track.Image` for free → Task 4 (`toCandidate`/`pickImage`) + Task 5 (`applyCandidate`).
- `enrich_candidates` write + pick-by-editing → Tasks 1 (schema) + 5 (loop) + tested.
- Event kinds (`enriched`/`ambiguous`/`miss`/`error`, plus `picked`) → Task 5.
- Cache as a second table in `cache.db`, keyed by `Track.Key()`, cache short-circuit → Tasks 2 + 5.
- Identity-mutation ordering documented (enrich before YouTube) → Task 8.
- `--interactive`: **explicitly deferred** by decision (pick-by-editing covers ambiguous resolution). Not in this plan — noted here so the omission is intentional, not a gap.
- `resolve prime` for enrichment positives: the spec mentioned it "can later" repopulate; **deferred** (out of scope this phase; not required for correctness). Noted intentionally.

**Placeholder scan:** none — every code step carries complete code. Two steps flag library-shape verification (the `FullTrack` literal in Task 4, the existing `Clear` signature in Task 2) with explicit fallback instructions rather than leaving them open.

**Type consistency:** `Candidate` defined once (Task 3, `score.go`) and consumed by Tasks 4–5. `Cache` interface methods (`GetEnrich`/`PutEnrich`) match `rcache.DB`'s methods (Task 2) exactly, so `*rcache.DB` satisfies `spotifyenrich.Cache`. `rcache.EnrichEntry` field set matches between Task 2 (definition), Task 4 (mapping), and Task 5 (usage). `Options`/`Event`/`EventKind` names match between Task 5 (definition) and Task 6 (consumption: `KindEnriched`/`KindPicked`/`KindAmbiguous`/`KindMiss`/`KindError`). `Enrich` returns `(int, error)` consistently. The command reuses the existing `openCache`/`resolveNoCache`/`hubPaths`/`log` symbols already in `cmd/resolve.go`.
