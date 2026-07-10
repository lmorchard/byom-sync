# AGENTS.md — byom-sync

Context for coding agents working on this repo. Read this first.

## What this is

`byom-sync` (Bring Your Own Music) is a Go CLI that extracts Spotify playlists
into a local, Git-friendly YAML "hub," then compiles that hub into destination
"spoke" formats. Hub and spoke: Spotify is one source; the YAML files are the
source of truth you own; exporters render them into M3U8 / JSPF / Markdown.

Companion project: [`byom-player`](https://github.com/lmorchard/byom-player), a
web component that plays the exported JSPF.

## Stack

Go 1.25 · Cobra (CLI) · Viper (config) · logrus · `github.com/zmb3/spotify/v2`
(+ `/v2/auth`) · `golang.org/x/oauth2` (PKCE) · `gopkg.in/yaml.v3` ·
`golang.org/x/sync/errgroup`. Scaffolded `--no-database` (the hub is files);
the one exception is `modernc.org/sqlite` (pure-Go, no cgo) backing the optional
YouTube resolution cache in `internal/rcache/` — an index, not a source of truth.

## Layout

- `cmd/` — Cobra commands: `root`, `version`, `init`, `auth`, `sync`, `export`,
  `resolve` (subcommands `youtube`, `prime`, `cache stats`, `cache clear`).
- `internal/playlist/` — the hub: `types.go` (`Playlist`/`Track`/`SyncState`,
  `Track.Key()`), `store.go` (`Load`/`LoadFile`/`FindFileByID`/`Save`/`Slug`),
  `merge.go` (`Merge`, `Archive`/`Mirror`).
- `internal/auth/` — `store.go` (token JSON cache, `ErrNoToken`), `auth.go`
  (PKCE flow, `Client`, `PersistRefreshed`).
- `internal/spotifyfetch/` — `fetch.go` (`ParseID`, `Fetch` w/ pagination,
  `convert`, `isCatalogStub`, `ListMyPlaylists`, `selectOwnedIDs`).
- `internal/export/` — `export.go` (`Exporter` iface + `Run` dispatcher),
  `m3u8.go`, `jspf.go`, `markdown.go`.
- `internal/youtube/` — resolver chain: `resolver.go` (`Resolver`/`Chain`/`Result`),
  `ytdlp.go` (yt-dlp search + `IsEmbeddable`), `youtube.go` (Data API search),
  `resolve.go` (`Resolve` loop, `ResolveOptions`, `Cache` interface, TTL logic).
- `internal/rcache/` — SQLite resolution cache: `Entry`, `Open`/`Get`/`Put`/
  `Stats`/`Clear`. Keyed by `Track.Key()`; DB at `$XDG_CONFIG_HOME/byom-sync/cache.db`
  (gitignored, disposable — `resolve prime` rebuilds positives from the hub).
- `internal/config/`, `internal/templates/` (embedded Markdown template).

## Commands (Makefile-first)

`make setup` (installs pinned tools) · `make build` · `make test` · `make lint`
· `make format`. There is no `make check`.

**golangci-lint is pinned to v2.12.2** in `Makefile` (`GOLANGCI_LINT_VERSION`)
AND `.github/workflows/ci.yml` — keep the two in sync when bumping, or local and
CI will disagree (this bit us: `make setup` had installed a v1 that missed
errcheck findings CI caught).

## Conventions & gotchas

- Formatting via `gofumpt`; lint via golangci-lint v2. **errcheck is strict** —
  use `_ =` for intentionally-ignored returns (e.g. `fmt.Fprintln`,
  `viper.BindPFlag`).
- **zmb3/spotify v2.4.3 quirk:** `FullTrack.ExternalIDs` is a `map[string]string`
  (not the typed struct on `master`). ISRC is `ft.ExternalIDs["isrc"]`. Revisit
  `spotifyfetch/convert()` if you bump the dep.
- **Auth:** authorization-code + PKCE (S256), no client secret. Tokens cache at
  `$XDG_CONFIG_HOME/byom-sync/token.json` (0600) with silent refresh.
- **Config:** `byom-sync.yaml` in cwd or `$XDG_CONFIG_HOME/byom-sync/`; keys
  `client_id`, `redirect_port` (8888), `dir`, `playlists`. Register the Spotify
  app redirect URI as exactly `http://127.0.0.1:8888/callback`.
- **Sync:** per-playlist YAML matched on `spotify_id` (filename is cosmetic).
  Track identity = ISRC, falling back to normalized `artist+title`. `archive`
  (default) soft-orphans removed tracks (`spotify_present:false` +
  `date_orphaned`); `mirror` overwrites. Playlist selection: config `playlists`
  by default, positional args override, `--all` = all owned. Catalog-removed
  stubs (empty title+artist) are filtered at fetch.
- **Native playlists:** a hub file with no `spotify_id` is a hand-authored
  ("native") playlist — just `title`/`creator`/`tracks`, where each track needs
  only `title` and `artist` (`album` optional). Provenance is *derived*, never
  stored: use `playlist.Playlist.Source()` / `IsNative()` (source `native` when
  no source ID is set), not ad-hoc `spotify_id == ""` checks — this is the single
  extension point for future ingestion sources. `sync` never touches native files
  (it matches by `spotify_id`; slug collisions get a `-<id>` suffix). Spotify-only
  behavior (orphan/`sync_state` emission) is gated on `Source()`.
- **Exporters:** m3u8 builds `{prefix}/{Artist}/{Album}/{Title}.{ext}` paths
  verbatim; jspf uses `urn:isrc:` identifiers + `location` (spotify_url); markdown
  is frontmatter + tracklist table via the embedded, init-overridable template.

## CI / release

`.github/workflows/`: `ci.yml` (PR lint+test), `release.yml` (tag `v*`, matrix
binaries), `rolling-release.yml` (push to main → `latest` prerelease). Actions
pinned to Node-24 versions (checkout@v7, setup-go@v6, action-gh-release@v3).

## Workflow

- **Use PRs**, not direct pushes to `main`. Branch → PR → CI green → merge.
- Dev-session artifacts live in `docs/dev-sessions/{timestamp}-{slug}/`
  (`spec.md`/`research.md`/`plan.md`/`notes.md`). The `/dev-session` skill drives
  spec → plan → execute → pr.
- Commit trailer: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
- Verify before claiming done: run `make lint && make test && make build` and
  read the output. For live Spotify behavior, a real Premium account + registered
  app is needed (that's manual).
