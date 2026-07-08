# byom-sync Implementation Plan

**Goal:** Build the `byom-sync` CLI — Spotify playlists → local YAML hub → M3U8/JSPF/Hugo spokes.

**Approach:** Scaffold from go-cli-builder (`--no-database --templates`). Pure data layer (hub + merge engine) and exporters first so slices 1–3 are valuable/testable without Spotify creds; then PKCE auth and the sync command layer on top. `zmb3/spotify/v2` for real pagination + 429 retry.

**Tech stack:** Go 1.25, Cobra/Viper/logrus (scaffold), `github.com/zmb3/spotify/v2` + `github.com/zmb3/spotify/v2/auth`, `golang.org/x/oauth2`, `golang.org/x/sync/errgroup`, `gopkg.in/yaml.v3`.

---

## Phase 1: Scaffold, module, core types

Stand up the project skeleton and the shared `Playlist`/`Track` types. Infrastructure scaffolding — **TDD opt-out** except for one round-trip type test.

**Files:**
- Run: `python /Users/lorchard/devel/lmorchard-agent-skills/go-cli-builder/scripts/scaffold_project.py byom-sync --no-database --templates` (scaffold into a temp dir, then move generated files into the repo root without clobbering `docs/` or `.git`).
- Modify: `go.mod` — set module `github.com/lmorchard/byom-sync`; `go get github.com/zmb3/spotify/v2 golang.org/x/oauth2 golang.org/x/sync gopkg.in/yaml.v3`.
- Create: `internal/playlist/types.go` — the hub structs.
- Test: `internal/playlist/types_test.go` — YAML round-trip.
- Modify: `.gitignore` — add `/byom-sync` binary, `token.json`, `*.db` already present.
- Modify: `byom-sync.yaml.example` — document `client_id`, `redirect_port`, `scopes`, `playlists`, `dir`.

**Key changes:**
```go
// internal/playlist/types.go
package playlist

import "time"

type Playlist struct {
	SpotifyID   string    `yaml:"spotify_id"`
	Title       string    `yaml:"title"`
	Creator     string    `yaml:"creator"`
	DateCreated time.Time `yaml:"date_created"`
	Tracks      []Track   `yaml:"tracks"`
}

type Track struct {
	Title      string    `yaml:"title"`
	Artist     string    `yaml:"artist"`
	Album      string    `yaml:"album,omitempty"`
	ISRC       string    `yaml:"isrc,omitempty"`
	DurationMS int       `yaml:"duration_ms,omitempty"`
	SyncState  SyncState `yaml:"sync_state"`
}

type SyncState struct {
	SpotifyPresent bool   `yaml:"spotify_present"`
	DateOrphaned   string `yaml:"date_orphaned,omitempty"`
}

// Key returns the merge identity: ISRC if present, else normalized "artist\ttitle".
func (t Track) Key() string {
	if t.ISRC != "" {
		return "isrc:" + t.ISRC
	}
	return "at:" + normalize(t.Artist) + "\t" + normalize(t.Title)
}

func normalize(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
```

**Verification — automated:**
- [x] `make build` succeeds (binary produced)
- [x] `make test` passes (`TestPlaylist_YAMLRoundTrip`)
- [x] `make lint` passes
- [x] `./byom-sync version` prints version

**Verification — manual:**
- [x] `docs/` and git history intact after moving scaffold files
- [x] `byom-sync.yaml.example` documents all five config keys

---

## Phase 2: Playlist hub — store + merge engine

Load/save one YAML file per playlist (matched on `spotify_id`) and the `archive`/`mirror` merge functions. Pure logic, no network — **TDD**.

**Files:**
- Create: `internal/playlist/store.go` — dir load/save, slug + spotify_id matching.
- Create: `internal/playlist/merge.go` — merge strategies.
- Test: `internal/playlist/store_test.go`, `internal/playlist/merge_test.go`.

**Key changes:**
```go
// store.go
// Load reads every *.yaml in dir into a slice.
func Load(dir string) ([]Playlist, error)
// LoadByID returns the playlist with matching SpotifyID and its filepath, ok=false if none.
func FindFileByID(dir, spotifyID string) (path string, ok bool, err error)
// Save writes p to dir. If an existing file has p.SpotifyID, overwrite it (preserve filename);
// else create "<slug(p.Title)>.yaml", appending "-<first6(SpotifyID)>" on collision.
func Save(dir string, p Playlist) (path string, err error)
func Slug(title string) string // lowercased, [a-z0-9]+ joined by "-", trimmed

// merge.go
type Strategy string
const ( Archive Strategy = "archive"; Mirror Strategy = "mirror" )

// Merge combines a locally-stored playlist with a freshly-fetched remote one.
//   Archive: union by Track.Key(). Remote tracks upsert (spotify_present=true, clear orphan).
//            Local tracks absent from remote keep their data but get spotify_present=false and,
//            if not already orphaned, date_orphaned=now (RFC3339 UTC). Order: remote order first,
//            then orphaned local tracks appended in their prior order.
//   Mirror:  return remote exactly (all spotify_present=true), discarding local-only tracks.
// Metadata (Title, Creator, DateCreated) always taken from remote.
func Merge(local, remote Playlist, strat Strategy, now time.Time) Playlist
```

```go
// merge.go archive core
func Merge(local, remote Playlist, strat Strategy, now time.Time) Playlist {
	out := remote // copies Title/Creator/DateCreated/SpotifyID
	if strat == Mirror {
		for i := range out.Tracks { out.Tracks[i].SyncState = SyncState{SpotifyPresent: true} }
		return out
	}
	remoteKeys := map[string]bool{}
	for i := range out.Tracks {
		out.Tracks[i].SyncState = SyncState{SpotifyPresent: true}
		remoteKeys[out.Tracks[i].Key()] = true
	}
	for _, lt := range local.Tracks {
		if remoteKeys[lt.Key()] { continue }
		lt.SyncState.SpotifyPresent = false
		if lt.SyncState.DateOrphaned == "" {
			lt.SyncState.DateOrphaned = now.UTC().Format(time.RFC3339)
		}
		out.Tracks = append(out.Tracks, lt)
	}
	return out
}
```

**Verification — automated:**
- [x] `make test` passes: archive adds new tracks, orphans missing local tracks (sets `spotify_present=false` + `date_orphaned`), preserves an already-set `date_orphaned`
- [x] `make test` passes: mirror discards local-only tracks, all `spotify_present=true`
- [x] `make test` passes: `Save` matches existing file by `spotify_id` (filename preserved even when title changed); collision appends id suffix; `Load` round-trips
- [x] `make lint` passes

**Verification — manual:**
- [ ] Eyeball a saved YAML file — field order/readability matches the spec schema (deferred to Phase 3 manual step, where a file gets hand-authored/exported)

---

## Phase 3: Exporters — m3u8, jspf, hugo

`Exporter` interface + three spokes + the `export` command group. Independently valuable: exports hand-authored YAML with no Spotify. **TDD** with output-assertion tests.

**Files:**
- Create: `internal/export/export.go` — interface + shared input/dir handling.
- Create: `internal/export/m3u8.go`, `internal/export/jspf.go`, `internal/export/hugo.go`.
- Create: `cmd/export.go` — `export` parent + `m3u8`/`jspf`/`hugo` subcommands.
- Modify: `internal/templates/default.md` → repurpose as the Hugo template; `templates.go` already embeds it.
- Test: `internal/export/m3u8_test.go`, `jspf_test.go`, `hugo_test.go`.

**Key changes:**
```go
// export.go
type Exporter interface {
	Export(p playlist.Playlist, outputPath string, cfg map[string]string) error
}
// Run resolves --input (file or dir) and --out, dispatching each playlist to e.
// File input → single out file. Dir input → out treated as dir; each writes
// "<input-basename>.<ext>" (ext from the exporter).
func Run(e Exporter, ext, input, out string, cfg map[string]string) error
```
- **m3u8:** `#EXTM3U`, then per track `#EXTINF:<sec>,<Artist> - <Title>` + line `filepath.Join(prefix, Artist, Album, Title) + "." + ext`. `ext` from `cfg["ext"]` default `flac`; `prefix` from `cfg["lib_prefix"]`. Only tracks with `SpotifyPresent` OR `--include-orphans`? → **no**, export all tracks (orphans included) — spec says m3u8 is a compile, not a filter. Skip tracks with empty Title.
- **jspf:** marshal `{"playlist":{"title","creator","date","track":[{"title","creator","album","duration":round(ms/1000),"identifier":["urn:isrc:<isrc>"]}]}}`; omit `identifier` when ISRC empty.
- **hugo:** `text/template` executing frontmatter (`title`, `creator`, `date`) + a Markdown table (`| # | Title | Artist | Album |`).

```go
// jspf.go shape
type jspfDoc struct{ Playlist jspfPlaylist `json:"playlist"` }
type jspfPlaylist struct {
	Title, Creator, Date string          `json:",omitempty"`
	Track                []jspfTrack     `json:"track"`
}
type jspfTrack struct {
	Title, Creator, Album string   `json:",omitempty"`
	Duration              int      `json:"duration,omitempty"`
	Identifier            []string `json:"identifier,omitempty"`
}
```

**Verification — automated:**
- [x] `make test`: m3u8 output has `#EXTM3U` header, one `#EXTINF` + path per track, path = `<prefix>/<Artist>/<Album>/<Title>.<ext>`, `--ext` override respected
- [x] `make test`: jspf is valid JSON, `identifier` = `["urn:isrc:<isrc>"]`, duration in seconds, `identifier` omitted when ISRC empty
- [x] `make test`: hugo output has YAML frontmatter + a tracklist table row per track
- [x] `make test`: dir-mode input writes one output file per input YAML
- [x] `make lint` passes

**Verification — manual:**
- [x] Hand-author a `sample.yaml`, run all three `export` subcommands, eyeball each output
- [x] `byom-sync export m3u8 --input sample.yaml --out out.m3u8 --lib-prefix /mnt/nas/music` produces expected paths

---

## Phase 4: Auth — PKCE OAuth + token cache

`byom-sync auth` runs the PKCE authorization-code flow and caches tokens; a shared helper builds an authenticated `*spotify.Client` with silent refresh. Network/browser — **manual verification** (unit-test the token store).

**Files:**
- Create: `internal/auth/store.go` — token JSON load/save at XDG path.
- Create: `internal/auth/auth.go` — authenticator build, PKCE flow, callback server, `Client(ctx)`.
- Create: `cmd/auth.go` — the `auth` command.
- Modify: `cmd/root.go` — Viper defaults for `redirect_port` (8888), `scopes`, `client_id`.
- Test: `internal/auth/store_test.go` — save/load round-trip, `configDir` honors `XDG_CONFIG_HOME`.

**Key changes:**
```go
// store.go
func tokenPath() string // $XDG_CONFIG_HOME/byom-sync/token.json, fallback ~/.config/byom-sync/token.json
func SaveToken(t *oauth2.Token) error   // 0o600, mkdir 0o700
func LoadToken() (*oauth2.Token, error) // ErrNoToken if absent

// auth.go
func newAuthenticator(clientID string, port int) *spotifyauth.Authenticator // WithClientID/WithRedirectURL/WithScopes(ScopePlaylistReadPrivate, ScopePlaylistReadCollaborative)
func RedirectURL(port int) string // http://127.0.0.1:<port>/callback

// RunInteractiveFlow: generate verifier := oauth2.GenerateVerifier(); state := random;
//   url := auth.AuthURL(state, oauth2.S256ChallengeOption(verifier)); open browser;
//   callback server on 127.0.0.1:port validates state (sync.Once), then
//   tok, err := auth.Token(ctx, state, r, oauth2.VerifierOption(verifier)); SaveToken(tok).
func RunInteractiveFlow(ctx context.Context, clientID string, port int) error

// Client builds an authenticated client with silent refresh via oauth2 TokenSource,
// persisting refreshed tokens back to disk.
func Client(ctx context.Context, clientID string, port int) (*spotify.Client, error)
```
```go
// auth.go Client — build authenticated client with silent refresh + persist.
// authr.Client returns an *http.Client backed by a self-refreshing oauth2
// TokenSource (it refreshes transparently on the first expired request).
// spotify.Client exposes the current token via (*Client).Token(); after use we
// compare it to the loaded token and re-save if the access token changed.
func Client(ctx context.Context, clientID string, port int) (*spotify.Client, *oauth2.Token, error) {
	tok, err := LoadToken()
	if err != nil { return nil, nil, err } // ErrNoToken → "run `byom-sync auth` first"
	authr := newAuthenticator(clientID, port)
	client := spotify.New(authr.Client(ctx, tok), spotify.WithRetry(true))
	return client, tok, nil // caller passes tok to PersistRefreshed when done
}

// PersistRefreshed re-saves the client's current token if it was refreshed.
// Call after sync work completes (the transport refreshes lazily on request).
func PersistRefreshed(client *spotify.Client, prev *oauth2.Token) {
	cur, err := client.Token()
	if err == nil && cur != nil && cur.AccessToken != prev.AccessToken {
		_ = SaveToken(cur)
	}
}
```

**Verification — automated:**
- [x] `make test`: token store save/load round-trip; `tokenPath` honors `XDG_CONFIG_HOME`; `LoadToken` returns `ErrNoToken` when absent
- [x] `make lint` passes
- [x] `byom-sync auth` with no `client_id` fails gracefully with a clear message (verified)

**Verification — manual (batched with Phase 5, needs a real Spotify app):**
- [ ] Register a Spotify app (redirect URI `http://127.0.0.1:8888/callback`), set `client_id` in config
- [ ] `byom-sync auth` opens browser, completes, prints success; `token.json` written at `0o600`
- [ ] Re-running a command after token expiry silently refreshes (no re-auth prompt)

---

## Phase 5: Sync — fetch, convert, merge, write

`byom-sync sync` pulls playlists (config list or positional args), converts, merges via Phase 2, writes files. **Unit-test** conversion + selection; **manual** end-to-end.

**Files:**
- Create: `internal/spotifyfetch/fetch.go` — fetch a playlist + all items (pagination), convert to `playlist.Playlist`.
- Create: `cmd/sync.go` — the `sync` command.
- Test: `internal/spotifyfetch/convert_test.go` — `FullTrack`→`Track`; `cmd/sync_test.go` — playlist-arg selection precedence.

**Key changes:**
```go
// fetch.go
// parseID extracts a Spotify playlist ID from a raw ID, spotify: URI, or open.spotify.com URL.
func parseID(raw string) (spotify.ID, error)

// convert maps a fetched FullTrack to our Track (skips nil tracks / episodes).
func convert(ft *spotify.FullTrack) playlist.Track {
	return playlist.Track{
		Title:      ft.Name,
		Artist:     joinArtists(ft.Artists), // ", "-joined SimpleArtist.Name
		Album:      ft.Album.Name,
		ISRC:       ft.ExternalIDs.ISRC,
		DurationMS: int(ft.Duration),
		SyncState:  playlist.SyncState{SpotifyPresent: true},
	}
}

// Fetch pulls a playlist and every track page.
func Fetch(ctx context.Context, c *spotify.Client, id spotify.ID) (playlist.Playlist, error) {
	fp, err := c.GetPlaylist(ctx, id)
	if err != nil { return playlist.Playlist{}, err }
	out := playlist.Playlist{
		SpotifyID: id.String(), Title: fp.Name, Creator: fp.Owner.DisplayName,
		DateCreated: time.Now().UTC(), // Spotify has no created date; set on first sync only (see sync.go)
	}
	page, err := c.GetPlaylistItems(ctx, id)
	for err == nil {
		for _, it := range page.Items {
			if it.Track.Track == nil { continue } // episode / unavailable
			out.Tracks = append(out.Tracks, convert(it.Track.Track))
		}
		err = c.NextPage(ctx, page)
	}
	if !errors.Is(err, spotify.ErrNoMorePages) { return out, err }
	return out, nil
}
```
```go
// sync.go selection: positional args replace the config list when present.
targets := args
if len(targets) == 0 { targets = viper.GetStringSlice("playlists") }
// client, tok, err := auth.Client(ctx, clientID, port)  // errors → "run `byom-sync auth` first"
// For each target: parseID → Fetch remote → if local file with same SpotifyID exists,
// Load it and carry its DateCreated forward; Merge(local, remote, strategy, now); Save.
// Bounded concurrency: errgroup with SetLimit(4); serialize the per-file Save (mutex).
// After the group finishes: auth.PersistRefreshed(client, tok).
```
- `sync` flags: `--dir` (default from config `dir`, else `./playlists`), `--strategy` (default `archive`).
- `DateCreated`: preserve the local file's value on re-sync; only stamp `time.Now().UTC()` for a brand-new playlist file.

**Verification — automated:**
- [x] `make test`: `convert` maps name/artists(joined)/album/ISRC/duration correctly; nil track skipped
- [x] `make test`: `parseID` handles raw ID, `spotify:playlist:<id>`, and `https://open.spotify.com/playlist/<id>?si=...`
- [x] `make test`: sync target selection — positional args override config `playlists`; empty args use config
- [x] `make lint` passes
- [x] `sync` error paths verified: bad strategy, no token (→ "run auth first"), no targets

**Verification — manual (needs a real Spotify app + account):**
- [ ] `byom-sync sync <real-playlist-url> --dir ./playlists` creates `<slug>.yaml` with correct tracks
- [ ] A playlist >100 tracks pulls ALL tracks (pagination works)
- [ ] Re-sync after removing a track upstream: archive keeps it with `spotify_present=false` + `date_orphaned`; mirror drops it
- [ ] Re-sync preserves original `date_created`
- [ ] End-to-end: `sync` a playlist, then `export m3u8/jspf/hugo` the result

---

## Cross-phase notes

- **One commit per phase**, message `Phase N: <name>`.
- **PKCE fallback:** if `oauth2.S256ChallengeOption`/`VerifierOption` don't wire cleanly through zmb3's `AuthURL`/`Token` (Phase 4), fall back to client-id+secret (`WithClientSecret`) — spec-sanctioned, add `client_secret` to config. Surface this before pivoting.
- **Numeric type:** `spotify.Numeric` is `int` — `int(ft.Duration)` is safe.
