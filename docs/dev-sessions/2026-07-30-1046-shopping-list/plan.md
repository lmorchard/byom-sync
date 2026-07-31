# `resolve purchase` Implementation Plan (Phase A)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `byom-sync resolve purchase`, a tiered enrichment pass that resolves a best-effort purchase URL per album and writes it into the hub and JSPF.

**Architecture:** Three independent sources (Bandcamp → iTunes → Discogs) behind one `Source` interface, run **tier-at-a-time** — each tier is a full pass over everything still unresolved, with its own rate limit. A match-confidence gate rejects wrong-album results. Results cache in a new `purchase_cache` SQLite table keyed by `source + album key`; the hub's `purchase_url` is what tells a later tier a track is already done.

**Tech Stack:** Go 1.25 · Cobra · `modernc.org/sqlite` · stdlib `net/http` + `encoding/json`. No new dependencies.

**Scope:** byom-sync only. The byom-player panel (Phase B, lmorchard/byom-player#55) is a separate plan in that repo.

## Global Constraints

- Format with `gofumpt`; lint with golangci-lint **v2.12.2** (pinned in `Makefile` and `.github/workflows/ci.yml`).
- **errcheck is strict.** Use `_ =` for intentionally-ignored returns.
- Verify with `make lint && make test && make build` and read the output.
- Branch → PR → CI green → merge. Never push to `main`. Work in a worktree under `./.claude/worktrees/`.
- Commit trailer: `Co-Authored-By:` naming the model that wrote the commit.
- Confidence threshold is **0.8**, reusing `spotifyenrich.DefaultThreshold`.
- Rate limits per source: Bandcamp ~1/s (politeness), iTunes ~3s (≈20/min), Discogs ~2.5s (25/min unauth; 60/min with a token).
- MusicBrainz is **not** a purchase source. It was measured at 3% with zero unique contribution and deliberately excluded. Do not add it.
- Never construct a store URL by hand. Use the URL the API returns. Two invented patterns (Bleep, Boomkat) were checked and did not work.

---

## File Structure

**New — `internal/match/`** (extracted so both `spotifyenrich` and `purchase` can score)
- `match.go` — `Norm`, `Sim`, `Levenshtein`
- `match_test.go`

**New — `internal/purchase/`** (one file per source; a dead tier becomes a deletion)
- `types.go` — `Source`, `Query`, `Result`, `Kind`, `Query.Key()`, `Score`
- `normalize.go` — `FirstArtist`, `CleanAlbum`
- `bandcamp.go`, `itunes.go`, `discogs.go` — one `Source` each
- `resolve.go` — the pass loop: `Resolve`, `Options`, `Event`, `Cache`
- matching `_test.go` for each, plus `testdata/*.json` fixtures

**Modified**
- `internal/spotifyenrich/score.go` — delegate to `internal/match`
- `internal/rcache/purchase.go` (new) + `rcache.go` — `purchase_cache` table, `Clear`
- `internal/playlist/types.go` — `Track.PurchaseURL`
- `internal/playlist/merge.go` — `adoptLocalFields` carries it (**or sync deletes it**)
- `internal/export/jspf.go` — emit `purchase_url`
- `internal/config/config.go` — `discogs_token`
- `cmd/resolve.go` — `resolve purchase`, plus fan-out in `runResolveAll`
- `README.md`, `AGENTS.md`

---

### Task 1: Extract `sim`/`norm` into `internal/match`

Pure refactor. `spotifyenrich` keeps identical behaviour; `purchase` needs the same primitives and must not copy them.

**Files:**
- Create: `internal/match/match.go`, `internal/match/match_test.go`
- Modify: `internal/spotifyenrich/score.go:60-158` (delete the helpers, delegate)

**Interfaces:**
- Produces: `match.Norm(string) string`, `match.Sim(a, b string) float64`. `Sim` expects already-normalized input.

- [ ] **Step 1: Write the failing test**

Create `internal/match/match_test.go`:

```go
package match

import "testing"

func TestNorm(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"The Beatles", "the beatles"},
		{"Sgt. Pepper's!", "sgt pepper s"},
		{"  Spaced   Out  ", "spaced out"},
		{"", ""},
	} {
		if got := Norm(tc.in); got != tc.want {
			t.Errorf("Norm(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSim(t *testing.T) {
	for _, tc := range []struct {
		name, a, b string
		min, max   float64
	}{
		{"identical", "come together", "come together", 1.0, 1.0},
		{"containment", "come together", "come together remastered 2019", 1.0, 1.0},
		{"both empty", "", "", 1.0, 1.0},
		{"one empty", "abc", "", 0.0, 0.0},
		{"unrelated", "hellbilly deluxe", "the sinister urge", 0.0, 0.6},
		{"short pattern strict", "go", "going home", 0.0, 0.5},
	} {
		got := Sim(tc.a, tc.b)
		if got < tc.min || got > tc.max {
			t.Errorf("%s: Sim(%q,%q) = %v, want in [%v,%v]", tc.name, tc.a, tc.b, got, tc.min, tc.max)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/match/ -v`
Expected: FAIL — package `match` does not exist.

- [ ] **Step 3: Create the package**

Create `internal/match/match.go` by moving the bodies of `norm`, `sim`, `levenshtein`, `min3` and the `minContainLen` const out of `internal/spotifyenrich/score.go` verbatim, exporting the first three. Copy the existing doc comments across — they explain the partial-ratio design and are worth keeping.

```go
// Package match provides normalized string similarity shared by the enrichment
// and purchase-link resolvers. Extracted from spotifyenrich so both score
// candidate matches the same way.
package match

import (
	"strings"
	"unicode"
)

// Norm lowercases and reduces a string to space-separated alphanumeric tokens,
// so punctuation and casing don't distort similarity.
func Norm(s string) string {
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

// minContainLen is the shortest a pattern string (the shorter of the two inputs
// to Sim) may be for the containment/sliding-window path to apply. Below this
// length, a short token can trivially appear as a substring of almost any longer
// string (e.g. "go" inside "going home"), which would spuriously score 1.0.
// Short patterns instead fall back to a stricter, symmetric full-string edit
// ratio. Tunable.
const minContainLen = 5

// Sim is a 0..1 similarity that rewards the shorter of the two strings matching
// a contiguous run of the longer one — a "partial ratio". This keeps loosely
// authored strings scoring high against fuller catalog strings ("come together"
// vs "come together remastered 2019") while wrong matches stay low. Two empty
// strings are identical (1.0); one empty and one not is 0.0. Inputs are expected
// already normalized (see Norm).
func Sim(a, b string) float64 {
	if a == b {
		return 1.0
	}
	if a == "" || b == "" {
		return 0.0
	}
	ra, rb := []rune(a), []rune(b)
	if len(ra) > len(rb) {
		ra, rb = rb, ra // ra is the shorter string — the pattern
	}
	if len(ra) < minContainLen {
		d := Levenshtein(ra, rb)
		return 1.0 - float64(d)/float64(len(rb))
	}
	best := 0.0
	for i := 0; i+len(ra) <= len(rb); i++ {
		d := Levenshtein(ra, rb[i:i+len(ra)])
		r := 1.0 - float64(d)/float64(len(ra))
		if r > best {
			best = r
			if best == 1.0 {
				break
			}
		}
	}
	return best
}

// Levenshtein computes edit distance between two rune slices.
func Levenshtein(a, b []rune) int {
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
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

- [ ] **Step 4: Run the new package's tests**

Run: `go test ./internal/match/ -v`
Expected: PASS.

- [ ] **Step 5: Delegate from spotifyenrich**

In `internal/spotifyenrich/score.go`: delete `norm`, `sim`, `levenshtein`, `min3` and the `minContainLen` const. Drop the now-unused `strings` and `unicode` imports, add `"github.com/lmorchard/byom-sync/internal/match"`. Add thin unexported aliases so the rest of the file is untouched:

```go
func norm(s string) string     { return match.Norm(s) }
func sim(a, b string) float64  { return match.Sim(a, b) }
```

- [ ] **Step 6: Verify spotifyenrich behaviour is unchanged**

Run: `go test ./internal/spotifyenrich/ -v`
Expected: PASS, with no test edits. If any test fails, the extraction changed behaviour — revert and redo the move verbatim.

- [ ] **Step 7: Lint and commit**

```bash
make lint && make test
git add internal/match internal/spotifyenrich/score.go
git commit -m "refactor: extract sim/norm into internal/match

The purchase-link resolver needs the same partial-ratio similarity that
spotifyenrich uses to gate candidate matches. Extract rather than copy.
Behaviour is unchanged; spotifyenrich's existing score tests pass without
modification."
```

---

### Task 2: `purchase_cache` table in rcache

**Files:**
- Create: `internal/rcache/purchase.go`, `internal/rcache/purchase_test.go`
- Modify: `internal/rcache/rcache.go:46-69` (register schema), `:159-188` (`Clear`)

**Interfaces:**
- Produces: `rcache.PurchaseEntry{URL, Source string; Score float64; CheckedAt time.Time}`, `(*DB).GetPurchase(key string) (PurchaseEntry, bool)`, `(*DB).PutPurchase(key string, e PurchaseEntry) error`, `(*DB).PurchaseStats(missCutoff time.Time) (Stats, error)`, `(*DB).ClearPurchaseSource(source string) (int64, error)`.
- Key format is the caller's business (Task 3 defines it as `source \t albumKey`); this layer treats it as opaque.

- [ ] **Step 1: Write the failing test**

Create `internal/rcache/purchase_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/rcache/ -run Purchase -v`
Expected: FAIL — `PurchaseEntry` undefined.

- [ ] **Step 3: Implement the table**

Create `internal/rcache/purchase.go`:

```go
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
```

- [ ] **Step 4: Register the schema and extend Clear**

In `internal/rcache/rcache.go`, inside `Open`, after the `artSchema` exec block, add the same shape:

```go
	if _, err := db.Exec(purchaseSchema); err != nil {
		_ = db.Close()
		return nil, err
	}
```

In `Clear`, after the `art_cache` block, add:

```go
	n4, err := del("purchase_cache", "url")
	if err != nil {
		return total, err
	}
	total += n4
```

Update `Clear`'s doc comment to say the resolution, enrichment, art, **and purchase** tables.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/rcache/ -v`
Expected: PASS, including the pre-existing tests.

- [ ] **Step 6: Commit**

```bash
make lint && make test
git add internal/rcache
git commit -m "feat(rcache): add purchase_cache table

Mirrors art_cache: URL, source, score, checked_at, with URL == \"\" as a
negative entry that expires via TTL. Keyed by \"<source>\\t<album>\" so each
tier owns its key space and ClearPurchaseSource can re-run one tier without
discarding the others."
```

---

### Task 3: Purchase types, keys, scoring, and query normalization

**Files:**
- Create: `internal/purchase/types.go`, `internal/purchase/normalize.go`, `internal/purchase/types_test.go`, `internal/purchase/normalize_test.go`

**Interfaces:**
- Consumes: `match.Norm`, `match.Sim` (Task 1); `spotifyenrich.DefaultThreshold`.
- Produces:
  - `type Kind string` with `KindAlbum = "album"`, `KindTrack = "track"`
  - `type Query struct { Artist, Album, Title string }`
  - `(Query) Kind() Kind` — `KindAlbum` when `Album != ""`, else `KindTrack`
  - `(Query) CacheKey(source string) string` — `source + "\t" + <album|track> + "\t" + normalized identity`
  - `(Query) Text() string` — the normalized free-text search string
  - `type Result struct { URL string; Kind Kind; Score float64 }`
  - `type Source interface { Name() string; Lookup(ctx context.Context, q Query) (Result, error) }`
  - `func Score(q Query, artist, album string) float64` — the combined score
  - `func Accept(q Query, artist, album string) (score float64, ok bool)` — the
    single acceptance decision; sources call this, not `Score`
  - `const Threshold = spotifyenrich.DefaultThreshold`
  - `const SubjectFloor = 0.70` — album/title similarity must clear this
    independently. A perfect artist match plus coincidental title overlap
    otherwise carries a wrong album over the combined threshold: measured,
    "Theatre Is Evil" vs "Piano Is Evil" scores 0.808 combined on a 0.615
    subject similarity. The floor is what rejects it.
  - `func FirstArtist(string) string`, `func CleanAlbum(string) string`

- [ ] **Step 1: Write the failing tests**

Create `internal/purchase/normalize_test.go`:

```go
package purchase

import "testing"

func TestFirstArtist(t *testing.T) {
	// Spotify joins collaborators with commas; the stores match a single name.
	// These strings are verbatim from the live hub.
	for _, tc := range []struct{ in, want string }{
		{"Cavedoll, Tim Phillips", "Cavedoll"},
		{"Sea Lemon, Benjamin Gibbard", "Sea Lemon"},
		{"Beach House", "Beach House"},
		{"  Spaced , Out ", "Spaced"},
		{"", ""},
	} {
		if got := FirstArtist(tc.in); got != tc.want {
			t.Errorf("FirstArtist(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCleanAlbum(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Crystals (feat. Benjamin Gibbard)", "Crystals"},
		{"Hellbilly Deluxe (Edited Version)", "Hellbilly Deluxe"},
		{"Fairytale (Deluxe Expanded Edition)", "Fairytale"},
		{"Sound Affects - Deluxe Edition", "Sound Affects"},
		{"The Queen Is Dead - 2011 Remaster", "The Queen Is Dead"},
		{"Once Twice Melody", "Once Twice Melody"},
		// Never strip down to nothing.
		{"(Untitled)", "(Untitled)"},
		{"", ""},
	} {
		if got := CleanAlbum(tc.in); got != tc.want {
			t.Errorf("CleanAlbum(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
```

Create `internal/purchase/types_test.go`:

```go
package purchase

import "testing"

func TestQueryKind(t *testing.T) {
	if got := (Query{Artist: "A", Album: "B"}).Kind(); got != KindAlbum {
		t.Errorf("Kind() = %q, want %q", got, KindAlbum)
	}
	if got := (Query{Artist: "A", Title: "T"}).Kind(); got != KindTrack {
		t.Errorf("Kind() = %q, want %q", got, KindTrack)
	}
}

func TestQueryCacheKeyIsSourceScoped(t *testing.T) {
	q := Query{Artist: "Beach House", Album: "Once Twice Melody"}
	if q.CacheKey("bandcamp") == q.CacheKey("itunes") {
		t.Error("cache keys must differ per source so each tier owns its key space")
	}
}

func TestQueryCacheKeyNormalizes(t *testing.T) {
	a := Query{Artist: "Beach House", Album: "Once Twice Melody"}
	b := Query{Artist: "  beach   house ", Album: "Once Twice Melody!"}
	if a.CacheKey("bandcamp") != b.CacheKey("bandcamp") {
		t.Error("cosmetically different but equivalent queries must share a cache key")
	}
}

// Album-scoped and track-scoped queries for the same artist must not collide.
func TestQueryCacheKeyScopeSeparation(t *testing.T) {
	album := Query{Artist: "Beach House", Album: "Superstar"}
	track := Query{Artist: "Beach House", Title: "Superstar"}
	if album.CacheKey("bandcamp") == track.CacheKey("bandcamp") {
		t.Error("album and track scopes must not share a cache key")
	}
}

func TestScore(t *testing.T) {
	q := Query{Artist: "Amanda Palmer", Album: "Theatre Is Evil"}
	exact := Score(q, "Amanda Palmer", "Theatre Is Evil")
	if exact < Threshold {
		t.Errorf("exact match scored %v, want >= %v", exact, Threshold)
	}
	// The real iTunes failure: a different album by the right artist.
	wrong := Score(q, "Amanda Palmer", "Piano Is Evil")
	if wrong >= Threshold {
		t.Errorf("wrong album scored %v, want < %v", wrong, Threshold)
	}
	// A fuller credit than we asked for should still pass — measured at 1.00.
	fuller := Score(q, "Amanda Palmer & The Grand Theft Orchestra", "Theatre Is Evil")
	if fuller < Threshold {
		t.Errorf("fuller artist credit scored %v, want >= %v", fuller, Threshold)
	}
}

// Real rejections from the measured run; each must stay below threshold.
func TestScoreRejectsMeasuredWrongAlbums(t *testing.T) {
	for _, tc := range []struct {
		q             Query
		artist, album string
	}{
		{Query{Artist: "Ride", Album: "Peace Sign"}, "Various Artists", "Classical Music for Zodiac Signs"},
		{Query{Artist: "Rob Zombie", Album: "Hellbilly Deluxe"}, "Rob Zombie", "The Sinister Urge"},
		{Query{Artist: "Sara Lov", Album: "I Already Love You"}, "Eddie Cochran", "Summertime Blues - EP"},
	} {
		if got := Score(tc.q, tc.artist, tc.album); got >= Threshold {
			t.Errorf("Score(%q/%q vs %q/%q) = %v, want < %v",
				tc.q.Artist, tc.q.Album, tc.artist, tc.album, got, Threshold)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/purchase/ -v`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement normalize.go**

```go
package purchase

import (
	"regexp"
	"strings"
)

// editionSuffix matches trailing edition/remaster noise that store catalogues
// generally don't carry, in either the " - Foo Edition" or "(Foo Edition)" form.
var editionSuffix = regexp.MustCompile(
	`(?i)\s*[-–]\s*(deluxe|remaster(ed)?|expanded|anniversary|special|edited)\b.*$` +
		`|\s*\((deluxe|remaster(ed)?|expanded|anniversary|special|edited|feat\.?)[^)]*\)\s*$`)

// trailingParen matches any trailing parenthetical, e.g. "(feat. X)".
var trailingParen = regexp.MustCompile(`\s*\([^)]*\)\s*$`)

// FirstArtist returns the first name from Spotify's comma-joined artist credit.
// The stores match a single primary artist; "Cavedoll, Tim Phillips" finds
// nothing while "Cavedoll" finds the record. Measured to rescue real misses.
func FirstArtist(s string) string {
	if i := strings.IndexByte(s, ','); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// CleanAlbum strips trailing parentheticals and edition markers. It never
// returns empty for a non-empty input — a title that is entirely parenthetical
// is left alone rather than erased.
func CleanAlbum(s string) string {
	out := strings.TrimSpace(editionSuffix.ReplaceAllString(s, ""))
	if out == "" {
		out = strings.TrimSpace(trailingParen.ReplaceAllString(s, ""))
	}
	if out == "" {
		return strings.TrimSpace(s)
	}
	return out
}
```

- [ ] **Step 4: Implement types.go**

```go
// Package purchase resolves a best-effort "where to buy this" URL for hub
// albums and tracks. Sources are tried tier-at-a-time by the caller, each as a
// full pass with its own rate limit; every fuzzy result is admitted only by a
// confidence gate, because stores will happily return a real but wrong album.
package purchase

import (
	"context"
	"strings"

	"github.com/lmorchard/byom-sync/internal/match"
	"github.com/lmorchard/byom-sync/internal/spotifyenrich"
)

// Kind distinguishes an album purchase from a single-track one. Bandcamp and
// iTunes both sell individual tracks, which is often what a shopping list wants.
type Kind string

const (
	KindAlbum Kind = "album"
	KindTrack Kind = "track"
)

// Threshold is the minimum Score for an accepted match. Shared with the Spotify
// enricher; empirically supported here — in a 31-album measured run accepted
// matches scored 1.00 and rejected ones 0.62 or below, so 0.8 sits in a wide gap.
const Threshold = spotifyenrich.DefaultThreshold

// Scoring weights. Artist and album matter equally for a purchase lookup, unlike
// track enrichment where the title dominates.
const (
	artistWeight = 0.5
	albumWeight  = 0.5
)

// Query is one purchase lookup. Album-scoped when Album is set, else track-scoped
// by Title. Callers pass already-normalized values (see FirstArtist/CleanAlbum).
type Query struct {
	Artist string
	Album  string
	Title  string
}

// Kind reports whether this is an album or a single-track lookup.
func (q Query) Kind() Kind {
	if q.Album != "" {
		return KindAlbum
	}
	return KindTrack
}

// subject returns whichever of Album/Title this query is about.
func (q Query) subject() string {
	if q.Album != "" {
		return q.Album
	}
	return q.Title
}

// Text is the free-text search string handed to a store's search endpoint.
func (q Query) Text() string {
	return strings.TrimSpace(q.Artist + " " + q.subject())
}

// CacheKey is the purchase_cache identity for this query under one source.
// Source-scoped so each tier owns its key space: a tier-1 miss must not stop
// tier 2 from trying. Scope-tagged so an album and a same-named track don't
// collide. Normalized so cosmetic differences share a row.
func (q Query) CacheKey(source string) string {
	return source + "\t" + string(q.Kind()) + "\t" +
		match.Norm(q.Artist) + "\t" + match.Norm(q.subject())
}

// Result is one store's answer. URL is empty for a clean miss.
type Result struct {
	URL   string
	Kind  Kind
	Score float64
}

// Source is one store. Implementations own their own HTTP calls and response
// shape; the caller owns pacing, caching, and tier order.
type Source interface {
	// Name is the stable identifier used in cache keys and CLI flags.
	Name() string
	// Lookup returns a Result whose URL is empty when the store has nothing
	// matching. A transport or decode failure is an error; "no match" is not.
	Lookup(ctx context.Context, q Query) (Result, error)
}

// Score rates a store's returned artist+album against the query, 0..1. This is
// the gate that stops a real-but-wrong album from becoming a purchase link:
// iTunes answers "amanda palmer theatre is evil" with "Piano Is Evil".
func Score(q Query, artist, album string) float64 {
	return artistWeight*match.Sim(match.Norm(q.Artist), match.Norm(artist)) +
		albumWeight*match.Sim(match.Norm(q.subject()), match.Norm(album))
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/purchase/ -v`
Expected: PASS.

If `TestScoreRejectsMeasuredWrongAlbums` fails, do **not** loosen the test — it encodes real store responses. Re-check the weights.

- [ ] **Step 6: Commit**

```bash
make lint && make test
git add internal/purchase
git commit -m "feat(purchase): Source interface, query keys, confidence gate

Scoring weights artist and album equally, unlike track enrichment where
title dominates. Tests encode the real wrong-album responses measured
during design (Piano Is Evil, The Sinister Urge, Classical Music for
Zodiac Signs) so a future weight change can't silently admit them.

Cache keys are source-scoped so a tier-1 miss doesn't stop tier 2, and
scope-tagged so an album and a same-named track don't collide."
```

---

### Task 4: Bandcamp source (tier 1)

**Files:**
- Create: `internal/purchase/bandcamp.go`, `internal/purchase/bandcamp_test.go`, `internal/purchase/testdata/bandcamp_hit.json`, `internal/purchase/testdata/bandcamp_empty.json`

**Interfaces:**
- Consumes: `Query`, `Result`, `Score`, `Threshold` (Task 3).
- Produces: `func NewBandcamp(client *http.Client, endpoint string) *Bandcamp`, implementing `Source` with `Name() == "bandcamp"`. `endpoint` empty means the real one — tests inject an `httptest` URL.

Endpoint: `POST https://bandcamp.com/api/bcsearch_public_api/1/autocomplete_elastic`, body `{"search_text": "...", "search_filter": "a"|"t", "full_page": false, "fan_id": null}`. Response: `{"auto": {"results": [{"type","name","band_name","item_url_path"}]}}`.

- [ ] **Step 1: Create fixtures**

`internal/purchase/testdata/bandcamp_hit.json` — trimmed from the real response:

```json
{"auto":{"results":[{"type":"a","id":702235397,"name":"Theatre Is Evil","band_name":"Amanda Palmer & The Grand Theft Orchestra","item_url_root":"https://amandapalmer.bandcamp.com","item_url_path":"https://amandapalmer.bandcamp.com/album/theatre-is-evil-2"}]}}
```

`internal/purchase/testdata/bandcamp_empty.json` — the real shape for a clean miss (verified against The Smiths' *The Queen Is Dead*):

```json
{"auto":{"results":[]}}
```

- [ ] **Step 2: Write the failing test**

Create `internal/purchase/bandcamp_test.go`:

```go
package purchase

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func serveFixture(t *testing.T, name string, capture *map[string]any) *httptest.Server {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			raw, _ := io.ReadAll(r.Body)
			m := map[string]any{}
			_ = json.Unmarshal(raw, &m)
			*capture = m
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestBandcampAlbumHit(t *testing.T) {
	var sent map[string]any
	srv := serveFixture(t, "bandcamp_hit.json", &sent)
	bc := NewBandcamp(srv.Client(), srv.URL)

	got, err := bc.Lookup(context.Background(), Query{Artist: "Amanda Palmer", Album: "Theatre Is Evil"})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	want := "https://amandapalmer.bandcamp.com/album/theatre-is-evil-2"
	if got.URL != want {
		t.Errorf("URL = %q, want %q", got.URL, want)
	}
	if got.Kind != KindAlbum {
		t.Errorf("Kind = %q, want %q", got.Kind, KindAlbum)
	}
	if got.Score < Threshold {
		t.Errorf("Score = %v, want >= %v", got.Score, Threshold)
	}
	if sent["search_filter"] != "a" {
		t.Errorf("search_filter = %v, want \"a\"", sent["search_filter"])
	}
}

// A clean zero-result miss must be a miss, never an error — measured behaviour
// for major-label catalogue that genuinely isn't on Bandcamp.
func TestBandcampCleanMiss(t *testing.T) {
	srv := serveFixture(t, "bandcamp_empty.json", nil)
	bc := NewBandcamp(srv.Client(), srv.URL)

	got, err := bc.Lookup(context.Background(), Query{Artist: "The Smiths", Album: "The Queen Is Dead"})
	if err != nil {
		t.Fatalf("clean miss must not error: %v", err)
	}
	if got.URL != "" {
		t.Errorf("URL = %q, want empty", got.URL)
	}
}

func TestBandcampTrackFilter(t *testing.T) {
	var sent map[string]any
	srv := serveFixture(t, "bandcamp_hit.json", &sent)
	bc := NewBandcamp(srv.Client(), srv.URL)

	if _, err := bc.Lookup(context.Background(), Query{Artist: "Beach House", Title: "Superstar"}); err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if sent["search_filter"] != "t" {
		t.Errorf("search_filter = %v, want \"t\" for a track query", sent["search_filter"])
	}
}

// A result that doesn't match the query must be rejected, not returned.
func TestBandcampBelowThresholdRejected(t *testing.T) {
	srv := serveFixture(t, "bandcamp_hit.json", nil)
	bc := NewBandcamp(srv.Client(), srv.URL)

	got, err := bc.Lookup(context.Background(), Query{Artist: "Nine Inch Nails", Album: "The Downward Spiral"})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.URL != "" {
		t.Errorf("URL = %q, want empty (fixture is a different album)", got.URL)
	}
}

func TestBandcampServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	bc := NewBandcamp(srv.Client(), srv.URL)

	if _, err := bc.Lookup(context.Background(), Query{Artist: "A", Album: "B"}); err == nil {
		t.Error("expected an error for HTTP 500")
	}
}

func TestBandcampName(t *testing.T) {
	if got := NewBandcamp(nil, "").Name(); got != "bandcamp" {
		t.Errorf("Name() = %q, want \"bandcamp\"", got)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/purchase/ -run Bandcamp -v`
Expected: FAIL — `NewBandcamp` undefined.

- [ ] **Step 4: Implement**

Create `internal/purchase/bandcamp.go`:

```go
package purchase

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// bandcampEndpoint is the search endpoint Bandcamp's own site calls. It is
// undocumented and unauthenticated: there is no public Bandcamp search API, and
// this is the only way to reach the catalogue. Measured as the best single
// source — one request, the exact album URL, and a clean zero-result miss rather
// than a wrong guess. Because it is unsanctioned it is deliberately one tier of
// a cascade: if it breaks or starts refusing traffic, the pass degrades to the
// remaining tiers instead of failing. Keep the request rate polite (~1/sec).
const bandcampEndpoint = "https://bandcamp.com/api/bcsearch_public_api/1/autocomplete_elastic"

// Bandcamp resolves purchase URLs from Bandcamp's search endpoint.
type Bandcamp struct {
	client   *http.Client
	endpoint string
}

// NewBandcamp returns a Bandcamp source. A zero endpoint means the real one;
// tests inject an httptest URL.
func NewBandcamp(client *http.Client, endpoint string) *Bandcamp {
	if client == nil {
		client = http.DefaultClient
	}
	if endpoint == "" {
		endpoint = bandcampEndpoint
	}
	return &Bandcamp{client: client, endpoint: endpoint}
}

func (*Bandcamp) Name() string { return "bandcamp" }

type bandcampResponse struct {
	Auto struct {
		Results []struct {
			Type        string `json:"type"`
			Name        string `json:"name"`
			BandName    string `json:"band_name"`
			ItemURLPath string `json:"item_url_path"`
		} `json:"results"`
	} `json:"auto"`
}

// Lookup searches Bandcamp for the query and returns the best-scoring result
// that clears the threshold. An empty result set is a miss, not an error.
func (b *Bandcamp) Lookup(ctx context.Context, q Query) (Result, error) {
	filter := "a"
	if q.Kind() == KindTrack {
		filter = "t"
	}
	payload, err := json.Marshal(map[string]any{
		"search_text":   q.Text(),
		"search_filter": filter,
		"full_page":     false,
		"fan_id":        nil,
	})
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.endpoint, bytes.NewReader(payload))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := b.client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("bandcamp search: status %d", resp.StatusCode)
	}

	var body bandcampResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Result{}, err
	}

	best := Result{Kind: q.Kind()}
	for _, r := range body.Auto.Results {
		if r.ItemURLPath == "" {
			continue
		}
		s, ok := Accept(q, r.BandName, r.Name)
		if !ok {
			continue // wrong record, or too weak a match to trust
		}
		if s > best.Score {
			best.Score, best.URL = s, r.ItemURLPath
		}
	}
	if best.URL == "" {
		return Result{Kind: q.Kind()}, nil // clean miss
	}
	return best, nil
}
```

Add the shared user agent to `types.go`:

```go
// userAgent identifies byom-sync to the stores. Discogs rejects default agents
// outright; the others are simply better behaved with a real one.
const userAgent = "byom-sync (+https://github.com/lmorchard/byom-sync)"
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/purchase/ -run Bandcamp -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
make lint && make test
git add internal/purchase
git commit -m "feat(purchase): Bandcamp source (tier 1)

Measured at 47% of hub albums across two independent samples, one request
each, returning the exact album URL. Fails clean: a major-label record
absent from Bandcamp returns zero results rather than a wrong guess, which
the tests pin down.

The endpoint is undocumented — Bandcamp has no public search API. It is
one tier of a cascade precisely so a break degrades the pass rather than
killing it."
```

---

### Task 5: iTunes source (tier 2)

**Files:**
- Create: `internal/purchase/itunes.go`, `internal/purchase/itunes_test.go`, `internal/purchase/testdata/itunes_wrong_album.json`, `internal/purchase/testdata/itunes_hit.json`, `internal/purchase/testdata/itunes_streamonly.json`

**Interfaces:**
- Produces: `func NewITunes(client *http.Client, endpoint string) *ITunes`, `Source` with `Name() == "itunes"`.

Endpoint: `GET https://itunes.apple.com/search?term=...&entity=album&limit=5`. Album fields: `artistName`, `collectionName`, `collectionViewUrl`, `collectionPrice`. Track queries use `entity=song` with `trackName`, `trackViewUrl`, `trackPrice`.

**Critical:** accept only when `Score >= Threshold` **and** the price is `> 0`. A `music.apple.com` link with no price is an Apple Music stream, not a purchase.

- [ ] **Step 1: Create fixtures**

`testdata/itunes_hit.json`:

```json
{"resultCount":1,"results":[{"artistName":"Clan of Xymox","collectionName":"Medusa","collectionViewUrl":"https://music.apple.com/us/album/medusa/123456","collectionPrice":9.99}]}
```

`testdata/itunes_wrong_album.json` — the real measured response for "amanda palmer theatre is evil":

```json
{"resultCount":1,"results":[{"artistName":"Amanda Palmer","collectionName":"Piano Is Evil","collectionViewUrl":"https://music.apple.com/us/album/piano-is-evil/1263366746","collectionPrice":9.99}]}
```

`testdata/itunes_streamonly.json` — right album, no purchase price:

```json
{"resultCount":1,"results":[{"artistName":"Rob Zombie","collectionName":"Hellbilly Deluxe","collectionViewUrl":"https://music.apple.com/us/album/hellbilly-deluxe/999","collectionPrice":-1}]}
```

- [ ] **Step 2: Write the failing test**

Create `internal/purchase/itunes_test.go`:

```go
package purchase

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func serveFile(t *testing.T, name string, capture *url.Values) *httptest.Server {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			*capture = r.URL.Query()
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestITunesHit(t *testing.T) {
	var q url.Values
	srv := serveFile(t, "itunes_hit.json", &q)
	it := NewITunes(srv.Client(), srv.URL)

	got, err := it.Lookup(context.Background(), Query{Artist: "Clan of Xymox", Album: "Medusa"})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.URL != "https://music.apple.com/us/album/medusa/123456" {
		t.Errorf("URL = %q", got.URL)
	}
	if q.Get("entity") != "album" {
		t.Errorf("entity = %q, want album", q.Get("entity"))
	}
}

// The failure that made the confidence gate mandatory: iTunes answers a
// "Theatre Is Evil" query with the real-but-different "Piano Is Evil".
func TestITunesRejectsWrongAlbum(t *testing.T) {
	srv := serveFile(t, "itunes_wrong_album.json", nil)
	it := NewITunes(srv.Client(), srv.URL)

	got, err := it.Lookup(context.Background(), Query{Artist: "Amanda Palmer", Album: "Theatre Is Evil"})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.URL != "" {
		t.Errorf("URL = %q, want empty — Piano Is Evil is a different album", got.URL)
	}
}

// A right-album result with no price is an Apple Music stream, not a purchase.
func TestITunesRejectsStreamOnly(t *testing.T) {
	srv := serveFile(t, "itunes_streamonly.json", nil)
	it := NewITunes(srv.Client(), srv.URL)

	got, err := it.Lookup(context.Background(), Query{Artist: "Rob Zombie", Album: "Hellbilly Deluxe"})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.URL != "" {
		t.Errorf("URL = %q, want empty — collectionPrice <= 0 means not purchasable", got.URL)
	}
}

func TestITunesTrackQueryUsesSongEntity(t *testing.T) {
	var q url.Values
	srv := serveFile(t, "itunes_hit.json", &q)
	it := NewITunes(srv.Client(), srv.URL)

	if _, err := it.Lookup(context.Background(), Query{Artist: "Beach House", Title: "Superstar"}); err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if q.Get("entity") != "song" {
		t.Errorf("entity = %q, want song", q.Get("entity"))
	}
}

func TestITunesName(t *testing.T) {
	if got := NewITunes(nil, "").Name(); got != "itunes" {
		t.Errorf("Name() = %q, want \"itunes\"", got)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/purchase/ -run ITunes -v`
Expected: FAIL — `NewITunes` undefined.

- [ ] **Step 4: Implement**

Create `internal/purchase/itunes.go`:

```go
package purchase

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

const itunesEndpoint = "https://itunes.apple.com/search"

// ITunes resolves purchase URLs from the iTunes Search API. No key required.
//
// iTunes Store music purchases are DRM-free (iTunes Plus, 256kbps AAC, since
// 2009) — it is Apple Music *streaming* that is protected. The distinction
// matters operationally: collectionViewUrl points at music.apple.com, which
// hosts both, so a result is only accepted when it carries a positive price.
// That is what separates "Apple has this to stream" from "Apple will sell you
// this," and it is most of the gap between this tier's 65% and 100%.
type ITunes struct {
	client   *http.Client
	endpoint string
}

// NewITunes returns an iTunes source. A zero endpoint means the real one.
func NewITunes(client *http.Client, endpoint string) *ITunes {
	if client == nil {
		client = http.DefaultClient
	}
	if endpoint == "" {
		endpoint = itunesEndpoint
	}
	return &ITunes{client: client, endpoint: endpoint}
}

func (*ITunes) Name() string { return "itunes" }

type itunesResponse struct {
	Results []struct {
		ArtistName        string  `json:"artistName"`
		CollectionName    string  `json:"collectionName"`
		CollectionViewURL string  `json:"collectionViewUrl"`
		CollectionPrice   float64 `json:"collectionPrice"`
		TrackName         string  `json:"trackName"`
		TrackViewURL      string  `json:"trackViewUrl"`
		TrackPrice        float64 `json:"trackPrice"`
	} `json:"results"`
}

// Lookup searches iTunes and returns the best priced result clearing the
// threshold. Query construction is deliberately the plain blended term: four
// constructions were measured (blended at limit 5 and 25, mixTerm, albumTerm)
// and none beat this one. albumTerm was actively worse — dropping the artist
// lets same-titled albums by other artists win. Do not "improve" this without
// re-measuring.
func (i *ITunes) Lookup(ctx context.Context, q Query) (Result, error) {
	entity := "album"
	if q.Kind() == KindTrack {
		entity = "song"
	}
	params := url.Values{
		"term":   {q.Text()},
		"entity": {entity},
		"limit":  {"5"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, i.endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := i.client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("itunes search: status %d", resp.StatusCode)
	}

	var body itunesResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Result{}, err
	}

	best := Result{Kind: q.Kind()}
	for _, r := range body.Results {
		name, link, price := r.CollectionName, r.CollectionViewURL, r.CollectionPrice
		if q.Kind() == KindTrack {
			name, link, price = r.TrackName, r.TrackViewURL, r.TrackPrice
		}
		if link == "" || price <= 0 {
			continue // stream-only or unpriced: not a purchase
		}
		s, ok := Accept(q, r.ArtistName, name)
		if !ok {
			continue
		}
		if s > best.Score {
			best.Score, best.URL = s, link
		}
	}
	if best.URL == "" {
		return Result{Kind: q.Kind()}, nil
	}
	return best, nil
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/purchase/ -run ITunes -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
make lint && make test
git add internal/purchase
git commit -m "feat(purchase): iTunes source (tier 2)

Measured at 65% of the tier-1 residue, one request each, every hit priced.
Accepts a result only when it clears the confidence gate AND carries a
positive price — a music.apple.com link without one is an Apple Music
stream, not a DRM-free purchase.

Query construction is the plain blended term on purpose: four variants
were measured and none beat it, with albumTerm actively worse because
dropping the artist lets same-titled albums by other artists win. Tests
pin the real Piano Is Evil response so the gate can't regress."
```

---

### Task 6: Discogs source (tier 3)

**Files:**
- Create: `internal/purchase/discogs.go`, `internal/purchase/discogs_test.go`, `internal/purchase/testdata/discogs_search.json`, `internal/purchase/testdata/discogs_release.json`, `internal/purchase/testdata/discogs_release_unavailable.json`

**Interfaces:**
- Produces: `func NewDiscogs(client *http.Client, baseURL, token string) *Discogs`, `Source` with `Name() == "discogs"`.

Two-step: `GET {base}/database/search?q=...&type=release&per_page=5` for candidates, then `GET` the winner's `resource_url` for authoritative `artists[].name` + `title` plus `num_for_sale`. Accept only when the **rescore** clears the threshold **and** `num_for_sale > 0`. `Authorization: Discogs token=<token>` when a token is set. A `User-Agent` is mandatory.

- [ ] **Step 1: Create fixtures**

`testdata/discogs_search.json` — note `title` is one `"Artist - Album"` string:

```json
{"results":[{"id":11662135,"title":"Rob Zombie - Hellbilly Deluxe","uri":"/release/11662135-Rob-Zombie-Hellbilly-Deluxe","resource_url":"RESOURCE_URL_PLACEHOLDER"}]}
```

The test rewrites `RESOURCE_URL_PLACEHOLDER` to the httptest URL.

`testdata/discogs_release.json`:

```json
{"id":11662135,"title":"Hellbilly Deluxe","artists":[{"name":"Rob Zombie"}],"uri":"/release/11662135-Rob-Zombie-Hellbilly-Deluxe","num_for_sale":36,"lowest_price":19.99}
```

`testdata/discogs_release_unavailable.json` — matches perfectly but nobody is selling it:

```json
{"id":999,"title":"Heaven Aggressed","artists":[{"name":"Daguerreotype"}],"uri":"/release/999-Daguerreotype-Heaven-Aggressed","num_for_sale":0,"lowest_price":null}
```

- [ ] **Step 2: Write the failing test**

Create `internal/purchase/discogs_test.go`:

```go
package purchase

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// serveDiscogs routes /database/search to the search fixture and everything
// else to the release fixture, rewriting the resource_url placeholder so the
// second request lands back on the same server.
func serveDiscogs(t *testing.T, releaseFixture string, gotAuth *string) *httptest.Server {
	t.Helper()
	read := func(n string) string {
		b, err := os.ReadFile(filepath.Join("testdata", n))
		if err != nil {
			t.Fatalf("read fixture %s: %v", n, err)
		}
		return string(b)
	}
	search, release := read("discogs_search.json"), read(releaseFixture)

	mux := http.NewServeMux()
	srv := httptest.NewUnstartedServer(mux)
	mux.HandleFunc("/database/search", func(w http.ResponseWriter, r *http.Request) {
		if gotAuth != nil {
			*gotAuth = r.Header.Get("Authorization")
		}
		_, _ = w.Write([]byte(strings.ReplaceAll(
			search, "RESOURCE_URL_PLACEHOLDER", "http://"+srv.Listener.Addr().String()+"/releases/11662135")))
	})
	mux.HandleFunc("/releases/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(release))
	})
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

func TestDiscogsHitWithListings(t *testing.T) {
	srv := serveDiscogs(t, "discogs_release.json", nil)
	dg := NewDiscogs(srv.Client(), srv.URL, "")

	got, err := dg.Lookup(context.Background(), Query{Artist: "Rob Zombie", Album: "Hellbilly Deluxe"})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	want := "https://www.discogs.com/release/11662135-Rob-Zombie-Hellbilly-Deluxe"
	if got.URL != want {
		t.Errorf("URL = %q, want %q", got.URL, want)
	}
	// The rescore uses the release lookup's clean artists[]/title, which measured
	// 1.00 on every survivor — far better than splitting "Artist - Album".
	if got.Score < 1.0 {
		t.Errorf("Score = %v, want 1.0 from the authoritative fields", got.Score)
	}
}

// A release nobody is selling is a dead link — worse than no link at all.
func TestDiscogsRejectsNothingForSale(t *testing.T) {
	srv := serveDiscogs(t, "discogs_release_unavailable.json", nil)
	dg := NewDiscogs(srv.Client(), srv.URL, "")

	got, err := dg.Lookup(context.Background(), Query{Artist: "Daguerreotype", Album: "Heaven Aggressed"})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.URL != "" {
		t.Errorf("URL = %q, want empty — num_for_sale is 0", got.URL)
	}
}

func TestDiscogsSendsToken(t *testing.T) {
	var auth string
	srv := serveDiscogs(t, "discogs_release.json", &auth)
	dg := NewDiscogs(srv.Client(), srv.URL, "secret123")

	if _, err := dg.Lookup(context.Background(), Query{Artist: "Rob Zombie", Album: "Hellbilly Deluxe"}); err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if auth != "Discogs token=secret123" {
		t.Errorf("Authorization = %q", auth)
	}
}

func TestDiscogsNoTokenSendsNoAuth(t *testing.T) {
	var auth string
	srv := serveDiscogs(t, "discogs_release.json", &auth)
	dg := NewDiscogs(srv.Client(), srv.URL, "")

	if _, err := dg.Lookup(context.Background(), Query{Artist: "Rob Zombie", Album: "Hellbilly Deluxe"}); err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if auth != "" {
		t.Errorf("Authorization = %q, want empty", auth)
	}
}

func TestDiscogsName(t *testing.T) {
	if got := NewDiscogs(nil, "", "").Name(); got != "discogs" {
		t.Errorf("Name() = %q, want \"discogs\"", got)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/purchase/ -run Discogs -v`
Expected: FAIL — `NewDiscogs` undefined.

- [ ] **Step 4: Implement**

Create `internal/purchase/discogs.go`:

```go
package purchase

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	discogsAPI  = "https://api.discogs.com"
	discogsSite = "https://www.discogs.com"
)

// Discogs resolves marketplace listings for secondhand physical media.
//
// This is a two-step lookup because the search response is insufficient on its
// own: it carries no num_for_sale, and its `title` is a single "Artist - Album"
// string that breaks whenever either side contains " - ". The release lookup
// supplies authoritative artists[]/title for rescoring plus the availability
// signal, so the second request pays for itself twice. Measured cost is ~1.4
// requests per album, since the lookup only fires for candidates that pass the
// first gate.
//
// A Discogs link is a listing for a used record: it does not fill a gap in a
// digital collection unless the record gets ripped, and a secondhand sale pays
// the artist nothing. That is why this is the last tier.
type Discogs struct {
	client  *http.Client
	baseURL string
	token   string
}

// NewDiscogs returns a Discogs source. A zero baseURL means the real API. token
// is optional: without one the rate limit is 25/min, with one 60/min.
func NewDiscogs(client *http.Client, baseURL, token string) *Discogs {
	if client == nil {
		client = http.DefaultClient
	}
	if baseURL == "" {
		baseURL = discogsAPI
	}
	return &Discogs{client: client, baseURL: strings.TrimRight(baseURL, "/"), token: token}
}

func (*Discogs) Name() string { return "discogs" }

type discogsSearch struct {
	Results []struct {
		Title       string `json:"title"`
		URI         string `json:"uri"`
		ResourceURL string `json:"resource_url"`
	} `json:"results"`
}

type discogsRelease struct {
	Title      string `json:"title"`
	URI        string `json:"uri"`
	NumForSale int    `json:"num_for_sale"`
	Artists    []struct {
		Name string `json:"name"`
	} `json:"artists"`
}

// get issues an authenticated GET and decodes JSON into out.
func (d *Discogs) get(ctx context.Context, rawURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	// Discogs rejects default user agents outright.
	req.Header.Set("User-Agent", userAgent)
	if d.token != "" {
		req.Header.Set("Authorization", "Discogs token="+d.token)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("discogs %s: status %d", rawURL, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Lookup searches Discogs, then confirms the best candidate against the release
// endpoint for an authoritative rescore and a marketplace-availability check.
func (d *Discogs) Lookup(ctx context.Context, q Query) (Result, error) {
	params := url.Values{
		"q":        {q.Text()},
		"type":     {"release"},
		"per_page": {"5"},
	}
	var search discogsSearch
	if err := d.get(ctx, d.baseURL+"/database/search?"+params.Encode(), &search); err != nil {
		return Result{}, err
	}

	// First pass: rank on the combined "Artist - Album" string. Fragile by
	// nature, so it only picks a candidate — never the final answer.
	bestScore, bestResource := 0.0, ""
	for _, r := range search.Results {
		if r.ResourceURL == "" {
			continue
		}
		artist, _, album := strings.Cut(r.Title, " - ")
		if album == "" {
			album = r.Title
		}
		// First pass only ranks candidates, so it uses the raw combined score
		// rather than Accept — the authoritative check happens below on the
		// release lookup's clean fields.
		if s := Score(q, artist, album); s > bestScore {
			bestScore, bestResource = s, r.ResourceURL
		}
	}
	if bestScore < Threshold || bestResource == "" {
		return Result{Kind: q.Kind()}, nil
	}

	// Second pass: authoritative fields + availability.
	var rel discogsRelease
	if err := d.get(ctx, bestResource, &rel); err != nil {
		return Result{}, err
	}
	if rel.NumForSale <= 0 {
		return Result{Kind: q.Kind()}, nil // nothing to buy — a dead link
	}
	names := make([]string, 0, len(rel.Artists))
	for _, a := range rel.Artists {
		names = append(names, a.Name)
	}
	score, ok := Accept(q, strings.Join(names, ", "), rel.Title)
	if !ok {
		return Result{Kind: q.Kind()}, nil
	}
	if rel.URI == "" {
		return Result{Kind: q.Kind()}, nil
	}
	// The URI comes from Discogs itself — never hand-construct a store path.
	return Result{URL: discogsSite + rel.URI, Kind: q.Kind(), Score: score}, nil
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/purchase/ -run Discogs -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
make lint && make test
git add internal/purchase
git commit -m "feat(purchase): Discogs source (tier 3)

Two-step by necessity: search carries no num_for_sale and its title is one
\"Artist - Album\" string that breaks on names containing \" - \". The release
lookup supplies authoritative artists[]/title for rescoring plus the
availability signal, so the second request pays for itself twice — the
rescore measured 1.00 on every survivor.

Rejects a release with num_for_sale == 0: a listing nobody is selling is a
dead link, worse than none. URL comes from the API's uri field, never
constructed."
```

---

### Task 7: The tier pass loop

**Files:**
- Create: `internal/purchase/resolve.go`, `internal/purchase/resolve_test.go`

**Interfaces:**
- Consumes: `Source`, `Query` (Task 3); `rcache.PurchaseEntry` (Task 2); `playlist.Playlist` (Task 8 adds `Track.PurchaseURL` — implement Task 8 first if compiling in order, or add the field as part of this task's first step).
- Produces:
  - `type Cache interface { GetPurchase(key string) (rcache.PurchaseEntry, bool); PutPurchase(key string, e rcache.PurchaseEntry) error }`
  - `type EventKind string` — `KindFilled`, `KindMissed`, `KindError`
  - `type Event struct { Kind EventKind; Artist, Album, URL, Source string; Err error }`
  - `type Options struct { Budget *int; Pace time.Duration; MissTTL time.Duration; Report func(Event); OnFilled func() error; Cache Cache; Now func() time.Time }`
  - `func Resolve(ctx context.Context, src Source, p *playlist.Playlist, opts Options) (filled int, err error)`

Modelled directly on `coverart.Resolve` (`internal/coverart/resolve.go`). Two behaviours it adds:

1. **Album fan-out.** One resolution fills `PurchaseURL` on *every* track sharing the album key, not just the one that triggered the lookup — the hub averages 1.93 tracks per album.
2. **In-run memo.** A key resolved earlier in the same pass is not looked up again even if the cache is disabled.

- [ ] **Step 1: Write the failing test**

Create `internal/purchase/resolve_test.go`:

```go
package purchase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/lmorchard/byom-sync/internal/rcache"
)

// fakeSource records calls and returns a canned answer per album.
type fakeSource struct {
	byAlbum map[string]string
	calls   int
	err     error
}

func (*fakeSource) Name() string { return "fake" }

func (f *fakeSource) Lookup(_ context.Context, q Query) (Result, error) {
	f.calls++
	if f.err != nil {
		return Result{}, f.err
	}
	if u, ok := f.byAlbum[q.Album]; ok {
		return Result{URL: u, Kind: KindAlbum, Score: 1.0}, nil
	}
	return Result{Kind: q.Kind()}, nil
}

type mapCache map[string]rcache.PurchaseEntry

func (m mapCache) GetPurchase(k string) (rcache.PurchaseEntry, bool) { e, ok := m[k]; return e, ok }
func (m mapCache) PutPurchase(k string, e rcache.PurchaseEntry) error {
	m[k] = e
	return nil
}

func trackOf(artist, album, title string) playlist.Track {
	return playlist.Track{Artist: artist, Album: album, Title: title}
}

// One lookup must fill every track on the same album.
func TestResolveFansOutAcrossAlbum(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{
		trackOf("Beach House", "Once Twice Melody", "Superstar"),
		trackOf("Beach House", "Once Twice Melody", "Pink Funeral"),
		trackOf("Beach House", "Once Twice Melody", "Runaway"),
	}}
	src := &fakeSource{byAlbum: map[string]string{
		"Once Twice Melody": "https://beachhouse.bandcamp.com/album/once-twice-melody",
	}}

	filled, err := Resolve(context.Background(), src, p, Options{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if filled != 3 {
		t.Errorf("filled = %d, want 3", filled)
	}
	if src.calls != 1 {
		t.Errorf("calls = %d, want 1 — album lookups must fan out", src.calls)
	}
	for i, tr := range p.Tracks {
		if tr.PurchaseURL == "" {
			t.Errorf("track %d has no purchase_url", i)
		}
	}
}

func TestResolveSkipsAlreadyResolved(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{
		{Artist: "A", Album: "B", Title: "T", PurchaseURL: "https://existing"},
	}}
	src := &fakeSource{}
	if _, err := Resolve(context.Background(), src, p, Options{}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if src.calls != 0 {
		t.Errorf("calls = %d, want 0 — an already-resolved track is how later tiers know to skip", src.calls)
	}
	if p.Tracks[0].PurchaseURL != "https://existing" {
		t.Error("existing purchase_url must not be overwritten")
	}
}

func TestResolveUsesCacheHit(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{trackOf("A", "B", "T")}}
	src := &fakeSource{}
	q := Query{Artist: "A", Album: "B"}
	cache := mapCache{q.CacheKey("fake"): {URL: "https://cached", Source: "fake", CheckedAt: time.Now()}}

	filled, err := Resolve(context.Background(), src, p, Options{Cache: cache})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if filled != 1 || p.Tracks[0].PurchaseURL != "https://cached" {
		t.Errorf("filled=%d url=%q, want 1 and the cached URL", filled, p.Tracks[0].PurchaseURL)
	}
	if src.calls != 0 {
		t.Errorf("calls = %d, want 0 on a cache hit", src.calls)
	}
}

func TestResolveHonorsFreshMiss(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{trackOf("A", "B", "T")}}
	src := &fakeSource{}
	q := Query{Artist: "A", Album: "B"}
	cache := mapCache{q.CacheKey("fake"): {CheckedAt: time.Now()}}

	if _, err := Resolve(context.Background(), src, p, Options{Cache: cache, MissTTL: time.Hour}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if src.calls != 0 {
		t.Errorf("calls = %d, want 0 — a fresh miss should be skipped", src.calls)
	}
}

func TestResolveRetriesExpiredMiss(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{trackOf("A", "B", "T")}}
	src := &fakeSource{}
	q := Query{Artist: "A", Album: "B"}
	cache := mapCache{q.CacheKey("fake"): {CheckedAt: time.Now().Add(-48 * time.Hour)}}

	if _, err := Resolve(context.Background(), src, p, Options{Cache: cache, MissTTL: time.Hour}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if src.calls != 1 {
		t.Errorf("calls = %d, want 1 — an expired miss should be retried", src.calls)
	}
}

func TestResolveRecordsMiss(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{trackOf("A", "Unknown", "T")}}
	cache := mapCache{}
	if _, err := Resolve(context.Background(), &fakeSource{}, p, Options{Cache: cache}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	e, ok := cache[Query{Artist: "A", Album: "Unknown"}.CacheKey("fake")]
	if !ok || e.URL != "" {
		t.Errorf("expected a negative cache entry, got %+v (ok=%v)", e, ok)
	}
}

func TestResolveBudgetStops(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{
		trackOf("A", "One", "T"), trackOf("B", "Two", "T"), trackOf("C", "Three", "T"),
	}}
	budget := 2
	src := &fakeSource{}
	if _, err := Resolve(context.Background(), src, p, Options{Budget: &budget}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if src.calls != 2 {
		t.Errorf("calls = %d, want 2 — budget must cap network attempts", src.calls)
	}
}

// A per-album failure is reported and skipped; the pass continues.
func TestResolveContinuesPastError(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{trackOf("A", "One", "T"), trackOf("B", "Two", "T")}}
	src := &fakeSource{err: errors.New("boom")}
	var errs int
	if _, err := Resolve(context.Background(), src, p, Options{
		Report: func(e Event) {
			if e.Kind == KindError {
				errs++
			}
		},
	}); err != nil {
		t.Fatalf("Resolve should not abort on per-album errors: %v", err)
	}
	if errs != 2 {
		t.Errorf("error events = %d, want 2", errs)
	}
}

// Albumless tracks resolve per-track and must not collide with each other.
func TestResolveAlbumlessTracks(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{
		trackOf("A", "", "First"), trackOf("A", "", "Second"),
	}}
	src := &fakeSource{}
	if _, err := Resolve(context.Background(), src, p, Options{}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if src.calls != 2 {
		t.Errorf("calls = %d, want 2 — albumless tracks are distinct lookups", src.calls)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/purchase/ -run Resolve -v`
Expected: FAIL — `Resolve` undefined (and `PurchaseURL` undefined until Task 8's field exists; add the field now if needed).

- [ ] **Step 3: Implement**

Create `internal/purchase/resolve.go`:

```go
package purchase

import (
	"context"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/lmorchard/byom-sync/internal/rcache"
)

// EventKind classifies one album's outcome, for narration.
type EventKind string

const (
	KindFilled EventKind = "filled"
	KindMissed EventKind = "missed"
	KindError  EventKind = "error"
)

// Event reports one album's outcome.
type Event struct {
	Kind   EventKind
	Artist string
	Album  string
	URL    string
	Source string
	Err    error
}

// Cache short-circuits resolution: a positive hit fills without a network call,
// a fresh miss skips the lookup. Satisfied by *rcache.DB.
type Cache interface {
	GetPurchase(key string) (rcache.PurchaseEntry, bool)
	PutPurchase(key string, e rcache.PurchaseEntry) error
}

// Options configures one tier's pass.
type Options struct {
	Budget   *int          // max network lookups this run; nil = unlimited
	Pace     time.Duration // pause between network lookups
	MissTTL  time.Duration // how long a cached miss suppresses a retry
	Report   func(Event)
	OnFilled func() error // persist hook, called after each filled album
	Cache    Cache
	Now      func() time.Time
}

// Resolve fills Track.PurchaseURL for every track in p that lacks one, using a
// single source. Callers run this once per tier, in order.
//
// One lookup fans out to every track sharing an album — the hub averages 1.93
// tracks per album, so this is most of the request saving. A track that already
// has a PurchaseURL is skipped, which is how a later tier knows an earlier one
// already answered.
//
// Per-album lookup errors are reported and skipped; only an OnFilled error
// aborts the run.
func Resolve(ctx context.Context, src Source, p *playlist.Playlist, opts Options) (filled int, err error) {
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
		if opts.OnFilled != nil {
			return opts.OnFilled()
		}
		return nil
	}
	fresh := func(ts time.Time, ttl time.Duration) bool {
		return ttl > 0 && now().Sub(ts) < ttl
	}

	// Group track indices by lookup identity so one answer fills them all.
	order := make([]string, 0, len(p.Tracks))
	groups := make(map[string][]int, len(p.Tracks))
	queries := make(map[string]Query, len(p.Tracks))
	for i := range p.Tracks {
		t := &p.Tracks[i]
		if t.PurchaseURL != "" {
			continue
		}
		q := Query{
			Artist: FirstArtist(t.Artist),
			Album:  CleanAlbum(t.Album),
			Title:  t.Title,
		}
		if q.Artist == "" || q.subject() == "" {
			continue // nothing to search on
		}
		key := q.CacheKey(src.Name())
		if _, seen := groups[key]; !seen {
			order = append(order, key)
			queries[key] = q
		}
		groups[key] = append(groups[key], i)
	}

	apply := func(key, url string) {
		for _, i := range groups[key] {
			p.Tracks[i].PurchaseURL = url
			filled++
		}
	}

	attempted := 0
	for _, key := range order {
		q := queries[key]

		if opts.Cache != nil {
			if e, ok := opts.Cache.GetPurchase(key); ok {
				switch {
				case e.URL != "":
					apply(key, e.URL)
					report(Event{Kind: KindFilled, Artist: q.Artist, Album: q.subject(), URL: e.URL, Source: "cache"})
					if perr := persist(); perr != nil {
						return filled, perr
					}
					continue
				case fresh(e.CheckedAt, opts.MissTTL):
					report(Event{Kind: KindMissed, Artist: q.Artist, Album: q.subject()})
					continue
				}
				// expired miss → fall through to a live lookup
			}
		}

		if opts.Budget != nil && *opts.Budget <= 0 {
			return filled, nil
		}
		if attempted > 0 && opts.Pace > 0 {
			if serr := sleep(ctx, opts.Pace); serr != nil {
				return filled, nil
			}
		}
		attempted++
		if opts.Budget != nil {
			*opts.Budget--
		}

		res, lerr := src.Lookup(ctx, q)
		if lerr != nil {
			report(Event{Kind: KindError, Artist: q.Artist, Album: q.subject(), Err: lerr})
			continue
		}
		if res.URL == "" {
			if opts.Cache != nil {
				_ = opts.Cache.PutPurchase(key, rcache.PurchaseEntry{Source: src.Name(), CheckedAt: now()})
			}
			report(Event{Kind: KindMissed, Artist: q.Artist, Album: q.subject()})
			continue
		}
		apply(key, res.URL)
		if opts.Cache != nil {
			_ = opts.Cache.PutPurchase(key, rcache.PurchaseEntry{
				URL: res.URL, Source: src.Name(), Score: res.Score, CheckedAt: now(),
			})
		}
		report(Event{Kind: KindFilled, Artist: q.Artist, Album: q.subject(), URL: res.URL, Source: src.Name()})
		if perr := persist(); perr != nil {
			return filled, perr
		}
	}
	return filled, nil
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

- [ ] **Step 4: Run tests**

Run: `go test ./internal/purchase/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
make lint && make test
git add internal/purchase
git commit -m "feat(purchase): tier pass loop with album fan-out

Modelled on coverart.Resolve — same budget, pacing, cache-TTL, and persist
hook shape. Adds album fan-out: one lookup fills every track sharing the
album, which is most of the request saving given the hub averages 1.93
tracks per album.

A track that already has purchase_url is skipped, which is how a later
tier knows an earlier one already answered."
```

---

### Task 8: Hub schema — `purchase_url` and the sync-clobber guard

**The dangerous task.** `purchase_url` is locally derived; Spotify never sends it back. `Merge` starts from the remote playlist, so any field not copied in `adoptLocalFields` is silently deleted on the next sync. This previously cost 8,292 `youtube_id`s in one playlist, with a zero exit code.

**Files:**
- Modify: `internal/playlist/types.go:63-88` (add field), `internal/playlist/merge.go:92-101` (`adoptLocalFields`) and its doc comment at `:16-33`
- Modify: `internal/playlist/merge_test.go` (add coverage)

**Interfaces:**
- Produces: `playlist.Track.PurchaseURL string` with yaml tag `purchase_url,omitempty`.

- [ ] **Step 1: Write the failing test**

Add to `internal/playlist/merge_test.go`:

```go
// purchase_url is locally derived — Spotify never sends it back — so Merge must
// carry it across or a single sync silently deletes every resolved link.
func TestMergePreservesPurchaseURL(t *testing.T) {
	local := Playlist{Tracks: []Track{{
		Title: "Superstar", Artist: "Beach House", Album: "Once Twice Melody",
		PurchaseURL: "https://beachhouse.bandcamp.com/album/once-twice-melody",
	}}}
	remote := Playlist{Tracks: []Track{{
		Title: "Superstar", Artist: "Beach House", Album: "Once Twice Melody",
	}}}

	for _, strat := range []Strategy{Archive, Mirror} {
		got := Merge(local, remote, strat, time.Now())
		if len(got.Tracks) != 1 {
			t.Fatalf("%s: got %d tracks, want 1", strat, len(got.Tracks))
		}
		if got.Tracks[0].PurchaseURL != local.Tracks[0].PurchaseURL {
			t.Errorf("%s: purchase_url = %q, want %q — sync would silently wipe it",
				strat, got.Tracks[0].PurchaseURL, local.Tracks[0].PurchaseURL)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/playlist/ -run PurchaseURL -v`
Expected: FAIL — `PurchaseURL` undefined.

- [ ] **Step 3: Add the field**

In `internal/playlist/types.go`, inside `Track`, after `ImageFile`:

```go
	// PurchaseURL is a best-effort "where to buy this" link filled by
	// `resolve purchase` (Bandcamp, iTunes, or Discogs). Locally derived —
	// Spotify has no equivalent — so it must be carried in adoptLocalFields.
	PurchaseURL string `yaml:"purchase_url,omitempty"`
```

- [ ] **Step 4: Carry it through the merge**

In `internal/playlist/merge.go`, add to `adoptLocalFields`:

```go
	remote.PurchaseURL = local.PurchaseURL
```

Update the `Merge` doc comment's list of surviving fields to include `purchase_url` alongside `youtube_id`, `image_file`, `spotify`, and `enrich_candidates`.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/playlist/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
make lint && make test
git add internal/playlist
git commit -m "feat(playlist): add Track.purchase_url, preserved across sync

purchase_url is locally derived and Spotify never sends it back, so
adoptLocalFields must copy it — Merge starts from the remote playlist and
anything not carried over is silently blanked. That failure mode
previously cost 8292 youtube_ids in one playlist with a zero exit code, so
the test covers both Archive and Mirror."
```

---

### Task 9: Emit `purchase_url` in JSPF

**Files:**
- Modify: `internal/export/jspf.go:61-74` (`jspfExt`), `:118-132` (population)
- Modify: `internal/export/export_test.go`

**Interfaces:**
- Produces: JSPF track extension gains `purchase_url`, a sibling of `resolved` under namespace `https://github.com/lmorchard/byom-sync`.

- [ ] **Step 1: Write the failing test**

Add to `internal/export/export_test.go`:

```go
func TestJSPFEmitsPurchaseURL(t *testing.T) {
	p := playlist.Playlist{Title: "T", Tracks: []playlist.Track{{
		Title: "Superstar", Artist: "Beach House", Album: "Once Twice Melody",
		PurchaseURL: "https://beachhouse.bandcamp.com/album/once-twice-melody",
	}}}
	out := filepath.Join(t.TempDir(), "p.jspf.json")
	if err := (JSPFExporter{}).Export(p, out, nil); err != nil {
		t.Fatalf("Export: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var doc struct {
		Playlist struct {
			Track []struct {
				Extension map[string][]struct {
					PurchaseURL string `json:"purchase_url"`
				} `json:"extension"`
			} `json:"track"`
		} `json:"playlist"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	ext := doc.Playlist.Track[0].Extension["https://github.com/lmorchard/byom-sync"]
	if len(ext) != 1 || ext[0].PurchaseURL != p.Tracks[0].PurchaseURL {
		t.Errorf("purchase_url not emitted; extension = %+v", ext)
	}
}

// A track without one must not gain an empty extension element.
func TestJSPFOmitsEmptyPurchaseURL(t *testing.T) {
	p := playlist.Playlist{Title: "T", Tracks: []playlist.Track{
		{Title: "X", Artist: "Y", SyncState: playlist.SyncState{SpotifyPresent: true}},
	}}
	out := filepath.Join(t.TempDir(), "p.jspf.json")
	if err := (JSPFExporter{}).Export(p, out, nil); err != nil {
		t.Fatalf("Export: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(data), "purchase_url") {
		t.Error("empty purchase_url should be omitted entirely")
	}
}
```

Ensure `encoding/json`, `os`, `path/filepath`, and `strings` are imported in the test file.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/export/ -run PurchaseURL -v`
Expected: FAIL — field not emitted.

- [ ] **Step 3: Implement**

In `internal/export/jspf.go`, add to `jspfExt`:

```go
	// PurchaseURL is a best-effort "where to buy this" link from
	// `resolve purchase`. byom-player renders it in the shopping list and
	// degrades to a constructed search URL when absent.
	PurchaseURL string `json:"purchase_url,omitempty"`
```

In the per-track loop, alongside the `YouTubeID` block:

```go
		if t.PurchaseURL != "" {
			ext.PurchaseURL = t.PurchaseURL
			hasExt = true
		}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/export/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
make lint && make test
git add internal/export
git commit -m "feat(export): emit purchase_url in the JSPF track extension

Sibling of resolved under the existing byom namespace, which byom-player's
manifest loader already reads. Omitted entirely when empty so tracks
without a link don't gain a bare extension element."
```

---

### Task 10: CLI — `resolve purchase`, config, and the `resolve all` fan-out

**Files:**
- Modify: `cmd/resolve.go` (new subcommand, flag vars, `init()` registration, `runResolveAll` fan-out)
- Modify: `internal/config/config.go` (add `discogs_token`)
- Create: `cmd/resolve_purchase_test.go`

**Interfaces:**
- Consumes: `purchase.Resolve`, `purchase.NewBandcamp/NewITunes/NewDiscogs`, `rcache` cache, `playlist.HubPaths`.
- Produces: `byom-sync resolve purchase [--input] [--source] [--limit] [--delay] [--no-cache]`.

**Gotcha (from AGENTS.md):** `runResolveAll` drives the per-stage globals, and `resolveNoCache` is shared state assigned by multiple stages. A new stage flag must be fanned out there too, or the pipeline and standalone command disagree.

- [ ] **Step 1: Write the failing test**

Create `cmd/resolve_purchase_test.go`:

```go
package cmd

import (
	"testing"
	"time"

	"github.com/lmorchard/byom-sync/internal/purchase"
)

func TestPurchaseSourcesForOrder(t *testing.T) {
	got, err := purchaseSourcesFor("all", "")
	if err != nil {
		t.Fatalf("purchaseSourcesFor: %v", err)
	}
	want := []string{"bandcamp", "itunes", "discogs"}
	if len(got) != len(want) {
		t.Fatalf("got %d sources, want %d", len(got), len(want))
	}
	for i, s := range got {
		if s.Name() != want[i] {
			t.Errorf("tier %d = %q, want %q", i, s.Name(), want[i])
		}
	}
}

func TestPurchaseSourcesForSingle(t *testing.T) {
	got, err := purchaseSourcesFor("itunes", "")
	if err != nil {
		t.Fatalf("purchaseSourcesFor: %v", err)
	}
	if len(got) != 1 || got[0].Name() != "itunes" {
		t.Errorf("got %v, want just itunes", got)
	}
}

func TestPurchaseSourcesForUnknown(t *testing.T) {
	if _, err := purchaseSourcesFor("napster", ""); err == nil {
		t.Error("expected an error for an unknown source")
	}
}

// Each tier has its own limit; a single --delay would be wrong for all of them.
func TestPurchasePaceForSource(t *testing.T) {
	for _, tc := range []struct {
		name string
		min  time.Duration
	}{
		{"bandcamp", time.Second},
		{"itunes", 3 * time.Second},
		{"discogs", 2400 * time.Millisecond},
	} {
		if got := purchasePaceFor(tc.name, 0); got < tc.min {
			t.Errorf("pace for %s = %v, want >= %v", tc.name, got, tc.min)
		}
	}
}

// An explicit --delay raises the floor but must never go below the source's own.
func TestPurchasePaceForRespectsFloor(t *testing.T) {
	if got := purchasePaceFor("itunes", 10*time.Millisecond); got < 3*time.Second {
		t.Errorf("pace = %v, want the source floor to win over a smaller --delay", got)
	}
	if got := purchasePaceFor("bandcamp", 5*time.Second); got != 5*time.Second {
		t.Errorf("pace = %v, want the larger explicit delay to win", got)
	}
}

var _ purchase.Source = (*purchase.Bandcamp)(nil)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -run Purchase -v`
Expected: FAIL — `purchaseSourcesFor` undefined.

- [ ] **Step 3: Add config support**

In `internal/config/config.go`, add a `DiscogsToken string` field bound to the `discogs_token` key, following exactly how the existing string keys are declared and defaulted in that file.

- [ ] **Step 4: Implement the helpers and command**

Add to `cmd/resolve.go`:

```go
var (
	purchaseInput   string
	purchaseSource  string
	purchaseLimit   int
	purchaseDelay   time.Duration
	purchaseNoCache bool
)

// purchaseTierOrder is the measured-best cascade. Bandcamp first: it resolves
// ~47% of hub albums in one request each and gives artist-friendly, DRM-free
// links. iTunes second at ~65% of what's left. Discogs last — a secondhand
// physical listing, which doesn't fill a digital gap unless the record is
// ripped. MusicBrainz is deliberately absent: measured at 3% with zero unique
// contribution.
var purchaseTierOrder = []string{"bandcamp", "itunes", "discogs"}

// purchaseSourcePaces are each store's own floor. A single --delay cannot
// express four different rate limits, so --delay acts only as an extra floor.
var purchaseSourcePaces = map[string]time.Duration{
	"bandcamp": 1100 * time.Millisecond, // undocumented endpoint — stay polite
	"itunes":   3100 * time.Millisecond, // ~20 req/min
	"discogs":  2500 * time.Millisecond, // 25 req/min unauthenticated
}

// purchaseSourcesFor builds the tier list for a --source value. "all" is the
// full cascade in measured order; any single source name is that tier alone.
func purchaseSourcesFor(name, discogsToken string) ([]purchase.Source, error) {
	build := func(n string) (purchase.Source, error) {
		switch n {
		case "bandcamp":
			return purchase.NewBandcamp(nil, ""), nil
		case "itunes":
			return purchase.NewITunes(nil, ""), nil
		case "discogs":
			return purchase.NewDiscogs(nil, "", discogsToken), nil
		default:
			return nil, fmt.Errorf("unknown purchase source %q (want one of: all, %s)",
				n, strings.Join(purchaseTierOrder, ", "))
		}
	}
	if name == "all" {
		out := make([]purchase.Source, 0, len(purchaseTierOrder))
		for _, n := range purchaseTierOrder {
			s, err := build(n)
			if err != nil {
				return nil, err
			}
			out = append(out, s)
		}
		return out, nil
	}
	s, err := build(name)
	if err != nil {
		return nil, err
	}
	return []purchase.Source{s}, nil
}

// purchasePaceFor returns the larger of the source's own floor and an explicit
// --delay, so a user can slow a tier down but never speed it past its limit.
func purchasePaceFor(name string, explicit time.Duration) time.Duration {
	pace := purchaseSourcePaces[name]
	if explicit > pace {
		return explicit
	}
	return pace
}

var resolvePurchaseCmd = &cobra.Command{
	Use:   "purchase",
	Short: "Fill missing purchase links (Bandcamp, then iTunes, then Discogs)",
	Long: `Find a best-effort "where to buy this" URL for every hub album that has no
purchase_url yet and write it into the YAML.

Runs tier-at-a-time, each tier a full pass over whatever is still unresolved:
Bandcamp (~47% of albums, artist-friendly and DRM-free), then iTunes (~65% of
what's left, DRM-free downloads, accepted only when the album carries a real
price rather than being stream-only), then Discogs (secondhand physical media,
accepted only when copies are actually listed for sale).

Every match passes a confidence gate, because stores will return a real but
wrong album — iTunes answers "Theatre Is Evil" with "Piano Is Evil".

--limit caps network lookups per tier; --delay is an extra floor on top of each
source's own rate limit. Set discogs_token in config to raise Discogs from
25/min to 60/min. Stopping after Bandcamp is a reasonable outcome: it is the
cheapest pass and gives the links most worth having.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runResolvePurchase(context.Background())
	},
}
```

Write `runResolvePurchase` following the structure of `runResolveArt`: resolve `--input` against the config hub dir, call `playlist.hubPaths`, open the cache via the existing `openCache` helper (setting `resolveNoCache = purchaseNoCache` first, per the shared-state gotcha), then for each source in the tier list, loop the hub files calling `purchase.Resolve` with:

```go
opts := purchase.Options{
	Budget:  budget, // &purchaseLimit when > 0, else nil — fresh per tier
	Pace:    purchasePaceFor(src.Name(), purchaseDelay),
	MissTTL: cfg.CacheMissTTL,
	Cache:   cache,
	Report:  reportPurchaseEvent,
	OnFilled: func() error { return playlist.Save(path, p) },
}
```

Log a per-tier summary line (`tier %s: filled %d, missed %d across %d file(s)`) so a long run is legible.

- [ ] **Step 5: Register flags and fan out into `resolve all`**

In `init()`, next to the `resolveArtCmd` block:

```go
	resolveCmd.AddCommand(resolvePurchaseCmd)
	resolvePurchaseCmd.Flags().StringVar(&purchaseInput, "input", "", "hub YAML file or directory (default: config dir)")
	resolvePurchaseCmd.Flags().StringVar(&purchaseSource, "source", "all", "which tier to run: all, bandcamp, itunes, discogs")
	resolvePurchaseCmd.Flags().IntVar(&purchaseLimit, "limit", 0, "max lookups per tier this run (0 = unlimited)")
	resolvePurchaseCmd.Flags().DurationVar(&purchaseDelay, "delay", 0, "extra floor on the pause between lookups (each source has its own minimum)")
	resolvePurchaseCmd.Flags().BoolVar(&purchaseNoCache, "no-cache", false, "bypass the purchase cache")
```

In `runResolveAll`, extend the existing fan-out assignments so the new stage participates:

```go
	resolveLimit, artLimit, enrichLimit, purchaseLimit = allLimit, allLimit, allLimit, allLimit
	resolveNoCache, artNoCache, enrichNoCache, purchaseNoCache = allNoCache, allNoCache, allNoCache, allNoCache
```

Leave `purchaseDelay` out of the shared `--delay` fan-out: it is a floor, and each source already has its own. Add a comment saying so, since the surrounding lines do fan `--delay` out.

Add the purchase stage to `runResolveAll`'s sequence **after** `resolve art` and before or after YouTube (order is independent). Update the command's `Long` text to list it.

- [ ] **Step 6: Run tests and build**

Run: `go test ./cmd/ -v && make build`
Expected: PASS, binary builds.

- [ ] **Step 7: Smoke-test against one real playlist**

```bash
./byom-sync resolve purchase --input <one-hub-file>.yaml --source bandcamp --limit 5
```

Expected: up to 5 lookups, some `filled` lines, `purchase_url` present in the YAML. Confirm re-running it makes **zero** network calls (everything cached or already resolved).

- [ ] **Step 8: Commit**

```bash
make lint && make test && make build
git add cmd internal/config
git commit -m "feat(cmd): add resolve purchase with per-tier rate limits

Tier-at-a-time so each pass has one rate limit and is independently
resumable. --delay is an extra floor rather than the pace itself, because
one duration cannot express Bandcamp ~1/s, iTunes 20/min, and Discogs
25/min.

Fans purchaseLimit and purchaseNoCache out in runResolveAll, per the
shared-globals gotcha in AGENTS.md; purchaseDelay deliberately stays out
since each source carries its own floor."
```

---

### Task 11: Documentation

**Files:**
- Modify: `README.md` (usage section after "Resolve YouTube IDs"), `AGENTS.md` (layout + conventions)

- [ ] **Step 1: Update README.md**

Add a `### Resolve purchase links` section after the YouTube one, covering: what it does, the tier order with measured hit rates, the `--source`/`--limit`/`--delay` flags, the optional `discogs_token`, the ~6 hour cold-fill estimate, and that stopping after Bandcamp is reasonable. Add `discogs_token` to the config example block. Mention `purchase_url` in the hub-schema example.

- [ ] **Step 2: Update AGENTS.md**

Three edits:

1. **Layout** — add `internal/purchase/` and `internal/match/` entries in the established style, and note the fourth `purchase_cache` table under `internal/rcache/`.
2. **Conventions** — add a `**Purchase links:**` bullet covering the tier order, the confidence gate, the iTunes price requirement, the Discogs two-step, and that MusicBrainz was measured and excluded. Update the recommended pipeline order to `resolve spotify → resolve art → resolve purchase → resolve youtube → export`.
3. **The sync-clobber list** — add `purchase_url` to the `adoptLocalFields` enumeration in the "Sync must not clobber locally-derived fields" bullet. This is the highest-value doc edit in the task.

- [ ] **Step 3: Verify and commit**

```bash
make lint && make test && make build
git add README.md AGENTS.md
git commit -m "docs: document resolve purchase

Adds purchase_url to the adoptLocalFields enumeration in AGENTS.md — the
list a future contributor reads before adding a locally-derived field, and
the one that prevents sync from silently deleting resolved links."
```

---

## Self-Review

**Spec coverage.** Every Phase A requirement maps to a task: tier order and the `Source` interface (3–7), query normalization (3), confidence gate with real measured rejections (3, 5), Bandcamp including track filter and clean miss (4), iTunes price gate (5), Discogs two-step and `num_for_sale` (6), per-source rate limits and `--source`/`--limit`/`--delay` (10), `purchase_cache` with per-source clearing (2), `internal/match` extraction (1), hub field (8), JSPF emission (9), `discogs_token` (10), docs (11).

Deliberately excluded, matching the spec: MusicBrainz as a purchase source, `lowest_price` storage, Bandcamp cover art (byom-sync#54), and Bleep/Boomkat search URLs.

**Two things the spec did not cover, added here from AGENTS.md:**

- **Task 8's sync-clobber guard.** `adoptLocalFields` is not mentioned anywhere in the spec, and omitting it would mean the next `sync` silently erases every resolved link. This is the single highest-risk item in the plan.
- **Task 10's `runResolveAll` fan-out.** The spec doesn't mention `resolve all`; AGENTS.md warns that stage flags must be fanned out there or the pipeline and standalone command disagree.

**Placeholder scan.** No TBDs. Three steps describe rather than show code — Task 10 Step 4 (`runResolvePurchase`), Task 11 Steps 1–2 (prose docs). Each names the exact existing function to mirror (`runResolveArt`) or the exact sections to edit, and Step 4 gives the `Options` literal verbatim, which is the part with real decisions in it.

**Type consistency.** `Query`/`Result`/`Source`/`Kind` are defined once in Task 3 and used unchanged in 4–7 and 10. `rcache.PurchaseEntry` is defined in Task 2 and consumed in 7. `purchase.Cache` (Task 7) is structurally satisfied by `*rcache.DB` via `GetPurchase`/`PutPurchase` (Task 2) — signatures match exactly. `Track.PurchaseURL` is introduced in Task 8 and consumed in 7 and 9; **Task 7 needs the field to compile**, which its Step 2 flags.

**Ordering.** 1 → 2 → 3 → (4, 5, 6 in any order) → 8 → 7 → 9 → 10 → 11. Task 8 is listed after 7 for narrative reasons but should be implemented before it, or Task 7's tests won't compile. Its Step 2 says so.

---

## Execution Handoff

Plan saved to `docs/dev-sessions/2026-07-30-1046-shopping-list/plan.md`. Two execution options:

1. **Subagent-Driven (recommended)** — a fresh subagent per task, review between tasks, fast iteration.
2. **Inline Execution** — execute tasks in this session with checkpoints for review.
