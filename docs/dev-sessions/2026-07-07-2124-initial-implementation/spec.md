# byom-sync CLI Spec

**Goal:** Give a music curator a Git-friendly, platform-agnostic home for their
Spotify playlists — extract playlists into local YAML ("the hub"), then compile
that YAML into M3U8, JSPF, and Hugo Markdown ("the spokes").

**Source:** User request 2026-07-07, refining `docs/initial-spec.md`.

## Current state

Greenfield repo — only the committed sketch (`docs/initial-spec.md`) exists. No
Go module yet. Two proven local patterns anchor the build (see
`research.md`):

- The **go-cli-builder scaffold** (`--no-database --templates`) provides
  Cobra/Viper/logrus wiring, a Makefile, CI/release workflows, and an embedded
  `text/template` system. Config loads via Viper (`cmd/root.go` `initConfig`).
- The sibling **spotify-to-markdown** proves Spotify OAuth against Spotify's
  endpoints but hand-rolls auth + a single-page API client with **no pagination
  and no retry** (`research.md` §2) — so its API layer is NOT reusable for
  playlists >100 tracks. Its PKCE/callback-server shape is instructive.

## Desired end state

A `byom-sync` binary scaffolded from go-cli-builder (no SQLite, templates on),
module `github.com/lmorchard/byom-sync`, with this command surface:

- `byom-sync auth` — PKCE OAuth against Spotify; caches tokens to disk; silent
  refresh on later commands.
- `byom-sync sync [playlist-url-or-id ...] --dir <path> --strategy <archive|mirror>`
  — pulls playlists into one YAML file each. With no positional args, syncs the
  `playlists:` list from config; with positional args, syncs exactly those
  (one-off override, config list ignored).
- `byom-sync export m3u8 --input <path> --out <path> --lib-prefix <string> [--ext flac]`
- `byom-sync export jspf --input <path> --out <path>`
- `byom-sync export hugo --input <path> --out <path>`
- Plus scaffold-provided `init` and `version`.

**Data schema (the hub)** — one YAML file per playlist, matched on `spotify_id`:

```yaml
spotify_id: "37i9dQZF1DXcBWIGoYBM5M"   # authoritative re-sync key
title: "Playlist Name"
creator: "User Name"
date_created: "2026-07-07T00:00:00Z"
tracks:
  - title: "Track Title"
    artist: "Artist Name"
    album: "Album Name"
    isrc: "GBA098000010"
    duration_ms: 354000
    sync_state:
      spotify_present: true
      date_orphaned: ""
```

`export` reads a single YAML file (or a directory of them) and emits:
- **m3u8:** `#EXTM3U` + `#EXTINF` per track; path `{prefix}/{Artist}/{Album}/{Title}.{ext}` (ext default `flac`).
- **jspf:** JSPF JSON, tracks as `{"identifier": ["urn:isrc:{isrc}"], "title", "creator", "album", "duration"}`.
- **hugo:** Markdown with YAML frontmatter (title/creator/date) + a tracklist table, via an embedded, `init`-overridable template.

## Design decisions

- **Decision:** Spotify access via `github.com/zmb3/spotify/v2` +
  `golang.org/x/oauth2`, auth-code flow with **PKCE (S256)**.
  - **Why:** zmb3/v2 gives real cursor pagination (`NextPage`) for playlists >100
    tracks and `spotify.WithRetry(true)` for 429 handling — both mandatory here
    and both absent from the sibling. PKCE avoids storing a client secret;
    x/oauth2 ≥0.10 supports it via `oauth2.GenerateVerifier`/`S256ChallengeOption`/
    `VerifierOption`, threaded through zmb3's `AuthURL(...opts)`/`Token(...opts)`.
  - **Rejected:** hand-rolling like the sibling (would reimplement pagination +
    retry from scratch); client-id+secret flow (needs a secret at rest). Fallback:
    if PKCE proves impractical with zmb3, drop to client-id+secret — noted, not
    expected.

- **Decision:** Config-listed playlists by default; positional args override.
  - **Why:** the config `playlists:` list is the curated, git-tracked source of
    truth for routine `sync`; positional args cover one-off pulls without editing
    config. Positional args, when present, fully replace the config list (no
    merge) — least surprising.
  - **Rejected:** "sync all owned playlists" (pulls algorithmic/followed noise).

- **Decision:** One YAML file per playlist, matched on the `spotify_id` header.
  - **Why:** clean per-playlist git diffs and hand-editing. Matching on the
    embedded `spotify_id` (not filename) means renaming a file's title-slug never
    breaks re-sync. Filename derived from a sanitized title slug; collisions get a
    short `spotify_id` suffix.
  - **Rejected:** single aggregate YAML (noisy diffs, awkward per-playlist edits).

- **Decision:** Track identity = ISRC, falling back to normalized `Artist+Title`
  when ISRC is absent.
  - **Why:** ISRC is stable across platforms; some tracks (local files, podcasts)
    lack one. Matches the spec's stated key.

- **Decision:** `archive` (default) is append-only + soft-orphan; `mirror`
  overwrites to exact remote state.
  - **Why:** archive protects curation history — tracks removed upstream get
    `spotify_present: false` + `date_orphaned` (RFC3339 `time.Now().UTC()`), never
    deleted. mirror is the escape hatch for a clean 1:1 copy.

- **Decision:** App config is Viper YAML (`byom-sync.yaml`, cwd or
  `--config`, plus `$XDG_CONFIG_HOME/byom-sync/`); OAuth tokens cache separately
  to `$XDG_CONFIG_HOME/byom-sync/token.json` (fallback `~/.config/...`).
  - **Why:** keeps go-cli-builder's Viper convention for settings (client_id,
    redirect_port, scopes, playlists, dir) while isolating rotating secrets in a
    single-purpose JSON file that's easy to gitignore and refresh.
  - **Rejected:** the sketch's single `config.json` for everything; SQLite token
    row (no DB in this tool).

- **Decision:** Scopes limited to `playlist-read-private` +
  `playlist-read-collaborative`.
  - **Why:** byom-sync only reads playlists; the sibling's listening-history
    scopes are irrelevant.

- **Decision:** Exporters implement a shared
  `Exporter` interface (`Export(p Playlist, outputPath string, cfg map[string]string) error`);
  `export` accepts a single `--input` file or a directory (each file → its own out file).
  - **Why:** matches the spec's "hub and spoke"; one dispatch path, three spokes.

## Patterns to follow

- Scaffold with `go-cli-builder`'s `scripts/scaffold_project.py byom-sync
  --no-database --templates` (`research.md` §1) — do NOT hand-write boilerplate.
- Cobra/Viper wiring, `GetConfig()`/`GetLogger()` accessors, flag→Viper binding:
  mirror `assets/templates/root.go.template` (`research.md` §1).
- OAuth callback server shape (localhost callback, `sync.Once`, CSRF `state`,
  browser open, graceful shutdown): mirror spotify-to-markdown's
  `internal/spotifyauth/auth.go` `RunInteractiveFlow` (`research.md` §2) — but
  swap its hand-rolled exchange for zmb3's `spotifyauth` package.
- Embedded, `init`-overridable output template: mirror
  `internal/templates/templates.go` `//go:embed` pattern (`research.md` §1) for
  the Hugo spoke.
- Config default path helper: mirror the XDG-based `defaultDatabasePath()` idea
  (`research.md` §2) for the token-cache path.

## What we're NOT doing

- **NOT** implementing the me-to-markdown `export --since/--until -o` orchestrator
  contract. byom-sync's `export` is a format compiler; the verb collides only in
  name (`research.md` §3). No `fetch`/`render`/`run` verbs.
- **NOT** pushing/writing back to Spotify (read-only tool).
- **NOT** resolving/verifying that `{prefix}/{Artist}/{Album}/{Title}.{ext}` paths
  exist on disk — m3u8 emits constructed paths verbatim.
- **NOT** adding SQLite or any local database.
- **NOT** concurrent multi-playlist workers beyond a simple bounded errgroup if
  it's trivial; if it adds meaningful complexity, sync playlists sequentially.
- **NOT** fuzzy track matching beyond ISRC → normalized Artist+Title.
- **NOT** supporting non-Spotify sources in this session.
- **NOT** Docker images (workflows ship with Docker commented out, as scaffolded).

## Open questions

- **Filename slug collision policy** — *Default:* sanitize title to
  `[a-z0-9-]`, append `-<first 6 of spotify_id>` only on collision. Proceed under
  this default.
- **`export` directory-mode output naming** — *Default:* when `--input` is a dir,
  `--out` is treated as an output dir and each playlist writes
  `<input-basename>.<m3u8|jspf|md>`. Proceed under this default.
- **JSPF `duration`** — JSPF spec uses seconds; *Default:* emit
  `round(duration_ms/1000)` as JSPF `duration`, keep `duration_ms` in the hub.
  Proceed under this default.
