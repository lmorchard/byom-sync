# Notes — byom-sync initial implementation

## Status: implementation complete, pending live Spotify verification

All 5 phases implemented, tested (`make test`/`make lint` green), and committed
one-per-phase on branch `initial-implementation`.

## What was built

- **Phase 1** — go-cli-builder scaffold (`--no-database --templates`), module
  `github.com/lmorchard/byom-sync`, core `Playlist`/`Track`/`SyncState` types
  with `Track.Key()` (ISRC → normalized artist+title fallback).
- **Phase 2** — `internal/playlist` store (per-file YAML matched on `spotify_id`,
  filename preserved across title changes, collision → id suffix) + archive/
  mirror merge engine (archive soft-orphans; mirror overwrites).
- **Phase 3** — `internal/export`: `Exporter` interface + `Run` dispatcher
  (file/dir input) + m3u8 / jspf / hugo spokes. Wired `export` command group.
  Verified end-to-end on a hand-authored YAML.
- **Phase 4** — `internal/auth`: JSON token cache (`$XDG_CONFIG_HOME/byom-sync/
  token.json`, 0o600) + PKCE (S256) authorization-code flow with local callback
  server, on `zmb3/spotify/v2/auth`. `auth` command + config defaults.
- **Phase 5** — `internal/spotifyfetch` (ParseID, paginated Fetch, convert) +
  `sync` command (config-list default, positional override, bounded errgroup,
  DateCreated preserved on re-sync, refreshed-token write-back).

## Key decisions / surprises

- Sibling `spotify-to-markdown` hand-rolls auth+API with no pagination/retry —
  NOT reused; went with `zmb3/spotify/v2` + `x/oauth2` PKCE as speced.
- **PKCE works cleanly** with zmb3 (`oauth2.GenerateVerifier` +
  `S256ChallengeOption`/`VerifierOption` through `AuthURL`/`Token`) — the
  spec's client-secret fallback was NOT needed.
- **Library-version gotcha:** `zmb3/spotify/v2` **v2.4.3** declares
  `FullTrack.ExternalIDs` as `map[string]string` (shadowing the embedded
  SimpleTrack's typed `TrackExternalIDs`). master has since changed it to the
  struct. ISRC read as `ft.ExternalIDs["isrc"]`. If we ever bump the dep, revisit
  `internal/spotifyfetch/fetch.go` convert().
- Scaffold Makefile has no `make check` target (only build/test/lint/format) —
  used those directly.
- m3u8 uses `path` (not `filepath`) so media paths stay `/`-separated for the
  target server regardless of host OS.

## Pending: manual verification (needs a real Spotify app + Premium account)

1. Register a Spotify app at https://developer.spotify.com/dashboard; add redirect
   URI `http://127.0.0.1:8888/callback`. Put `client_id` in `byom-sync.yaml`.
2. `byom-sync auth` → browser consent → expect success + `token.json` at 0o600.
3. `byom-sync sync <playlist-url> --dir ./playlists` → expect `<slug>.yaml`.
4. Sync a playlist with >100 tracks → confirm ALL tracks captured (pagination).
5. Remove a track upstream, re-sync: archive keeps it orphaned
   (`spotify_present:false` + `date_orphaned`); `--strategy mirror` drops it.
6. Re-sync preserves original `date_created`.
7. `export m3u8|jspf|hugo` the synced files end-to-end.

## Next

After manual verification passes → `/dev-session pr` (self-review, squash?/push,
open PR, Copilot review).
