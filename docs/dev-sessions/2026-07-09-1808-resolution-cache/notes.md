# Notes — Resolution cache (SQLite)

Issue: [#8](https://github.com/lmorchard/byom-sync/issues/8) · Branch: `feat/resolution-cache`

## Outcome

Shipped an optional SQLite resolution cache in front of the YouTube resolver.
All 7 planned tasks complete; lint clean, tests pass, build OK.

## What it does

- **`internal/rcache`** — SQLite store (`modernc.org/sqlite`, pure-Go) keyed by
  `Track.Key()`. `Entry` = video_id (''=miss), source, embeddable (tri-state),
  resolved_at, checked_at. `Open`/`Get`/`Put`/`Stats`/`Clear`.
- **`youtube.Resolve`** — optional `Cache` on `ResolveOptions`; nil = unchanged
  behavior. Positive hit → reuse id (no TTL). Fresh miss → skip (miss TTL).
  `--reresolve` trusts a fresh embeddable verdict (embed TTL) instead of a yt-dlp
  verify. Cache hits don't consume `--limit` / `--delay`.
- **`Result.Embeddable *bool`** — yt-dlp sets `true` (it only returns embeddable
  ids); youtube-search leaves nil.
- **Commands** — `resolve prime` (seed from existing hub ids; `--assume-embeddable`
  defaults true), `resolve cache stats`, `resolve cache clear [--misses-only]`,
  `resolve youtube --no-cache`.
- **Config** — `cache_path`, `cache_miss_ttl` (720h), `cache_embed_ttl` (720h).

## Design decisions (from brainstorming)

- **SQLite over JSON sidecar** — Les chose the queryable index.
- **`--assume-embeddable` defaults true** — the hub was resolved by the
  embeddable-preferring resolver, so trust it; opt out to force re-verify.
- **No separate `embeddable_checked_at`** — `checked_at` already serves that role
  for positive entries (miss-check and embed-check never apply to one entry).
- **Clock seam lives in the resolve layer** (`ResolveOptions.Now`, `primeCache`'s
  `now` param), not in `rcache` — `rcache` is a dumb store; callers stamp times.

## Live verification

- Resolved 3 tracks (network, ~10s) → wiped the ids from the YAML → re-resolved:
  all 3 refilled **via cache in 0s**. The re-export/wipe win, confirmed.
- `resolve prime --input playlists` on the real hub: **4,186 keys seeded, 1,004
  cross-playlist duplicates collapsed** (isolated cache; real hub untouched).
- `cache stats` / `clear --misses-only` / `clear` all behave.

## Follow-ups / out of scope

- Migrations (`schema_version`) — only if the schema changes.
- Richer analytics ("what's unresolved everywhere").
- Caching resolver sources beyond YouTube (the `source` column anticipates it).
- README had **no** resolve section at all (feature postdated it) — added one
  covering both `resolve youtube` and the cache.
