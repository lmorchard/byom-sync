# Spec: prefer embeddable YouTube videos (byom-sync #9)

## Problem

`resolve youtube` takes the top `ytsearch1:` hit, but many videos have embedding
disabled (IFrame error 101/150). Those bake into the hub as ids that show
"Watch on YouTube" and don't play in byom-player's embedded player. Need to
resolve to an **embeddable** video, and re-check/replace already-resolved ids.

## Findings

- yt-dlp exposes `playable_in_embed` (True/False) only under **full extraction**
  (~2.3s/video); `--flat-playlist` shows `NA` (~0.6s for several ids).
- Most top hits ARE embeddable, so the common case should stay cheap.

## Design (chosen: optimized common case)

### `YtdlpResolver`

- `candidates` (default 5).
- `Resolve(track)`:
  1. Flat search top N ids: `yt-dlp --flat-playlist --print id "ytsearchN:<q>"` (fast).
  2. For each id in order, verify embeddability (full extract); first embeddable wins.
  3. None embeddable → `Result{}` (miss; leave unresolved — don't bake a broken id).
  - Flat-search error → error. A per-candidate verify error → propagate (transient;
    the track retries later) rather than silently pick a worse match.
- `IsEmbeddable(ctx, videoID) (bool, error)`:
  `yt-dlp --print "%(playable_in_embed)s" "https://www.youtube.com/watch?v=<id>"`
  → `True`. Used by both `Resolve` (per candidate) and the re-resolve pass.

### `youtube.Resolve` → options struct

The param list is already long; adding `reresolve`/`verify` makes it worse.
Refactor to:

```go
type ResolveOptions struct {
    Budget     *int
    Pace       time.Duration
    Report     func(Event)
    OnResolved func() error
    Reresolve  bool
    Verify     func(ctx context.Context, videoID string) (bool, error)
}
func Resolve(ctx, r Resolver, p *playlist.Playlist, opts ResolveOptions) (resolved int, stopped string, err error)
```

Behavior addition (reresolve): for a track that **already has** a `youtube_id`:
- normal (`Reresolve=false`) → skip (as today);
- `Reresolve=true` with `Verify` set → verify the existing id; embeddable → keep
  (skip); not embeddable → clear it and resolve fresh (counts as a resolution when
  refilled). A verify error → skip the track (keep its id), don't abort the run.
- `Reresolve=true`, `Verify=nil` → skip (nothing to verify with).

### `cmd/resolve.go`

- New `--reresolve` flag (default false). When set, pass `Reresolve: true` and
  `Verify: <ytdlp>.IsEmbeddable` (only the yt-dlp resolver verifies embeddability).
- Narrate: `re-resolving <artist> - <title> (embed disabled)` when replacing.

## Testing

- `ytdlp_test.go` (injected `run`): Resolve picks the first embeddable candidate
  (skips a non-embeddable top hit); all non-embeddable → miss; flat error →
  error; verify error → error. `IsEmbeddable` parses True/False.
- `resolve_test.go`: adapt to `ResolveOptions`. Reresolve: existing embeddable id
  kept (no re-resolve); existing non-embeddable id cleared + refilled; verify
  error keeps the id and continues; `Reresolve` off leaves existing ids untouched.
- No real yt-dlp/network in tests.

## Out of scope

- byom-player 101/150 handling (#14, separate).
- Data API `videos.list` embeddability verifier (yt-dlp keeps it key-free).
- Tuning candidate count via a flag.

## Ops note

An enrichment run in progress should be stopped; after merge, a `--reresolve`
run repairs the non-embeddable ids already in the hub.
