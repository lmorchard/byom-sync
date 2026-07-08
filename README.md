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

`--input` may be a single YAML file or a directory. When it's a directory,
`--out` is treated as an output directory and each playlist is written as
`<name>.<ext>`.

M3U8 track paths are built as `{lib-prefix}/{Artist}/{Album}/{Title}.{ext}` and
emitted as-is; the files aren't checked against the filesystem.

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
    added_at: "2026-05-29T04:02:20Z"    # when it was added to the playlist
    sync_state:
      spotify_present: true
      date_orphaned: ""
```

Tracks are matched across syncs by ISRC, falling back to a normalized
artist + title. Files are matched to remote playlists by `spotify_id`, so a file
can be renamed freely.

Spotify's API doesn't expose a true playlist creation date, so `date_created`
records when `byom-sync` first synced the playlist. The per-track `added_at`
values carry the real curation history.

## Limitations

- Read-only: `byom-sync` never writes back to Spotify.
- Tracks removed from Spotify's catalog can appear as empty-title entries; M3U8
  skips them (no usable path), while JSPF and Markdown include them.
- Exporters don't yet surface `added_at` or `spotify_url`.

## Development

```sh
make setup      # install gofumpt + golangci-lint
make test       # go test ./...
make lint       # golangci-lint
make build      # build ./byom-sync
```

## License

MIT — see [LICENSE](LICENSE).
