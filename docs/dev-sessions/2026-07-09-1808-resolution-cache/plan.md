# Resolution Cache Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a persistent SQLite cache in front of the YouTube resolver chain so resolved ids, misses, and embeddability verdicts are reused across playlists and runs.

**Architecture:** A new `internal/rcache` package owns the SQLite dependency and exposes a tiny `Get`/`Put`/`Stats`/`Clear` store keyed by `Track.Key()`. `youtube.Resolve` gains an optional `Cache` on its options and consults it before any network call (nil cache = today's behavior). `cmd/resolve.go` opens the DB, wires TTLs/flags, and gains `prime`, `cache stats`, and `cache clear` subcommands.

**Tech Stack:** Go 1.25 · `modernc.org/sqlite` (pure-Go, no cgo) · Cobra · Viper · `gopkg.in/yaml.v3`.

## Global Constraints

- Go 1.25; `gofumpt` formatting; golangci-lint v2 (**errcheck is strict** — use `_ =` for intentionally-ignored returns).
- Driver: `modernc.org/sqlite` (pure Go). Register with a blank import; `sql.Open` driver name is `"sqlite"`.
- Cache is an accelerator only. `youtube_id` is still written into the YAML on every resolution. A nil cache MUST reproduce today's exact behavior.
- Cache DB default location: `$XDG_CONFIG_HOME/byom-sync/cache.db` (fallback `~/.config/byom-sync/cache.db`); overridable via `cache_path`.
- TTL defaults: `cache_miss_ttl = 720h`, `cache_embed_ttl = 720h`. A TTL `<= 0` means "never trust freshness" (always re-attempt / re-verify).
- Commit trailer on every commit: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
- Verify with `make lint && make test && make build` before claiming a task done.

## File Structure

- **Create** `internal/rcache/rcache.go` — `Entry`, `DB`, `Open`/`Close`/`Get`/`Put`/`Stats`/`Clear`, schema.
- **Create** `internal/rcache/rcache_test.go` — store round-trip, TTL-agnostic storage, upsert, clear, stats.
- **Modify** `internal/youtube/resolver.go` — add `Embeddable *bool` to `Result`.
- **Modify** `internal/youtube/ytdlp.go` — set `Embeddable` true on a yt-dlp hit.
- **Modify** `internal/youtube/resolve.go` — add `Cache` interface + cache/TTL fields to `ResolveOptions`; consult and update the cache.
- **Create** `internal/youtube/cache_test.go` — `Resolve` cache behavior via a fake `Cache`.
- **Modify** `cmd/resolve.go` — open cache, wire into `runResolveYouTube`, `--no-cache`, config defaults, `prime`, `cache stats`, `cache clear`.
- **Modify** `cmd/root.go` — `viper.SetDefault` for `cache_path`, `cache_miss_ttl`, `cache_embed_ttl`.
- **Modify** `.gitignore` — ignore `cache.db`.
- **Modify** `README.md` / `AGENTS.md` — document the cache + new subcommands.

---

### Task 1: `internal/rcache` store (SQLite)

**Files:**
- Create: `internal/rcache/rcache.go`
- Test: `internal/rcache/rcache_test.go`
- Modify: `go.mod`, `go.sum` (add `modernc.org/sqlite`)

**Interfaces:**
- Produces:
  - `type Entry struct { VideoID string; Source string; Embeddable *bool; ResolvedAt time.Time; CheckedAt time.Time }`
  - `func Open(path string) (*DB, error)`
  - `func (d *DB) Close() error`
  - `func (d *DB) Get(key string) (Entry, bool)` — false on no-row or any read error (cache degrades to miss).
  - `func (d *DB) Put(key string, e Entry) error` — upsert.
  - `type Stats struct { Total, Positive, Negative, ExpiredNegative int }`
  - `func (d *DB) Stats(missCutoff time.Time) (Stats, error)` — `ExpiredNegative` counts negatives with `checked_at < missCutoff`.
  - `func (d *DB) Clear(missesOnly bool) (int64, error)` — rows deleted.

- [ ] **Step 1: Add the dependency**

Run: `go get modernc.org/sqlite@latest`
Expected: `go.mod`/`go.sum` updated, no error.

- [ ] **Step 2: Write the failing test**

Create `internal/rcache/rcache_test.go`:

```go
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
	_ = db.Put("k", Entry{VideoID: "v1", CheckedAt: now})           // embeddable nil, negative-looking source
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
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/rcache/`
Expected: FAIL — `undefined: Open` / `undefined: DB`.

- [ ] **Step 4: Write the implementation**

Create `internal/rcache/rcache.go`:

```go
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
		`SELECT video_id, source, embeddable, resolved_at, checked_at FROM resolution_cache WHERE key = ?`, key)
	var (
		e         Entry
		source    sql.NullString
		emb       sql.NullInt64
		resolved  sql.NullString
		checked   sql.NullString
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
		key, e.VideoID, source, emb, resolved, e.CheckedAt.UTC().Format(time.RFC3339))
	return err
}

// Stats summarizes cache contents. ExpiredNegative counts negative entries whose
// checked_at is before missCutoff (i.e. would be re-attempted).
type Stats struct{ Total, Positive, Negative, ExpiredNegative int }

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
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/rcache/`
Expected: PASS.

- [ ] **Step 6: Lint, format, commit**

```bash
make format && make lint
git add internal/rcache go.mod go.sum
git commit -m "feat(rcache): SQLite resolution cache store

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: `Embeddable` on resolver `Result`

**Files:**
- Modify: `internal/youtube/resolver.go`
- Modify: `internal/youtube/ytdlp.go:66`
- Test: `internal/youtube/ytdlp_test.go` (add a case)

**Interfaces:**
- Produces: `Result.Embeddable *bool` — set to `true` by the yt-dlp resolver (it only returns embeddable ids); left `nil` by `SearchResolver` (unverified).

- [ ] **Step 1: Write the failing test**

Add to `internal/youtube/ytdlp_test.go` (adapt the fake-`run` setup already used by neighboring tests in that file):

```go
func TestYtdlpResolveMarksEmbeddable(t *testing.T) {
	calls := 0
	y := YtdlpResolver{Bin: "yt-dlp", run: func(ctx context.Context, name string, args ...string) (string, error) {
		calls++
		if calls == 1 {
			return "vid1\nvid2\n", nil // search results
		}
		return "True\n", nil // playable_in_embed for vid1
	}}
	res, err := y.Resolve(context.Background(), playlist.Track{Artist: "A", Title: "T"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.VideoID != "vid1" {
		t.Fatalf("VideoID: got %q", res.VideoID)
	}
	if res.Embeddable == nil || !*res.Embeddable {
		t.Fatalf("Embeddable: want non-nil true, got %v", res.Embeddable)
	}
}
```

(Confirm the exact `run` field/signature against the existing tests in `ytdlp_test.go`; reuse their pattern verbatim.)

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/youtube/ -run TestYtdlpResolveMarksEmbeddable`
Expected: FAIL — `res.Embeddable` is nil (field doesn't exist yet / not set).

- [ ] **Step 3: Implement**

In `internal/youtube/resolver.go`, extend `Result`:

```go
type Result struct {
	VideoID string
	Source  string
	// Embeddable reports whether the producing resolver confirmed embedded
	// playback. nil = not verified (e.g. the youtube-search fallback).
	Embeddable *bool
}
```

In `internal/youtube/ytdlp.go`, change the hit return (line ~66):

```go
		if embeddable {
			t := true
			return Result{VideoID: id, Source: "yt-dlp", Embeddable: &t}, nil
		}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/youtube/`
Expected: PASS (existing tests unaffected — new field is additive).

- [ ] **Step 5: Commit**

```bash
make format && make lint
git add internal/youtube/resolver.go internal/youtube/ytdlp.go internal/youtube/ytdlp_test.go
git commit -m "feat(youtube): Result carries embeddability from the resolver

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Cache-aware `youtube.Resolve`

**Files:**
- Modify: `internal/youtube/resolve.go`
- Test: `internal/youtube/cache_test.go`

**Interfaces:**
- Consumes: `rcache.Entry` (Task 1), `Result.Embeddable` (Task 2).
- Produces (additions to `ResolveOptions`):
  - `Cache Cache` — optional; nil = today's behavior.
  - `Now func() time.Time` — defaults to `time.Now`.
  - `MissTTL time.Duration`, `EmbedTTL time.Duration`.
  - `type Cache interface { Get(key string) (rcache.Entry, bool); Put(key string, e rcache.Entry) error }`

Cache policy inserted into the per-track loop **before** the budget/pace block so hits never consume `--limit` or `--delay`:

- Resolve path (track has no id): positive cache hit → fill id, report `KindResolved` with `Source:"cache"`, persist, `continue`. Fresh negative hit (`Now()-CheckedAt < MissTTL`) → report `KindMiss`, `continue`. Expired/absent → fall through to network, then `Put` the result (positive with `Embeddable` from `Result`, or negative miss).
- Reresolve path (track has an id): if cache has a positive entry with matching `VideoID`, `Embeddable==true`, and fresh (`< EmbedTTL`) → report `KindKept`, `continue` (no yt-dlp verify). Otherwise verify as today, then `Put` the fresh verdict.
- Positive cache hits have **no** TTL (a resolved id is reused forever). Only misses use `MissTTL`; only embeddability uses `EmbedTTL`.

- [ ] **Step 1: Write the failing test**

Create `internal/youtube/cache_test.go`:

```go
package youtube

import (
	"context"
	"testing"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/lmorchard/byom-sync/internal/rcache"
)

// fakeCache is an in-memory Cache for tests.
type fakeCache struct {
	m    map[string]rcache.Entry
	puts int
}

func newFakeCache() *fakeCache { return &fakeCache{m: map[string]rcache.Entry{}} }
func (c *fakeCache) Get(k string) (rcache.Entry, bool) { e, ok := c.m[k]; return e, ok }
func (c *fakeCache) Put(k string, e rcache.Entry) error { c.m[k] = e; c.puts++; return nil }

// stubResolver counts calls and returns a fixed result.
type stubResolver struct {
	res   Result
	calls int
}

func (s *stubResolver) Name() string { return "stub" }
func (s *stubResolver) Resolve(ctx context.Context, t playlist.Track) (Result, error) {
	s.calls++
	return s.res, nil
}

func track(id string) playlist.Track { return playlist.Track{Artist: "A", Title: "T", ISRC: id} }

func TestResolvePositiveCacheHitSkipsResolver(t *testing.T) {
	cache := newFakeCache()
	cache.m[track("US1").Key()] = rcache.Entry{VideoID: "cached", CheckedAt: time.Now()}
	r := &stubResolver{res: Result{VideoID: "network"}}
	p := playlist.Playlist{Tracks: []playlist.Track{track("US1")}}
	n, _, err := Resolve(context.Background(), r, &p, ResolveOptions{Cache: cache, MissTTL: time.Hour, EmbedTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if r.calls != 0 {
		t.Fatalf("resolver called %d times; want 0 (cache hit)", r.calls)
	}
	if p.Tracks[0].YouTubeID != "cached" || n != 1 {
		t.Fatalf("got id=%q n=%d", p.Tracks[0].YouTubeID, n)
	}
}

func TestResolveFreshMissSkipsResolver(t *testing.T) {
	cache := newFakeCache()
	cache.m[track("US2").Key()] = rcache.Entry{VideoID: "", CheckedAt: time.Now()}
	r := &stubResolver{res: Result{VideoID: "network"}}
	p := playlist.Playlist{Tracks: []playlist.Track{track("US2")}}
	_, _, _ = Resolve(context.Background(), r, &p, ResolveOptions{Cache: cache, MissTTL: time.Hour, EmbedTTL: time.Hour})
	if r.calls != 0 {
		t.Fatalf("resolver called %d times; want 0 (fresh miss)", r.calls)
	}
}

func TestResolveExpiredMissReattemptsAndCaches(t *testing.T) {
	cache := newFakeCache()
	cache.m[track("US3").Key()] = rcache.Entry{VideoID: "", CheckedAt: time.Now().Add(-48 * time.Hour)}
	r := &stubResolver{res: Result{VideoID: "found", Source: "stub"}}
	p := playlist.Playlist{Tracks: []playlist.Track{track("US3")}}
	_, _, _ = Resolve(context.Background(), r, &p, ResolveOptions{Cache: cache, MissTTL: time.Hour, EmbedTTL: time.Hour})
	if r.calls != 1 {
		t.Fatalf("resolver called %d times; want 1 (expired miss)", r.calls)
	}
	if got, _ := cache.Get(track("US3").Key()); got.VideoID != "found" {
		t.Fatalf("cache not updated: %+v", got)
	}
}

func TestResolveMissIsCached(t *testing.T) {
	cache := newFakeCache()
	r := &stubResolver{res: Result{}} // no match
	p := playlist.Playlist{Tracks: []playlist.Track{track("US4")}}
	_, _, _ = Resolve(context.Background(), r, &p, ResolveOptions{Cache: cache, MissTTL: time.Hour, EmbedTTL: time.Hour})
	got, ok := cache.Get(track("US4").Key())
	if !ok || got.VideoID != "" {
		t.Fatalf("miss not cached: ok=%v %+v", ok, got)
	}
}

func TestReresolveEmbedCacheHitSkipsVerify(t *testing.T) {
	cache := newFakeCache()
	tr := track("US5")
	tr.YouTubeID = "vid"
	cache.m[tr.Key()] = rcache.Entry{VideoID: "vid", Embeddable: func() *bool { b := true; return &b }(), CheckedAt: time.Now()}
	verifyCalls := 0
	r := &stubResolver{}
	p := playlist.Playlist{Tracks: []playlist.Track{tr}}
	_, _, _ = Resolve(context.Background(), r, &p, ResolveOptions{
		Cache: cache, MissTTL: time.Hour, EmbedTTL: time.Hour, Reresolve: true,
		Verify: func(ctx context.Context, id string) (bool, error) { verifyCalls++; return true, nil },
	})
	if verifyCalls != 0 {
		t.Fatalf("Verify called %d times; want 0 (fresh embed cache)", verifyCalls)
	}
	if p.Tracks[0].YouTubeID != "vid" {
		t.Fatalf("id changed: %q", p.Tracks[0].YouTubeID)
	}
}

func TestNilCacheUnchangedBehavior(t *testing.T) {
	r := &stubResolver{res: Result{VideoID: "network", Source: "stub"}}
	p := playlist.Playlist{Tracks: []playlist.Track{track("US6")}}
	_, _, _ = Resolve(context.Background(), r, &p, ResolveOptions{}) // no cache
	if r.calls != 1 || p.Tracks[0].YouTubeID != "network" {
		t.Fatalf("nil-cache path changed: calls=%d id=%q", r.calls, p.Tracks[0].YouTubeID)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/youtube/ -run 'TestResolve|TestReresolve|TestNilCache'`
Expected: FAIL — `unknown field Cache in struct literal` / `undefined: Cache`.

- [ ] **Step 3: Implement**

In `internal/youtube/resolve.go`, add the import and the interface, extend `ResolveOptions`, and thread the cache. Full replacement of the options struct and `Resolve` body:

```go
import (
	"context"
	"errors"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/lmorchard/byom-sync/internal/rcache"
)

// Cache is the optional resolution cache consulted before any network call.
type Cache interface {
	Get(key string) (rcache.Entry, bool)
	Put(key string, e rcache.Entry) error
}

// ResolveOptions configures a Resolve run.
type ResolveOptions struct {
	Budget     *int
	Pace       time.Duration
	Report     func(Event)
	OnResolved func() error
	Reresolve  bool
	Verify     func(ctx context.Context, videoID string) (bool, error)

	// Cache, if non-nil, short-circuits resolution: reused ids and fresh misses
	// avoid the network entirely (and do not consume Budget or Pace). Results are
	// written back for future runs.
	Cache    Cache
	Now      func() time.Time // clock for TTL checks; defaults to time.Now
	MissTTL  time.Duration    // negative-result freshness window
	EmbedTTL time.Duration    // embeddability freshness window (Reresolve)
}

func Resolve(ctx context.Context, r Resolver, p *playlist.Playlist, opts ResolveOptions) (resolved int, stopped string, err error) {
	report := func(e Event) {
		if opts.Report != nil {
			opts.Report(e)
		}
	}
	persist := func() error {
		if opts.OnResolved != nil {
			return opts.OnResolved()
		}
		return nil
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	fresh := func(ts time.Time, ttl time.Duration) bool {
		return ttl > 0 && now().Sub(ts) < ttl
	}
	cachePut := func(key string, e rcache.Entry) {
		if opts.Cache != nil {
			_ = opts.Cache.Put(key, e)
		}
	}

	attempted := 0
	for i := range p.Tracks {
		t := &p.Tracks[i]
		reverify := t.YouTubeID != "" && opts.Reresolve && opts.Verify != nil
		if t.YouTubeID != "" && !reverify {
			continue
		}
		key := t.Key()

		// Cache short-circuits — before budget/pace so hits are free.
		if opts.Cache != nil {
			if e, ok := opts.Cache.Get(key); ok {
				if reverify {
					if e.VideoID == t.YouTubeID && e.Embeddable != nil && *e.Embeddable && fresh(e.CheckedAt, opts.EmbedTTL) {
						report(Event{Kind: KindKept, Artist: t.Artist, Title: t.Title, VideoID: t.YouTubeID})
						continue
					}
				} else if e.VideoID != "" {
					t.YouTubeID = e.VideoID
					resolved++
					report(Event{Kind: KindResolved, Artist: t.Artist, Title: t.Title, VideoID: e.VideoID, Source: "cache"})
					if err := persist(); err != nil {
						return resolved, "", err
					}
					continue
				} else if fresh(e.CheckedAt, opts.MissTTL) {
					report(Event{Kind: KindMiss, Artist: t.Artist, Title: t.Title})
					continue
				}
			}
		}

		if opts.Budget != nil && *opts.Budget <= 0 {
			return resolved, "", nil
		}
		if attempted > 0 && opts.Pace > 0 {
			if err := sleep(ctx, opts.Pace); err != nil {
				return resolved, "", err
			}
		}
		attempted++
		if opts.Budget != nil {
			*opts.Budget--
		}

		replacing := false
		if reverify {
			ok, verr := opts.Verify(ctx, t.YouTubeID)
			if verr != nil {
				report(Event{Kind: KindError, Artist: t.Artist, Title: t.Title, Err: verr})
				continue
			}
			if ok {
				yes := true
				cachePut(key, rcache.Entry{VideoID: t.YouTubeID, Source: "cache", Embeddable: &yes, ResolvedAt: now(), CheckedAt: now()})
				report(Event{Kind: KindKept, Artist: t.Artist, Title: t.Title, VideoID: t.YouTubeID})
				continue
			}
			replacing = true
			t.YouTubeID = ""
		}

		res, rerr := r.Resolve(ctx, *t)
		if errors.Is(rerr, ErrQuotaExceeded) {
			return resolved, StopQuota, nil
		}
		if errors.Is(rerr, ErrRateLimited) {
			return resolved, StopRateLimit, nil
		}
		if rerr != nil {
			report(Event{Kind: KindError, Artist: t.Artist, Title: t.Title, Err: rerr})
			continue
		}

		if res.VideoID != "" {
			t.YouTubeID = res.VideoID
			resolved++
			kind := KindResolved
			if replacing {
				kind = KindReplaced
			}
			cachePut(key, rcache.Entry{VideoID: res.VideoID, Source: res.Source, Embeddable: res.Embeddable, ResolvedAt: now(), CheckedAt: now()})
			report(Event{Kind: kind, Artist: t.Artist, Title: t.Title, VideoID: res.VideoID, Source: res.Source})
			if err := persist(); err != nil {
				return resolved, "", err
			}
		} else if replacing {
			cachePut(key, rcache.Entry{VideoID: "", CheckedAt: now()})
			report(Event{Kind: KindRemoved, Artist: t.Artist, Title: t.Title})
			if err := persist(); err != nil {
				return resolved, "", err
			}
		} else {
			cachePut(key, rcache.Entry{VideoID: "", CheckedAt: now()})
			report(Event{Kind: KindMiss, Artist: t.Artist, Title: t.Title})
		}
	}
	return resolved, "", nil
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/youtube/`
Expected: PASS (all new cache tests + existing tests).

- [ ] **Step 5: Commit**

```bash
make format && make lint
git add internal/youtube/resolve.go internal/youtube/cache_test.go
git commit -m "feat(youtube): consult resolution cache in Resolve

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Wire the cache into `resolve youtube`

**Files:**
- Modify: `cmd/resolve.go`
- Modify: `cmd/root.go` (viper defaults)
- Modify: `.gitignore`

**Interfaces:**
- Consumes: `rcache.Open`, `youtube.ResolveOptions{Cache, MissTTL, EmbedTTL}`.
- Produces: `func defaultCachePath() string`; `func openCache() (*rcache.DB, error)` (nil, nil when `--no-cache`); package vars `resolveNoCache bool`.

- [ ] **Step 1: Add viper defaults**

In `cmd/root.go` `initConfig`, after the existing `SetDefault` calls:

```go
	viper.SetDefault("cache_path", "")            // empty → defaultCachePath()
	viper.SetDefault("cache_miss_ttl", "720h")    // 30d negative-result TTL
	viper.SetDefault("cache_embed_ttl", "720h")   // 30d embeddability TTL
```

- [ ] **Step 2: Add cache helpers + flag in `cmd/resolve.go`**

Add imports `"github.com/lmorchard/byom-sync/internal/rcache"` and (for `defaultCachePath`) `"path/filepath"` (already imported) and `"os"` (already imported). Add:

```go
var resolveNoCache bool

// defaultCachePath mirrors the auth config-dir logic: $XDG_CONFIG_HOME/byom-sync
// (or ~/.config/byom-sync), file cache.db.
func defaultCachePath() string {
	base := ""
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		base = filepath.Join(v, "byom-sync")
	} else if home, err := os.UserHomeDir(); err == nil {
		base = filepath.Join(home, ".config", "byom-sync")
	} else {
		base = "byom-sync"
	}
	return filepath.Join(base, "cache.db")
}

// openCache opens the resolution cache unless --no-cache is set (then nil,nil).
func openCache() (*rcache.DB, error) {
	if resolveNoCache {
		return nil, nil
	}
	path := viper.GetString("cache_path")
	if path == "" {
		path = defaultCachePath()
	}
	return rcache.Open(path)
}
```

Register the flag in `init()`:

```go
	resolveYouTubeCmd.Flags().BoolVar(&resolveNoCache, "no-cache", false, "bypass the resolution cache (pure network resolution)")
```

- [ ] **Step 3: Use the cache in `runResolveYouTube`**

After the resolver chain is built and before the per-file loop, open the cache and defer close; wire TTLs and pass into `ResolveOptions`:

```go
	cache, err := openCache()
	if err != nil {
		return fmt.Errorf("open cache: %w", err)
	}
	if cache != nil {
		defer func() { _ = cache.Close() }()
	}
	missTTL := viper.GetDuration("cache_miss_ttl")
	embedTTL := viper.GetDuration("cache_embed_ttl")
```

In the `youtube.Resolve(...)` call add the fields (only addition — the cache is nil when `--no-cache`, and `youtube.Resolve` requires a non-nil interface value to act, so pass `cache` typed as the interface only when non-nil):

```go
		opts := youtube.ResolveOptions{
			Budget:     budget,
			Pace:       resolveDelay,
			Report:     report,
			OnResolved: onResolved,
			Reresolve:  resolveReresolve,
			Verify:     ytdlp.IsEmbeddable,
			MissTTL:    missTTL,
			EmbedTTL:   embedTTL,
		}
		if cache != nil {
			opts.Cache = cache
		}
		n, stop, err := youtube.Resolve(ctx, chain, &p, opts)
```

> Note: assign `opts.Cache = cache` only inside the `if cache != nil` guard. Assigning a nil `*rcache.DB` to the interface field would make `opts.Cache != nil` true (typed-nil), and `Resolve` would call methods on a nil DB. The guard keeps the interface field a true nil.

- [ ] **Step 4: gitignore the cache DB**

Append to `.gitignore`:

```
# resolution cache (rebuildable via `resolve prime`)
cache.db
```

- [ ] **Step 5: Build + manual smoke**

Run: `make build && ./byom-sync resolve youtube --help`
Expected: build OK; help lists `--no-cache`.

Run: `./byom-sync resolve youtube --input playlists --limit 1`
Expected: runs; on a second run of the same track the log shows a `cache`-sourced resolution or a skipped miss (with `--verbose`).

- [ ] **Step 6: Commit**

```bash
make format && make lint && make test
git add cmd/resolve.go cmd/root.go .gitignore
git commit -m "feat(resolve): wire resolution cache into resolve youtube (+ --no-cache)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: `resolve prime`

**Files:**
- Modify: `cmd/resolve.go`
- Test: `cmd/resolve_prime_test.go`

**Interfaces:**
- Consumes: `hubPaths`, `playlist.LoadFile`, `rcache.DB.Put`, `openCache`.
- Produces: `resolvePrimeCmd`; package var `primeAssumeEmbeddable bool` (default true); package var `primeInput string`.

- [ ] **Step 1: Write the failing test**

Create `cmd/resolve_prime_test.go`. It exercises the prime *logic* against a temp cache + temp hub. Factor the loop into a testable function `primeCache(paths []string, db *rcache.DB, assumeEmbeddable bool, now time.Time) (seeded, dupes int, err error)` and test that:

```go
package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/lmorchard/byom-sync/internal/rcache"
)

func TestPrimeCacheSeedsAndCountsDupes(t *testing.T) {
	dir := t.TempDir()
	// two playlists sharing one ISRC track (dup), plus one unique + one without id.
	write := func(name string, p playlist.Playlist) string {
		path := filepath.Join(dir, name)
		if err := playlist.SaveFile(path, p); err != nil {
			t.Fatal(err)
		}
		return path
	}
	shared := playlist.Track{Artist: "A", Title: "T", ISRC: "US1", YouTubeID: "yt1"}
	p1 := write("a.yaml", playlist.Playlist{SpotifyID: "a", Tracks: []playlist.Track{
		shared,
		{Artist: "B", Title: "U", ISRC: "US2", YouTubeID: "yt2"},
		{Artist: "C", Title: "V", ISRC: "US3"}, // no id — skipped
	}})
	p2 := write("b.yaml", playlist.Playlist{SpotifyID: "b", Tracks: []playlist.Track{shared}}) // dup

	db, err := rcache.Open(filepath.Join(dir, "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	now := time.Date(2026, 7, 9, 0, 0, 0, 0, time.UTC)
	seeded, dupes, err := primeCache([]string{p1, p2}, db, true, now)
	if err != nil {
		t.Fatal(err)
	}
	if seeded != 2 { // US1 and US2 (US3 has no id)
		t.Fatalf("seeded=%d want 2", seeded)
	}
	if dupes != 1 { // US1 seen twice
		t.Fatalf("dupes=%d want 1", dupes)
	}
	e, ok := db.Get(playlist.Track{ISRC: "US1"}.Key())
	if !ok || e.VideoID != "yt1" || e.Source != "prime" {
		t.Fatalf("US1 entry: ok=%v %+v", ok, e)
	}
	if e.Embeddable == nil || !*e.Embeddable {
		t.Fatalf("assume-embeddable: want true, got %v", e.Embeddable)
	}
}

func TestPrimeCacheNoAssumeLeavesEmbeddableNil(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.yaml")
	if err := playlist.SaveFile(path, playlist.Playlist{SpotifyID: "a", Tracks: []playlist.Track{
		{Artist: "A", Title: "T", ISRC: "US1", YouTubeID: "yt1"},
	}}); err != nil {
		t.Fatal(err)
	}
	db, _ := rcache.Open(filepath.Join(dir, "cache.db"))
	defer func() { _ = db.Close() }()
	if _, _, err := primeCache([]string{path}, db, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	e, _ := db.Get(playlist.Track{ISRC: "US1"}.Key())
	if e.Embeddable != nil {
		t.Fatalf("no-assume: want nil embeddable, got %v", *e.Embeddable)
	}
	_ = os.Remove(path)
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./cmd/ -run TestPrimeCache`
Expected: FAIL — `undefined: primeCache`.

- [ ] **Step 3: Implement `primeCache` + command**

Add to `cmd/resolve.go`:

```go
var (
	primeInput            string
	primeAssumeEmbeddable bool
)

// primeCache seeds the cache from tracks that already have a youtube_id. It
// returns how many keys were seeded and how many cross-playlist duplicates were
// collapsed onto an already-seen key.
func primeCache(paths []string, db *rcache.DB, assumeEmbeddable bool, now time.Time) (seeded, dupes int, err error) {
	seen := map[string]bool{}
	for _, path := range paths {
		p, lerr := playlist.LoadFile(path)
		if lerr != nil {
			return seeded, dupes, fmt.Errorf("load %s: %w", path, lerr)
		}
		for _, t := range p.Tracks {
			if t.YouTubeID == "" {
				continue
			}
			key := t.Key()
			if seen[key] {
				dupes++
			} else {
				seen[key] = true
				seeded++
			}
			e := rcache.Entry{VideoID: t.YouTubeID, Source: "prime", ResolvedAt: now, CheckedAt: now}
			if assumeEmbeddable {
				yes := true
				e.Embeddable = &yes
			}
			if perr := db.Put(key, e); perr != nil {
				return seeded, dupes, fmt.Errorf("cache put: %w", perr)
			}
		}
	}
	return seeded, dupes, nil
}

var resolvePrimeCmd = &cobra.Command{
	Use:   "prime",
	Short: "Seed the resolution cache from tracks that already have a youtube_id",
	Long: `Walk the hub and upsert every track that already has a youtube_id into the
resolution cache, so subsequent resolve runs reuse that work instead of hitting
the network. Positive entries only — misses can't be reconstructed from the YAML.

--assume-embeddable (default true) marks seeded ids as embeddable, so --reresolve
trusts them for the embed TTL window. Set --assume-embeddable=false to seed them
unverified (the next --reresolve then checks each once). The default trusts the
hub, which was resolved by the embeddable-preferring resolver; the tradeoff is
that a video gone private/dead since resolution isn't caught until the TTL lapses
or you clear the cache.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if resolveNoCache {
			return fmt.Errorf("--no-cache is incompatible with prime")
		}
		input := primeInput
		if input == "" {
			input = viper.GetString("dir")
		}
		paths, err := hubPaths(input)
		if err != nil {
			return err
		}
		db, err := openCache()
		if err != nil {
			return fmt.Errorf("open cache: %w", err)
		}
		defer func() { _ = db.Close() }()
		seeded, dupes, err := primeCache(paths, db, primeAssumeEmbeddable, time.Now())
		if err != nil {
			return err
		}
		log.Infof("primed cache: %d keys seeded, %d cross-playlist duplicates collapsed (assume-embeddable=%v)", seeded, dupes, primeAssumeEmbeddable)
		return nil
	},
}
```

Register in `init()`:

```go
	resolveCmd.AddCommand(resolvePrimeCmd)
	resolvePrimeCmd.Flags().StringVar(&primeInput, "input", "", "hub YAML file or directory (default: config dir)")
	resolvePrimeCmd.Flags().BoolVar(&primeAssumeEmbeddable, "assume-embeddable", true, "mark seeded ids as embeddable (skip re-verify within the embed TTL)")
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./cmd/ -run TestPrimeCache`
Expected: PASS.

- [ ] **Step 5: Manual smoke**

Run: `make build && ./byom-sync resolve prime --input playlists`
Expected: logs e.g. `primed cache: ~4750 keys seeded, ~2600 ... duplicates collapsed`.

- [ ] **Step 6: Commit**

```bash
make format && make lint && make test
git add cmd/resolve.go cmd/resolve_prime_test.go
git commit -m "feat(resolve): resolve prime seeds cache from resolved ids

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: `resolve cache stats` and `resolve cache clear`

**Files:**
- Modify: `cmd/resolve.go`

**Interfaces:**
- Consumes: `openCache`, `rcache.DB.Stats`, `rcache.DB.Clear`.
- Produces: `resolveCacheCmd` (parent), `resolveCacheStatsCmd`, `resolveCacheClearCmd`; package var `clearMissesOnly bool`.

- [ ] **Step 1: Implement the subcommands**

Add to `cmd/resolve.go`:

```go
var clearMissesOnly bool

var resolveCacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Inspect or clear the resolution cache",
}

var resolveCacheStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show resolution cache coverage",
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openCache()
		if err != nil {
			return fmt.Errorf("open cache: %w", err)
		}
		defer func() { _ = db.Close() }()
		missTTL := viper.GetDuration("cache_miss_ttl")
		s, err := db.Stats(time.Now().Add(-missTTL))
		if err != nil {
			return err
		}
		log.Infof("cache: %d entries — %d resolved, %d misses (%d expired, re-attempted next run)",
			s.Total, s.Positive, s.Negative, s.ExpiredNegative)
		return nil
	},
}

var resolveCacheClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Delete cache entries (all, or --misses-only)",
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openCache()
		if err != nil {
			return fmt.Errorf("open cache: %w", err)
		}
		defer func() { _ = db.Close() }()
		n, err := db.Clear(clearMissesOnly)
		if err != nil {
			return err
		}
		what := "all entries"
		if clearMissesOnly {
			what = "miss entries"
		}
		log.Warnf("cleared %d %s from the resolution cache", n, what)
		return nil
	},
}
```

Register in `init()`:

```go
	resolveCmd.AddCommand(resolveCacheCmd)
	resolveCacheCmd.AddCommand(resolveCacheStatsCmd)
	resolveCacheCmd.AddCommand(resolveCacheClearCmd)
	resolveCacheClearCmd.Flags().BoolVar(&clearMissesOnly, "misses-only", false, "clear only negative (miss) entries, keeping resolved ids")
```

> `openCache` returns nil when `--no-cache` is set, but `--no-cache` is a flag on `resolve youtube`, not on these subcommands, so `db` is always non-nil here.

- [ ] **Step 2: Build + manual smoke**

Run: `make build && ./byom-sync resolve cache stats`
Expected: prints entry counts (after a prime, matches the primed totals).

Run: `./byom-sync resolve cache clear --misses-only`
Expected: `cleared N miss entries ...`.

- [ ] **Step 3: Commit**

```bash
make format && make lint && make test
git add cmd/resolve.go
git commit -m "feat(resolve): resolve cache stats/clear subcommands

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Documentation

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`

- [ ] **Step 1: Document the cache**

In `README.md`, under the resolve section, add a short subsection covering: what the cache is (accelerator, gitignored, `$XDG_CONFIG_HOME/byom-sync/cache.db`), the `prime` workflow (`resolve prime` once to seed from existing ids), `--no-cache`, TTL config keys (`cache_miss_ttl`, `cache_embed_ttl`, default 30d), and `resolve cache stats`/`clear`.

In `AGENTS.md`, under "Layout" add `internal/rcache/` (SQLite resolution cache), and under "Commands"/relevant section note the new `resolve prime`, `resolve cache stats`, `resolve cache clear`, and `--no-cache`. Note the `modernc.org/sqlite` dependency (the scaffold's `--no-database` no longer strictly holds — the hub is still files; the cache is an optional index).

- [ ] **Step 2: Commit**

```bash
git add README.md AGENTS.md
git commit -m "docs: document the resolution cache and new resolve subcommands

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Self-Review

**Spec coverage:**
- SQLite storage + schema → Task 1. ✓
- Positive/negative/embeddability semantics + TTLs → Task 3 (logic), Task 1 (storage). ✓
- Resolve integration (lookup/skip/put, budget/pace exemption, `--reresolve` embed skip, `--no-cache`, nil = unchanged) → Tasks 3–4. ✓
- Priming + `--assume-embeddable` default-true + dup counting → Task 5. ✓
- `cache stats` / `cache clear --misses-only` → Task 6. ✓
- Config keys `cache_path`/`cache_miss_ttl`/`cache_embed_ttl` → Task 4. ✓
- Clock seam for deterministic TTL tests → `ResolveOptions.Now` (Task 3); `primeCache` takes `now` (Task 5). ✓ (Refinement vs spec: the clock seam lives in the resolve layer, not inside `rcache` — `rcache` is a dumb store and callers stamp timestamps. Same testability, cleaner separation.)
- Gitignore cache.db → Task 4. ✓
- Docs → Task 7. ✓

**Placeholder scan:** none — every code step has complete code.

**Type consistency:** `Entry`, `Cache`, `Result.Embeddable`, `primeCache`, `openCache`, `defaultCachePath` used with identical signatures across tasks. `Source:"cache"` used consistently for cache-served and verify-kept positive writes.

**Note on `resolved_at`:** for positive writes on the reverify-keep path, `ResolvedAt` is stamped `now()` (last-confirmed time) rather than the original resolution time. `resolved_at` is informational only — no TTL depends on it — so this is acceptable; the spec's "when last obtained" is relaxed to "when the positive entry was last written/confirmed."
