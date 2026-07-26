# Spec — Recursive hub discovery, one-shot enrichment, headless auth

**Date:** 2026-07-26
**Branch:** `feat/recursive-hub-and-enrich-all`
**Status:** approved design, pending implementation plan

## Summary

Three related fixes, driven by real breakage found while hand-authoring a
playlist for `mixtapes.lmorchard.com`:

1. **Recursive hub discovery** — `hubPaths()` only globs one directory level, so
   `resolve {youtube,spotify,art}` and `dates` silently do nothing on a hub whose
   playlists live in subdirectories. This is a live regression.
2. **`resolve all`** — collapse the three-command enrichment pipeline into one
   invocation with up-front prerequisite checks.
3. **`auth --manual`** — make authentication possible on a headless/remote host.

They ship together because (2) is only usable once (1) works, and (3) is what
makes (2) runnable on the deployment box.

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
- **YAGNI on `export`.** It has the same shallow-glob bug but a different
  failure mode; it is explicitly out of scope (see Non-goals).

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

`hubPaths()` is the single discovery point for five commands — `cmd/dates.go:34`
and `cmd/resolve.go:112,334,527,682` (`youtube`, `spotify`, `art`, `prime`). One
fix repairs all five.

### The change

Replace the glob with a `filepath.WalkDir` that mirrors the site generator's
rules (`internal/site/tree.go:44-56`):

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

Table-driven tests in `cmd/`, using `t.TempDir()`:

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

## Part 2 — `byom-sync resolve all`

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

## Part 3 — `byom-sync auth --manual`

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

- **`export.Run()` recursion.** `internal/export/export.go:36` has the identical
  shallow-glob bug, but making it recursive flattens a tree into one `--out`
  directory, so two same-named playlists in different folders would silently
  overwrite each other. Fixing it needs a decision about mirroring the tree in
  the output, which is a separate design.
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
