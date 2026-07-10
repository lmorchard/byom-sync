# Phase 4 — Spotify Art (capture + backfill) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Get Spotify album art onto tracks that have a `spotify_id`: capture it during `sync` going forward, and backfill existing tracks by making `resolve art` Spotify-first (batched fetch by id) with the MusicBrainz path as the fallback for genuinely off-Spotify tracks.

**Architecture:** A shared `spotifyfetch.PickImage` (moved from `spotifyenrich`) picks the best album image; `sync`'s `convert()` now sets `Track.Image` from it. A new batched `spotifyfetch.FetchTrackArt` fetches album art for many track ids via `client.GetTracks` (50/call). `resolve art` runs a Spotify pass first (fill art for tracks with a `spotify_id`), then the existing MusicBrainz per-track pass on whatever remains; it degrades to MusicBrainz-only (with a warning) when there's no Spotify token. Cover Art Archive URLs are normalized to `https`.

**Tech Stack:** Go 1.25 · `github.com/zmb3/spotify/v2` · stdlib. No new deps.

## Global Constraints

- Go 1.25; no cgo.
- Formatting via `gofumpt` (`make format`); lint via golangci-lint v2 (`make lint`); **errcheck strict** (`_ =` for ignored returns).
- Run `make lint && make test && make build` before claiming done; read output.
- Commit trailer on every commit: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
- Branch `feat/spotify-art` (stacked on `feat/cover-art` / PR #20). No direct pushes to `main`.
- **WORKING TREE HYGIENE:** pre-existing uncommitted changes (`.gitignore`, untracked `playlists/`, `tmp/`, etc.) are NOT part of this work — never touch them; never run `git checkout`/`restore`/`stash`/`reset`/`clean` or `git add .`/`-A`. Stage only each task's files by explicit path.
- **Live Spotify behavior is manual** (needs a real account/token). Automated tests use fakes/interfaces.

## File Structure

- `internal/spotifyfetch/fetch.go` — **modify.** Add exported `PickImage` + `DefaultImageMaxWidth` (moved from spotifyenrich); `convert()` sets `Image`; add `FetchTrackArt` + a `trackGetter` interface.
- `internal/spotifyfetch/fetch_test.go` — **modify.** `TestPickImage` (moved here), a convert-sets-image test, and a `FetchTrackArt` test with a fake getter.
- `internal/spotifyenrich/search.go` — **modify.** Remove local `pickImage`/`defaultImageMaxWidth`; use `spotifyfetch.PickImage`/`spotifyfetch.DefaultImageMaxWidth`.
- `internal/spotifyenrich/search_test.go` — **modify.** Remove `TestPickImage` (moved to spotifyfetch).
- `internal/coverart/coverartarchive.go` — **modify.** Normalize returned image URLs to `https`.
- `internal/coverart/coverart_test.go` — **modify.** Assert an `http://` CAA URL is returned as `https://`.
- `cmd/resolve.go` — **modify.** `runResolveArt`: Spotify-first pass (batched) + auth-degrade, then the MusicBrainz pass; add `applyTrackArt` helper.
- `cmd/resolve_spotify_test.go` — **modify.** Unit-test `applyTrackArt`.
- `AGENTS.md` — **modify.** Document Spotify-first `resolve art` + sync art capture.

---

### Task 1: Shared `PickImage` + capture art in `sync`

**Files:**
- Modify: `internal/spotifyfetch/fetch.go`, `internal/spotifyenrich/search.go`
- Test: `internal/spotifyfetch/fetch_test.go`, `internal/spotifyenrich/search_test.go`

**Interfaces:**
- Produces: `spotifyfetch.PickImage(images []spotify.Image, maxWidth int) string`, `spotifyfetch.DefaultImageMaxWidth = 640`.
- `convert()` now sets `Track.Image`.
- `spotifyenrich` consumes `spotifyfetch.PickImage` instead of its local copy.

**Context:** `pickImage` + `defaultImageMaxWidth` currently live in `internal/spotifyenrich/search.go` (used by `toCandidate`). Move them to `spotifyfetch` (the Spotify→model adapter, the natural home), exported, and have both `spotifyfetch.convert` and `spotifyenrich.toCandidate` use the shared version. No import cycle: `spotifyfetch` does not import `spotifyenrich`.

- [ ] **Step 1: Write/move the failing tests**

Add to `internal/spotifyfetch/fetch_test.go` (moved from spotifyenrich, `pickImage`→`PickImage`):

```go
func TestPickImage(t *testing.T) {
	imgs := []spotify.Image{{URL: "xl", Width: 1000}, {URL: "l", Width: 640}, {URL: "s", Width: 64}}
	if got := PickImage(imgs, 640); got != "l" {
		t.Errorf("largest<=640: got %q", got)
	}
	if got := PickImage([]spotify.Image{{URL: "xl", Width: 1000}}, 640); got != "xl" {
		t.Errorf("fallback smallest-above-cap: got %q", got)
	}
	if got := PickImage(nil, 640); got != "" {
		t.Errorf("empty: got %q", got)
	}
}

func TestConvertCapturesImage(t *testing.T) {
	item := spotify.PlaylistItem{Track: spotify.PlaylistItemTrack{Track: &spotify.FullTrack{
		SimpleTrack: spotify.SimpleTrack{Name: "T", Artists: []spotify.SimpleArtist{{Name: "A"}}},
		Album:       spotify.SimpleAlbum{Name: "Alb", Images: []spotify.Image{{URL: "cover", Width: 640}}},
	}}}
	got := convert(item)
	if got.Image != "cover" {
		t.Errorf("convert should capture album art: %q", got.Image)
	}
}
```

If the `spotify.PlaylistItem`/`PlaylistItemTrack`/`FullTrack` literal shape doesn't compile against v2.4.3, adjust nesting to match (`go doc github.com/zmb3/spotify/v2 PlaylistItem` / `FullTrack`); the asserted value (`Image == "cover"`) is what matters.

Remove `TestPickImage` from `internal/spotifyenrich/search_test.go`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/spotifyfetch/ -run 'TestPickImage|TestConvertCapturesImage' -v`
Expected: FAIL — `undefined: PickImage`, and `convert` doesn't set Image.

- [ ] **Step 3: Move `PickImage` into spotifyfetch + set Image in convert()**

In `internal/spotifyfetch/fetch.go`, add near the top-level helpers:

```go
// DefaultImageMaxWidth is the preferred upper bound for album art width.
const DefaultImageMaxWidth = 640

// PickImage returns the URL of the largest album image no wider than maxWidth;
// if none qualify it returns the smallest available; "" when there are none.
func PickImage(images []spotify.Image, maxWidth int) string {
	best := ""
	bestW := -1
	fallback := ""
	fallbackW := 1 << 30
	for _, img := range images {
		w := int(img.Width)
		if w <= maxWidth {
			if w > bestW {
				bestW = w
				best = img.URL
			}
		} else if w < fallbackW {
			fallbackW = w
			fallback = img.URL
		}
	}
	if best != "" {
		return best
	}
	return fallback
}
```

In `convert()`, add the `Image` field:

```go
		DurationMS: int(ft.Duration),
		Image:      PickImage(ft.Album.Images, DefaultImageMaxWidth),
		AddedAt:    item.AddedAt,
```

- [ ] **Step 4: Update spotifyenrich to use the shared helper**

In `internal/spotifyenrich/search.go`:
- Delete the local `defaultImageMaxWidth` const and the `pickImage` function.
- In `toCandidate`, change `Image: pickImage(ft.Album.Images, defaultImageMaxWidth)` to `Image: spotifyfetch.PickImage(ft.Album.Images, spotifyfetch.DefaultImageMaxWidth)`.
- Add `"github.com/lmorchard/byom-sync/internal/spotifyfetch"` to the imports.

(Also remove any now-unused import in `search.go` if `pickImage`'s removal orphans one — run the build to check.)

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/spotifyfetch/ ./internal/spotifyenrich/ -v`
Expected: PASS (moved `TestPickImage`, new convert test, and all existing spotifyenrich tests still green using the shared helper).

- [ ] **Step 6: Commit**

```bash
git add internal/spotifyfetch/fetch.go internal/spotifyfetch/fetch_test.go internal/spotifyenrich/search.go internal/spotifyenrich/search_test.go
git commit -m "feat(sync): capture Spotify album art; share PickImage in spotifyfetch

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Batched `FetchTrackArt`

**Files:**
- Modify: `internal/spotifyfetch/fetch.go`
- Test: `internal/spotifyfetch/fetch_test.go`

**Interfaces:**
- Produces:
  - `type TrackGetter interface { GetTracks(ctx context.Context, ids []spotify.ID, opts ...spotify.RequestOption) ([]*spotify.FullTrack, error) }` (`*spotify.Client` satisfies it).
  - `func FetchTrackArt(ctx context.Context, g TrackGetter, ids []string, maxWidth int) (map[string]string, error)` — maps `spotify_id` → best album-image URL for the ids that resolved and have art.

**Context:** `client.GetTracks` accepts up to 50 ids per call and returns `[]*spotify.FullTrack` in request order (nil entries for not-found ids). `FetchTrackArt` chunks ids into 50s, calls the getter, and builds a map from `string(ft.ID)` to `PickImage(ft.Album.Images, maxWidth)`, skipping nil tracks and empty-image results. The `TrackGetter` interface makes it unit-testable with a fake (no live Spotify).

- [ ] **Step 1: Write the failing test**

Add to `internal/spotifyfetch/fetch_test.go`:

```go
type fakeTrackGetter struct {
	byID  map[string]*spotify.FullTrack
	calls [][]spotify.ID
}

func (f *fakeTrackGetter) GetTracks(_ context.Context, ids []spotify.ID, _ ...spotify.RequestOption) ([]*spotify.FullTrack, error) {
	f.calls = append(f.calls, ids)
	out := make([]*spotify.FullTrack, len(ids))
	for i, id := range ids {
		out[i] = f.byID[string(id)] // nil when absent
	}
	return out, nil
}

func TestFetchTrackArt(t *testing.T) {
	withArt := func(id, url string) *spotify.FullTrack {
		return &spotify.FullTrack{
			SimpleTrack: spotify.SimpleTrack{ID: spotify.ID(id)},
			Album:       spotify.SimpleAlbum{Images: []spotify.Image{{URL: url, Width: 640}}},
		}
	}
	g := &fakeTrackGetter{byID: map[string]*spotify.FullTrack{
		"a": withArt("a", "art-a"),
		"c": withArt("c", ""), // resolved but no images -> skipped
		// "b" not found -> nil
	}}
	got, err := FetchTrackArt(context.Background(), g, []string{"a", "b", "c"}, 640)
	if err != nil {
		t.Fatalf("FetchTrackArt: %v", err)
	}
	if got["a"] != "art-a" {
		t.Errorf("a: got %q", got["a"])
	}
	if _, ok := got["b"]; ok {
		t.Errorf("not-found id should be absent: %v", got["b"])
	}
	if _, ok := got["c"]; ok {
		t.Errorf("no-image id should be absent: %v", got["c"])
	}
}

func TestFetchTrackArt_Chunks(t *testing.T) {
	ids := make([]string, 120)
	for i := range ids {
		ids[i] = fmt.Sprintf("id%d", i)
	}
	g := &fakeTrackGetter{byID: map[string]*spotify.FullTrack{}}
	if _, err := FetchTrackArt(context.Background(), g, ids, 640); err != nil {
		t.Fatalf("FetchTrackArt: %v", err)
	}
	if len(g.calls) != 3 { // 120 ids -> 50 + 50 + 20
		t.Errorf("expected 3 chunked calls, got %d", len(g.calls))
	}
	if len(g.calls[0]) != 50 || len(g.calls[2]) != 20 {
		t.Errorf("chunk sizes wrong: %d / %d", len(g.calls[0]), len(g.calls[2]))
	}
}
```

Ensure `fetch_test.go` imports `"context"` and `"fmt"`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/spotifyfetch/ -run TestFetchTrackArt -v`
Expected: FAIL — `undefined: FetchTrackArt` / `TrackGetter`.

- [ ] **Step 3: Implement**

Add to `internal/spotifyfetch/fetch.go`:

```go
// TrackGetter fetches full tracks by id (satisfied by *spotify.Client). Abstracted
// for testability.
type TrackGetter interface {
	GetTracks(ctx context.Context, ids []spotify.ID, opts ...spotify.RequestOption) ([]*spotify.FullTrack, error)
}

// artBatchSize is the Spotify GetTracks per-call id limit.
const artBatchSize = 50

// FetchTrackArt returns a map of spotify track id -> best album-image URL for the
// given ids, fetched in batches of 50. Ids that don't resolve, or resolve without
// album art, are simply absent from the map.
func FetchTrackArt(ctx context.Context, g TrackGetter, ids []string, maxWidth int) (map[string]string, error) {
	out := make(map[string]string, len(ids))
	for start := 0; start < len(ids); start += artBatchSize {
		end := start + artBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		chunk := make([]spotify.ID, 0, end-start)
		for _, id := range ids[start:end] {
			chunk = append(chunk, spotify.ID(id))
		}
		tracks, err := g.GetTracks(ctx, chunk)
		if err != nil {
			return nil, fmt.Errorf("get tracks: %w", err)
		}
		for _, ft := range tracks {
			if ft == nil {
				continue
			}
			if url := PickImage(ft.Album.Images, maxWidth); url != "" {
				out[string(ft.ID)] = url
			}
		}
	}
	return out, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/spotifyfetch/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/spotifyfetch/fetch.go internal/spotifyfetch/fetch_test.go
git commit -m "feat(spotifyfetch): batched FetchTrackArt (album art by track id)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Normalize Cover Art Archive URLs to https

**Files:**
- Modify: `internal/coverart/coverartarchive.go`
- Test: `internal/coverart/coverart_test.go`

**Interfaces:**
- Consumes/Produces: internal to `FrontImage` — the returned URL is upgraded from `http://` to `https://`.

**Context:** The Cover Art Archive JSON sometimes returns `http://` image URLs, which are mixed-content-blocked when byom-player runs on an https page. `coverartarchive.org` serves https, so upgrade the scheme before returning.

- [ ] **Step 1: Write the failing test**

Add to `internal/coverart/coverart_test.go`:

```go
func TestResolver_UpgradesHTTPArtToHTTPS(t *testing.T) {
	srv, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/ws/2/release-group"):
			_, _ = w.Write([]byte(`{"release-groups":[{"id":"rg"}]}`))
		case r.URL.Path == "/release-group/rg":
			// CAA returns an http:// thumbnail URL
			_, _ = w.Write([]byte(`{"images":[{"front":true,"image":"http://caa/x.jpg","thumbnails":{"500":"http://caa/500.jpg"}}]}`))
		default:
			http.NotFound(w, r)
		}
	})
	r := Resolver{
		MB:  &MBClient{HTTP: srv.Client(), BaseURL: srv.URL + "/ws/2", UserAgent: "ua"},
		CAA: &CAAClient{HTTP: srv.Client(), BaseURL: srv.URL},
	}
	res, err := r.Resolve(context.Background(), playlist.Track{Artist: "A", Title: "B", Album: "C"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.ImageURL != "https://caa/500.jpg" {
		t.Errorf("http art URL should be upgraded to https: %q", res.ImageURL)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/coverart/ -run TestResolver_UpgradesHTTPArtToHTTPS -v`
Expected: FAIL — got `http://caa/500.jpg`.

- [ ] **Step 3: Implement**

In `internal/coverart/coverartarchive.go`, add a helper and apply it to both return paths in `FrontImage`:

```go
import (
	...
	"strings"
)

// httpsURL upgrades an http:// URL to https:// (Cover Art Archive serves https;
// http URLs would be mixed-content-blocked in an https page).
func httpsURL(u string) string {
	if rest, ok := strings.CutPrefix(u, "http://"); ok {
		return "https://" + rest
	}
	return u
}
```

In `FrontImage`, wrap the two returned URLs:

```go
		if u500 := img.Thumbnails["500"]; u500 != "" {
			return httpsURL(u500), nil
		}
		return httpsURL(img.Image), nil
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/coverart/ -v`
Expected: PASS (new test + all existing coverart tests; existing tests using `https://caa/...` URLs are unaffected since `httpsURL` leaves https untouched).

- [ ] **Step 5: Commit**

```bash
git add internal/coverart/coverartarchive.go internal/coverart/coverart_test.go
git commit -m "fix(coverart): normalize Cover Art Archive URLs to https

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Spotify-first `resolve art`

**Files:**
- Modify: `cmd/resolve.go`
- Test: `cmd/resolve_spotify_test.go`

**Interfaces:**
- Consumes: `auth.Client`, `spotifyfetch.{FetchTrackArt,DefaultImageMaxWidth}`, `coverart.*`, `playlist.*`, existing `openCache`/`hubPaths`/`log`.
- Produces: a restructured `runResolveArt`; a helper `applyTrackArt(p *playlist.Playlist, artByID map[string]string) int`.

**Context:** Restructure `runResolveArt` into two passes per playlist:
1. **Spotify pass (best source, batched):** collect the ids of tracks that lack an `image` and have a `spotify_id`; `spotifyfetch.FetchTrackArt` them; fill `Track.Image` via `applyTrackArt`. Requires an authenticated client — obtained once up front; if `auth.Client` fails (no token), log a warning and skip the Spotify pass (MusicBrainz-only).
2. **MusicBrainz pass (fallback):** run the existing `coverart.Resolve` over whatever tracks still lack an image (off-Spotify, or Spotify had no art). `--limit`/`--delay` apply to this pass (the Spotify pass is cheap/batched).

`applyTrackArt` is a pure helper: for each track with `Image == ""` whose `SpotifyID` is in `artByID`, set `Image` and count. It's the unit-tested seam; the auth + network orchestration around it is build/help-verified (live is manual).

- [ ] **Step 1: Write the failing test (the helper)**

Add to `cmd/resolve_spotify_test.go`:

```go
func TestApplyTrackArt(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{
		{Title: "A", SpotifyID: "s1"},                 // gets art
		{Title: "B", SpotifyID: "s2", Image: "keep"},  // already has image -> untouched
		{Title: "C", SpotifyID: "s3"},                 // no art in map -> untouched
		{Title: "D"},                                  // no spotify_id -> untouched
	}}
	n := applyTrackArt(p, map[string]string{"s1": "art1", "s2": "art2"})
	if n != 1 {
		t.Fatalf("filled = %d, want 1", n)
	}
	if p.Tracks[0].Image != "art1" {
		t.Errorf("track A should get art1: %q", p.Tracks[0].Image)
	}
	if p.Tracks[1].Image != "keep" {
		t.Errorf("track B image must not be overwritten: %q", p.Tracks[1].Image)
	}
	if p.Tracks[2].Image != "" || p.Tracks[3].Image != "" {
		t.Errorf("tracks C/D should stay imageless")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -run TestApplyTrackArt -v`
Expected: FAIL — `undefined: applyTrackArt`.

- [ ] **Step 3: Implement the helper + restructure runResolveArt**

Add the helper to `cmd/resolve.go`:

```go
// applyTrackArt fills Image for tracks that lack one and whose spotify_id has art
// in artByID. Returns how many were filled.
func applyTrackArt(p *playlist.Playlist, artByID map[string]string) int {
	filled := 0
	for i := range p.Tracks {
		t := &p.Tracks[i]
		if t.Image != "" || t.SpotifyID == "" {
			continue
		}
		if url, ok := artByID[t.SpotifyID]; ok && url != "" {
			t.Image = url
			filled++
		}
	}
	return filled
}
```

Restructure `runResolveArt`. Keep the resolver/cache/budget setup, but add the Spotify pass before the MusicBrainz loop. Replace the body with:

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

	// Spotify pass client (best art source). Optional: degrade to MusicBrainz-only
	// when there's no token.
	var spotClient *spotify.Client
	if client, tok, aerr := auth.Client(ctx, viper.GetString("client_id"), viper.GetInt("redirect_port")); aerr != nil {
		log.Warnf("no Spotify token (%v) — filling art from MusicBrainz only; run `byom-sync auth` for Spotify art", aerr)
	} else {
		spotClient = client
		defer auth.PersistRefreshed(client, tok)
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
		if countMissingArt(p) == 0 {
			log.Infof("%s: all tracks have art (%d tracks)", filepath.Base(path), len(p.Tracks))
			continue
		}
		base := filepath.Base(path)

		// Spotify pass: batch-fetch album art by id for imageless tracks.
		spot := 0
		if spotClient != nil {
			var ids []string
			for _, t := range p.Tracks {
				if t.Image == "" && t.SpotifyID != "" {
					ids = append(ids, t.SpotifyID)
				}
			}
			if len(ids) > 0 {
				artByID, ferr := spotifyfetch.FetchTrackArt(ctx, spotClient, ids, spotifyfetch.DefaultImageMaxWidth)
				if ferr != nil {
					return fmt.Errorf("spotify art %s: %w", path, ferr)
				}
				spot = applyTrackArt(&p, artByID)
			}
		}

		// MusicBrainz pass: fill whatever still lacks art.
		need := countMissingArt(p)
		var got, missed int
		if need > 0 {
			log.Infof("%s: %d from Spotify; %d remaining for MusicBrainz", base, spot, need)
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
			opts := coverart.Options{Budget: budget, Pace: artDelay, Report: report, MissTTL: missTTL}
			if cache != nil {
				opts.Cache = cache
			}
			n, rerr := coverart.Resolve(ctx, resolver, &p, opts)
			if rerr != nil {
				if serr := playlist.SaveFile(path, p); serr != nil {
					return fmt.Errorf("save %s: %w", path, serr)
				}
				return fmt.Errorf("resolve art %s: %w", path, rerr)
			}
			got += n // (n counts MusicBrainz fills; got mirrors it via events)
		} else {
			log.Infof("%s: %d filled from Spotify (none left for MusicBrainz)", base, spot)
		}

		if serr := playlist.SaveFile(path, p); serr != nil {
			return fmt.Errorf("save %s: %w", path, serr)
		}
		total += spot + got
		log.Infof("%s: %d art filled (%d Spotify, %d MusicBrainz), %d no-art", base, spot+got, spot, got, missed)
		if budget != nil && *budget <= 0 {
			log.Warnf("art limit reached — stopping (progress saved)")
			break
		}
	}
	log.Warnf("Cover art done: %d track(s) filled", total)
	return nil
}
```

Note: `got` is incremented by the event handler (`KindFilled`) AND by `n`; that double-counts. Use only the event handler count — so drop `got += n` and rely on `got` from events (which counts each `KindFilled`). Correct the code: remove the `got += n` line; keep `n` only for the return-value check. (Implementer: make `got` come solely from the report events; `n` from `coverart.Resolve` equals the event `KindFilled` count, so use one source — the events — to avoid double counting.)

Add imports to `cmd/resolve.go` if missing: `"github.com/lmorchard/byom-sync/internal/spotifyfetch"` and `"github.com/zmb3/spotify/v2"`.

- [ ] **Step 4: Run tests + build + smoke-check**

Run: `go test ./cmd/ -run 'TestApplyTrackArt|TestCountNeedingEnrich' -v` → PASS.
Run: `make build && ./byom-sync resolve art --help` → shows the command (flags unchanged: `--input`/`--limit`/`--delay`/`--no-cache`).
Run: `make test` → all packages pass.

- [ ] **Step 5: Commit**

```bash
git add cmd/resolve.go cmd/resolve_spotify_test.go
git commit -m "feat(resolve): Spotify-first 'resolve art' (batched art by id, MusicBrainz fallback)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Document Spotify art in AGENTS.md

**Files:**
- Modify: `AGENTS.md`

**Context:** Update the cover-art convention bullet and the Sync bullet to reflect: `sync` now captures album art; `resolve art` is Spotify-first (batched by id) with MusicBrainz as the fallback, degrading to MusicBrainz-only without a token.

- [ ] **Step 1: Edit the Sync bullet**

Append to the **Sync** bullet in "Conventions & gotchas": note that `convert()` now captures album art into `Track.Image` from `Album.Images`.

- [ ] **Step 2: Rewrite the cover-art convention bullet**

Update the **Cover art** bullet to describe the new flow: `Track.Image` comes from `sync` (album art captured at fetch), `resolve spotify` (enrichment), or `resolve art`. `resolve art` is now Spotify-first — a batched `GetTracks`-by-id pass fills art for tracks with a `spotify_id`, then MusicBrainz (release-group by artist+album, else recording) fills the rest; it degrades to MusicBrainz-only (with a warning) when there's no Spotify token. CAA URLs are normalized to https.

- [ ] **Step 3: Verify + commit**

Run: `git diff AGENTS.md` (well-formed, only intended edits).

```bash
git add AGENTS.md
git commit -m "docs(agents): document Spotify art capture + Spotify-first resolve art

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Final verification

- [ ] Run: `make lint && make test && make build` — all green.
- [ ] Live check (optional, manual, needs a token): `./byom-sync resolve art --input playlists/<small synced playlist>.yaml --limit 0` — confirm the Spotify pass fills most/all tracks fast, and the summary shows "N Spotify, M MusicBrainz".

---

## Self-Review

**Coverage:** shared `PickImage` (Task 1); sync captures art (Task 1); batched Spotify art fetch (Task 2); CAA https normalization (Task 3); Spotify-first `resolve art` with auth-degrade + MusicBrainz fallback (Task 4); docs (Task 5). Matches the approved design (unify into `resolve art`, degrade to MusicBrainz-only, capture in sync, https fix).

**Placeholder scan:** none — complete code. Two library-shape caveats flagged with fallback instructions (the `PlaylistItem`/`FullTrack` test literals in Tasks 1–2). Task 4 explicitly calls out and corrects a double-count (`got`) so the implementer doesn't ship it.

**Type consistency:** `spotifyfetch.PickImage`/`DefaultImageMaxWidth` defined in Task 1, consumed by Task 2 (`FetchTrackArt`) and Task 4 (command). `TrackGetter` satisfied by `*spotify.Client` (Task 4 passes `spotClient`). `applyTrackArt` signature matches between Task 4 def and its test. `coverart.Resolve`/`Options`/`Event` unchanged from Phase 3. `FetchTrackArt` returns `map[string]string` consumed by `applyTrackArt`.
