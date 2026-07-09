# Notes: prefer embeddable YouTube videos (#9)

## Summary

`resolve youtube` now resolves to a video that actually plays in an embedded
player, and can repair ids already in the hub.

- **Embeddable pick:** yt-dlp flat-searches the top N (5), then verifies each
  candidate's `playable_in_embed` (full extraction) and takes the first `True`.
  None embeddable → left unresolved (don't bake a broken id).
- **`YtdlpResolver.IsEmbeddable`** reused for candidate verification and re-check.
- **`--reresolve`:** re-checks tracks that already have a `youtube_id`; keeps ones
  still embeddable, clears + re-resolves ones that aren't. Normal runs still skip
  resolved tracks.
- **`youtube.Resolve` → `ResolveOptions` struct** (the positional list was getting
  long; `Reresolve`/`Verify` would have made it worse).

## Findings

- `playable_in_embed` is only exposed under full extraction (~2.3s/video);
  `--flat-playlist` shows `NA` (~0.6s). Chose the optimized common case: flat
  search for candidates + lazy per-candidate verify, so the common (top hit
  embeddable) case is ~1 flat + 1 verify.

## Status

- Tests: ytdlp (pick-first-embeddable, top-embeddable, none→miss, search/verify
  errors, candidate count, IsEmbeddable); Resolve reresolve (replace / keep /
  verify-error-keeps / off). `make lint && test && build` green.
- **Live-verified:** fresh resolve of "Ladytron - Playgirl" → embeddable
  `qMH6wljk4Xw`; `--reresolve` re-checked it, kept it (0 replaced). Replace path
  is unit-tested (no known non-embeddable id handy for a live replace).

## Ops

- Stop any in-progress enrichment run; after merge, `resolve youtube --reresolve`
  repairs the non-embeddable ids already in the hub. (Re-resolve is slower — full
  extraction per existing id — so a `--limit` per run is sensible.)

## Narration (Les, during trial)

A `--reresolve` trial looked broken: 24s of silence then "resolved 0 (nothing to
save)". It was actually correct (first N ids all embeddable → kept → hit --limit),
but kept-verifies emitted no narration and the summary read like failure. Fixed:

- `Event` carries a `Kind` (resolved/kept/replaced/removed/miss/error); the command
  narrates each track (`kept: … (still embeddable)`, `replaced: … -> new`,
  `removed: …` at WARN) and a per-file tally (`re-checked — N kept, M replaced,
  …`).
- Also fixed: a non-embeddable id with no embeddable alternative is now removed
  *and persisted* (the in-memory clear previously wasn't saved, since OnResolved
  only fired on a new id).
- Note: `--limit` counts every re-check (each is a yt-dlp verify), so a full
  repair pass wants a large/no limit.

## Follow-ups

- byom-player #14 (handle 101/150 → unavailable/skip) — defensive, still worth it.
- Candidate count is a const (5); expose a flag only if needed.
