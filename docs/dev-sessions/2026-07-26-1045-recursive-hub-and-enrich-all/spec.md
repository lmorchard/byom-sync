# Spec — Recursive hub discovery, recursive export, one-shot enrichment, headless auth

**Date:** 2026-07-26
**Branch:** `feat/recursive-hub-and-enrich-all`
**Status:** approved design, pending implementation plan

## Summary

Four related fixes, driven by real breakage found while hand-authoring a
playlist for `mixtapes.lmorchard.com`:

1. **Recursive hub discovery** — `hubPaths()` only globs one directory level, so
   `resolve {youtube,spotify,art}`, `prime`, and `dates` silently do nothing on a
   hub whose playlists live in subdirectories. This is a live regression.
2. **Recursive `export`** — the same shallow-glob bug in a second code path,
   fixed by mirroring the hub tree into the output directory.
3. **`resolve all`** — collapse the three-command enrichment pipeline into one
   invocation with up-front prerequisite checks.
4. **`auth --manual`** — make authentication possible on a headless/remote host.

They ship together because (3) is only usable once (1) works, (4) is what makes
(3) runnable on the deployment box, and (1) and (2) are the same bug in two
places — fixed once, in one shared helper.

## Guiding decisions (from brainstorming)

- **One definition of "the hub."** The site generator already walks the tree
  correctly; the resolvers should not disagree with it. Fix the shared helper,
  don't add per-command flags.
- **Fail before doing work, not after.** A pipeline that spends twenty minutes
  crawling cover art and *then* discovers `yt-dlp` is missing is worse than one
  that refuses to start.
- **Sequencing knowledge belongs in tested Go**, not in a Makefile. The Makefile
  target is a convenience alias, not the source of truth.
- **No new traversal semantics.** Reuse the skip rules `internal/site/tree.go`
  already established (dotfiles, top-level `art/`).
- **Output mirrors input.** Recursive `export` reproduces the hub's directory
  structure rather than flattening it, making basename collisions structurally
  impossible instead of merely detected.

---

## Part 1 — Recursive hub discovery

### The bug

`cmd/resolve.go:808`:

```go
matches, err := filepath.Glob(filepath.Join(input, "*.yaml"))
```

`mixtapes.lmorchard.com` files every playlist under `playlists/00-conceptual/`,
`playlists/01-covers/`, etc. So `--input playlists` matches zero files and the
commands exit successfully having done nothing:

```
$ byom-sync resolve art --input playlists --limit 1
level=warning msg="no playlist YAML files found under playlists — nothing to do"
```

This regressed when that hub reorganised its playlists into subdirectories; the
commands had worked when the YAML sat at the top level.

### Blast radius

`hubPaths()` is the discovery point for five commands — `cmd/dates.go:34` and
`cmd/resolve.go:112,334,527,682` (`youtube`, `spotify`, `art`, and `prime`, whose
call sits inside its `RunE` closure). One fix repairs all five.

`internal/export/export.go:36` has the *same* glob in a second package
(Part 2), and `internal/playlist/Load()` has it a third time. Rather than fix
the same bug three times, the walk becomes one exported helper.

### The change

Add the canonical walker to `internal/playlist/store.go`, next to `LoadFile`:

```go
// HubPaths returns every playlist YAML under input, recursively.
func HubPaths(input string) ([]string, error)
```

`cmd.hubPaths()` becomes a thin delegation to it, and `export.Run()` (Part 2)
uses the same function — so "what counts as the hub" has exactly one definition,
shared by the resolvers, the exporters, and (via the same skip rules) the site
generator.

`playlist.Load(dir)` also delegates, making it recursive for consistency. It is
currently referenced only by `internal/playlist/store_test.go:45` and by no
production code, so this is a no-risk alignment rather than a behavior change
anyone can observe.

The walk mirrors the site generator's rules (`internal/site/tree.go:44-56`):

- Collect every `*.yaml` at any depth.
- Skip entries whose name begins with `.` — editor/VCS cruft and macOS
  AppleDouble sidecars (`._*.yaml`), which would otherwise be parsed as
  playlists and fail on binary content.
- Skip a top-level `art/` directory — the content-addressed cover-art store
  holds images, not playlists.
- Sort results so ordering is deterministic across runs and platforms.
- A non-directory `--input` still returns that single path unchanged.

Recursion is unconditional; no flag. A hub with no subdirectories behaves
exactly as before, so the only behavior change is that previously-invisible
files become visible — which is the bug being fixed.

### Testing

Table-driven tests in `internal/playlist/`, using `t.TempDir()`, plus a thin
`cmd/` test confirming `hubPaths` delegates:

| Case | Expectation |
|---|---|
| flat dir of `*.yaml` | all found (regression guard) |
| nested subdirectories | all found at any depth |
| top-level `art/` containing `.yaml` | skipped |
| nested `art/` (not at root) | **not** skipped — only the root store is special |
| dotfiles + dotdirs | skipped |
| single file as `--input` | returned as-is |
| empty dir | empty slice, no error |
| unsorted filesystem order | results sorted |
| missing path | error |

---

## Part 2 — Recursive `export` with a mirrored tree

### The bug

`internal/export/export.go:36` globs `<input>/*.yaml`, so
`export m3u8 --input ./playlists` on a nested hub writes nothing — the same
silent no-op as Part 1, in a path Part 1's fix doesn't reach.

### The change

`export.Run()` switches to `playlist.HubPaths()` and reproduces the hub's
structure under `--out`:

```
playlists/00-conceptual/drones.yaml   →   out/00-conceptual/drones.m3u8
playlists/01-covers/numan-s-shadow.yaml → out/01-covers/numan-s-shadow.m3u8
```

Mechanically: for each discovered path, take `filepath.Rel(input, path)`, swap
the extension for the exporter's, join onto `out`, and `MkdirAll` the parent
before writing. Basename collisions across folders become structurally
impossible, so no collision detection is needed.

Single-file `--input` is unchanged: `--out` remains the exact output path.

A flat-output mode is deliberately omitted (YAGNI) — it can be added as
`--flat` later if a media server actually needs it.

### Art root is already correct

`artRootOf()` (`cmd/export.go:52`) resolves `--input`'s directory as the root
that hub-relative `image_file` values are joined against. Exporting the hub root
recursively keeps that root correct, because `image_file` is stored
hub-relative. No change needed.

(Pre-existing, unchanged by this work: exporting a *subdirectory* with
`--embed-art` resolves `image_file` against that subdirectory and misses the
store at the hub root. Out of scope here.)

### Testing

Extends the existing `TestRun_DirModeWritesFilePerInput` and
`TestRun_FileModeSingleOutput` (`internal/export/export_test.go:388,413`):

- Nested input produces a mirrored output tree.
- Same basename in two folders yields two distinct files, neither overwritten.
- Intermediate output directories are created.
- Single-file mode still writes to the exact `--out` path.

---

## Part 3 — `byom-sync resolve all`

### Behavior

Runs the enrichment stages in dependency order:

```
resolve spotify  →  resolve art --download  →  resolve youtube
```

The order matters: `resolve spotify` writes the ISRCs that the art and YouTube
stages use as their cache identity (`Track.Key()`).

```sh
byom-sync resolve all --input playlists/00-conceptual/my-mixtape.yaml
```

### Flags

| Flag | Default | Notes |
|---|---|---|
| `--input` | config `dir` | file or directory, recursive per Part 1 |
| `--limit` | `0` | passed to each stage (unlimited) |
| `--delay` | unset | when **not** passed, each stage keeps its own default (youtube 500ms, spotify 200ms, art per-MusicBrainz pacing); when passed, the value overrides all three. Detected with `cmd.Flags().Changed("delay")` rather than a sentinel value |
| `--no-cache` | `false` | applied uniformly to all stages |
| `--download` | `true` | art stage; the site serves local copies |
| `--skip-spotify` / `--skip-art` / `--skip-youtube` | `false` | for reruns |

`--download` defaults to **true** here (unlike `resolve art`, where it is
opt-in), because the only reason to run the whole pipeline is to prepare a
playlist for the site, and the site serves `image_file` copies to survive
source-URL rot.

### Preflight

Before any stage runs, check the prerequisites of every *enabled* stage and
report all failures at once:

- Spotify token via `auth.LoadToken()` (`internal/auth/store.go:52`) — needed by
  the `spotify` stage and by the art stage's Spotify-first pass. Purely local,
  no network.
- `yt-dlp` on `PATH` via `exec.LookPath` — needed by the `youtube` stage.

If anything is missing, abort with a single message naming each missing
prerequisite and its remedy (`run byom-sync auth`, `install yt-dlp`). Skipped
stages are not checked, so `--skip-youtube` runs fine on a host without yt-dlp.

This is a deliberate divergence from `resolve art`, which currently *degrades*
to MusicBrainz-only when there is no token. Within `resolve all` a missing token
is an error, because the user asked for the full pipeline; run the stages
individually to get the degrading behavior.

### Implementation shape

The three `runResolve*` functions already share the `func(context.Context) error`
signature and read package-level flag vars. `resolve all` will:

1. Populate the per-stage globals from its own flags.
2. Run preflight.
3. Call the stages in order through an injectable slice of named stage funcs, so
   sequencing and skip logic are testable without network or credentials.

**Landmine to neutralise:** `resolveNoCache` is a single global written by both
`runResolveArt` (`cmd/resolve.go:371`) and `runResolveSpotify`
(`cmd/resolve.go:545`). In a single process running art *then* youtube, the art
stage's `--no-cache` value silently becomes the youtube stage's. Driving all
three from one `--no-cache` flag makes this consistent; the assignment sites get
a comment noting the coupling.

### Testing

- Stage sequencing: stages run in the documented order.
- Each `--skip-*` flag removes exactly its stage.
- Preflight reports *all* missing prerequisites together, not just the first.
- Preflight ignores prerequisites of skipped stages.
- A stage returning an error aborts the pipeline and surfaces that error.

All via injected stage funcs and a faked prerequisite checker — no network.

---

## Part 4 — `byom-sync auth --manual`

### The problem

`RedirectURL()` (`internal/auth/auth.go:24`) hardcodes
`http://127.0.0.1:<port>/callback`, and the callback server binds `127.0.0.1`
(`internal/auth/auth.go:88`). Both are correct — Spotify mandates that exact
loopback URI for PKCE apps and matches it as an exact string.

But over SSH, the consent page redirects the *laptop's* browser to the
*laptop's* port 8888, where nothing is listening. The authorization code is
delivered to the wrong machine, so `byom-sync auth` on `myriad-docker` can never
complete. `xdg-open` also fails there, which is cosmetic but adds noise.

The redirect URL cannot be changed to fix this: Spotify rejects non-loopback
hosts for PKCE.

### The change

Add `--manual` to `auth`, which skips both the browser launch and the callback
listener:

1. Print the consent URL.
2. User opens it on any machine and authorizes.
3. The browser lands on a connection error — but the address bar still holds
   `http://127.0.0.1:8888/callback?code=...&state=...`.
4. User pastes that full URL back into the terminal.
5. Parse it, verify `state` matches, exchange the code with the PKCE verifier
   held in the same process, save the token.

Accept either the full pasted URL or a bare `code` value, since address-bar
copying is fiddly. On a `state` mismatch, fail with an explicit CSRF message
rather than attempting the exchange.

No listener is bound in this mode, so it cannot collide with a port already in
use — a small side benefit.

### Testing

- Parsing a full pasted redirect URL yields the right code.
- Parsing a bare code works.
- `state` mismatch is rejected before any exchange is attempted.
- Surrounding whitespace in the paste is tolerated.

The token exchange itself talks to Spotify and stays untested, matching how the
existing interactive flow is covered.

### Also documented, not built

SSH local forwarding solves the same problem with no code:

```sh
ssh -L 8888:127.0.0.1:8888 myriad-docker
```

The laptop browser's request to `127.0.0.1:8888` then tunnels to the server's
listener and the normal flow completes. This goes in the README as the
no-flag alternative.

---

## Non-goals

- **A `--flat` export mode.** Mirroring covers the known use cases; add it if a
  media server turns out to need a single flat directory.
- **Fixing `--embed-art` when exporting a subdirectory.** Pre-existing art-root
  edge case, noted in Part 2, unchanged by this work.
- **Changing `resolve art`'s degrade-without-token behavior.** Only `resolve all`
  treats a missing token as fatal.
- **Parallelising stages.** They are ordered by data dependency, and both
  MusicBrainz (~1 req/sec) and YouTube are rate-limited.
- **Auto-installing `yt-dlp`.** Preflight reports it; the user installs it.

## Consumer changes (`mixtapes.lmorchard.com`)

- `make enrich PL=<path>` → `byom-sync resolve all --input $(PL)`, with `PL`
  defaulting to `playlists`.
- `make resolve-art` keeps working and stops being a no-op once Part 1 lands.
- README: drop the non-recursive caveat, document `make enrich`, and add the
  headless-auth note for the server.

## Risks

- **Recursion touches more files than before.** That is the point, but on the
  mixtapes hub `resolve art` now sees 58 playlists instead of 0. Users who
  relied on shallow behavior to scope a run should pass a subdirectory. The
  existing `--limit` flag remains the throttle.
- **Preflight is stricter than the individual commands.** Documented above; the
  per-stage commands still degrade as they always did.
- **`export` output layout changes — but only where it currently produces
  nothing.** A flat hub exports exactly as before (`Rel()` is just the basename,
  so no subdirectories appear). A nested hub previously wrote zero files, so
  there is no existing output anyone could be depending on. The one observable
  difference is that `--out` may now contain directories.
