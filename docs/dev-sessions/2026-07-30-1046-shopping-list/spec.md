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
- `manifest.ts:59` already reads a `resolved` object out of the byom-sync JSPF
  extension namespace, so there is an established path for sync to hand
  pre-computed data to the player.

byom-sync:

- A MusicBrainz client (`internal/coverart/musicbrainz.go`).
- `rcache`, a SQLite cache whose `art_cache` hit/miss/TTL shape
  (`internal/rcache/art.go`) is the model for anything similar.
- `resolve youtube` / `resolve art`: incremental, resumable enrichment passes
  with `--limit` and `--delay`.
- `spotifyenrich.Score` plus unexported `sim` / `norm` helpers
  (`internal/spotifyenrich/score.go`) — a tuned partial-ratio string similarity
  with a `DefaultThreshold` of 0.8, and the `EnrichCandidate.Score` pattern for
  recording ambiguous matches instead of guessing.

Missing: nothing aggregates the misses, nothing renders them, and no purchase
links exist anywhere.

## Source research

Every source below was probed against live APIs on 2026-07-30. Numbers are
measured, not quoted from documentation.

| Source | Reqs/album | Rate limit | Returns | Role |
| --- | --- | --- | --- | --- |
| Bandcamp (undocumented) | 1 | unpublished | Exact album + track URLs | Tier 1 |
| MusicBrainz `url-rels` | up to 4 | ~1/s | Typed purchase relations | Tier 2 |
| iTunes Search | 1 | ~20/min | Priced `music.apple.com` links | Tier 3 |
| Discogs | 1 | 25/min unauth, 60 auth | Marketplace listings (physical) | Tier 4 |
| Odesli / song.link | 1 | 10/min | Amazon store links, **no Bandcamp** | Rejected |

### Bandcamp — the best source, and unsanctioned

`POST https://bandcamp.com/api/bcsearch_public_api/1/autocomplete_elastic` with
`{"search_text": "...", "search_filter": "a"}` returns an `item_url_path` that is
the exact album page:

```
https://amandapalmer.bandcamp.com/album/theatre-is-evil-2
```

Byte-identical to what MusicBrainz produced, in one fuzzy-text request rather
than a search plus up to three edition lookups. `search_filter: "t"` does the
same for individual tracks, which covers both albumless tracks and the common
"I only want this one song" case.

**Measured hit rate against the live hub:** a 30-album random sample returned
14/30 = 47%, rising to 16/30 = 53% with query normalization (below). Remaining
misses are largely correct: compilations ("80s Rock Essentials") and major-label
catalog (Shinedown, Fluke, Robert Miles) that genuinely aren't on Bandcamp.

**It fails cleanly.** A query for The Smiths' *The Queen Is Dead* — major label,
definitely absent — returned zero results rather than a wrong guess.

**Risk, stated plainly.** The endpoint is undocumented and unauthenticated. It is
what Bandcamp's own site calls, but nothing obliges them to keep it, and
automated access is likely not permitted by their terms. Practically this is a
personal tool making a one-time, heavily cached pass at about one request per
second. The mitigation is architectural, not legal: it is one tier of a cascade,
so if it breaks the pass degrades to sanctioned sources rather than dying.

### MusicBrainz — sanctioned, slower, spottier

`GET /ws/2/release/<mbid>?inc=url-rels` returns typed relations including
`purchase for download` (type `98e08c20-8402-4163-8970-53504bb6a1e4`). A probe
for *Theatre Is Evil* returned the same Bandcamp URL, but via a
`download for free` relation.

Coverage is per-edition and thin: that probe found three MusicBrainz releases
with the title, and only one carried the link — the second had an Amazon ASIN,
the third nothing. `ws/2` sends `access-control-allow-origin: *`, which is
irrelevant here since resolution happens server-side.

### iTunes — broad, but silently wrong

`GET https://itunes.apple.com/search?entity=album` needs no key and returns
`collectionViewUrl` plus a `collectionPrice`, confirming purchasability.

It is the reason the confidence gate is mandatory: a query for
"amanda palmer theatre is evil" returned ***Piano Is Evil*** — a real but
different album, with no signal that it was a poor match. Any tier that answers
fuzzily must be scored and thresholded.

### Discogs — a different kind of buying

Unauthenticated search works (HTTP 200) and reports its budget in
`x-discogs-ratelimit: 25` per minute; 60 with a token. Discogs is a marketplace
for physical media, so it answers "buy the vinyl" rather than "buy the download."
Included as the last tier precisely because it covers what the others can't.

### Odesli — rejected

Keyed by Spotify track id it works and returns `amazonStore` links, but it
carries **no Bandcamp coverage**, and its unauthenticated 10 req/min ceiling
means over 12 hours for the hub. Better sources are faster.

## Architecture

Fetching is front-loaded into site generation. byom-sync resolves purchase links
during enrichment and bakes them into the hub; the player only displays what it
finds and falls back to a search URL otherwise.

This keeps rate-limited network work out of the browser, reuses the MusicBrainz
client and cache byom-sync already has, and makes links durable and
version-controlled rather than dependent on a live third-party call at view time.

The two halves ship independently. Phase B is useful on day one using only
constructed search URLs, so it does not block on Phase A's slow initial fill.

### Phase A — byom-sync: `resolve purchase`

A new enrichment command with the same ergonomics as its siblings: `--input`,
`--limit`, `--delay`, attempts only what's unresolved, resumable. Plus
`--source bandcamp|musicbrainz|itunes|discogs|all` (default `all`).

**Tier-at-a-time, not per-album cascade.** Each tier runs as its own pass over
everything still unresolved, in order. This matters for three reasons: every pass
is a simple single-source loop with one rate limit, each is independently
resumable, and stopping after tier 1 is a legitimate outcome rather than a
half-finished state. A per-album cascade would interleave four different rate
limits in one loop for no benefit.

Sources implement a small interface so tiers are pluggable and individually
testable:

```go
// A Source resolves a buyable URL for one album or track.
type Source interface {
    Name() string
    Lookup(ctx context.Context, q Query) (Result, error)
}

type Query  struct { Artist, Album, Title string }
type Result struct { URL, Kind string; Score float64 }
```

`Kind` is `album` or `track`. A `Result` is accepted only when `Score` clears the
threshold; otherwise the tier reports a miss and the next tier gets a turn.

**Query normalization**, measured to matter — it moved the Bandcamp sample from
47% to 53%:

- Take only the first artist from Spotify's comma-joined artist strings
  ("Cavedoll, Tim Phillips" → "Cavedoll"). This rescued two of the sampled
  misses on its own.
- Strip parenthetical album suffixes ("Crystals (feat. …)" → "Crystals") and
  edition markers ("Deluxe Edition", "- Remaster").

**Confidence gate.** Score the returned artist + album against the query and
require a threshold before accepting. This needs `sim` and `norm`, currently
unexported in `internal/spotifyenrich/score.go`. Extract them into a shared
`internal/match` package and have `spotifyenrich` consume it — a targeted
refactor of code this feature genuinely depends on, not opportunistic cleanup.
Reuse `DefaultThreshold` (0.8) as the starting value; it is tunable.

**Rate limiting is per source**, not one global `--delay`: Bandcamp self-throttled
to ~1/s out of politeness, MusicBrainz 1/s as required, iTunes ~20/min, Discogs
25/min (60 with a token). A single delay flag cannot express this, so each source
declares its own limiter and `--delay` becomes an optional floor applied on top.

Results cache in a new `purchase_cache` table in `rcache`, mirroring `art_cache`
(`internal/rcache/art.go`): URL, `source`, `score`, `checked_at`, and negative
entries with a miss TTL so an album that later appears for sale gets retried.
Storing `source` lets a single tier be cleared and re-run without discarding the
others.

### Phase A — schema and wire format

One new field on `playlist.Track` (`internal/playlist/types.go:63`):

```yaml
purchase_url: https://amandapalmer.bandcamp.com/album/theatre-is-evil-2
```

A single resolution is written to `purchase_url` on **every** track sharing that
album key, across every playlist in the hub. Deduplicating by album key matters
for request cost even though results store per track: the hub averages 1.93
tracks per distinct album.

Per-track rather than an album-keyed sidecar. Measured against the live hub:

```
58 playlist files · 13,873 tracks · 13,832 with an album
7,165 distinct (artist, album) pairs
duplication factor: 1.93 tracks per album
```

A sidecar would concentrate 7,165 entries into one ~30k-line file while saving
only about half the total lines, since most albums appear exactly once. Per-track
spreads ~13,832 lines across 58 files, keeps each playlist self-contained, and
needs no join. It also handles Bandcamp selling individual tracks, which is often
what a shopping list actually wants.

No `purchase_source` field in the hub — the player derives a display label from
the URL's hostname. The cache keeps `source` for operational purposes.

Tracks with no album (41 in the current hub) key by artist + title and resolve
against track-level search instead.

Emitted by `internal/export/jspf.go` as a sibling of `resolved` inside the
existing extension element:

```json
"extension": { "https://github.com/lmorchard/byom-sync": [
  { "resolved": {"youtube": "..."}, "purchase_url": "https://..." }
] }
```

### Phase A — cover art is out of scope

The Bandcamp response also carries a cover-art id that resolves to a 1500×1500
image, well above Spotify's 640px. That is a separate feature and is not part of
this session: see byom-sync#54. Nothing about art — including capturing the id —
lands here.

### Phase B — byom-player: the shopping list panel

**Availability.** The action appears only when the active provider implements
`checkAvailability` — Subsonic, Jellyfin, Plex. Hidden for YouTube and Spotify.

**The sweep is summoned, never automatic.** A full-playlist scan starts only when
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

On the Go side each purchase source is its own file behind the `Source`
interface, so a broken or removed tier is a deletion rather than a surgery.

## Honesty constraint

A provider "miss" is often a metadata mismatch rather than a real absence — the
collection may hold the track under a different album or artist spelling. The
panel says so plainly rather than presenting the list as ground truth. Per-row
dismissal is deliberately out of scope for v1.

## Testing

byom-sync:

- Table tests per source over recorded JSON fixtures: Bandcamp autocomplete
  (hit, clean zero-result miss, track-filter form), MusicBrainz relation
  filtering and edition picking including the multi-release case where only one
  edition carries a link, iTunes including the *Piano Is Evil* wrong-album
  response, Discogs search.
- Confidence-gate tests proving the iTunes wrong-album case is rejected.
- Normalization tests for comma-joined artists and parenthetical album suffixes,
  using the real strings from the sampled misses.
- Cascade tests: tier order, that a below-threshold result falls through, and
  that a tier failing entirely doesn't abort the pass.
- `purchase_cache` tests mirroring `internal/rcache/art_test.go` — hits, negative
  entries, TTL expiry, per-source clearing.
- `internal/match` tests move with the extracted `sim` / `norm`; existing
  `spotifyenrich` score tests must still pass unchanged.

byom-player:

- Unit tests for `shoppingList.ts` collation: grouping, ordering, `unknown`
  exclusion, and search-URL fallback when no `purchase_url` is present.
- Panel behavior following existing `ByomPlayer.test.ts` patterns, including that
  no sweep starts until the panel is summoned.

## Out of scope for v1

- Cross-playlist aggregation. The panel is per-playlist.
- Per-row dismissal or a persistent "already bought" state.
- Bandcamp cover art in any form, including capturing the id (byom-sync#54).
- A `byom-sync` CLI that reads collection availability directly. Sync has no
  Subsonic/Jellyfin/Plex client and gains one only if this proves worth it.

## Cost

Cold fill across the hub, tier by tier, each throttled to its own limit:

- Tier 1 Bandcamp: 7,165 albums at ~1/s ≈ **2 hours**, resolving ~53%.
- Tier 2 MusicBrainz: ~3,400 remaining at up to 4 requests, 1/s ≈ up to 3.7 hours.
- Tiers 3–4 iTunes and Discogs: whatever survives, at 20–25/min.

Worst case is most of a day, but it is incremental, resumable, and stopping after
tier 1 is a perfectly good outcome — that single pass is both the cheapest and
the highest-yield. `--limit` chunks any tier.

## Open questions

- Is 0.8 the right confidence threshold for purchase sources? Inherited from
  `spotifyenrich`, never tuned against these responses.
- MusicBrainz candidate strategy: release search scored by artist+title, or
  release-group first then browse its releases? Worth a spike before building
  tier 2.
- Is the 3-lookup cap on MusicBrainz editions right? A cost bound, not measured
  against hit rate.
- Tiers 3–4 are specified from single probes. Their real hit rate on the hub's
  Bandcamp-and-MusicBrainz misses is unmeasured, and may not justify building
  them — worth sampling before committing, the way tier 1 was.
