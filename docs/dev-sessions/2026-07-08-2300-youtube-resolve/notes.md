# Notes: byom-sync YouTube resolution

## Live-testing follow-ups (Les, after first manual runs)

Three fixes came out of Les running it against his real key:

1. **Narration.** The command was near-silent — `Resolve` swallowed per-track
   search errors and only a terminal summary printed. `Resolve` now reports each
   outcome via a callback; the command logs a start line, per-file counts, and
   per-track results. Hits/misses at INFO (`--verbose`); search errors, quota,
   rate-limit, and summary at WARN (always visible).
2. **Surfaced API error detail.** Non-2xx responses now include Google's own
   reason/message (e.g. "API key not valid", referrer restrictions) instead of a
   bare `HTTP 400`. This demystified the "same key works in byom-player" question:
   the browser sends a Referer that a referrer-restricted key needs; a CLI call
   does not — that shows as a specific reason now.
3. **Rate limiting (429).** Firing all searches in a burst tripped the per-interval
   rate limit — every request came back 429. `HTTPSearcher` now retries 429 with
   backoff (honoring `Retry-After`) and returns `ErrRateLimited` if it persists;
   `Resolve` paces searches (`--delay`, default 500ms) and stops cleanly on
   sustained rate limiting or quota (distinct stop reasons). `--delay` is tunable
   if 500ms still trips the limit.

## Summary

Added `byom-sync resolve youtube`, which searches the YouTube Data API for hub
tracks missing a video ID, stores the ID durably in the YAML hub, and emits it
into the JSPF `resolved.youtube` extension on export. Part 1 of the cross-repo
YouTube feature (part 2 = byom-player consumption, a separate session).

- `internal/youtube`: `Searcher` seam + `HTTPSearcher` (net/http → Data API v3
  `search.list`, top result, quota detection via `ErrQuotaExceeded`) + `Resolve`
  (fills only empty `youtube_id`s, respects a `*int` search budget, stops on
  quota leaving progress intact, skips per-track errors).
- Hub: `Track.YouTubeID` (`yaml:"youtube_id"`) + `playlist.SaveFile`.
- `cmd/resolve.go`: `resolve youtube [--input] [--limit]`, key from
  `youtube_api_key` / `YOUTUBE_API_KEY`, load → resolve → save per file, summary
  log (`stopped: done|quota|limit`).
- `internal/export/jspf.go`: writes `extension[NS][0].resolved.youtube`;
  structured so issue #5's `sync_state` can join the same block.

## Status

- 5 TDD tasks, all green. `make format && make lint && make test && make build`
  all pass (lint 0 issues).
- **No real API calls in tests** (`httptest` + fake `Searcher`). Live resolution
  spends quota — manual only.

## Key decisions

- **Store IDs in the hub YAML, resolve incrementally.** Forced by quota: Data API
  search ≈ 100 units against ~10k/day ≈ ~100 searches/day. Re-runs skip tracks
  that already have an ID; export never re-searches.
- **Dedicated `resolve` command** (not a `sync` flag / export-time) so quota
  spend is explicit and separate from syncing.
- **v1 matching = top result of `"<artist> <title>"`.** Best-effort; may grab a
  live/cover/lyric video. Tuning deferred.
- No new dependencies — plain `net/http` to the same endpoint byom-player uses.

## Contract (shared with byom-player part 2)

```json
"extension": { "https://github.com/lmorchard/byom-sync": [
  { "resolved": { "youtube": "<videoId>" } }
]}
```

## Environment / process

- Worktree at `/Users/lorchard/devel/byom-sync-wt/youtube-resolve` (external path,
  branched from `origin/main`) — byom-sync's main checkout was mid-edit on
  `add-agents-md` with uncommitted changes, so an external worktree kept clear of
  it and avoided touching its `.gitignore`.
- The session's native worktree tool is bound to the byom-player repo, so this
  used the `git worktree` fallback.

## Follow-ups

- **byom-player part 2:** consume `resolved.youtube` (embedded → cache → live
  search → give up).
- **Issue #5:** emit `sync_state` in the same extension block (structure ready).
- Match-quality tuning (query template, duration proximity) if top-result picks
  disappoint.
- Consider moving the Spotify URL into `resolved` for symmetry (breaking; own PR).

## Manual live check (pending, spends quota)

```
byom-sync resolve youtube --input ./playlists --limit 3
byom-sync export jspf --input ./playlists --out ./out
```

Expect ≤3 `youtube_id`s in the hub, a re-run skipping them, and `resolved.youtube`
in the exported JSPF.
