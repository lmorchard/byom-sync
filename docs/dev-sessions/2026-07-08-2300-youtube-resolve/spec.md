# Spec: byom-sync YouTube resolution

## Goal

Enrich the hub with a resolved YouTube video ID per track, searched once via the
YouTube Data API and stored durably in the YAML hub, then emitted into the JSPF
`resolved.youtube` extension so byom-player can play YouTube without an on-demand
search. Part 1 of the cross-repo YouTube feature (part 2 is byom-player
consumption).

## Why store in the hub (not resolve at export)

YouTube Data API `search.list` costs ~100 quota units against a ~10,000/day
default — about **100 searches/day**. A large library can't be resolved in one
run, so resolution must be **incremental and durable**: search only tracks
missing an ID, write the ID into the hub YAML (the source of truth you own), and
never re-search. Export just copies the stored ID.

## Contract

The resolved ID lands in the JSPF track extension (namespace
`https://github.com/lmorchard/byom-sync`):

```json
"extension": {
  "https://github.com/lmorchard/byom-sync": [{ "resolved": { "youtube": "dQw4w9WgXcQ" } }]
}
```

The extension writer is structured so issue #5's `sync_state` can join the same
block later (out of scope here).

## Components

### Hub schema (`internal/playlist/types.go`)

Add `Track.YouTubeID string` (`yaml:"youtube_id,omitempty"`). Empty = unresolved.

### `internal/playlist/store.go`

Add `SaveFile(path string, p Playlist) error` (mirrors `LoadFile`) — writes a
playlist back to the exact path it was loaded from, preserving filename.

### `internal/youtube` (new package)

- `var ErrQuotaExceeded = errors.New(...)` — sentinel for a 403 quota response.
- `type Searcher interface { Search(ctx context.Context, query string) (videoID string, err error) }`.
  - `videoID == ""` (nil error) = the API answered but had no result (a clean miss).
- `type HTTPSearcher struct { APIKey string; Client *http.Client }` implementing
  `Searcher` via `GET https://www.googleapis.com/youtube/v3/search?part=snippet&type=video&maxResults=1&q=<query>&key=<key>`,
  parsing `items[0].id.videoId`. HTTP 403 whose body reason is `quotaExceeded`/
  `dailyLimitExceeded` → `ErrQuotaExceeded`; other non-200 → error.
- `func Resolve(ctx, s Searcher, p *playlist.Playlist, budget *int) (resolved int, quotaHit bool, err error)`:
  - `budget *int` counts remaining **searches** this run; `nil` = unlimited.
  - For each track with an empty `YouTubeID`:
    - stop if `budget != nil && *budget <= 0`;
    - `id, err := s.Search(ctx, "<artist> <title>")`; a search was performed →
      if `budget != nil`, `*budget--`;
    - `errors.Is(err, ErrQuotaExceeded)` → return `quotaHit=true` (stop);
    - other `err` → log + skip that track (don't abort the whole run);
    - non-empty `id` → set `t.YouTubeID`, `resolved++`.

  Quota stop and budget stop are distinct signals; both leave already-resolved
  IDs on the mutated playlist for the caller to persist.

### `cmd/resolve.go` (new command)

`byom-sync resolve youtube [--input <file|dir>] [--limit N]`:
- `resolve` is a parent command; `youtube` the subcommand (room for others later).
- `--input` defaults to config `dir` (the hub). File → that playlist; dir →
  every `*.yaml`.
- `--limit N` caps searches this run (0 = unlimited; the quota stop is the real
  backstop).
- API key from `viper.GetString("youtube_api_key")` (also `YOUTUBE_API_KEY` via
  `AutomaticEnv`). Empty key → clear error before any work.
- Per file: `LoadFile` → `youtube.Resolve` → `SaveFile` iff anything resolved.
  Stop the loop on quota or exhausted budget; log a summary either way
  (`resolved N; stopped: quota|limit|done`).

### Config (`cmd/root.go`)

Add `viper.SetDefault("youtube_api_key", "")`. Document it in
`byom-sync.yaml.example`.

### Export (`internal/export/jspf.go`)

Add extension structs; when `t.YouTubeID != ""`, set
`jt.Extension = { <NS>: [{ resolved: { youtube: <id> } }] }`. Namespace as a
package const.

## Testing

- `internal/youtube`: `HTTPSearcher` against `httptest.Server` — URL/params,
  `videoId` parse, empty-result → `""`, 403-quota → `ErrQuotaExceeded`,
  other non-200 → error. `Resolve` with a fake `Searcher` — resolves only empty
  IDs, respects budget, stops on quota (leaving later tracks untouched), skips a
  track whose search errors and continues.
- `internal/playlist`: `youtube_id` YAML round-trips; `SaveFile` writes to the
  given path and reloads equal.
- `internal/export`: a track with `YouTubeID` emits the extension JSON at the
  right namespace/shape; a track without emits no `extension` key.
- No real API calls anywhere in tests. Live resolution (real key + quota) is
  manual.

## Defaults (v1)

- Query = `"<artist> <title>"`, take the top result. Matching is best-effort
  (may pick a live/cover/lyric video); tuning is a later refinement.
- `--limit` default unlimited; quota is the backstop.
- No `sync_state` extension writing (issue #5).

## Out of scope

- byom-player consumption (part 2, separate session).
- Moving the Spotify URL into `resolved` (separate migration).
- Match-quality heuristics; per-track re-resolution/refresh of existing IDs.
