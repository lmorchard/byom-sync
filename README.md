# byom-sync

**Bring Your Own Music.** A Go CLI that pulls your Spotify playlists into local,
Git-friendly YAML files, then compiles those files into other formats: M3U8 for
local media servers, JSPF for web components, and Markdown with YAML frontmatter
for static site generators.

The idea is a hub and spoke: Spotify is one source, your YAML files are the hub
(the source of truth you own and can version-control), and the exporters are
spokes that render the hub into whatever a given destination needs.

## Why

Playlist curation lives inside streaming platforms, where it's hard to back up,
diff, or reuse elsewhere. `byom-sync` copies that curation into plain YAML you
control. Tracks removed upstream aren't lost by default — they're kept and marked
orphaned, so your history survives even when Spotify's catalog changes.

## Features

- Spotify auth via the authorization-code + PKCE flow (no client secret to store)
- Sync individual playlists, a configured list, or every playlist you own
- Pagination and 429 retry handled by the Spotify client (tested against an
  8,000+ track playlist)
- One YAML file per playlist, matched on the Spotify playlist ID (renaming a file
  never breaks re-sync)
- Two sync strategies: `archive` (append-only, soft-orphans removed tracks) and
  `mirror` (exact overwrite)
- Exporters for M3U8, JSPF, and Markdown with YAML frontmatter
- A static site generator (`byom-sync site`) that builds a browsable site of
  `<byom-player>` pages from the hub

## Install

```sh
go install github.com/lmorchard/byom-sync@latest
```

Or build from source:

```sh
git clone https://github.com/lmorchard/byom-sync
cd byom-sync
make build      # produces ./byom-sync
```

## Spotify application setup

`byom-sync` needs a Spotify application to authenticate against.

1. Create an app in the [Spotify Developer Dashboard](https://developer.spotify.com/dashboard).
2. Add a Redirect URI of exactly `http://127.0.0.1:8888/callback`
   (use the loopback IP `127.0.0.1`, not `localhost` — Spotify requires it).
3. Under **User Management**, add the Spotify account you'll authenticate with
   (name + email). Development-mode apps reject accounts that aren't listed,
   sometimes with a confusingly generic `server_error`.
4. Copy the app's **Client ID** into your config (below).

Dashboard changes can take a few minutes to propagate to Spotify's authorize
endpoint, so if the first auth attempt fails with a redirect-URI error, wait a
moment and retry.

## Configuration

`byom-sync` reads `byom-sync.yaml` from the current directory or from
`$XDG_CONFIG_HOME/byom-sync/`. Run `byom-sync init` to generate a starter file,
or copy [`byom-sync.yaml.example`](byom-sync.yaml.example).

```yaml
# Spotify application client ID.
client_id: "your-spotify-client-id"

# OAuth callback port (must match the registered redirect URI).
redirect_port: 8888

# Where per-playlist YAML files live.
dir: "./playlists"

# Playlists synced when `byom-sync sync` runs with no arguments.
# Accepts raw IDs, spotify:playlist:<id> URIs, or open.spotify.com URLs.
playlists:
  - "https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M"

# YouTube resolution cache (optional; see "Resolve YouTube IDs").
# cache_path: ""          # default: $XDG_CONFIG_HOME/byom-sync/cache.db
# cache_miss_ttl: "720h"  # re-attempt an unmatched track after this long
# cache_embed_ttl: "720h" # re-verify a cached embeddable id after this long

# Discogs API token (optional; see "Resolve purchase links").
# Without one, Discogs lookups are limited to 25 req/min; with one, 60 req/min.
# discogs_token: ""
```

The OAuth token is cached separately at
`$XDG_CONFIG_HOME/byom-sync/token.json` (mode `0600`) and refreshed silently.

## Usage

### Authenticate

```sh
byom-sync auth
```

Opens your browser to Spotify's consent page, captures the redirect locally, and
caches a token. Later commands refresh it automatically.

On a headless or remote host, the normal flow can't complete: Spotify pins the
redirect URI to `http://127.0.0.1:<port>/callback` and matches it literally, so
over SSH the consent page redirects *your laptop's* browser to *your laptop's*
port and the code never reaches the machine running the command. Two ways
around it:

```sh
# Relay the code by hand — no callback server, works from any shell
byom-sync auth --manual

# Or forward the port before connecting, then use the normal flow
ssh -L 8888:127.0.0.1:8888 myhost
```

### Sync

```sh
# Sync the playlists listed in config
byom-sync sync

# Sync specific playlists (overrides the config list)
byom-sync sync https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M

# Sync every playlist you own
byom-sync sync --all

# ...including followed and algorithmic playlists
byom-sync sync --all --include-followed

# Choose a strategy and target directory
byom-sync sync --all --strategy mirror --dir ./playlists
```

**Strategies:**

- `archive` (default): append-only. New tracks are added. A track that's gone
  from the remote playlist is kept locally, marked `spotify_present: false`, and
  stamped with `date_orphaned`.
- `mirror`: overwrites the local file to match the remote playlist exactly;
  local-only tracks are dropped.

### Export

```sh
# M3U8 for a local media server (Navidrome, Mopidy, ...)
byom-sync export m3u8 --input ./playlists --out ./m3u8 --lib-prefix /mnt/nas/music

# Override the file extension (default: flac)
byom-sync export m3u8 --input playlist.yaml --out playlist.m3u8 --lib-prefix /music --ext mp3

# JSPF JSON
byom-sync export jspf --input ./playlists --out ./jspf

# Markdown with YAML frontmatter + tracklist table (Hugo and similar)
byom-sync export markdown --input ./playlists --out ./content/playlists
```

`--input` may be a single YAML file or a directory. When it's a directory, the
hub is walked recursively and `--out` mirrors its structure — so
`playlists/01-covers/numan-s-shadow.yaml` exports to
`<out>/01-covers/numan-s-shadow.<ext>`. Mirroring rather than flattening means
two playlists sharing a basename in different folders can't overwrite each other.

M3U8 track paths are built as `{lib-prefix}/{Artist}/{Album}/{Title}.{ext}` and
emitted as-is; the files aren't checked against the filesystem.

### Enrich everything at once

```sh
# Full pipeline over one playlist: spotify -> art -> purchase -> youtube
byom-sync resolve all --input playlists/00-conceptual/my-mixtape.yaml

# Skip a stage (also skips its prerequisite check)
byom-sync resolve all --skip-youtube
byom-sync resolve all --skip-purchase
```

`resolve all` runs the four enrichment stages in dependency order — the Spotify
stage writes the ISRCs that the art and YouTube stages use as their cache
identity. The purchase stage has no such dependency (it only reads
artist/album/title); it runs third here just to keep shopping metadata grouped
near cover art. Prerequisites for every enabled stage (a cached Spotify token,
`yt-dlp` on `PATH`) are checked *before* any stage runs, so a missing tool is
reported immediately rather than after a long art crawl.

`--download` defaults to true here, unlike `resolve art`. A missing Spotify token
is fatal here rather than degrading to MusicBrainz-only art; run `resolve art`
on its own if you want the degrading behavior.

### Resolve YouTube IDs

Fill in a `youtube_id` for hub tracks that lack one, so exporters (e.g. JSPF for
[`byom-player`](https://github.com/lmorchard/byom-player)) can play them. Resolution
uses [`yt-dlp`](https://github.com/yt-dlp/yt-dlp) (required on `PATH`), preferring
videos that allow embedded playback.

```sh
# Resolve missing ids across the hub (only missing tracks are attempted)
byom-sync resolve youtube --input ./playlists

# Re-check existing ids and replace any no longer embeddable
byom-sync resolve youtube --reresolve

# Cap network searches this run; pace them to stay under rate limits
byom-sync resolve youtube --limit 100 --delay 500ms
```

#### Resolution cache

Resolution is backed by an optional SQLite cache (an accelerator — the YAML hub
stays the source of truth, and `youtube_id` is still written into the YAML). It's
keyed by track identity (ISRC, else normalized artist+title), so a track resolved
in one playlist is reused across every playlist and across runs — including after
a wipe or re-export. It also caches misses (skipped until `cache_miss_ttl`
expires) and embeddability verdicts (trusted by `--reresolve` until
`cache_embed_ttl`). The DB lives at `$XDG_CONFIG_HOME/byom-sync/cache.db` and is
disposable — delete it and it rebuilds.

```sh
# Seed the cache from ids already resolved in the hub (do this once, no network).
# Defaults to trusting those ids as embeddable; pass --assume-embeddable=false to
# have the next --reresolve verify each instead.
byom-sync resolve prime

# Inspect coverage / clear entries
byom-sync resolve cache stats
byom-sync resolve cache clear --misses-only     # re-attempt unmatched tracks
byom-sync resolve cache clear --source discogs  # drop one purchase tier's rows
byom-sync resolve cache clear                   # wipe everything

# Bypass the cache for one run (pure network resolution)
byom-sync resolve youtube --no-cache
```

### Resolve purchase links

Fill in a `purchase_url` for hub albums that lack one — a best-effort "where to
buy this" link, e.g. for a shopping list. Three tiers run in order, each a full
pass over whatever the previous tier left unresolved:

1. **Bandcamp** — one request per album against Bandcamp's own search endpoint.
   Artist-friendly, DRM-free links. Measured at **34%** of all hub albums.
2. **iTunes** — the iTunes Search API. Measured at **66%** of what Bandcamp
   missed.
   Accepted only when the result carries a real price: iTunes Store downloads
   are DRM-free, but a `music.apple.com` link with no price is an Apple Music
   *stream*, not a purchase.
3. **Discogs** — marketplace search + release lookup, accepted only when copies
   are actually listed for sale. Fills **~26%** of what the other two missed —
   365 releases on the reference hub, mostly out-of-print, vinyl-only and
   compilation records the digital stores don't carry. A Discogs link is secondhand physical media
   — it doesn't fill a gap in a digital collection unless the record gets
   ripped, which is why it runs last.

**On those numbers.** All three are population figures from full runs over a real
~14k-track hub, not samples — together **85% of distinct albums** (6,236 of
7,316) and **90% of tracks** (12,690 of 14,155). Every match passes a
confidence gate before being accepted, since a store's search will happily
return a real but wrong album for a same-artist query.

(MusicBrainz was evaluated as a fourth source and dropped: it measured a 3% hit
rate with zero contribution unique to it.)

```sh
# Fill missing purchase links across the hub (all three tiers)
byom-sync resolve purchase --input ./playlists

# Run just one tier, e.g. to catch up Discogs alone after adding a token
byom-sync resolve purchase --source discogs

# Cap network lookups this run; add an extra pacing floor beyond each store's own
byom-sync resolve purchase --limit 200 --delay 2s

# Un-write one tier's links and resolve them again from scratch
byom-sync resolve purchase --source discogs --reresolve
```

`--source` selects `all` (default, the full cascade) or a single tier
(`bandcamp`, `itunes`, `discogs`). `--limit` caps lookups *per tier* per run;
`--delay` is an extra floor on top of each store's own rate limit (Bandcamp
~1/sec, iTunes ~20/min, Discogs 25/min — or 60/min with `discogs_token`, an
optional config setting that isn't otherwise required).

`--reresolve` is the recovery path for a tier that filled the hub with bad
links: it drops that tier's cached rows, blanks every `purchase_url` in the hub
pointing at that store, and resolves them fresh. Links the tier no longer finds
stay gone, so pair it with `--source`. To drop only the cache and leave the hub
alone, use `resolve cache clear --source <tier>`.

Each tier also stops early if its lookups fail repeatedly in a row, rather than
firing thousands more requests at a store that has started refusing them; the
remaining tiers still run.

A cold fill across a hub of ~7,165 albums takes roughly 6 hours: ~2h for
Bandcamp, ~3.2h for iTunes, 30–75 minutes for Discogs. Like the other resolve
commands it's incremental and resumable, so stopping after the Bandcamp tier
alone is a reasonable outcome — it's the cheapest pass and gives the links most
worth having.

### Generate a static site

Compile the hub into a navigable static site — one page per playlist embedding
[`<byom-player>`](https://github.com/lmorchard/byom-player), a tree that mirrors
your hub's subdirectories, a shared nav sidebar, Open Graph metadata, and an RSS
feed whose items list the opening tracks as links. This is the generator behind
sites like `mixtapes.lmorchard.com`.

```sh
# Build the site from the hub into ./dist
byom-sync site --input ./playlists --out ./dist --base-url https://example.com
```

Configure defaults under a `site:` block in `byom-sync.yaml` (only `base_url` is
required):

```yaml
site:
  base_url: https://mixtapes.example.com   # required: canonical URLs, OG tags, CNAME, feed
  title: mixtapes                          # site name in headers + <title>
  out_dir: ./dist
  provider: youtube                        # provider <byom-player> boots with
  providers: [youtube, spotify]            # optional: offered in the player's picker
  feed_track_limit: 20                     # tracks listed per RSS item (<=0 for all)
  feed_item_limit: 25                      # newest playlists in the feed (<=0 for all)
  # youtube_search_endpoint: https://...   # host attributes baked into each page
  # spotify_client_id: "..."
  # player_src: https://cdn.jsdelivr.net/npm/@lmorchard/byom-player@1.0.2/dist/byom-player.js
```

The output is a static tree ready for any static host: a landing page, one page
per playlist (each with a chrome-less `/embed/` variant for iframing into a
blog), `site-index.json`, `feed.xml`, and a `CNAME`. File playlists into
subdirectories in the hub and the site mirrors that structure as its nav tree.

Set `featured: true` on a playlist to promote it: featured playlists appear in a
`Featured` list at the top of the landing page and at the top of the sidebar nav
on every playlist and folder page, newest `date_updated` first. They also keep
their usual place in the year groups and folder listings, and the flag works at
any depth in the hub.

Playback provider and credentials are handled by the player's own settings panel;
the generator only sets the defaults above. See
[`.github/workflows/example-site-deploy.yml.example`](.github/workflows/example-site-deploy.yml.example)
for a GitHub Pages deploy workflow to copy into your content repo.

## The hub schema

Each playlist is one YAML file:

```yaml
spotify_id: "37i9dQZF1DXcBWIGoYBM5M"
title: "Playlist Name"
creator: "User Name"
description: "Optional playlist description"
date_created: "2026-07-07T00:00:00Z"   # first time byom-sync synced it
tracks:
  - title: "Track Title"
    artist: "Artist Name"
    album: "Album Name"
    isrc: "GBA098000010"
    spotify_id: "1a2b3c..."
    spotify_url: "https://open.spotify.com/track/1a2b3c..."
    duration_ms: 354000
    purchase_url: "https://artist.bandcamp.com/album/album-name"  # from `resolve purchase`
    added_at: "2026-05-29T04:02:20Z"    # when it was added to the playlist
    sync_state:
      spotify_present: true
      date_orphaned: ""
```

Tracks are matched across syncs by ISRC, falling back to a normalized
artist + title. Files are matched to remote playlists by `spotify_id`, so a file
can be renamed freely.

Playlists may be filed in subdirectories to any depth — `resolve`, `dates`,
`export`, and `site` all walk the hub recursively. Dotfiles and the hub-root
`art/` store are skipped.

`sync`, however, still matches and writes files at the hub root only: it
doesn't search subdirectories for an existing file to update. If you've moved
a playlist into a subdirectory, `sync` won't find it there and will write a
duplicate at the hub root instead.

Spotify's API doesn't expose a true playlist creation date, so `date_created`
records when `byom-sync` first synced the playlist. The per-track `added_at`
values carry the real curation history.

## Limitations

- Read-only: `byom-sync` never writes back to Spotify.
- Tracks removed from Spotify's catalog (empty title, no artists) are skipped at
  sync time, since they carry no usable metadata.

## Development

```sh
make setup      # install gofumpt + golangci-lint
make test       # go test ./...
make lint       # golangci-lint
make build      # build ./byom-sync
```

## License

MIT — see [LICENSE](LICENSE).
