# Spec — Resolution cache/index (SQLite)

Session: 2026-07-09 · Branch: `feat/resolution-cache` · Issue: [#8](https://github.com/lmorchard/byom-sync/issues/8)

## Summary

Add a persistent SQLite cache that sits in front of the YouTube resolver chain.
It's an accelerator/index, not a new source of truth: the per-playlist YAML hub
stays authoritative and `youtube_id` still gets baked into the YAML on every
resolution. The cache only decides whether a network call (yt-dlp search or
embeddability verify) is needed.

The cache is keyed by `Track.Key()` — the same ISRC-or-`artist\ttitle` identity
used for merge — so a track resolved in one playlist is reused everywhere, and
that reuse survives across runs and across a YAML wipe / re-export.

## Motivation

Measured against the current 59-playlist hub (~13,900 track entries):

- **Cross-playlist duplication.** ~11,250 unique ISRC tracks vs ~13,875 ISRC
  occurrences → ~2,625 (19%) are cross-playlist duplicates. A fresh or
  post-wipe resolve re-does that ~19% of work. One track appears in 11
  playlists; ~500 appear in 5+.
- **Recurring miss re-attempts.** A track with no YouTube match keeps
  `youtube_id: ""` in the YAML forever, so it's re-attempted (a real yt-dlp
  call) on *every* run. This is the largest recurring cost.
- **Re-export / wipe cost.** Regenerating or clearing `youtube_id`s forces a
  full re-resolve against the network.
- **`--reresolve` is slow.** Re-verifying embeddability of every existing id via
  yt-dlp is expensive and largely redundant run-to-run.

The cache addresses all four. ~4,750 ids are already resolved in the hub today;
priming seeds those into the cache with zero network calls.

## Non-goals

- The hub does **not** move into a database. YAML files remain the
  human-readable, diffable, version-controlled source of truth.
- The cache is disposable/rebuildable (positive entries via `prime`); it is not
  backed up or version-controlled. It is gitignored.
- No analytics/reporting beyond a basic `stats` command. The queryable SQLite
  store leaves the door open for more later, but that's out of scope now.

## Storage

`modernc.org/sqlite` (pure-Go driver, no cgo — keeps the build toolchain-only).

Single table:

```sql
CREATE TABLE resolution_cache (
  key         TEXT PRIMARY KEY,  -- Track.Key(): "isrc:..." or "at:artist\ttitle"
  video_id    TEXT NOT NULL,     -- '' = known miss (negative entry)
  source      TEXT,              -- yt-dlp | youtube-search | prime | cache
  embeddable  INTEGER,           -- 1 / 0 / NULL (unknown, e.g. unverified prime)
  resolved_at TEXT,              -- RFC3339; when a positive id was last obtained
  checked_at  TEXT NOT NULL      -- RFC3339; last attempt/verify — drives both TTLs
);
```

**Location:** `$XDG_CONFIG_HOME/byom-sync/cache.db`, alongside `token.json`.
Gitignored. One cache per hub/user. Overridable via the `cache_path` config key.

Schema is created on first open (idempotent `CREATE TABLE IF NOT EXISTS`). No
migration framework yet; a `schema_version` pragma/table can be added if the
schema ever changes.

## Entry semantics

An entry is exactly one of two kinds at any moment:

- **Positive** (`video_id != ''`): a known good id.
  - `checked_at` = last time embeddability was confirmed (initial resolve picks
    an embeddable result; `--reresolve` re-verifies). This *is* the
    "embeddability last checked" timestamp — no separate column is needed
    because the miss-check and embed-check roles never apply to the same entry.
  - `embeddable` = `1` (verified good), `0` (verified bad — transient; a `0`
    entry is normally cleared and re-resolved on the next reresolve), or `NULL`
    (never verified — only from `prime --assume-embeddable=false`).
- **Negative / miss** (`video_id == ''`): we searched and found nothing.
  - `checked_at` = when we last searched.

### TTLs

Both config/flag-overridable. Defaults:

- `cache_miss_ttl = 720h` (30d) — a miss is trusted (skipped) while
  `now - checked_at < miss_ttl`; after that it's re-attempted so newly-available
  tracks get picked up.
- `cache_embed_ttl = 720h` (30d) — under `--reresolve`, an `embeddable=1` entry
  is trusted while `now - checked_at < embed_ttl`; after that it's re-verified.

## Resolve integration

New package `internal/rcache` owns the SQLite dep and exposes a small interface;
`youtube.Resolve` consults it via an optional `Cache` field on `ResolveOptions`.
A nil cache reproduces today's behavior exactly (keeps existing tests untouched).

```go
type Cache interface {
    Get(key string) (Entry, bool)
    Put(key string, e Entry) error
}
```

Per track that needs work (empty `youtube_id`, or under `--reresolve`):

```
1. cache.Get(track.Key())
2. positive hit                 → fill youtube_id from cache, no network,
                                   source recorded as "cache"        [dedup / wipe recovery]
3. negative hit & fresh         → report miss, skip                  [negative caching]
4. otherwise (no entry, or
   expired miss)                → call resolver chain as today,
                                   then cache.Put(result)            [positive OR miss; feeds future runs]
```

Positive hits are still written into the YAML — the cache never replaces the
baked-in `youtube_id`.

**Budget & pacing:** cache hits (positive or fresh-miss) are *not* network calls,
so they do **not** consume the `--limit` budget and do **not** trigger the
`--delay` pause. Only paths that actually call a resolver/verify count against
`--limit` and pace with `--delay`.

**`--reresolve` path:** if the cache says `embeddable=1` and fresh
(`now - checked_at < embed_ttl`), keep the id without a yt-dlp verify. Otherwise
verify as today (also when `embeddable` is `NULL` or `0`), then `cache.Put` the
fresh verdict + `checked_at`.

**Persistence cadence:** cache writes piggyback on the existing checkpoint/flush
cadence in `cmd/resolve.go` (batched, not one transaction per track).

**`--no-cache` flag:** disables cache lookups and writes for the run (pure
network resolution — an escape hatch and a way to force a full re-verify).
Cache is default-on once it exists.

## Priming — `byom-sync resolve prime`

Walks the hub YAML and upserts every track that already has a `youtube_id`:

- `video_id = youtube_id`, `source = prime`, `resolved_at = checked_at = now`.
- `embeddable`:
  - **default (`--assume-embeddable=true`)** → `1`. The current hub was resolved
    by the embeddable-preferring resolver, so trusting it lets `--reresolve`
    skip the mass re-verify within `embed_ttl`.
  - **`--assume-embeddable=false`** → `NULL`. Seeds ids unverified so a later
    `--reresolve` checks each once. Use when the hub predates the
    embeddable-preference logic or you don't trust its provenance.
- Idempotent; re-priming refreshes timestamps. Reports keys seeded and how many
  cross-playlist duplicates collapsed onto the same key.

**Caveat (documented in `--help`):** `--assume-embeddable` (the default) means a
video that went private/dead *since* it was resolved won't be caught until
`embed_ttl` expires or the cache is cleared. That's the accepted cost of
skipping the check on a hub you trust.

## Management & config

- **`byom-sync resolve cache stats`** — total keys, positive vs negative counts,
  and how many negatives are currently expired. (Payoff for the SQLite store.)
- **`byom-sync resolve cache clear [--misses-only]`** — manual invalidation.
  `--misses-only` drops only negative entries so genuine unresolvables get
  re-attempted without losing positive work.
- **Config keys** (viper; each mirrored by a flag): `cache_path`,
  `cache_miss_ttl`, `cache_embed_ttl`.

## Clock seam

`internal/rcache` takes a `now func() time.Time` (defaulting to `time.Now`) so
TTL expiry is deterministic in tests.

## Testing

- **`internal/rcache`** (against a temp on-disk `cache.db`): Get/Put round-trip;
  positive vs miss; TTL expiry via injected clock; prime upsert + idempotency +
  duplicate collapse; `--assume-embeddable` true→`embeddable=1` and
  false→`NULL`.
- **`youtube.Resolve` with a fake `Cache`** (no SQLite): positive hit skips
  resolver; fresh miss skips; expired miss re-attempts; `--reresolve` embed-cache
  hit skips verify; `NULL`/`0` embeddable forces verify. Nil cache = unchanged
  behavior.
- **`cmd`**: `prime` seeds expected keys; `--no-cache` bypasses; `cache clear`
  / `--misses-only` remove the right rows.

## Out-of-scope / future

- `schema_version` + migrations (only if the schema changes).
- Richer analytics ("what's unresolved everywhere," per-source breakdowns).
- Caching for resolver sources beyond YouTube (the `source` column already
  anticipates this).
