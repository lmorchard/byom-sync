# Notes — byom-sync initial implementation

## Status: implementation complete, live-verified against Spotify

All 5 phases implemented, tested (`make test`/`make lint` green), committed
one-per-phase on branch `initial-implementation`, and verified end-to-end
against a real Spotify account (see "Live verification" below).

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

## Live verification (done against a real Spotify account)

- ✅ `auth` PKCE flow → `token.json` written at `0o600`.
- ✅ `sync` "Today's Top Hits" (50 tracks) → correct schema/ISRC/duration.
- ✅ Re-sync idempotent; `date_created` preserved.
- ✅ Archive orphaning (`spotify_present:false` + `date_orphaned` RFC3339) via a
  synthetic local-only track; `--strategy mirror` drops it.
- ✅ All three exporters correct on live data.
- ✅ Pagination: synced a 153-track playlist, all pages pulled via NextPage, no
  drops/double-fetches (one repeated ISRC was a genuine duplicate playlist entry).
- ⏳ Token silent-refresh NOT observed (token < 1h old); low risk — oauth2
  TokenSource handles it, cached-token re-runs already succeed.

### Setup friction (Spotify-side, not code)
Hit intermittent `redirect_uri: Not matching configuration` and `server_error`
during auth setup. Root causes: (1) Spotify dashboard redirect-URI changes take a
few minutes to propagate to the authorize endpoint; (2) dev-mode apps require the
authorizing account in the **User Management** allowlist (missing user → generic
`server_error` after consent). Fixed by adding the account + waiting for
propagation. No byom-sync code change was needed.

### Test artifacts left in ./playlists/ (gitignored)
`today-s-top-hits.yaml`, `bleep-bloop-bop-synthpop.yaml` — from verification.
`*.yaml` is gitignored so they won't be committed; delete if unwanted.

## Next

After manual verification passes → `/dev-session pr` (self-review, squash?/push,
open PR, Copilot review).
