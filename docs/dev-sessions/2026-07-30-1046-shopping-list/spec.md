# Shopping list: what my collection is missing

Issues: [byom-sync#50](https://github.com/lmorchard/byom-sync/issues/50) (Phase A,
enrichment) · [byom-player#55](https://github.com/lmorchard/byom-player/issues/55)
(Phase B, panel)

## Problem

A playlist played through a personal-collection provider (Subsonic, Jellyfin,
Plex) will have tracks the collection doesn't hold. The player already knows
which ones — it marks them `unavailable` as it scans — but that knowledge is
scattered across tracklist rows and lost when the page closes.

Turn it into a shopping list: the missing tracks, collated by artist and album,
each with a link to somewhere the music can be bought.

## What already exists

byom-player:

- `SubsonicProvider`, `JellyfinProvider`, `PlexProvider` in `src/providers/`.
- `AudioProvider.checkAvailability()` returns `'available' | 'unavailable' |
  'unknown'` (`src/providers/types.ts:13`). The tri-state matters: `unavailable`
  is a clean miss, `unknown` is a transient failure that must not become a
  shopping-list row.
- `AvailabilityQueue` (`src/availability.ts`) runs throttled checks, one at a
  time, 300ms between uncached ones. Checked indices persist for the session and
  are never re-checked.
- `LocalStorageResolutionCache` (`src/providers/resolutionCache.ts`) caches
  resolutions per provider scope, including negative results with a TTL.
- `manifest.ts:59` already reads a `resolved` object out of the byom-sync JSPF
  extension namespace, so there is an established path for sync to hand
  pre-computed data to the player.

byom-sync:

- A MusicBrainz client (`internal/coverart/musicbrainz.go`).
- `rcache`, a SQLite cache with an `art_cache` table whose hit/miss/TTL shape
  (`internal/rcache/art.go`) is the model for anything similar.
- `resolve youtube` / `resolve art`: incremental, resumable enrichment passes
  with `--limit` and `--delay`.

Missing: nothing aggregates the misses, nothing renders them, and no purchase
links exist anywhere.

## Feasibility findings

Both verified against live APIs on 2026-07-30, not assumed.

**Bandcamp has no usable API.** The official one at `bandcamp.com/developer` is
sales reporting for artists and labels who already sell on the platform. There
is no public search or metadata endpoint. Third-party wrappers are scrapers,
which a browser can't call anyway for CORS reasons.

**MusicBrainz carries real purchase links.** A release lookup with
`?inc=url-rels` returns typed URL relations, including `purchase for download`
(relationship type `98e08c20-8402-4163-8970-53504bb6a1e4`). A probe for Amanda
Palmer's *Theatre Is Evil* returned:

```
download for free -> https://amandapalmer.bandcamp.com/album/theatre-is-evil-2
```

`ws/2` responds with `access-control-allow-origin: *`, so a browser could call
it directly — but see the architecture decision below for why it won't.

**Coverage is per-edition and spotty.** That same probe found three MusicBrainz
releases titled *Theatre Is Evil*. Only one carried the Bandcamp link; the
second had an Amazon ASIN and the third had nothing. Getting a good link means
picking the right edition among many, at roughly 1 request per second.

## Architecture

Fetching is front-loaded into site generation. byom-sync resolves purchase links
during enrichment and bakes them into the hub; the player only displays what it
finds and falls back to a search URL otherwise.

This keeps rate-limited network work out of the browser, reuses the MusicBrainz
client and cache byom-sync already has, and makes the links durable and
version-controlled rather than dependent on a live third-party call at view time.

The two halves ship independently. Phase B is useful on day one using only
constructed search URLs, so it does not block on Phase A's slow initial fill.

### Phase A — byom-sync: `resolve purchase`

A new enrichment command with the same ergonomics as its siblings: `--input`,
`--limit`, `--delay`, attempts only what's unresolved, resumable.

Per distinct album key (normalized artist + album):

1. Search MusicBrainz releases by artist and album.
2. For the top candidate, `GET /release/<mbid>?inc=url-rels`.
3. Keep relations typed `purchase for download`, `purchase for mail-order`, or
   `download for free`. Exclude the streaming types — this is a shopping list.
4. On no match, try the next candidate, capped at **3 lookups per album**. The
   cap is a deliberate cost ceiling, not a claim about correctness.
5. When several relations match, prefer a Bandcamp host.

Deduplicating by album key matters for request cost even though results are
stored per track: the hub averages 1.93 tracks per distinct album. A single
resolution is written to `purchase_url` on **every** track sharing that album
key, across every playlist in the hub.

Tracks with no album (41 in the current hub) are keyed by artist + title
instead, resolved as a recording rather than a release, and fall back to an
artist + title search URL in the player.

Results cache in a new `purchase_cache` table in `rcache`, mirroring `art_cache`
exactly — URL, source, `checked_at`, and negative entries with a miss TTL so an
album that later appears on Bandcamp gets retried rather than skipped forever.

### Phase A — schema and wire format

One new field on `playlist.Track` (`internal/playlist/types.go:63`):

```yaml
purchase_url: https://amandapalmer.bandcamp.com/album/theatre-is-evil-2
```

Stored per track rather than in an album-keyed sidecar. Measured against the
live hub, a sidecar was the wrong call:

```
58 playlist files · 13,873 tracks · 13,832 with an album
7,165 distinct (artist, album) pairs
duplication factor: 1.93 tracks per album
```

A sidecar would concentrate 7,165 entries into one ~30k-line file while saving
only about half the total lines, since most albums appear exactly once. Per-track
spreads ~13,832 lines across 58 files, keeps each playlist self-contained, and
needs no join. It also handles the case where Bandcamp sells a single track,
which is often what a shopping list actually wants.

No `purchase_source` field — the player derives a display label from the URL's
hostname.

Emitted by `internal/export/jspf.go` as a sibling of `resolved` inside the
existing extension element:

```json
"extension": { "https://github.com/lmorchard/byom-sync": [
  { "resolved": {"youtube": "..."}, "purchase_url": "https://..." }
] }
```

### Phase B — byom-player: the shopping list panel

**Availability.** The action appears only when the active provider implements
`checkAvailability` — Subsonic, Jellyfin, Plex. Hidden for YouTube and Spotify.

**The sweep is summoned, never automatic.** A full-playlist scan only starts when
the user explicitly opens the shopping list. Lazy visible-window scanning behaves
exactly as it does today when the panel is closed. This is a firm constraint: the
sweep is throttled and expensive, and must not run on its own.

While sweeping:

- Queue every track index, bypassing the `retain()` pruning that normally drops
  scrolled-past rows (`src/availability.ts:55`).
- Reuse the queue's existing `done` set, so only unchecked indices cost anything.
  A playlist already scrolled through completes almost immediately.
- Show progress as `checked n / total`. At a 300ms cooldown a 250-track playlist
  is roughly 75 seconds worst case, faster as cache hits skip the delay.
- Render results incrementally so the list is useful before the sweep finishes.
- Closing the panel mid-sweep stops queueing new checks and keeps results already
  gathered; re-summoning resumes from there.

**Collation.** Group `unavailable` tracks by artist, then album. Preserve playlist
order within an album. Sort albums by missing-track count descending, then artist
name. Tracks with no album group under their artist in a single untitled bucket,
sorted last within that artist. `unknown` tracks are excluded from the list
entirely and surfaced separately as a "couldn't check" count.

**Links.** Use the baked-in `purchase_url` when present. Otherwise construct
`https://bandcamp.com/search?q=<artist>+<album>&item_type=a`. Every row gets a
working link, so nothing is a dead end.

**Export.** Copy as Markdown, and download as a file.

## Boundaries

Collation lives in its own module, `src/shoppingList.ts`, as a pure function of
`(tracks, statuses) → grouped model`. It is the logic worth testing and it has no
reason to touch the 1,749-line `ByomPlayer.ts`.

## Honesty constraint

A provider "miss" is often a metadata mismatch rather than a real absence — the
collection may hold the track under a different album or artist spelling. The
panel says so plainly rather than presenting the list as ground truth. Per-row
dismissal is deliberately out of scope for v1.

## Testing

byom-sync:

- Table tests over recorded MusicBrainz JSON fixtures for relation-type
  filtering and edition picking, including the multi-release case where only one
  edition carries a link.
- `purchase_cache` tests mirroring `internal/rcache/art_test.go`, covering hits,
  negative entries, and TTL expiry.

byom-player:

- Unit tests for `shoppingList.ts` collation: grouping, ordering, `unknown`
  exclusion, and search-URL fallback when no `purchase_url` is present.
- Panel behavior following existing `ByomPlayer.test.ts` patterns, including that
  no sweep starts until the panel is summoned.

## Out of scope for v1

- Cross-playlist aggregation. The panel is per-playlist.
- Per-row dismissal or a persistent "already bought" state.
- Any purchase-link source other than MusicBrainz, plus the search-URL fallback.
- A `byom-sync` CLI that reads collection availability directly. Sync has no
  Subsonic/Jellyfin/Plex client and gains one only if this proves worth it.

## Cost note

Filling `purchase_url` across the whole hub from cold is roughly 7,165 albums at
up to 3 requests each, throttled to about 1 request per second. That is multiple
hours of wall clock. It is incremental and resumable, so it can run in chunks via
`--limit`, but it is not a quick job.
