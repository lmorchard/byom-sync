# Research — byom-sync initial implementation

Greenfield project. Research targets: (1) what the `go-cli-builder` scaffold
generates, and (2) how the sibling `spotify-to-markdown` tool solves Spotify
auth/API access, since it's the closest proven pattern in this ecosystem.

## 1. go-cli-builder scaffold (`--no-database --templates`)

Skill root: `/Users/lorchard/devel/lmorchard-agent-skills/go-cli-builder/`.
Scaffolder: `scripts/scaffold_project.py`.

- **Files created:** `main.go`, `go.mod`, `Makefile`, `.gitignore`,
  `<name>.yaml.example`, `cmd/{root,version,constants,init}.go`,
  `internal/config/config.go`, `internal/templates/{templates.go,default.md}`,
  `.github/workflows/{ci,release,rolling-release}.yml`. No `internal/database`.
- **Stack (go.mod):** `go 1.25`, `spf13/cobra v1.9.1`, `spf13/viper v1.20.1`,
  `sirupsen/logrus v1.9.3`. (`go-sqlite3` stripped by `--no-database`.)
- **Config system** (`cmd/root.go` `initConfig`): Viper YAML config named
  `<name>` searched in `.` (or explicit `--config`); `AutomaticEnv()`; persistent
  flags `--config/--verbose/-v/--debug/--log-json` bound to Viper. `GetConfig()`
  lazily builds `config.Config` from Viper getters. Logging via logrus,
  level from `debug`/`verbose`.
- **`--templates`** adds `cmd/init.go` (writes `<name>.yaml` + a template file,
  `--force`/`--template-file` flags) and `internal/templates/templates.go`
  (`//go:embed default.md` → `GetDefaultTemplate()`). `default.md` is a
  `text/template`.
- **Makefile targets:** `setup build run clean lint format test`. `build` uses
  `CGO_ENABLED=1` only with a DB (stripped under `--no-database`). LDFLAGS inject
  `main.version/commit/date`.
- **CI:** `ci.yml` (PR lint+`go test -race`), `release.yml` (tag `v*`, matrix
  linux/darwin amd64+arm64 + windows/amd64, checksums, gh-release),
  `rolling-release.yml` (push to main → `latest` prerelease).

## 2. spotify-to-markdown auth + API (KEY DIVERGENCE FROM SPEC)

Repo: `github.com/lmorchard/spotify-to-markdown`. Scaffolded from the same skill.

- **Does NOT use `zmb3/spotify/v2` or `golang.org/x/oauth2`.** Both the OAuth
  client (`internal/spotifyauth/`) and the API client (`internal/spotify/`) are
  hand-written. go.mod = cobra/viper/logrus/go-sqlite3 only.
- **OAuth flow** (`spotifyauth/auth.go` `RunInteractiveFlow`): authorization-code
  **+ PKCE (S256)** — no client secret. Random CSRF `state`. Local callback
  server at `127.0.0.1:{RedirectPort}/callback` (default port **8888**),
  `sync.Once`, 3s shutdown timeout. Opens browser. Token exchange POSTs to
  `https://accounts.spotify.com/api/token`.
- **Scopes** (`DefaultScopes`): `user-read-recently-played`,
  `user-read-currently-playing`, `user-top-read`, `user-library-read`.
- **Token storage:** SQLite `auth_tokens` table, single row `id=1`, upsert. NOT a
  JSON file (because this tool has a DB). `AccessToken(ctx)` does silent refresh:
  load → if expired, `refresh()` → carry over old refresh token if new one empty
  → save. `ErrNoToken` prompts "run `auth` first".
- **API client** (`spotify/client.go`): `spotify.New(tokenSource, httpClient)`;
  every request runs through `do()` which fetches access token, sets
  `Authorization: Bearer`. **NO retry/backoff.** Endpoints: `/me`,
  `/me/player/recently-played`. **NO pagination loop** — fetches one page of ≤50.
- **Config:** `SpotifyConfig{ClientID, RedirectPort, Scopes}`,
  `OutputConfig{File, Template}`. Env prefix `SPOTIFY_`. DB default path
  XDG_STATE_HOME-based (`defaultDatabasePath()`).
- **Command topology:** `auth`, `validate-auth`, `fetch`, `render`, `run`,
  `export`, `init`, `version`.

## 3. me-to-markdown `export` contract

`spotify-to-markdown`'s `export --since/--until -o` composes `fetch` (populate
archive) then windowed `render`, passing values by mutating Viper keys +
`renderCmd.Flags().Set(...)`. This is the orchestrator contract for
fetch→markdown tools. **byom-sync's `export` means something different** (compile
hub YAML → M3U8/JSPF/Hugo), so byom-sync is NOT a me-to-markdown participant and
its `export` verb collides only in name.

## Implications for byom-sync (to resolve in Q&A)

1. **Library choice:** spec mandates `zmb3/spotify/v2` + `x/oauth2` (real
   pagination/retry for playlists >100 tracks); sibling proves hand-rolled PKCE
   works but has no pagination and no retry. Genuine fork.
2. **No DB → token storage** must be a file (spec says
   `~/.config/byom-sync/config.json`), unlike sibling's SQLite row.
3. **Playlist selection** mechanism is unspecified — sync needs to know WHICH
   playlists to pull.
4. **Schema gap:** `Playlist` struct has no Spotify playlist ID field, but
   archive/mirror re-sync needs to map local YAML file → remote playlist.
5. **`--dir` layout** (one file per playlist? filename derivation?) unspecified.
6. **`.flac` hardcoded** in M3U8 path template — should be configurable.
