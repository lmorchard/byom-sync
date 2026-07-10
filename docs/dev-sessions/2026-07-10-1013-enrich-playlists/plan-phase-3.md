# Phase 3 — Cover Art Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make cover art visible in exports and fill art gaps from an alternate source: wire the existing `Track.Image` into JSPF, add a playlist-level image, and add a `resolve art` command that finds cover art via MusicBrainz → Cover Art Archive for any track missing an image (including off-Spotify tracks).

**Architecture:** A new `internal/coverart` package mirrors the youtube/enrich resolver pattern: a `Resolver` (MusicBrainz search → Cover Art Archive front image) and a `Resolve` loop that fills `Track.Image`, backed by a third `art_cache` table in the shared `cache.db`. The JSPF exporter gains an `image` member on tracks and playlists. No auth or API key is needed — MusicBrainz and the Cover Art Archive are public (MusicBrainz requires a descriptive User-Agent and ~1 req/sec pacing, handled by the existing delay machinery).

**Tech Stack:** Go 1.25 · stdlib `net/http` + `net/http/httptest` (no new deps) · `modernc.org/sqlite` · Cobra/Viper · `gopkg.in/yaml.v3`.

## Global Constraints

- Go 1.25; no cgo.
- Formatting via `gofumpt` (`make format`); lint via golangci-lint v2 (`make lint`).
- **errcheck is strict** — assign intentionally-ignored returns to `_ =` (e.g. `_ = resp.Body.Close()`, `_ = cache.PutArt(...)`).
- **No `import "node:*"`-style mistakes** — this is Go; `tsconfig` note doesn't apply. Do not add dependencies.
- Run `make lint && make test && make build` before claiming done; read the output.
- Commit trailer on every commit: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
- Work on branch `feat/cover-art`; PR into `main`. No direct pushes to `main`.
- Smallest reasonable change; no unrelated refactoring.
- **HTTP hygiene:** every response body is closed; every request carries the User-Agent; a non-200 is handled (not treated as success). Network clients take a configurable base URL so tests hit an `httptest.Server`.
- **Art is independent of `spotify:false`:** `resolve art` fills any track lacking an `image`, regardless of the enrichment opt-out.

## File Structure

- `internal/playlist/types.go` — **modify.** Add `Playlist.Image`.
- `internal/playlist/types_test.go` — **modify.** Round-trip test for `Playlist.Image`.
- `internal/export/jspf.go` — **modify.** Emit `image` on `jspfTrack` (from `Track.Image`) and `jspfPlaylist` (from `Playlist.Image`, falling back to the first track's image).
- `internal/export/export_test.go` — **modify.** Assert image emission + playlist fallback.
- `internal/rcache/art.go` — **create.** `ArtEntry`, `art_cache` schema, `GetArt`/`PutArt`/`ArtStats`.
- `internal/rcache/art_test.go` — **create.** Round-trip, miss, stats, clear-all-three.
- `internal/rcache/rcache.go` — **modify.** `Open` creates the art schema; `Clear` clears the art table too.
- `internal/coverart/musicbrainz.go` — **create.** `MBClient` (release-group + recording search), query building, JSON parsing.
- `internal/coverart/coverartarchive.go` — **create.** `CAAClient` (front image for a release / release-group MBID).
- `internal/coverart/resolver.go` — **create.** `Resolver` (album-first, recording fallback), `Result`, `Arter` interface.
- `internal/coverart/resolve.go` — **create.** `Resolve` loop, `Options`, `Event`, `EventKind`, `Cache` interface.
- `internal/coverart/*_test.go` — **create.** httptest-backed client tests + fake-resolver loop tests.
- `cmd/resolve.go` — **modify.** Add `resolve art` subcommand + flags; extend `cache stats` with an art line.
- `AGENTS.md` — **modify.** Document `resolve art` + the coverart package + the third cache table.

---

### Task 1: JSPF art wiring + `Playlist.Image`

**Files:**
- Modify: `internal/playlist/types.go`, `internal/export/jspf.go`
- Test: `internal/playlist/types_test.go`, `internal/export/export_test.go`

**Interfaces:**
- Consumes: `playlist.Track.Image` (already exists), `playlist.Playlist`.
- Produces: `Playlist.Image string` (yaml `image,omitempty`); JSPF `image` members.

**Context:** `Track.Image` already exists and is populated by Spotify enrichment, but the JSPF exporter never emits it — so that art is currently invisible. This task surfaces it and adds a playlist-level image. `jspfTrack`/`jspfPlaylist` are structs in `jspf.go`; add an `Image` field with json tag `image,omitempty`. The playlist image is `p.Image` when set, else the first track's `Image` (or empty).

- [ ] **Step 1: Write the failing tests**

Add to `internal/playlist/types_test.go`:

```go
func TestPlaylist_ImageRoundTrip(t *testing.T) {
	data, err := yaml.Marshal(Playlist{Title: "T", Image: "https://img/pl.jpg"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), "image: https://img/pl.jpg") {
		t.Errorf("playlist image not serialized:\n%s", data)
	}
	// omitempty: no image -> no image key at the playlist level
	bare, _ := yaml.Marshal(Playlist{Title: "T"})
	for _, line := range strings.Split(string(bare), "\n") {
		if strings.HasPrefix(line, "image:") {
			t.Errorf("bare playlist should omit image:\n%s", bare)
		}
	}
}
```

Add to `internal/export/export_test.go` (inside `TestJSPFExport`, after the existing track assertions, before the closing brace):

```go
	// track image emitted from Track.Image
	if t0["image"] != "https://img/song-one.jpg" {
		t.Errorf("track image not emitted: %v", t0["image"])
	}
	// playlist image falls back to the first track's image when unset
	if pl["image"] != "https://img/song-one.jpg" {
		t.Errorf("playlist image should fall back to first track image: %v", pl["image"])
	}
```

And give the first sample track an image — in `samplePlaylist()` (export_test.go), add `Image: "https://img/song-one.jpg"` to the first track literal.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/playlist/ ./internal/export/ -run 'TestPlaylist_ImageRoundTrip|TestJSPFExport' -v`
Expected: FAIL — `Playlist has no field Image` (build error) and missing `image` keys.

- [ ] **Step 3: Add `Playlist.Image`**

In `internal/playlist/types.go`, add to the `Playlist` struct (after `Description`):

```go
	Description string `yaml:"description,omitempty"`
	// Image is playlist-level cover art (a URL). When unset, exporters fall back
	// to the first track's image.
	Image string `yaml:"image,omitempty"`
```

- [ ] **Step 4: Emit image in the JSPF exporter**

In `internal/export/jspf.go`:

Add `Image` to `jspfPlaylist`:

```go
type jspfPlaylist struct {
	Title   string      `json:"title,omitempty"`
	Creator string      `json:"creator,omitempty"`
	Date    string      `json:"date,omitempty"`
	Image   string      `json:"image,omitempty"`
	Track   []jspfTrack `json:"track"`
}
```

Add `Image` to `jspfTrack` (after `Album`):

```go
	Album      string               `json:"album,omitempty"`
	Image      string               `json:"image,omitempty"`
```

Set the track image in the track loop (after the `Album:` line in the `jspfTrack{...}` literal):

```go
		jt := jspfTrack{
			Title:    t.Title,
			Creator:  t.Artist,
			Album:    t.Album,
			Image:    t.Image,
			Duration: (t.DurationMS + 500) / 1000, // round to nearest second
		}
```

Set the playlist image after building `doc` (after the `DateCreated` block, before the track loop):

```go
	doc.Playlist.Image = playlistImage(p)
```

Add the helper at the end of the file:

```go
// playlistImage returns the playlist's own image, or the first track's image as
// a fallback so a playlist still has cover art when none was set explicitly.
func playlistImage(p playlist.Playlist) string {
	if p.Image != "" {
		return p.Image
	}
	for _, t := range p.Tracks {
		if t.Image != "" {
			return t.Image
		}
	}
	return ""
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/playlist/ ./internal/export/ -v`
Expected: PASS (new + existing).

- [ ] **Step 6: Commit**

```bash
git add internal/playlist/types.go internal/playlist/types_test.go internal/export/jspf.go internal/export/export_test.go
git commit -m "feat(export): emit cover art in JSPF (track + playlist image)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: rcache art table

**Files:**
- Create: `internal/rcache/art.go`, `internal/rcache/art_test.go`
- Modify: `internal/rcache/rcache.go`

**Interfaces:**
- Consumes: the existing `*DB` handle (`d.db`), the `Stats` struct.
- Produces:
  - `type ArtEntry struct { ImageURL string; Source string; CheckedAt time.Time }` — `ImageURL == ""` means a known miss.
  - `func (d *DB) GetArt(key string) (ArtEntry, bool)`
  - `func (d *DB) PutArt(key string, e ArtEntry) error`
  - `func (d *DB) ArtStats(missCutoff time.Time) (Stats, error)`
  - `Clear` now also deletes from `art_cache`.

**Context:** Mirror `internal/rcache/enrich.go` exactly (same shape: schema const, entry struct, Get/Put with `sql.Null*`, Stats via a row loop, `checked_at` written unconditionally as a plain RFC3339 string — never a NULL, matching `PutEnrich`). The miss column for `art_cache` is `image_url`.

- [ ] **Step 1: Write the failing test**

Create `internal/rcache/art_test.go`:

```go
package rcache

import (
	"testing"
	"time"
)

func TestArtPutGetPositive(t *testing.T) {
	db := openTemp(t)
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	in := ArtEntry{ImageURL: "https://caa/front-500.jpg", Source: "musicbrainz-release-group", CheckedAt: now}
	if err := db.PutArt("at:kavinsky\tnightcall\tnightcall", in); err != nil {
		t.Fatalf("PutArt: %v", err)
	}
	got, ok := db.GetArt("at:kavinsky\tnightcall\tnightcall")
	if !ok || got.ImageURL != in.ImageURL || got.Source != in.Source {
		t.Fatalf("GetArt: %+v ok=%v", got, ok)
	}
	if !got.CheckedAt.Equal(now) {
		t.Fatalf("CheckedAt: got %v want %v", got.CheckedAt, now)
	}
}

func TestArtGetMissingIsFalse(t *testing.T) {
	db := openTemp(t)
	if _, ok := db.GetArt("nope"); ok {
		t.Fatal("GetArt: want !ok for missing key")
	}
}

func TestArtPutZeroCheckedAt(t *testing.T) {
	db := openTemp(t)
	// A miss with a zero CheckedAt must insert cleanly (checked_at is NOT NULL).
	if err := db.PutArt("miss", ArtEntry{ImageURL: ""}); err != nil {
		t.Fatalf("PutArt zero CheckedAt: %v", err)
	}
	if _, ok := db.GetArt("miss"); !ok {
		t.Fatal("miss entry should be retrievable")
	}
}

func TestArtStatsAndClearAllTables(t *testing.T) {
	db := openTemp(t)
	recent := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	_ = db.Put("yt", Entry{VideoID: "v", CheckedAt: recent})
	_ = db.PutEnrich("e", EnrichEntry{SpotifyID: "sid", CheckedAt: recent})
	_ = db.PutArt("apos", ArtEntry{ImageURL: "u", CheckedAt: recent})
	_ = db.PutArt("amiss", ArtEntry{ImageURL: "", CheckedAt: recent})

	s, err := db.ArtStats(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ArtStats: %v", err)
	}
	if s.Total != 2 || s.Positive != 1 || s.Negative != 1 {
		t.Fatalf("ArtStats: %+v", s)
	}

	if _, err := db.Clear(false); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	for _, check := range []func() bool{
		func() bool { _, ok := db.Get("yt"); return ok },
		func() bool { _, ok := db.GetEnrich("e"); return ok },
		func() bool { _, ok := db.GetArt("apos"); return ok },
	} {
		if check() {
			t.Error("Clear(false) should empty all three tables")
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/rcache/ -run TestArt -v`
Expected: FAIL — `undefined: ArtEntry` / `db.PutArt undefined`.

- [ ] **Step 3: Create `art.go`**

Create `internal/rcache/art.go`:

```go
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
```

- [ ] **Step 4: Wire schema + Clear in `rcache.go`**

In `Open`, after the `enrichSchema` block, add:

```go
	if _, err := db.Exec(artSchema); err != nil {
		_ = db.Close()
		return nil, err
	}
```

In `Clear`, after the `enrichment_cache` deletion, add the third table (before `return total, nil`):

```go
	n3, err := del("art_cache", "image_url")
	if err != nil {
		return total, err
	}
	total += n3
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/rcache/ -v`
Expected: PASS (new `TestArt*` + all existing rcache tests).

- [ ] **Step 6: Commit**

```bash
git add internal/rcache/art.go internal/rcache/art_test.go internal/rcache/rcache.go
git commit -m "feat(rcache): add art cache table alongside resolution + enrichment

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: MusicBrainz + Cover Art Archive clients + Resolver

**Files:**
- Create: `internal/coverart/musicbrainz.go`, `internal/coverart/coverartarchive.go`, `internal/coverart/resolver.go`
- Test: `internal/coverart/coverart_test.go`

**Interfaces:**
- Consumes: `playlist.Track`, stdlib `net/http`.
- Produces:
  - `type Result struct { ImageURL string; Source string }` — empty `ImageURL` = no art found.
  - `type Arter interface { Resolve(ctx context.Context, t playlist.Track) (Result, error) }`
  - `type MBClient struct { HTTP *http.Client; BaseURL string; UserAgent string }` with `SearchReleaseGroup(ctx, artist, album) (mbid string, err error)` and `SearchRecordingRelease(ctx, artist, title) (releaseMBID string, err error)`.
  - `type CAAClient struct { HTTP *http.Client; BaseURL string }` with `FrontImage(ctx, entity, mbid string) (url string, err error)` (`entity` is `"release-group"` or `"release"`).
  - `type Resolver struct { MB *MBClient; CAA *CAAClient }` implementing `Arter`.
  - `const DefaultUserAgent = "byom-sync ( https://github.com/lmorchard/byom-sync )"`
  - `const MBBaseURL = "https://musicbrainz.org/ws/2"`, `const CAABaseURL = "https://coverartarchive.org"`

**Context:** MusicBrainz search endpoints (`fmt=json`): `GET {MBBaseURL}/release-group?query=<lucene>&fmt=json&limit=5` returns `{"release-groups":[{"id":...}]}`; `GET {MBBaseURL}/recording?query=<lucene>&fmt=json&limit=5` returns `{"recordings":[{"id":...,"releases":[{"id":...}]}]}`. The Lucene query for release-group is `artist:"A" AND releasegroup:"B"`; for recording it is `artist:"A" AND recording:"B"`. Cover Art Archive: `GET {CAABaseURL}/{entity}/{mbid}` returns `{"images":[{"front":true,"image":"...","thumbnails":{"500":"..."}}]}` (HTTP 404 when a release/group has no art). Every MusicBrainz request MUST send a `User-Agent` header (they block generic ones). Base URLs are struct fields so tests point them at an `httptest.Server`.

Resolver strategy (matches the design decision "album when present, else recording", with a recording fallback):
1. If `t.Album != ""`: `SearchReleaseGroup` → if an MBID comes back, `CAA.FrontImage("release-group", mbid)`. If that yields a URL, return it (`Source: "musicbrainz-release-group"`).
2. Otherwise, or if step 1 found no image: `SearchRecordingRelease` → if a release MBID comes back, `CAA.FrontImage("release", mbid)` → return it (`Source: "musicbrainz-recording"`).
3. No image anywhere → `Result{}` (empty), nil error.

- [ ] **Step 1: Write the failing test**

Create `internal/coverart/coverart_test.go`:

```go
package coverart

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

// mbAndCaaServer returns an httptest server standing in for both MusicBrainz and
// the Cover Art Archive, routed by path. It records the User-Agent seen.
func testServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, func() string) {
	t.Helper()
	var lastUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastUA = r.Header.Get("User-Agent")
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, func() string { return lastUA }
}

func TestResolver_AlbumPath(t *testing.T) {
	srv, ua := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/ws/2/release-group"):
			q := r.URL.Query().Get("query")
			if !strings.Contains(q, `releasegroup:"Abbey Road"`) || !strings.Contains(q, `artist:"The Beatles"`) {
				t.Errorf("unexpected release-group query: %q", q)
			}
			_, _ = w.Write([]byte(`{"release-groups":[{"id":"rg-mbid-1"}]}`))
		case r.URL.Path == "/release-group/rg-mbid-1":
			_, _ = w.Write([]byte(`{"images":[{"front":true,"image":"https://caa/img.jpg","thumbnails":{"500":"https://caa/500.jpg"}}]}`))
		default:
			http.NotFound(w, r)
		}
	})
	r := Resolver{
		MB:  &MBClient{HTTP: srv.Client(), BaseURL: srv.URL + "/ws/2", UserAgent: "test-agent"},
		CAA: &CAAClient{HTTP: srv.Client(), BaseURL: srv.URL},
	}
	res, err := r.Resolve(context.Background(), playlist.Track{Artist: "The Beatles", Title: "Come Together", Album: "Abbey Road"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.ImageURL != "https://caa/500.jpg" {
		t.Errorf("want the 500px thumbnail, got %q", res.ImageURL)
	}
	if res.Source != "musicbrainz-release-group" {
		t.Errorf("source: %q", res.Source)
	}
	if ua() != "test-agent" {
		t.Errorf("User-Agent not sent: %q", ua())
	}
}

func TestResolver_RecordingFallbackWhenNoAlbum(t *testing.T) {
	srv, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/ws/2/recording"):
			_, _ = w.Write([]byte(`{"recordings":[{"id":"rec1","releases":[{"id":"rel-mbid-1"}]}]}`))
		case r.URL.Path == "/release/rel-mbid-1":
			_, _ = w.Write([]byte(`{"images":[{"front":true,"image":"https://caa/rel.jpg","thumbnails":{}}]}`))
		default:
			http.NotFound(w, r)
		}
	})
	r := Resolver{
		MB:  &MBClient{HTTP: srv.Client(), BaseURL: srv.URL + "/ws/2", UserAgent: "ua"},
		CAA: &CAAClient{HTTP: srv.Client(), BaseURL: srv.URL},
	}
	res, err := r.Resolve(context.Background(), playlist.Track{Artist: "Darkness On Demand", Title: "Tragedy For You"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.ImageURL != "https://caa/rel.jpg" { // no 500 thumb -> full image
		t.Errorf("want full image fallback, got %q", res.ImageURL)
	}
	if res.Source != "musicbrainz-recording" {
		t.Errorf("source: %q", res.Source)
	}
}

func TestResolver_MissWhenCAA404(t *testing.T) {
	srv, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/ws/2/release-group") {
			_, _ = w.Write([]byte(`{"release-groups":[{"id":"rg"}]}`))
			return
		}
		http.NotFound(w, r) // CAA has no art for this MBID
	})
	r := Resolver{
		MB:  &MBClient{HTTP: srv.Client(), BaseURL: srv.URL + "/ws/2", UserAgent: "ua"},
		CAA: &CAAClient{HTTP: srv.Client(), BaseURL: srv.URL},
	}
	res, err := r.Resolve(context.Background(), playlist.Track{Artist: "A", Title: "B", Album: "C"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.ImageURL != "" {
		t.Errorf("expected a miss (no art), got %q", res.ImageURL)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/coverart/ -v`
Expected: FAIL — package/types undefined (build error).

- [ ] **Step 3: Write `musicbrainz.go`**

Create `internal/coverart/musicbrainz.go`:

```go
// Package coverart resolves cover-art URLs for tracks via MusicBrainz search
// and the Cover Art Archive. Public APIs, no key; MusicBrainz needs a
// descriptive User-Agent and ~1 req/sec (paced by the caller).
package coverart

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	MBBaseURL        = "https://musicbrainz.org/ws/2"
	CAABaseURL       = "https://coverartarchive.org"
	DefaultUserAgent = "byom-sync ( https://github.com/lmorchard/byom-sync )"
)

// MBClient queries the MusicBrainz search API.
type MBClient struct {
	HTTP      *http.Client
	BaseURL   string // e.g. MBBaseURL; overridable for tests
	UserAgent string
}

// SearchReleaseGroup returns the top release-group MBID matching artist+album,
// or "" when there is no match.
func (c *MBClient) SearchReleaseGroup(ctx context.Context, artist, album string) (string, error) {
	q := fmt.Sprintf(`artist:%s AND releasegroup:%s`, luceneQuote(artist), luceneQuote(album))
	var out struct {
		ReleaseGroups []struct {
			ID string `json:"id"`
		} `json:"release-groups"`
	}
	if err := c.search(ctx, "release-group", q, &out); err != nil {
		return "", err
	}
	if len(out.ReleaseGroups) == 0 {
		return "", nil
	}
	return out.ReleaseGroups[0].ID, nil
}

// SearchRecordingRelease returns the first release MBID of the top recording
// matching artist+title, or "" when there is no match.
func (c *MBClient) SearchRecordingRelease(ctx context.Context, artist, title string) (string, error) {
	q := fmt.Sprintf(`artist:%s AND recording:%s`, luceneQuote(artist), luceneQuote(title))
	var out struct {
		Recordings []struct {
			ID       string `json:"id"`
			Releases []struct {
				ID string `json:"id"`
			} `json:"releases"`
		} `json:"recordings"`
	}
	if err := c.search(ctx, "recording", q, &out); err != nil {
		return "", err
	}
	for _, rec := range out.Recordings {
		if len(rec.Releases) > 0 {
			return rec.Releases[0].ID, nil
		}
	}
	return "", nil
}

func (c *MBClient) search(ctx context.Context, entity, query string, out any) error {
	u := fmt.Sprintf("%s/%s?%s", c.BaseURL, entity, url.Values{
		"query": {query},
		"fmt":   {"json"},
		"limit": {"5"},
	}.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("musicbrainz %s: status %d", entity, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// luceneQuote wraps a value in double quotes for a Lucene field query, dropping
// any embedded double quotes so they can't break the query syntax.
func luceneQuote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, "") + `"`
}
```

- [ ] **Step 4: Write `coverartarchive.go`**

Create `internal/coverart/coverartarchive.go`:

```go
package coverart

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// CAAClient fetches cover art metadata from the Cover Art Archive.
type CAAClient struct {
	HTTP    *http.Client
	BaseURL string // e.g. CAABaseURL; overridable for tests
}

// FrontImage returns the URL of the front cover for a MusicBrainz entity
// ("release" or "release-group") MBID: the 500px thumbnail when available, else
// the full image. Returns "" when the entity has no art (HTTP 404) or no front
// image.
func (c *CAAClient) FrontImage(ctx context.Context, entity, mbid string) (string, error) {
	u := fmt.Sprintf("%s/%s/%s", c.BaseURL, entity, mbid)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil // no art for this MBID
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("cover art archive %s/%s: status %d", entity, mbid, resp.StatusCode)
	}
	var out struct {
		Images []struct {
			Front      bool              `json:"front"`
			Image      string            `json:"image"`
			Thumbnails map[string]string `json:"thumbnails"`
		} `json:"images"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	for _, img := range out.Images {
		if !img.Front {
			continue
		}
		if u500 := img.Thumbnails["500"]; u500 != "" {
			return u500, nil
		}
		return img.Image, nil
	}
	return "", nil
}
```

- [ ] **Step 5: Write `resolver.go`**

Create `internal/coverart/resolver.go`:

```go
package coverart

import (
	"context"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

// Result is a resolved cover-art URL and which path produced it. An empty
// ImageURL means no art was found.
type Result struct {
	ImageURL string
	Source   string
}

// Arter resolves cover art for a track. Abstracted for testability.
type Arter interface {
	Resolve(ctx context.Context, t playlist.Track) (Result, error)
}

// Resolver finds cover art via MusicBrainz + the Cover Art Archive.
type Resolver struct {
	MB  *MBClient
	CAA *CAAClient
}

// Resolve tries the album path first (release-group art when the track has an
// album), then falls back to the recording path (art from the recording's first
// release). Returns an empty Result (nil error) when no art is found.
func (r Resolver) Resolve(ctx context.Context, t playlist.Track) (Result, error) {
	if t.Album != "" {
		mbid, err := r.MB.SearchReleaseGroup(ctx, t.Artist, t.Album)
		if err != nil {
			return Result{}, err
		}
		if mbid != "" {
			url, err := r.CAA.FrontImage(ctx, "release-group", mbid)
			if err != nil {
				return Result{}, err
			}
			if url != "" {
				return Result{ImageURL: url, Source: "musicbrainz-release-group"}, nil
			}
		}
	}

	relMBID, err := r.MB.SearchRecordingRelease(ctx, t.Artist, t.Title)
	if err != nil {
		return Result{}, err
	}
	if relMBID != "" {
		url, err := r.CAA.FrontImage(ctx, "release", relMBID)
		if err != nil {
			return Result{}, err
		}
		if url != "" {
			return Result{ImageURL: url, Source: "musicbrainz-recording"}, nil
		}
	}
	return Result{}, nil
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/coverart/ -v`
Expected: PASS (all three resolver tests).

- [ ] **Step 7: Commit**

```bash
git add internal/coverart/musicbrainz.go internal/coverart/coverartarchive.go internal/coverart/resolver.go internal/coverart/coverart_test.go
git commit -m "feat(coverart): MusicBrainz + Cover Art Archive resolver

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Art resolve loop

**Files:**
- Create: `internal/coverart/resolve.go`, `internal/coverart/resolve_test.go`

**Interfaces:**
- Consumes: `Arter`, `Result` (Task 3); `rcache.ArtEntry` (Task 2); `playlist` types.
- Produces:
  - `type EventKind string` with `KindFilled`, `KindMiss`, `KindError`.
  - `type Event struct { Kind EventKind; Artist, Title, ImageURL, Source string; Err error }`
  - `type Cache interface { GetArt(key string) (rcache.ArtEntry, bool); PutArt(key string, e rcache.ArtEntry) error }`
  - `type Options struct { Budget *int; Pace time.Duration; Report func(Event); OnFilled func() error; Cache Cache; Now func() time.Time; MissTTL time.Duration }`
  - `func Resolve(ctx context.Context, a Arter, p *playlist.Playlist, opts Options) (filled int, err error)`

**Context:** Mirror the structure of `spotifyenrich.Enrich` but simpler (fill-or-miss, no candidates/canonicalize/pick). Per track: skip if `Image != ""`; cache short-circuit (positive → fill; fresh miss → skip; expired → fall through); else resolve; a non-empty `ImageURL` fills `Track.Image` (+ positive cache write + `KindFilled`); an empty result writes a miss + `KindMiss`; a resolver error reports `KindError` and continues. Budget decremented per network attempt; pace between attempts; cache hits consume neither. Reuse a local `sleep(ctx, d)` helper identical to the one in `spotifyenrich`.

- [ ] **Step 1: Write the failing test**

Create `internal/coverart/resolve_test.go`:

```go
package coverart

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/lmorchard/byom-sync/internal/rcache"
)

type fakeArter struct {
	byTitle map[string]Result
	err     error
	calls   int
}

func (f *fakeArter) Resolve(_ context.Context, t playlist.Track) (Result, error) {
	f.calls++
	if f.err != nil {
		return Result{}, f.err
	}
	return f.byTitle[t.Title], nil
}

func TestResolve_FillsMissingArt(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{{Title: "Song", Artist: "A"}}}
	a := &fakeArter{byTitle: map[string]Result{"Song": {ImageURL: "https://img", Source: "musicbrainz-recording"}}}
	n, err := Resolve(context.Background(), a, p, Options{})
	if err != nil || n != 1 {
		t.Fatalf("Resolve: n=%d err=%v", n, err)
	}
	if p.Tracks[0].Image != "https://img" {
		t.Errorf("image not filled: %+v", p.Tracks[0])
	}
}

func TestResolve_SkipsTracksWithImage(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{{Title: "Song", Artist: "A", Image: "existing"}}}
	a := &fakeArter{}
	_, _ = Resolve(context.Background(), a, p, Options{})
	if a.calls != 0 {
		t.Errorf("track with an image should be skipped: calls=%d", a.calls)
	}
}

func TestResolve_Miss(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{{Title: "Nope", Artist: "A"}}}
	a := &fakeArter{byTitle: map[string]Result{}} // empty Result = miss
	var kinds []EventKind
	n, _ := Resolve(context.Background(), a, p, Options{Report: func(e Event) { kinds = append(kinds, e.Kind) }})
	if n != 0 || len(kinds) != 1 || kinds[0] != KindMiss {
		t.Fatalf("expected one miss: n=%d kinds=%v", n, kinds)
	}
}

func TestResolve_ErrorReportedNotFatal(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{{Title: "Song", Artist: "A"}}}
	a := &fakeArter{err: errors.New("boom")}
	var kinds []EventKind
	n, err := Resolve(context.Background(), a, p, Options{Report: func(e Event) { kinds = append(kinds, e.Kind) }})
	if err != nil || n != 0 || len(kinds) != 1 || kinds[0] != KindError {
		t.Fatalf("expected one error event, non-fatal: n=%d err=%v kinds=%v", n, err, kinds)
	}
}

func TestResolve_BudgetCaps(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{{Title: "One", Artist: "A"}, {Title: "Two", Artist: "B"}}}
	a := &fakeArter{byTitle: map[string]Result{"One": {ImageURL: "1"}, "Two": {ImageURL: "2"}}}
	budget := 1
	_, _ = Resolve(context.Background(), a, p, Options{Budget: &budget})
	if a.calls != 1 {
		t.Errorf("budget=1 should attempt one track: calls=%d", a.calls)
	}
}

type fakeCache struct{ m map[string]rcache.ArtEntry }

func (c *fakeCache) GetArt(k string) (rcache.ArtEntry, bool)  { e, ok := c.m[k]; return e, ok }
func (c *fakeCache) PutArt(k string, e rcache.ArtEntry) error { c.m[k] = e; return nil }

func TestResolve_CachePositiveShortCircuits(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{{Title: "Song", Artist: "A"}}}
	key := p.Tracks[0].Key()
	cache := &fakeCache{m: map[string]rcache.ArtEntry{key: {ImageURL: "cached", CheckedAt: time.Now()}}}
	a := &fakeArter{}
	n, _ := Resolve(context.Background(), a, p, Options{Cache: cache})
	if n != 1 || a.calls != 0 || p.Tracks[0].Image != "cached" {
		t.Fatalf("cache hit should fill without a lookup: n=%d calls=%d image=%q", n, a.calls, p.Tracks[0].Image)
	}
}

func TestResolve_CacheFreshMissSkips(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	p := &playlist.Playlist{Tracks: []playlist.Track{{Title: "Nope", Artist: "A"}}}
	key := p.Tracks[0].Key()
	cache := &fakeCache{m: map[string]rcache.ArtEntry{key: {ImageURL: "", CheckedAt: now}}}
	a := &fakeArter{}
	n, _ := Resolve(context.Background(), a, p, Options{
		Cache: cache, MissTTL: 24 * time.Hour, Now: func() time.Time { return now },
	})
	if n != 0 || a.calls != 0 {
		t.Fatalf("fresh miss should skip lookup: n=%d calls=%d", n, a.calls)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/coverart/ -run TestResolve -v`
Expected: FAIL — `Resolve`/`Options`/`Event` undefined.

- [ ] **Step 3: Write `resolve.go`**

Create `internal/coverart/resolve.go`:

```go
package coverart

import (
	"context"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/lmorchard/byom-sync/internal/rcache"
)

// EventKind classifies a track's art outcome, for narration.
type EventKind string

const (
	KindFilled EventKind = "filled" // art found and written to the track
	KindMiss   EventKind = "miss"   // no art found
	KindError  EventKind = "error"  // lookup error (track left unchanged)
)

// Event reports one track's outcome.
type Event struct {
	Kind     EventKind
	Artist   string
	Title    string
	ImageURL string
	Source   string
	Err      error
}

// Cache short-circuits art resolution: a positive hit fills without a network
// call; a fresh miss skips re-looking-up. Results are written back.
type Cache interface {
	GetArt(key string) (rcache.ArtEntry, bool)
	PutArt(key string, e rcache.ArtEntry) error
}

// Options configures a Resolve run.
type Options struct {
	Budget   *int
	Pace     time.Duration
	Report   func(Event)
	OnFilled func() error
	Cache    Cache
	Now      func() time.Time
	MissTTL  time.Duration
}

// Resolve fills Track.Image for every track in p that lacks one, mutating tracks
// in place. Per-track lookup errors are reported and skipped; only an OnFilled
// error aborts the run.
func Resolve(ctx context.Context, a Arter, p *playlist.Playlist, opts Options) (filled int, err error) {
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
	cachePut := func(key string, e rcache.ArtEntry) {
		if opts.Cache != nil {
			_ = opts.Cache.PutArt(key, e)
		}
	}

	attempted := 0
	for i := range p.Tracks {
		t := &p.Tracks[i]
		if t.Image != "" {
			continue // already has art
		}
		key := t.Key()

		if opts.Cache != nil {
			if e, ok := opts.Cache.GetArt(key); ok {
				switch {
				case e.ImageURL != "":
					t.Image = e.ImageURL
					filled++
					report(Event{Kind: KindFilled, Artist: t.Artist, Title: t.Title, ImageURL: e.ImageURL, Source: "cache"})
					if perr := persist(); perr != nil {
						return filled, perr
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

		res, rerr := a.Resolve(ctx, *t)
		if rerr != nil {
			report(Event{Kind: KindError, Artist: t.Artist, Title: t.Title, Err: rerr})
			continue
		}
		if res.ImageURL == "" {
			cachePut(key, rcache.ArtEntry{CheckedAt: now()})
			report(Event{Kind: KindMiss, Artist: t.Artist, Title: t.Title})
			continue
		}
		t.Image = res.ImageURL
		filled++
		cachePut(key, rcache.ArtEntry{ImageURL: res.ImageURL, Source: res.Source, CheckedAt: now()})
		report(Event{Kind: KindFilled, Artist: t.Artist, Title: t.Title, ImageURL: res.ImageURL, Source: res.Source})
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

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/coverart/ -v`
Expected: PASS (all coverart tests — Task 3 + Task 4).

- [ ] **Step 5: Commit**

```bash
git add internal/coverart/resolve.go internal/coverart/resolve_test.go
git commit -m "feat(coverart): art resolve loop with cache short-circuit

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: `resolve art` command

**Files:**
- Modify: `cmd/resolve.go`

**Interfaces:**
- Consumes: `coverart.{Resolver,MBClient,CAAClient,Resolve,Options,Event,Kind*,MBBaseURL,CAABaseURL,DefaultUserAgent}`, `playlist.{LoadFile,SaveFile}`, existing `hubPaths`/`openCache`/`resolveNoCache`/`log` helpers, Viper (`cache_miss_ttl`, `musicbrainz_user_agent`).
- Produces: a `resolve art` cobra subcommand.

**Context:** Mirror `runResolveSpotify` (same file) but with no auth (MusicBrainz/CAA are public) and no ambiguous/candidate handling. Build the resolver from `coverart` with a default 1.1s delay to respect MusicBrainz's ~1 req/sec limit. `resolve art` fills any track missing `image` regardless of `spotify:false`. Always persist after `Resolve` (cheap; keeps parity with the spotify command's save-always). The User-Agent is read from Viper key `musicbrainz_user_agent`, defaulting to `coverart.DefaultUserAgent`.

- [ ] **Step 1: Add flag vars + command**

At the top var block of `cmd/resolve.go`, add:

```go
var (
	artInput   string
	artLimit   int
	artDelay   time.Duration
	artNoCache bool
)
```

Add the command (near `resolveSpotifyCmd`):

```go
var resolveArtCmd = &cobra.Command{
	Use:   "art",
	Short: "Fill missing cover art from MusicBrainz + the Cover Art Archive",
	Long: `Find cover art for every hub track that has no image yet and write the URL
into the YAML. Looks up MusicBrainz (release-group by artist+album when an album
is present, else the recording by artist+title) and fetches the front cover from
the Cover Art Archive. Public APIs — no key needed. Independent of spotify:false,
so off-Spotify tracks get art too.

--limit caps tracks attempted per run; --delay paces MusicBrainz requests (its
~1 req/sec policy).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runResolveArt(context.Background())
	},
}
```

- [ ] **Step 2: Add the run function**

Add to `cmd/resolve.go`:

```go
func runResolveArt(ctx context.Context) error {
	input := artInput
	if input == "" {
		input = viper.GetString("dir")
	}
	paths, err := hubPaths(input)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		log.Warnf("no playlist YAML files found under %s — nothing to do", input)
		return nil
	}

	ua := viper.GetString("musicbrainz_user_agent")
	if ua == "" {
		ua = coverart.DefaultUserAgent
	}
	resolver := coverart.Resolver{
		MB:  &coverart.MBClient{HTTP: http.DefaultClient, BaseURL: coverart.MBBaseURL, UserAgent: ua},
		CAA: &coverart.CAAClient{HTTP: http.DefaultClient, BaseURL: coverart.CAABaseURL},
	}

	resolveNoCache = artNoCache
	cache, err := openCache()
	if err != nil {
		return fmt.Errorf("open cache: %w", err)
	}
	if cache != nil {
		defer func() { _ = cache.Close() }()
	}
	missTTL := viper.GetDuration("cache_miss_ttl")

	var budget *int
	if artLimit > 0 {
		budget = &artLimit
	}

	total := 0
	for _, path := range paths {
		p, lerr := playlist.LoadFile(path)
		if lerr != nil {
			return fmt.Errorf("load %s: %w", path, lerr)
		}
		need := countMissingArt(p)
		base := filepath.Base(path)
		if need == 0 {
			log.Infof("%s: all tracks have art (%d tracks)", base, len(p.Tracks))
			continue
		}
		log.Infof("%s: %d of %d tracks need art", base, need, len(p.Tracks))

		var got, missed int
		report := func(e coverart.Event) {
			switch e.Kind {
			case coverart.KindFilled:
				got++
				log.Debugf("  art: %s - %s -> %s (via %s)", e.Artist, e.Title, e.ImageURL, e.Source)
			case coverart.KindMiss:
				missed++
				log.Debugf("  no art: %s - %s", e.Artist, e.Title)
			case coverart.KindError:
				log.Warnf("  error: %s - %s: %v", e.Artist, e.Title, e.Err)
			}
		}

		opts := coverart.Options{
			Budget:  budget,
			Pace:    artDelay,
			Report:  report,
			MissTTL: missTTL,
		}
		if cache != nil {
			opts.Cache = cache
		}
		n, rerr := coverart.Resolve(ctx, resolver, &p, opts)
		if serr := playlist.SaveFile(path, p); serr != nil {
			return fmt.Errorf("save %s: %w", path, serr)
		}
		if rerr != nil {
			return fmt.Errorf("resolve art %s: %w", path, rerr)
		}
		log.Infof("%s: %d art filled, %d no-art", base, got, missed)
		total += n
		if budget != nil && *budget <= 0 {
			log.Warnf("art limit reached — stopping (progress saved)")
			break
		}
	}
	log.Warnf("Cover art done: %d track(s) filled", total)
	return nil
}

// countMissingArt counts tracks with no image yet.
func countMissingArt(p playlist.Playlist) int {
	n := 0
	for _, t := range p.Tracks {
		if t.Image == "" {
			n++
		}
	}
	return n
}
```

Add the imports `"net/http"` and `"github.com/lmorchard/byom-sync/internal/coverart"` to `cmd/resolve.go`.

- [ ] **Step 3: Register the command + flags**

In `func init()` of `cmd/resolve.go`, after the spotify registration, add:

```go
	resolveCmd.AddCommand(resolveArtCmd)
	resolveArtCmd.Flags().StringVar(&artInput, "input", "", "hub YAML file or directory (default: config dir)")
	resolveArtCmd.Flags().IntVar(&artLimit, "limit", 0, "max tracks attempted this run (0 = unlimited)")
	resolveArtCmd.Flags().DurationVar(&artDelay, "delay", 1100*time.Millisecond, "pause between MusicBrainz lookups (~1 req/sec policy)")
	resolveArtCmd.Flags().BoolVar(&artNoCache, "no-cache", false, "bypass the art cache")
```

- [ ] **Step 4: Build + smoke-check**

Run: `make build && ./byom-sync resolve art --help`
Expected: build succeeds; help shows the `art` subcommand with four flags. (Live MusicBrainz/CAA behavior is manual.)

Run: `go test ./cmd/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/resolve.go
git commit -m "feat(resolve): add 'resolve art' cover-art command

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Report art coverage in `cache stats`

**Files:**
- Modify: `cmd/resolve.go` (the `resolveCacheStatsCmd` RunE)

**Interfaces:**
- Consumes: `db.ArtStats` (Task 2).
- Produces: an extra log line in `cache stats`.

**Context:** `cache clear` already covers all three tables via Task 2's `Clear`. Add a third stats line after the enrichment line, reusing the existing `missTTL` var.

- [ ] **Step 1: Add the art stats line**

In `resolveCacheStatsCmd`'s `RunE`, after the enrichment `log.Infof(...)` line, add:

```go
	as, err := db.ArtStats(time.Now().Add(-missTTL))
	if err != nil {
		return err
	}
	log.Infof("art cache: %d entries — %d found, %d misses (%d expired)",
		as.Total, as.Positive, as.Negative, as.ExpiredNegative)
```

- [ ] **Step 2: Build + verify**

Run: `make build && make test`
Expected: build + all tests pass.

Run: `./byom-sync resolve cache stats 2>&1 | head`
Expected: three lines — resolution, enrichment, and the new art cache line.

- [ ] **Step 3: Commit**

```bash
git add cmd/resolve.go
git commit -m "feat(resolve): report art cache coverage in 'cache stats'

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Document cover art in AGENTS.md

**Files:**
- Modify: `AGENTS.md`

**Interfaces:**
- Consumes: nothing.
- Produces: documentation only.

**Context:** Document `resolve art`, the `internal/coverart/` package, and the third cache table. Update the `resolve` subcommand list, the `internal/rcache/` bullet, and the Exporters bullet (JSPF now emits `image`).

- [ ] **Step 1: Update the Layout section**

In the `cmd/` bullet, add `art` to the `resolve` subcommands:
`resolve` (subcommands `youtube`, `spotify`, `art`, `prime`, `cache stats`, `cache clear`).

Add a Layout bullet after the `internal/spotifyenrich/` bullet:

```markdown
- `internal/coverart/` — cover-art resolution: `musicbrainz.go` (`MBClient`:
  release-group + recording search), `coverartarchive.go` (`CAAClient`: front
  image for a release/release-group MBID), `resolver.go` (`Resolver`/`Arter`,
  album-first then recording fallback), `resolve.go` (`Resolve` loop, `Options`,
  `Event`, `Cache`). Public APIs, no key; MusicBrainz needs a User-Agent + ~1
  req/sec pacing.
```

Update the `internal/rcache/` bullet to mention the third table:

```markdown
- `internal/rcache/` — SQLite cache with three tables in one `cache.db`:
  `resolution_cache` (YouTube), `enrichment_cache` (Spotify), and `art_cache`
  (cover art: `ArtEntry`, `GetArt`/`PutArt`). `Stats`/`EnrichStats`/`ArtStats`
  and `Clear` span all three; keyed by `Track.Key()`; gitignored, disposable.
```

- [ ] **Step 2: Add a cover-art convention bullet**

Under "Conventions & gotchas", after the Enrichment bullet, add:

```markdown
- **Cover art:** `Track.Image` (album cover URL) is populated by `resolve spotify`
  (free from the Spotify search response) and, for gaps, by `resolve art`, which
  queries MusicBrainz (release-group by artist+album, else recording by
  artist+title) and stores the Cover Art Archive front image. `resolve art` fills
  any track missing an image regardless of `spotify:false`, so off-Spotify tracks
  get art. `Playlist.Image` is playlist-level art (falls back to the first track's
  image at export). Pipeline: `resolve spotify` → `resolve art` → `resolve youtube`
  → `export`.
```

Update the Exporters bullet to note the image member:

```markdown
- **Exporters:** m3u8 builds `{prefix}/{Artist}/{Album}/{Title}.{ext}` paths
  verbatim; jspf uses `urn:isrc:`/`urn:byom:` identifiers + `location`
  (spotify_url) + `image` (track and playlist cover art); markdown is frontmatter
  + tracklist table via the embedded, init-overridable template.
```

- [ ] **Step 3: Verify + commit**

Run: `git diff AGENTS.md` (well-formed, only the intended edits).

```bash
git add AGENTS.md
git commit -m "docs(agents): document cover art (resolve art + coverart package)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Final verification (after all tasks)

- [ ] **Full suite + build:**

Run: `make lint && make test && make build`
Expected: lint clean, all tests pass, build succeeds. Read the output.

- [ ] **Live check (optional, real network):**

`./byom-sync resolve art --input playlists/<a native playlist>.yaml --limit 3`
Confirm at least one track gets an `image:` URL (and, ideally, one of the off-Spotify tracks resolves art via MusicBrainz). Manual, not gating. Adding the tribute-CD album name to the album-less tracks first will sharpen those matches.

---

## Self-Review

**Spec coverage (Phase 3 section):**
- `Track.Image` → JSPF: Task 1 (the previously-missing wiring). `Playlist.Image` + JSPF playlist image w/ first-track fallback: Task 1.
- Spotify art free in Phase 2: already delivered; Task 1 makes it visible.
- `resolve art` via MusicBrainz → Cover Art Archive, own cache table keyed by `Track.Key()`, User-Agent + pacing: Tasks 2–6.
- Album-first / recording-fallback lookup (the approved decision): Task 3 `Resolver.Resolve`.
- `--download` deferred: not built (noted).
- Independent of `spotify:false`: Task 5 `countMissingArt` + loop skip only on existing image.

**Placeholder scan:** none — every code step is complete. HTTP base URLs are injectable for tests; the live path uses the real constants.

**Type consistency:** `coverart.Cache` methods (`GetArt`/`PutArt`) match `rcache.DB` (Task 2) so `*rcache.DB` satisfies it. `rcache.ArtEntry` field set matches between Task 2 (def) and Task 4 (use). `Result`/`Arter` (Task 3) consumed by the loop (Task 4) and command (Task 5). `Options`/`Event`/`Kind*` names match between Task 4 (def) and Task 5 (use: `KindFilled`/`KindMiss`/`KindError`). `Resolve` returns `(int, error)`. Command reuses existing `openCache`/`resolveNoCache`/`hubPaths`/`log`. The `sleep` helper is defined once in `coverart/resolve.go` (not duplicated within the package).
