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

- `cmd/` — Cobra commands: `root`, `version`, `init`, `auth`, `sync`, `import`,
  `export`, `resolve` (subcommands `youtube`, `spotify`, `prime`, `cache stats`,
  `cache clear`), `site`, `dates`.
- `internal/playlist/` — the hub: `types.go` (`Playlist`/`Track`/`SyncState`,
  `Track.Key()`), `store.go` (`Load`/`LoadFile`/`FindFileByID`/`Save`/`Slug`),
  `merge.go` (`Merge`, `Archive`/`Mirror`), `dates.go` (`RefreshDates`,
  `EnsureImportedDate`).
- `internal/auth/` — `store.go` (token JSON cache, `ErrNoToken`), `auth.go`
  (PKCE flow, `Client`, `PersistRefreshed`).
- `internal/spotifyfetch/` — `fetch.go` (`ParseID`, `Fetch` w/ pagination,
  `convert`, `isCatalogStub`, `ListMyPlaylists`, `selectOwnedIDs`).
- `internal/export/` — `export.go` (`Exporter` iface + `Run` dispatcher),
  `m3u8.go`, `jspf.go`, `markdown.go`.
- `internal/youtube/` — resolver chain: `resolver.go` (`Resolver`/`Chain`/`Result`),
  `ytdlp.go` (yt-dlp search + `IsEmbeddable`), `youtube.go` (Data API search),
  `resolve.go` (`Resolve` loop, `ResolveOptions`, `Cache` interface, TTL logic).
- `internal/spotifyenrich/` — reverse enrichment: `score.go` (`Candidate`,
  `Score`, similarity), `search.go` (`Searcher`/`ClientSearcher`, `buildQuery`,
  `toCandidate`, image pick), `enrich.go` (`Enrich` loop, `Options`, `Event`,
  `Cache`, `applyCandidate`). Fills empty technical fields on confident matches;
  writes `enrich_candidates` for ambiguous ones.
- `internal/rcache/` — SQLite cache with two tables in one `cache.db`:
  `resolution_cache` (YouTube: `Entry`, `Get`/`Put`) and `enrichment_cache`
  (Spotify: `EnrichEntry`, `GetEnrich`/`PutEnrich`). `Stats`/`EnrichStats`/`Clear`
  span both; keyed by `Track.Key()`; gitignored, disposable.
- `internal/config/`, `internal/templates/` (embedded Markdown template).
- `internal/site/` — the static site generator (`byom-sync site`): recursive
  hub walk → per-playlist JSPF + HTML pages embedding `<byom-player>`,
  `site-index.json` + `<byom-site-nav>`, OG metadata, RSS. Reuses
  `export.JSPFExporter`.

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
  Track identity (`Track.Key()`) = ISRC, falling back to normalized
  `artist+title+album` (`ContentKey()`). `archive`
  (default) soft-orphans removed tracks (`spotify_present:false` +
  `date_orphaned`); `mirror` overwrites. Playlist selection: config `playlists`
  by default, positional args override, `--all` = all owned. Catalog-removed
  stubs (empty title+artist) are filtered at fetch.
- **Dates:** three playlist-level fields. `date_imported` is when byom-sync first
  saw the playlist (Spotify exposes no true creation date); `date_created` and
  `date_updated` are the earliest and latest track `added_at` (all tracks,
  orphaned included), falling back to `date_imported` when no track has one.
  Sync stamps/preserves `date_imported` then recomputes the pair via
  `Playlist.RefreshDates()`; native `import` stamps `date_imported`. Run
  `byom-sync dates` to backfill/refresh the whole hub in place — it migrates a
  pre-change file by promoting its old `date_created` to `date_imported`
  (`EnsureImportedDate`), and is idempotent.
- **Native playlists:** a hub file with no `spotify_id` is a hand-authored
  ("native") playlist — just `title`/`creator`/`tracks`, where each track needs
  only `title` and `artist` (`album` optional). Provenance is *derived*, never
  stored: use `playlist.Playlist.Source()` / `IsNative()` (source `native` when
  no source ID is set), not ad-hoc `spotify_id == ""` checks — this is the single
  extension point for future ingestion sources. `sync` never touches native files
  (it matches by `spotify_id`; slug collisions get a `-<id>` suffix). Spotify-only
  behavior (orphan/`sync_state` emission) is gated on `Source()`. `import <file>`
  builds a native playlist from a plain-text `{artist} - {title}` list
  (`playlist.ParseText`; `# title:`/`# creator:` header lines, split on the first
  ` - `, malformed lines skipped with a warning); writes `<dir>/<slug>.yaml`,
  refusing to overwrite without `--force`.
- **Enrichment (reverse path):** `resolve spotify` searches Spotify per track and
  fills only *empty* technical fields (`isrc`, `spotify_id`, `spotify_url`,
  `duration_ms`, `album`, `image`), preserving authored `title`/`artist`/`album`
  unless `--canonicalize`. Only matches scoring ≥ threshold (0.8, in
  `spotifyenrich`) auto-fill; below that, the track's top matches are written as
  `enrich_candidates` — accept one by copying its `spotify_id` up to the track's
  own `spotify_id` and re-running. Set `spotify: false` on a track (a `*bool`:
  absent/`true` = enrich, `false` = opt out) to assert it has no Spotify
  equivalent — `resolve spotify` then skips it and clears any stale candidates.
  Recommended pipeline order:
  author/`sync` → `resolve spotify` → `resolve youtube` → `export`, so YouTube
  resolution and its cache are keyed on the enriched ISRC (`Track.Key()` is
  ISRC-first).
- **Exporters:** m3u8 builds `{prefix}/{Artist}/{Album}/{Title}.{ext}` paths
  verbatim; jspf uses `urn:isrc:` identifiers (or a synthesized
  `urn:byom:<sha1(ContentKey)>` when a track has no ISRC, so every track is
  addressable) + `location` (spotify_url); markdown is frontmatter + tracklist
  table via the embedded, init-overridable template. Playlist `date_created` maps
  to the JSPF `date` and markdown `date`; `date_updated`/`date_imported` ride a
  playlist-level byom extension in JSPF (namespace
  `https://github.com/lmorchard/byom-sync`), and `date_updated` also appears as
  markdown `updated`. (byom-player does not yet read the playlist-level extension.)

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
