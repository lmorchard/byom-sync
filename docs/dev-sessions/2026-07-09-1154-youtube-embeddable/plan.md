# Plan: prefer embeddable YouTube videos (byom-sync #9)

> TDD, task by task. `make lint && make test && make build` green each task.

### Task 1: refactor `youtube.Resolve` to a `ResolveOptions` struct (no behavior change)

**Files:** `internal/youtube/resolve.go`, `internal/youtube/resolve_test.go`, `cmd/resolve.go`.

- [ ] Change signature to `Resolve(ctx, r Resolver, p *playlist.Playlist, opts ResolveOptions)` with `ResolveOptions{ Budget *int; Pace time.Duration; Report func(Event); OnResolved func() error }` (reresolve/verify added in Task 3). Update body to read `opts.Budget` etc.
- [ ] Update `cmd/resolve.go` call site to pass an options struct.
- [ ] Update every `resolve_test.go` call to the struct form.
- [ ] `make test` green (pure refactor). Commit `refactor(youtube): Resolve takes a ResolveOptions struct`.

### Task 2: embeddable-aware YtdlpResolver + IsEmbeddable

**Files:** `internal/youtube/ytdlp.go`, `internal/youtube/ytdlp_test.go`.

- [ ] Tests (injected `run` dispatches on args — `--flat-playlist` = search, else verify):
  - top hit non-embeddable, 2nd embeddable → returns 2nd id.
  - top hit embeddable → returns it (verifies only 1).
  - all candidates non-embeddable → `Result{}` (miss).
  - flat-search error → error. verify error → error.
  - `IsEmbeddable`: "True\n" → true; "False\n" → false.
- [ ] Implement:
  - `YtdlpResolver.candidates int` (default 5 via a helper).
  - `Resolve`: flat `ytsearch<N>:` `--print id` → ids; for each, `IsEmbeddable` → first true wins; none → `Result{}`.
  - `IsEmbeddable(ctx, id)`: `run(ctx, bin, "--no-warnings", "--print", "%(playable_in_embed)s", "https://www.youtube.com/watch?v="+id)` → trimmed first line == "True".
- [ ] Green. Commit `feat(youtube): resolve to an embeddable video (yt-dlp playable_in_embed)`.

### Task 3: `--reresolve` (verify + replace existing non-embeddable ids)

**Files:** `internal/youtube/resolve.go`, `internal/youtube/resolve_test.go`, `cmd/resolve.go`.

- [ ] Tests:
  - `Reresolve:true` + `Verify` returning false for an existing id → id cleared, re-resolved via the chain (resolver called), replaced.
  - `Reresolve:true` + `Verify` true → id kept, resolver NOT called.
  - `Reresolve:true` + `Verify` errors → id kept, track skipped, run continues.
  - `Reresolve:false` → existing ids untouched (resolver not called).
- [ ] Implement in `Resolve`: add `Reresolve bool`, `Verify func(ctx, string)(bool,error)` to `ResolveOptions`. In the loop, when `t.YouTubeID != ""`:
  - `!Reresolve || Verify == nil` → `continue`.
  - else `ok, err := Verify(ctx, t.YouTubeID)`; err → report + `continue`; ok → `continue`; !ok → `t.YouTubeID = ""` (fall through to resolve).
- [ ] `cmd/resolve.go`: `--reresolve` flag; build a `youtube.YtdlpResolver` handle for `Verify: ytdlp.IsEmbeddable` (only when yt-dlp is in the chain); pass in opts. Narrate replacement.
- [ ] `make format && make lint && make test && make build`. Commit `feat(resolve): --reresolve replaces non-embeddable ids`.

### Task 4 (manual): live check

- [ ] `resolve youtube --input <hub> --limit 3` — confirm resolved ids are embeddable (spot-check a couple play in an embed).
- [ ] `resolve youtube --reresolve --input <hub> --limit 5` on the existing hub — confirm non-embeddable ids get replaced, embeddable ones kept.
