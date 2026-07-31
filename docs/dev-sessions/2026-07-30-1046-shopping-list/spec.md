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

| Source | Reqs/album | Rate limit | Hit rate (measured) | Role |
| --- | --- | --- | --- | --- |
| Bandcamp (undocumented) | 1 | unpublished | 47% of all albums | Tier 1 |
| iTunes Search | 1 | ~20/min | 65% of tier-1 misses | Tier 2 |
| Discogs | 1.4 | 25/min unauth, 60 auth | 39% of tier-1 misses, +2 unique | Tier 3 |
| MusicBrainz `url-rels` | 2+ | ~1/s | **3%, zero unique** | Dropped |
| Odesli / song.link | 1 | 10/min | not reached — no Bandcamp coverage | Rejected |

### The funnel, measured end to end

A 60-album random sample of the live hub, each tier run only on what the
previous tiers missed, with the confidence gate applied throughout:

| Tier | Input | Hits | Rate | Requests |
| --- | --- | --- | --- | --- |
| 1 Bandcamp | 60 | 28 | 47% | 60 |
| 2 MusicBrainz | 32 | 1 | 3% | 60 |
| 3 iTunes | 31 | 20 | 65% | 31 |
| 4 Discogs | 31 | 12 (2 unique) | 39% | 44 |

**Cumulative coverage is 51/60 = 85%, identical with or without MusicBrainz.**

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

**Measured hit rate against the live hub: 47%**, consistent across two
independent random samples (14/30 and 28/60, the second with normalization
already applied). An earlier 30-album run suggested normalization lifted this to
53%; the larger sample did not reproduce that, so 47% is the number to plan
against and the lift was noise. Remaining misses are largely correct:
compilations ("80s Rock Essentials") and major-label catalog (Shinedown, Fluke,
Robert Miles) genuinely absent from Bandcamp.

**It fails cleanly.** A query for The Smiths' *The Queen Is Dead* — major label,
definitely absent — returned zero results rather than a wrong guess.

**Risk, stated plainly.** The endpoint is undocumented and unauthenticated. It is
what Bandcamp's own site calls, but nothing obliges them to keep it, and
automated access is likely not permitted by their terms. Practically this is a
personal tool making a one-time, heavily cached pass at about one request per
second. The mitigation is architectural, not legal: it is one tier of a cascade,
so if it breaks the pass degrades to sanctioned sources rather than dying.

### MusicBrainz — dropped after measurement

The original design made this tier 1. Measurement removed it entirely.

`GET /ws/2/release/<mbid>?inc=url-rels` returns typed relations including
`purchase for download` (type `98e08c20-8402-4163-8970-53504bb6a1e4`), and the
mechanism works. The data isn't there. Run against the 32 albums Bandcamp missed,
it produced **1 hit for 60 requests** — the most expensive tier and the least
productive.

The request math rules out the confidence gate as the cause: 32 searches plus 28
detail lookups means candidate releases *were* found and fetched. They simply
carry no purchase relations. MusicBrainz catalogues releases thoroughly and
purchase URLs sparsely.

Its single hit was Protocell's *Magonia*, resolving to
`music.apple.com/.../1786770588`. Because tier 2 consumed it, iTunes never got a
turn — so it was checked directly afterward, and iTunes returns **the same album
id**. MusicBrainz's unique contribution across the whole sample is zero.

Dropping it also deletes the edition-picking problem, the 3-lookup cap, and the
open question about candidate strategy.

### iTunes — the real tier 2

`GET https://itunes.apple.com/search?entity=album` needs no key and returns
`collectionViewUrl` plus `collectionPrice`. Measured on the 31 albums Bandcamp
and MusicBrainz both missed: **20 hits, 65%**, one request each, every one
carrying a real price.

**On DRM:** iTunes Store music *purchases* have been DRM-free since 2009 —
"iTunes Plus", 256kbps AAC, no usage restrictions. Apple Music *streaming* is
DRM-protected; a bought download is not. So there is no DRM argument for
demoting this tier.

There is, however, a real link-quality problem hiding behind that distinction.
`collectionViewUrl` points at `music.apple.com`, the Apple Music surface, where
the foregrounded action is "listen" rather than "buy". Two mitigations:

- **Require `collectionPrice > 0`** before accepting an iTunes result, so a row
  only appears when the album is genuinely purchasable rather than stream-only.
  All 20 measured hits carried a price (0.99–9.99), so this costs nothing.
- Pair every row with the DRM-free store search links below, so the reader always
  has a route that is unambiguously a purchase.

It is also the reason the confidence gate exists. An early probe for
"amanda palmer theatre is evil" returned ***Piano Is Evil*** — a real but
different album, with no signal it was a poor match. In the measured run the gate
caught the same failure mode three times: Ride's *Peace Sign* matched against
"Classical Music for Zodiac Signs" (0.37), Rob Zombie's *Hellbilly Deluxe*
against "The Sinister Urge" (0.62), Sara Lov's *I Already Love You* against
"Summertime Blues - EP" (0.21).

**65% is iTunes' ceiling here, and query construction is not a lever.** Four
constructions were measured against the same 31-album residue, 124 requests:

| Variant | Hits | |
| --- | --- | --- |
| `term="artist album"`, limit 5 (baseline) | 20/31 (65%) | — |
| same, limit 25 | 20/31 (65%) | no change |
| `attribute=mixTerm`, limit 25 | 20/31 (65%) | no change |
| `attribute=albumTerm`, limit 25 | 17/31 (55%) | worse |

No variant rescued any gated album. `albumTerm` is actively harmful: dropping the
artist lets same-titled albums by other artists dominate, and the gate correctly
rejects them.

The initial read — that 65% understated iTunes — was wrong. **8 of the 11 gated
cases return no priced result at all**, which is absence or stream-only status,
not a matching failure. Rob Zombie's *Hellbilly Deluxe* returns nothing priced
even when searching its exact album title, which for a platinum record means
Apple no longer sells it rather than that the query was poor.

The purchase gate is doing real work here: it is what distinguishes "Apple has
this to stream" from "Apple will sell you this."

### Discogs — small, additive, and in scope

Unauthenticated search works and reports its budget in `x-discogs-ratelimit: 25`
per minute; 60 with a free personal access token. A `User-Agent` is mandatory —
Discogs rejects default agents.

Raw search scored 13/31 on the residue, but a raw match is not a purchasable
record. Re-measured with an availability gate, **12/31**, and critically **both
albums unique to Discogs survive**:

| Album | Copies for sale | From |
| --- | --- | --- |
| Sara Lov — *I Already Love You* | 7 | $5.06 |
| Rob Zombie — *Hellbilly Deluxe* | 36 | $19.99 |

Only one match dropped (zero copies listed), and iTunes already covers it, so
cumulative coverage is unchanged at 85%.

**Two-step lookup**, because the search response is insufficient on its own:

1. `GET /database/search?q=<artist> <album>&type=release&per_page=5`. Search
   results carry no `num_for_sale`, and their `title` is a single
   `"Artist - Album"` string that breaks when either side contains " - ". Use
   this only to pick candidates.
2. `GET` the best candidate's `resource_url`. This returns authoritative
   `artists[].name` and `title` for scoring, plus `num_for_sale` and
   `lowest_price`.

**Accept only when the rescore clears the threshold *and* `num_for_sale > 0`.**
Rescoring on the clean fields returned 1.00 for all 12 survivors, so the second
request pays for itself twice: it removes the string-splitting fragility and it
prevents linking to a release nobody is selling.

The second request only fires for candidates that pass the first gate — measured
at 1.4 requests per album, not 2.

**URL comes from the API**, never constructed: the search result's `uri` field
(`/release/11662135-Rob-Zombie-Hellbilly-Deluxe`) appended to
`https://www.discogs.com`. Discogs' own site 403s scripted traffic, so any
hand-built path would be unverifiable — and this session has already burned two
invented URL patterns.

**`lowest_price` is deliberately not stored in the hub.** It is genuinely useful
in the panel (the sampled range was $3.02 to $193.68, and a $193.68 pressing is
worth knowing about before clicking) but prices move constantly. Writing them
into version-controlled YAML would churn diffs across thousands of tracks on
every re-resolve, and a stale price is worse than none.

What a Discogs link *is*, stated plainly: a marketplace listing for secondhand
physical media. It does not fill a gap in a digital collection unless the record
gets ripped, and a secondhand sale pays the artist nothing. That is why it is
last, and why shipping tiers 1–2 and stopping would still be defensible.

### Secondary links: DRM-free store searches

Resolved links answer "where can I buy this exact record". They do not express a
preference about *who to buy it from*. Every shopping-list row therefore also
carries a small set of constructed search URLs to DRM-free stores — no API, no
rate limit, no resolution step, and they never fail to produce a link.

- Bandcamp search (already the fallback when tier 1 misses):
  `https://bandcamp.com/search?q=<artist>+<album>&item_type=a`
- Qobuz: `https://www.qobuz.com/us-en/search?q=<artist>+<album>` — **verified**,
  returns server-rendered results.

Two verified links is the whole set. Bleep (`https://bleep.com/search?q=`) and
Boomkat (`https://boomkat.com/products?q[keywords]=`) were tried and **do not
work** — both patterns were checked in a real browser and returned no results.
Their real search URL formats are something else and were not chased down; two
DRM-free searches per row is enough, and more would clutter the album header for
diminishing benefit. Recorded here so the same guesses don't get re-derived.

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
`--source bandcamp|itunes|discogs|all` (default `all`).

**Tier-at-a-time, not per-album cascade.** Each tier runs as its own pass over
everything still unresolved, in order. This matters for three reasons: every pass
is a simple single-source loop with one rate limit, each is independently
resumable, and stopping after any tier is a legitimate outcome rather than a
half-finished state. A per-album cascade would interleave three different rate
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
threshold; otherwise the tier reports a miss and the next tier gets a turn. A
source may impose extra acceptance conditions of its own — iTunes requires
`collectionPrice > 0` so stream-only records don't surface as purchases.

**Query normalization.** Its measured effect is smaller than first thought (see
above), but it is cheap and clearly correct:

- Take only the first artist from Spotify's comma-joined artist strings
  ("Cavedoll, Tim Phillips" → "Cavedoll"). Rescued two misses in the first
  sample; the effect did not persist as a measurable lift in the larger one.
- Strip parenthetical album suffixes ("Crystals (feat. …)" → "Crystals") and
  edition markers ("Deluxe Edition", "- Remaster").

**Confidence gate.** Score the returned artist + album against the query and
require a threshold before accepting. This needs `sim` and `norm`, currently
unexported in `internal/spotifyenrich/score.go`. Extract them into a shared
`internal/match` package and have `spotifyenrich` consume it — a targeted
refactor of code this feature genuinely depends on, not opportunistic cleanup.
Reuse `DefaultThreshold` (0.8) as the starting value. The measured run supports
it: accepted matches scored 1.00 and rejected ones scored 0.62 or below, so 0.8
sits in a wide gap rather than on a cliff edge.

**Rate limiting is per source**, not one global `--delay`: Bandcamp self-throttled
to ~1/s out of politeness, iTunes ~20/min, Discogs 25/min (60 with a token). A
single delay flag cannot express this, so each source declares its own limiter and
`--delay` becomes an optional floor applied on top.

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

**Links.** Each album row carries a primary link and a secondary set:

- *Primary*: the baked-in `purchase_url` when present, labelled by hostname
  ("Bandcamp", "Apple", "Discogs"). When absent, the Bandcamp search URL.
- *Secondary*: constructed DRM-free store searches (Bandcamp, Qobuz), rendered
  compactly on the album header rather than repeated per track, so a row stays
  readable.

Every row gets a working link either way, so nothing is a dead end.

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

- Table tests per source over recorded JSON fixtures: Bandcamp (hit, clean
  zero-result miss, track-filter form), iTunes including the *Piano Is Evil*
  wrong-album response and a stream-only record with no `collectionPrice`,
  Discogs two-step (search → release lookup) including a `num_for_sale: 0`
  release that must be rejected despite scoring 1.00.
- Confidence-gate tests using the real rejections from the measured run — Ride's
  *Peace Sign* against "Classical Music for Zodiac Signs" (0.37), Rob Zombie
  against "The Sinister Urge" (0.62) — proving each is rejected at 0.8.
- Normalization tests for comma-joined artists and parenthetical album suffixes,
  using the real strings from the sampled misses.
- Cascade tests: tier order, that a below-threshold result falls through, and
  that a tier failing entirely doesn't abort the pass.
- A Discogs search result whose `title` contains " - " inside the artist or album
  name, proving the second-request rescore avoids the split-string trap.
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

Cold fill across the hub, tier by tier, each throttled to its own limit, using the
measured hit rates:

- Tier 1 Bandcamp: 7,165 albums at ~1/s ≈ **2 hours**, resolving ~47% (3,400).
- Tier 2 iTunes: ~3,800 remaining at ~20/min ≈ **3.2 hours**, resolving ~65%.
- Tier 3 Discogs: ~1,300 remaining at 1.4 requests each ≈ 1,850 requests —
  **~75 minutes** unauthenticated, **~30** with a free personal access token.

Roughly **6 hours** total for ~85% coverage, down from the most-of-a-day the
MusicBrainz-first design implied. Incremental, resumable, and `--limit` chunks any
tier. Stopping after tier 1 remains reasonable: it is the cheapest pass and yields
the links most worth having.

## Open questions

- (Resolved) Discogs is in scope as tier 3. It contributes 2 unique albums out
  of 60 and is the only route to them; both survive the `num_for_sale` gate.
- Threshold 0.8 is supported by a bimodal score distribution on one 60-album
  sample. Worth re-checking if iTunes query construction changes.
- The 60-album sample is a single draw. Bandcamp measured 47% on both a 30-album
  and a 60-album sample, which is reassuring, but tiers 2–3 have one measurement
  each.

## Superseded

Recorded because the reasoning is worth not repeating:

- **MusicBrainz as tier 1**, then as tier 2. Dropped after measuring 3% and zero
  unique contribution. The mechanism worked; the data wasn't there.
- **An album-keyed sidecar** for `purchase_url`. Dropped after measuring a
  duplication factor of 1.93 rather than the ~10x assumed.
- **Odesli.** Rejected on no Bandcamp coverage and a 10 req/min ceiling.
- **Bandcamp cover art** in this feature, including capturing the id. Split to
  byom-sync#54 after measuring that only 0.3% of tracks lack art.
- **Improving iTunes coverage via query construction.** Four variants measured;
  none beat the baseline and one was worse. 65% is the ceiling. Do not re-try
  `albumTerm` — it loses the artist signal.
- **Promoting Discogs above iTunes on DRM grounds.** The premise doesn't hold —
  iTunes purchases are DRM-free (iTunes Plus, since 2009); it's Apple Music
  streaming that is protected. Reordering would have swapped a DRM-free download
  for a secondhand vinyl listing on the 11 albums both sources cover, with no
  coverage gain. The underlying preference is served instead by keeping Bandcamp
  first, gating iTunes on a real price, and adding DRM-free store search links.
