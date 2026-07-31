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
  `export`, `resolve` (subcommands `all`, `youtube`, `spotify`, `art`, `purchase`,
  `prime`, `cache stats`, `cache clear`), `site`, `dates`.
- `internal/playlist/` — the hub: `types.go` (`Playlist`/`Track`/`SyncState`,
  `Track.Key()`), `store.go` (`HubPaths` — the canonical recursive hub walk, plus
  `Load`/`LoadFile`/`FindFileByID`/`Save`/`Slug`),
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
- `internal/match/` — normalized string similarity shared by `spotifyenrich` and
  `purchase`: `Norm` (token normalization) and `Sim` (partial-ratio edit
  distance over normalized strings), extracted from `spotifyenrich/score.go` so
  both packages score candidate matches identically.
- `internal/spotifyenrich/` — reverse enrichment: `score.go` (`Candidate`,
  `Score`, similarity), `search.go` (`Searcher`/`ClientSearcher`, `buildQuery`,
  `toCandidate`, image pick), `enrich.go` (`Enrich` loop, `Options`, `Event`,
  `Cache`, `applyCandidate`). Fills empty technical fields on confident matches;
  writes `enrich_candidates` for ambiguous ones.
- `internal/coverart/` — cover-art resolution: `musicbrainz.go` (`MBClient`:
  release-group + recording search), `coverartarchive.go` (`CAAClient`: front
  image for a release/release-group MBID), `resolver.go` (`Resolver`/`Arter`,
  album-first then recording fallback), `resolve.go` (`Resolve` loop, `Options`,
  `Event`, `Cache`). Public APIs, no key; MusicBrainz needs a User-Agent + ~1
  req/sec pacing.
- `internal/artstore/` — content-addressed cover-art download store: `artstore.go`
  (`Store.Save`/`Load` for persistent local art with dedup by image bytes).
- `internal/purchase/` — purchase-link resolution: `types.go` (`Query`/`Result`/
  `Source`/`Kind`, `Score`/`Accept` confidence gate, `Threshold`/`SubjectFloor`),
  `bandcamp.go`, `itunes.go`, `discogs.go` (the three tiers — see "Purchase
  links" below), `resolve.go` (`Resolve` loop, `Options`, `Event`, `Cache`,
  one-lookup-fills-every-track-on-the-album fan-out). No MusicBrainz source: it
  was measured and excluded (see below).
- `internal/rcache/` — SQLite cache with four tables in one `cache.db`:
  `resolution_cache` (YouTube), `enrichment_cache` (Spotify), `art_cache`
  (cover art: `ArtEntry`, `GetArt`/`PutArt`), and `purchase_cache` (purchase
  links: `PurchaseEntry`, `GetPurchase`/`PutPurchase`, `ClearPurchaseSource` to
  wipe one tier's rows without discarding the others'). `Stats`/`EnrichStats`/
  `ArtStats`/`PurchaseStats` and `Clear` span all four; the first three key by
  `Track.Key()`, but purchase keys by `Query.CacheKey(source)` (source+kind+
  normalized artist+subject) since a tier-1 miss must not block tier 2 from
  trying the same album. Gitignored, disposable.
- `internal/config/`, `internal/templates/` (embedded Markdown template).
- `internal/site/` — the static site generator (`byom-sync site`): recursive
  hub walk → per-playlist JSPF + HTML pages embedding `<byom-player>`,
  `site-index.json` + `<byom-site-nav>`, OG metadata, RSS. Reuses
  `export.JSPFExporter`. Content pages (`site.pages_dir`, default `./pages`):
  `*.md` with YAML frontmatter (`title`/`order`) → `/pages/<slug>/` pages linked in the
  header. The site copies the hub's cover-art store (`<hub>/art/`) into the build
  output and references downloaded images as `base_url + image_file` in each
  `playlist.jspf.json` (via the exporter's `art_base` option) and the OpenGraph
  image, serving downloaded art from the site to survive source-URL rot; tracks
  without a local cached copy retain their source URLs. A playlist with
  `featured: true` is additionally promoted into a flat `Featured` list at the top
  of the landing page and of the sidebar nav (`featuredOf` walks the whole tree);
  it keeps its normal position in the year groups and nav tree as well.
  `site-index.json` is an object — `{"featured": [...], "children": [...]}` —
  with the featured list pre-sorted server-side.
  The RSS feed (`internal/site/feedbody.go`) gives each item a rich HTML body —
  cover art, playlist prose, a meta line, and the first `site.feed_track_limit`
  tracks (default 20) as YouTube links, falling back to an `https://`
  `spotify_url`; a track with neither still appears, unlinked — written to both
  `<description>` and `<content:encoded>` because many readers render only the
  former. Track thumbnails and the per-item `<enclosure>` use locally stored art
  only: an enclosure needs a byte length, which would otherwise mean a network
  request during an offline build. The feed carries the newest
  `site.feed_item_limit` playlists (default 25).

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
  stubs (empty title+artist) are filtered at fetch. The `convert()` function also
  captures album art into `Track.Image` from `Album.Images`.
- **Sync must not clobber locally-derived fields.** `Merge` starts from the
  *remote* playlist (`out := remote`), so anything Spotify doesn't send back is
  blank unless explicitly carried over. `Merge` copies playlist-level `featured` +
  hero art, and `adoptLocalFields` copies each surviving track's `youtube_id`,
  `image_file`, `purchase_url`, `spotify` opt-out, and `enrich_candidates`;
  `Image` is the one field where remote wins when non-empty. Without this, a single sync wiped every
  `resolve` result — on the live hub that was 8292 `youtube_id`s and 8318
  `image_file`s in one playlist, silently, with a zero exit code. When you add a
  locally-derived field to `Playlist` or `Track`, add it here too, or sync will
  quietly delete it. A side effect worth knowing: because the lookup is by
  `Track.Key()`, duplicate remote entries of the same song (common in
  scrobble-log playlists) all inherit the one local track's resolved ids.
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
  Recommended pipeline order: author/`sync` → `resolve spotify` → `resolve art`
  → `resolve purchase` → `resolve youtube` → `export`.
- **Purchase links:** `resolve purchase` fills `Track.PurchaseURL` with a
  best-effort "where to buy this" link, running the tiers below in order, each
  a full pass over whatever the previous tier left unresolved: Bandcamp →
  iTunes → Discogs.

  **Hit rates, measured on the live hub, not sampled.** Bandcamp **34%** (2,358
  of 7,025 distinct albums, after the artist-identity check). iTunes **66%** of
  what Bandcamp left (3,110 of 4,690). Together that is **78% of distinct
  albums** and **83% of tracks** — 5,707 of 7,292 albums, 11,655 of 14,119
  tracks. Discogs has not been run at volume; its **~39%** is still a ~30-album
  sample and should be treated as such.

  Two notes on how these numbers moved. Bandcamp was documented at 47% until a
  full run showed 34%: the 47% counted whether the gate *accepted* a result,
  never whether the artist was right, and roughly a third of those acceptances
  were impostor accounts. iTunes' sampled 65% did hold up at volume. The old
  "~85% cumulative" was arithmetic over three numbers, one of which was wrong;
  the 78% above is measured.

  An earlier "~47%" for Bandcamp came from counting whether the gate *accepted*
  a result, never whether the artist was right. Roughly a third of those
  acceptances were impostor accounts. When quoting a hit rate here, say what it
  counted. Every candidate passes the same confidence
  gate as `spotifyenrich` (`purchase.Accept`/`Score`, built on `internal/match`,
  plus a `SubjectFloor` on top of `Threshold`) because a store's search will
  happily return a real but wrong album for a same-artist query (iTunes answers
  "Theatre Is Evil" with "Piano Is Evil"). **Bandcamp needs a second, different
  check:** its `band_name` is free text the uploader controls, so cover bands,
  DJ edits, karaoke and stem packs put the *original* artist's name in it and
  score a legitimate 1.000 on both fields — the gate cannot see the difference.
  `hostIsArtist` compares the account subdomain, which is far harder to spoof,
  and does so by *prefix*: an artist's own account carries a suffix
  ("glosserband", "ghostcopnyc") while a tribute embeds the name mid-string
  ("nevermindatributetonirvana"), so a substring test would let tributes
  through. Measured on 6 mainstream stress cases plus 30 random hub albums, it
  dropped 5 of 11 accepted results and every one of the 5 was wrong, with no
  correct match lost. This only shows up for well-known artists — nobody
  uploads an impostor "Slow Glows" page — which is why the original 47%
  sampling in #50 missed it entirely (that number counted acceptance, never
  identity). The full-hub run afterwards put the real rate at 34%.

  **A cached result predates any later matching fix.** `purchase_cache` stores
  the resolved URL, so a hit short-circuits the source entirely — gate, identity
  check and all. After the artist-identity fix landed, a full Bandcamp run still
  re-applied three impostor links, because those albums had been cached by a
  test run made *before* the fix existed. Clear a tier's cache
  (`resolve cache clear --source <tier>`, or `--reresolve`) after any change to
  how that tier matches, or the change silently will not apply to anything
  already cached. Note `ClearPurchaseSource` wipes the tier's whole key space,
  misses included, so the next run re-queries everything it previously skipped
  (byom-sync#64).

  iTunes results are accepted only when
  the album carries a real price — iTunes Store downloads are DRM-free, but a
  `music.apple.com` link with no price is an Apple Music *stream*, not a
  purchase. Discogs is a two-step lookup: its search response's `title` is an
  unreliable "Artist - Album" string with no availability signal, so a
  candidate that clears a first-pass rank spends a second request on the
  release endpoint for authoritative artist/title fields plus `num_for_sale`
  (zero for sale is a dead link, rejected) — and even a live listing is
  secondhand physical media, which doesn't fill a gap in a digital collection
  unless the record is ripped, hence last tier. MusicBrainz was measured as a
  fourth source and dropped: 3% hit rate, zero contribution unique to it.
  **Discogs `uri` trap:** the search response's `uri` is site-relative
  (`/release/249504-…`) but the release resource's is absolute
  (`https://www.discogs.com/release/249504-…`). `discogs.go` reads the release
  resource, so it must *not* prefix the site; doing so emitted
  `https://www.discogs.comhttps://www.discogs.com/release/…` for every hit.
  `discogsPermalink` tolerates both shapes, and `testdata/discogs_release.json`
  now carries the real absolute form (a fixture captured from the wrong endpoint
  is what let the bug ship).
  `discogs_token` (optional, a viper default in `cmd/root.go`, not
  `internal/config`) raises Discogs from 25 to 60 req/min. **Pace floors are
  per *lookup*, but Discogs makes two requests per lookup**, so its floor is
  double the per-request gap (`purchaseSourceRequestsPerLookup`). Pacing it as
  one request ran a live sample at ~48 req/min against a 25/min limit and drew
  HTTP 429s — which `Resolve` counts as errors, so the consecutive-error
  breaker would abort the tier mid-run. Rate pacing and the
  consecutive-error streak live in a `purchase.Tier` the caller creates once per
  tier and passes to every per-file `Resolve` call — call-local state would leave
  the first lookup of every hub file unpaced and would never accumulate a streak.
  A tier stops with `purchase.StopErrors` after `maxConsecutiveErrors` failures
  in a row; later tiers still run. `--reresolve` un-writes a tier's answers
  (its cache rows via `rcache.ClearPurchaseSource`, plus every `purchase_url`
  matching `cmd.purchaseSourceMarkers`) and resolves them fresh;
  `resolve cache clear --source <tier>` does the cache half alone. A cold fill across a
  ~7,165-album hub runs roughly 6 hours (~2h Bandcamp, ~3.2h iTunes, 30–75min
  Discogs); incremental and resumable, and stopping after the Bandcamp tier
  alone is a reasonable outcome — cheapest pass, best links.
- **Cover art:** `Track.Image` (album cover URL) is populated by `sync` (album art
  captured at fetch), `resolve spotify` (enrichment from Spotify search response),
  or `resolve art` (MusicBrainz/Cover Art Archive fill). `resolve art` is Spotify-first:
  a batched `GetTracks`-by-id pass fills art for tracks with a `spotify_id`, then
  MusicBrainz (release-group by artist+album, else recording by artist+title)
  fills remaining gaps. It degrades gracefully to MusicBrainz-only (with a warning)
  when there's no Spotify token. CAA URLs are normalized to https. `resolve art`
  fills any track missing an image regardless of `spotify:false`, so off-Spotify
  tracks get art. `Playlist.Image` is an authored playlist-level hero URL; when
  set it wins over the first-track fallback at export/site, otherwise the first
  track's image is used. `resolve art --download` saves resolved art into a
  shared, content-addressed `<hub>/art/<hh>/<hash>.<ext>` store (dedup by image
  bytes) and records `Track.ImageFile` — plus `Playlist.ImageFile` for the hero —
  (hub-relative; `Image` stays the source URL).
  `export jspf --embed-art` inlines those local copies as `data:` URLs for a
  self-contained file (run `--download` first; network-free). Pipeline: `resolve
  spotify` → `resolve art` → `resolve purchase` → `resolve youtube` → `export`.
- **Exporters:** m3u8 builds `{prefix}/{Artist}/{Album}/{Title}.{ext}` paths
  verbatim; jspf uses `urn:isrc:` identifiers (or a synthesized
  `urn:byom:<sha1(ContentKey)>` when a track has no ISRC, so every track is
  addressable) + `location` (spotify_url) + `image` (track and playlist cover
  art); markdown is frontmatter + tracklist table via the embedded,
  init-overridable template. Playlist `date_created` maps to the JSPF `date` and
  markdown `date`; `date_updated`/`date_imported` ride a playlist-level byom
  extension in JSPF (namespace `https://github.com/lmorchard/byom-sync`), and
  `date_updated` also appears as markdown `updated`. (byom-player does not yet
  read the playlist-level date extension.)
- **Hub discovery is recursive and centralized.** `playlist.HubPaths(input)` is
  the single definition of "which files are in the hub": it walks subdirectories
  to any depth, skips dotfiles (including macOS `._*.yaml` sidecars), and skips
  the hub-root `art/` store. `cmd.hubPaths`, `export.Run`, and `playlist.Load`
  all delegate to it, and it matches `internal/site/tree.go`'s rules. Do not
  reintroduce a `filepath.Glob(dir + "/*.yaml")` — that shallow glob silently
  found zero playlists in a subdirectory-organized hub, which broke `resolve`
  and `dates` for months without an error. `FindFileByID` was a missed call site
  of exactly that bug: because it scanned only the hub root, `sync` decided every
  subdirectory-filed playlist was new and wrote a duplicate at the root — 57 of
  them on the live hub in one run, leaving the originals stale. It delegates to
  `HubPaths` now, treating a not-yet-created hub directory as empty rather than an
  error (sync calls it before anything creates the directory).
- **`resolve all` drives the per-stage globals.** The stage functions
  (`runResolveSpotify`/`runResolveArt`/`runResolveYouTube`) read package-level
  flag vars, and `resolveNoCache` in particular is assigned by two of them. When
  adding a stage flag, fan it out in `runResolveAll` too, or the pipeline and the
  standalone command will disagree.

## CI / release

`.github/workflows/`: `ci.yml` (PR lint+test), `release.yml` (tag `v*`, matrix
binaries), `rolling-release.yml` (push to main → `latest` prerelease). Actions
pinned to Node-24 versions (checkout@v7, setup-go@v6, action-gh-release@v3).

## Workflow

- **Use PRs**, not direct pushes to `main`. Branch → PR → CI green → merge.
- Dev-session artifacts live in `docs/dev-sessions/{timestamp}-{slug}/`
  (`spec.md`/`research.md`/`plan.md`/`notes.md`). The `/dev-session` skill drives
  spec → plan → execute → pr.
- Commit trailer: end agent-authored commits with a `Co-Authored-By:` line naming
  whichever model actually wrote them. Don't copy a version out of this file —
  it would go stale, and the trailer should say who did the work.
- Verify before claiming done: run `make lint && make test && make build` and
  read the output. For live Spotify behavior, a real Premium account + registered
  app is needed (that's manual).
