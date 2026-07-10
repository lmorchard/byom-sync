# Spec — Enrich hand-authored playlists (reverse path) + cover art

**Date:** 2026-07-10
**Branch:** `feat/enrich-playlists`
**Status:** approved design, pending implementation plan

## Summary

Today the hub flows one way: Spotify → hub YAML → resolve YouTube → export. This
adds a **reverse** path — author a playlist by hand in the hub, then *enrich* it
with metadata looked up from Spotify — plus a cross-cutting **cover art**
capability. It reuses the existing resolver shape (`internal/youtube`,
`internal/rcache`, `resolve` subcommands) almost wholesale.

Three phases, built in order, one design:

1. **Native playlists** — let the hub safely hold hand-authored playlists.
2. **Spotify enrichment** — reverse lookup that fills technical metadata.
3. **Cover art** — art URLs on tracks/playlists, exported into JSPF.

## Guiding decisions (from brainstorming)

- **Provenance is derived, never labeled.** A playlist's source comes from *which
  source-ID field is populated*, not a stored `source:` field.
- **Match integrity over convenience.** A wrong Spotify match writes a wrong ISRC,
  and ISRC is the track merge identity (`Track.Key()`). Auto-accept only confident
  matches; flag the rest rather than guess.
- **Preserve authored intent.** Enrichment never overwrites the user's text by
  default; it fills empty technical fields only.
- **Git-friendly hub.** Art is stored as URLs, not downloaded binaries (download
  deferred to a later, optional flag).
- **No premature abstraction.** The enrichment loop rhymes with the YouTube
  resolver but is kept separate; revisit a shared interface only if a third
  resolver appears.

---

## Phase 1 — Native playlists

**Goal:** the hub can hold hand-authored playlists, and nothing Spotify-specific
misfires on them.

### Authoring format

A native playlist is a YAML file in the hub dir with **no `spotify_id`**:

```yaml
title: Late Night Drives
creator: Les
tracks:
  - title: Come Together
    artist: The Beatles
    album: Abbey Road      # optional
  - title: Nightcall
    artist: Kavinsky
```

The existing lenient loader (`playlist.loadFile`) already accepts this — missing
fields default to zero values. `date_created` optional; no per-track `sync_state`
required.

### Provenance helper (core of this phase)

Add to `internal/playlist`:

```go
type Source string
const ( SourceSpotify Source = "spotify"; SourceNative Source = "native" )

func (p Playlist) Source() Source {
    if p.SpotifyID != "" { return SourceSpotify }
    return SourceNative
}
func (p Playlist) IsNative() bool { return p.Source() == SourceNative }
```

Route existing `SpotifyID == ""`-style assumptions through this helper. Extending
to YouTube-sourced playlists later = add a `youtube_playlist_id` field + one case.

### Correctness fixes gated on provenance

1. **JSPF export (`internal/export/jspf.go`, ~line 85).** The orphan block
   (`if !t.SyncState.SpotifyPresent { … spotify_present:false … }`) must be
   skipped for native playlists. A native track's `SpotifyPresent` defaults to
   `false`, so today **every native track would export as "orphaned"** and trip
   byom-player's orphan indicator. Guard becomes:
   `if p.Source() == SourceSpotify && !t.SyncState.SpotifyPresent`.

2. **`sync` (`cmd/sync.go`, `internal/playlist/merge.go`).** `sync` matches local
   files by `FindFileByID(spotify_id)`; native files have no ID so they are
   already invisible to it. Add an explicit guard on the `--all`/merge paths so a
   future change cannot regress this, plus a test.

### Out of scope for Phase 1

No enrichment, no new commands. A native track with no Spotify match and no
YouTube ID exports with title/artist/album and no playable `location` — expected;
Phases 2–3 fill it in.

### Tests

- Minimal native file round-trips through load/save.
- `Source()` / `IsNative()` truth table.
- JSPF export of a native playlist emits **no** `spotify_present` extension.
- JSPF export of a synced Spotify playlist still emits orphan state as before.
- `sync` leaves a native file untouched (including under `--all`).

---

## Phase 2 — Spotify enrichment

**Goal:** fill technical metadata on hand-authored tracks by looking them up in
Spotify, protecting merge identity.

### Command

`resolve spotify [playlists...]`, mirroring `resolve youtube`: same flag
vocabulary (`--limit` budget, `--delay` pace, incremental persist via `SaveFile`,
event narration). Also usable to top up any track missing Spotify metadata.

Flags:
- `--interactive` — resolve flagged (ambiguous) tracks by picking from candidates.
- `--canonicalize` (off by default) — overwrite authored text with Spotify's.
- `--reenrich` — re-attempt tracks that already have a `spotify_id`.

### New package `internal/spotifyenrich`

A `Resolve`-style loop paralleling `youtube.Resolve` in structure (budget, pace,
`Report(Event)`, `OnResolved` persist, cache short-circuit) but filling a *field
set* rather than a single ID. Deliberately **not** abstracted into a shared
generic resolver yet.

### Per-track flow

1. Skip if already enriched (`SpotifyID != ""`) and no `enrich_candidates`,
   unless `--reenrich`. (See the pick-by-editing rule for the
   `spotify_id`+`enrich_candidates` case.)
2. Cache lookup keyed by `Track.Key()`.
3. Build a Spotify search query with field filters:
   `track:"<title>" artist:"<artist>"` (+ `album:"<album>"` when present), falling
   back to a plain concatenated query if the filtered search returns nothing.
4. Score the top candidates; pick the best.
5. **Confident** (score ≥ threshold) → fill fields, strip any stale
   `enrich_candidates`, report `enriched`.
   **Not confident** → leave text untouched, write `enrich_candidates`, report
   `ambiguous`.
   **No results at all** → report `miss`.

### Scoring (tunable, centralized)

Normalized string similarity on artist + title (reuse `playlist.normalize`),
album as a tiebreaker, duration proximity *when the authored track has a
duration* (usually it won't). Combine into a 0–1 score; auto-accept above a
threshold starting ~0.8. Weights/threshold are constants in one place — expected
to need feel-tuning against real playlists.

### Field-fill policy

- Fill only-empty technical fields: `isrc`, `spotify_id`, `spotify_url`,
  `duration_ms`, and `album` if blank.
- Capture album cover art URL into `Track.Image` now (free from the search
  response) — Phase 3 formalizes art but the data is already here.
- Preserve authored `title`/`artist`/`album` by default; `--canonicalize`
  overwrites them with Spotify's strings.

### Ambiguous tracks — `enrich_candidates`

An ambiguous track gets an `enrich_candidates` list (omitempty; enriched tracks
never carry it):

```yaml
- title: Nightcall
  artist: Kavinsky
  enrich_candidates:
    - spotify_id: 0lVo...   # paste this up to `spotify_id:` to accept
      title: Nightcall
      artist: Kavinsky, Lovefoxxx
      album: Nightcall
      isrc: FR...
      duration_ms: 258000
      score: 0.74
    - spotify_id: 7xGf...
      ...
```

**Pick-by-editing rule:** to accept a candidate, copy its `spotify_id` onto the
track's own `spotify_id:` and re-run `resolve spotify`. The loop treats
**`spotify_id` set + `enrich_candidates` still present** as "user picked": fetch
that exact track by ID (`GetTrack`), fill remaining technical fields, and strip
`enrich_candidates`. A track with `spotify_id` and no candidates is done and
skipped. Confident auto-matches also strip stale candidates. Re-running unchanged
rewrites the same candidate list (idempotent).

`--interactive` is the alternative: for below-threshold tracks, show the top N
candidates and let the user pick or skip inline.

### Event kinds

`enriched`, `ambiguous`, `miss`, `error` (mirroring the YouTube resolver's
event-reporting style).

### Cache

Mirror the `rcache` pattern as a **second table in the existing `cache.db`** (one
cache artifact, gitignored/disposable). Stores resolved metadata (or a miss)
keyed by `Track.Key()`. Cache writes use the *pre-enrichment* key (the lookup key
for next run). `resolve prime` can later repopulate positives from the hub.

### Identity-mutation ordering

`Track.Key()` is ISRC-first. A native track keys as `at:artist\ttitle`; once
enrichment fills ISRC its key changes to `isrc:…`. Therefore **enrich before
resolving YouTube** so the YouTube cache keys on the stable ISRC. Document the
recommended pipeline order in AGENTS.md:
author/`sync` → `resolve spotify` → `resolve youtube` → `export`.

### Tests

- Query construction (filtered + fallback).
- Scoring truth table incl. confident vs. ambiguous boundary.
- Confident match fills only-empty fields, preserves authored text, sets `Image`.
- `--canonicalize` overwrites text.
- Ambiguous track writes `enrich_candidates`; pick-by-editing applies + strips.
- Cache round-trip; cache short-circuit avoids network.
- Budget/pace honored.

---

## Phase 3 — Cover art

**Goal:** art URLs on tracks and playlists, visible to byom-player via JSPF.

### Schema

- `Track.Image string` (`omitempty`) — album cover URL.
- `Playlist.Image string` (`omitempty`) — playlist-level art.

### JSPF output

Add `image` to `jspfTrack` and `jspfPlaylist` (both valid JSPF members), emitted
when set.

### Sourcing (Spotify-first, MusicBrainz fallback)

- **Spotify art is free in Phase 2** — the enrichment search response carries
  `Album.Images` (multiple sizes). Pick the largest ≤ ~640px, store on
  `Track.Image`. A confidently-enriched track already has art with no extra calls.
- **`resolve art [playlists...]`** fills *gaps*: tracks with no `image` (native
  tracks that never matched Spotify, or Spotify tracks lacking art). Queries
  **MusicBrainz** to resolve a release, fetches the cover from the **Cover Art
  Archive**. Its own resolver + cache table in `cache.db`, keyed by `Track.Key()`.
  MusicBrainz requires a descriptive User-Agent and ~1 req/sec — the existing
  pace/budget machinery covers this.

### Playlist-level image

Derived, not fetched — default to the first track's `image` (or leave empty). No
separate playlist-art lookup unless wanted later.

### `--download`

Explicitly deferred. A later flag would fetch URLs into a hub asset dir and
rewrite `image` to a relative path. Phase 3 ships URLs only.

### Tests

- JSPF emits `image` when set, omits when empty.
- Enrichment populates `Track.Image` from Spotify album images.
- `resolve art` fills only gap tracks, skips tracks that already have art.
- MusicBrainz client respects pacing / User-Agent.
- Cache round-trip.

---

## Cross-cutting notes

- **AGENTS.md** updates: document the reverse/native path, the derived-provenance
  model, the recommended pipeline order, and the new `resolve` subcommands.
- **`resolve prime` / `cache stats` / `cache clear`** should account for the new
  cache tables (enrichment, art) alongside the existing YouTube table.
- **Verification:** live Spotify/MusicBrainz behavior needs a real account +
  registered app (manual). Automated tests use fakes/fixtures.
