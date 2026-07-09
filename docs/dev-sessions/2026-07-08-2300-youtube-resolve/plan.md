# byom-sync YouTube Resolution — Implementation Plan

> **For agentic workers:** implement task-by-task; each task is TDD (failing test → minimal code → green → commit). Steps use checkbox (`- [ ]`) syntax.

**Goal:** Add `byom-sync resolve youtube`, which searches the YouTube Data API for tracks missing a video ID, stores the ID in the hub YAML, and emits it into the JSPF `resolved.youtube` extension on export.

**Architecture:** A `youtube.Searcher` seam (HTTP impl + fake for tests) and a `youtube.Resolve` function that mutates hub playlists in place; a thin `cmd/resolve.go` orchestrates load → resolve → save with a per-run search budget and quota stop. Export gains an extension block.

**Tech Stack:** Go 1.25, Cobra, Viper, `net/http` (no new deps), `gopkg.in/yaml.v3`, `encoding/json`. Lint: golangci-lint v2 (errcheck strict — `_ =` intentional ignores). Format: gofumpt.

## Global Constraints

- **No new dependencies** — YouTube search uses `net/http` against the Data API v3 REST endpoint.
- **errcheck strict**: `_ =` for intentionally-ignored returns.
- Format with `gofumpt` (via `make format`); verify with `make lint && make test && make build`.
- Namespace constant `https://github.com/lmorchard/byom-sync` for the JSPF extension.
- No real API calls in tests (`httptest` + fake `Searcher`).
- Commit trailer: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.

## File structure

- Create: `internal/youtube/youtube.go` (`Searcher`, `ErrQuotaExceeded`, `HTTPSearcher`), `internal/youtube/resolve.go` (`Resolve`), `internal/youtube/youtube_test.go`, `internal/youtube/resolve_test.go`.
- Modify: `internal/playlist/types.go` (`Track.YouTubeID`), `internal/playlist/store.go` (`SaveFile`) + `store_test.go` (or `types_test.go`).
- Create: `cmd/resolve.go`. Modify: `cmd/root.go` (`youtube_api_key` default).
- Modify: `internal/export/jspf.go` (extension) + `internal/export/export_test.go`.
- Modify: `byom-sync.yaml.example` (document `youtube_api_key`).

---

### Task 1: Hub field + `SaveFile`

**Files:** `internal/playlist/types.go`, `internal/playlist/store.go`, `internal/playlist/store_test.go`.

- [ ] **Step 1: Failing test** — append to `internal/playlist/store_test.go` (create if absent, `package playlist`):

```go
func TestSaveFileRoundTripsYouTubeID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pl.yaml")
	p := Playlist{
		SpotifyID: "PID", Title: "T",
		Tracks: []Track{{Title: "S", Artist: "A", YouTubeID: "vid123"}},
	}
	if err := SaveFile(path, p); err != nil {
		t.Fatalf("SaveFile: %v", err)
	}
	got, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if got.Tracks[0].YouTubeID != "vid123" {
		t.Errorf("youtube_id did not round-trip: %q", got.Tracks[0].YouTubeID)
	}
}
```

Add imports as needed (`path/filepath`, `testing`).

- [ ] **Step 2: Run — expect FAIL** (`YouTubeID` field and `SaveFile` undefined):

```
cd internal/playlist && go test ./... 2>&1 | tail -20
```

- [ ] **Step 3: Implement.** In `types.go`, add to `Track` (after `DurationMS`, before `AddedAt` is fine):

```go
	YouTubeID  string    `yaml:"youtube_id,omitempty"`
```

In `store.go`, add below `LoadFile`:

```go
// SaveFile writes a single playlist to an exact path (used to update a hub file
// in place, preserving its filename).
func SaveFile(path string, p Playlist) error {
	data, err := yaml.Marshal(p)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
```

- [ ] **Step 4: Run — expect PASS.** `cd internal/playlist && go test ./...`

- [ ] **Step 5: Commit** — `feat(hub): add Track.YouTubeID and playlist.SaveFile`

---

### Task 2: `youtube.Searcher` + `HTTPSearcher`

**Files:** `internal/youtube/youtube.go`, `internal/youtube/youtube_test.go`.

**Produces:** `Searcher` iface, `ErrQuotaExceeded`, `HTTPSearcher`.

- [ ] **Step 1: Failing test** — `internal/youtube/youtube_test.go`:

```go
package youtube

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestSearcher(h http.HandlerFunc) (HTTPSearcher, *httptest.Server) {
	srv := httptest.NewServer(h)
	return HTTPSearcher{APIKey: "KEY", Client: srv.Client(), baseURL: srv.URL}, srv
}

func TestHTTPSearcherReturnsTopVideoID(t *testing.T) {
	var gotQuery, gotKey string
	s, srv := newTestSearcher(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("q")
		gotKey = r.URL.Query().Get("key")
		_, _ = w.Write([]byte(`{"items":[{"id":{"videoId":"vid42"}}]}`))
	})
	defer srv.Close()

	id, err := s.Search(context.Background(), "Kavinsky Nightcall")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if id != "vid42" {
		t.Errorf("id = %q, want vid42", id)
	}
	if gotQuery != "Kavinsky Nightcall" || gotKey != "KEY" {
		t.Errorf("q=%q key=%q", gotQuery, gotKey)
	}
}

func TestHTTPSearcherEmptyResult(t *testing.T) {
	s, srv := newTestSearcher(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"items":[]}`))
	})
	defer srv.Close()
	id, err := s.Search(context.Background(), "no match")
	if err != nil || id != "" {
		t.Errorf("want empty/no-error, got id=%q err=%v", id, err)
	}
}

func TestHTTPSearcherQuotaExceeded(t *testing.T) {
	s, srv := newTestSearcher(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"errors":[{"reason":"quotaExceeded"}]}}`))
	})
	defer srv.Close()
	_, err := s.Search(context.Background(), "x")
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("want ErrQuotaExceeded, got %v", err)
	}
}

func TestHTTPSearcherOtherErrorStatus(t *testing.T) {
	s, srv := newTestSearcher(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer srv.Close()
	_, err := s.Search(context.Background(), "x")
	if err == nil || errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("want a non-quota error, got %v", err)
	}
}
```

- [ ] **Step 2: Run — expect FAIL** (package/types undefined): `cd internal/youtube && go test ./...`

- [ ] **Step 3: Implement** `internal/youtube/youtube.go`:

```go
// Package youtube resolves tracks to YouTube video IDs via the Data API and
// fills them into the hub. Search costs ~100 quota units each (~100/day on the
// default budget), so resolution is incremental and IDs are stored in the hub.
package youtube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// ErrQuotaExceeded is returned when the Data API reports the daily quota is
// spent, so the caller can stop and persist progress.
var ErrQuotaExceeded = errors.New("youtube: quota exceeded")

// Searcher resolves a query to the top matching video ID, or "" when the API
// answered with no result (a clean miss).
type Searcher interface {
	Search(ctx context.Context, query string) (videoID string, err error)
}

const defaultBaseURL = "https://www.googleapis.com/youtube/v3/search"

// HTTPSearcher calls the YouTube Data API v3 search.list endpoint.
type HTTPSearcher struct {
	APIKey  string
	Client  *http.Client
	baseURL string // overridable in tests; defaults to the Data API endpoint
}

func (h HTTPSearcher) Search(ctx context.Context, query string) (string, error) {
	client := h.Client
	if client == nil {
		client = http.DefaultClient
	}
	base := h.baseURL
	if base == "" {
		base = defaultBaseURL
	}

	q := url.Values{}
	q.Set("part", "snippet")
	q.Set("type", "video")
	q.Set("maxResults", "1")
	q.Set("q", query)
	q.Set("key", h.APIKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusForbidden && isQuota(body) {
		return "", ErrQuotaExceeded
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("youtube search: HTTP %d", resp.StatusCode)
	}

	var parsed struct {
		Items []struct {
			ID struct {
				VideoID string `json:"videoId"`
			} `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("youtube search: decode: %w", err)
	}
	if len(parsed.Items) == 0 {
		return "", nil
	}
	return parsed.Items[0].ID.VideoID, nil
}

func isQuota(body []byte) bool {
	var e struct {
		Error struct {
			Errors []struct {
				Reason string `json:"reason"`
			} `json:"errors"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &e); err != nil {
		return false
	}
	for _, x := range e.Error.Errors {
		if x.Reason == "quotaExceeded" || x.Reason == "dailyLimitExceeded" {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run — expect PASS.** `cd internal/youtube && go test ./...`

- [ ] **Step 5: Commit** — `feat(youtube): HTTP Searcher with quota detection`

---

### Task 3: `youtube.Resolve` over hub playlists

**Files:** `internal/youtube/resolve.go`, `internal/youtube/resolve_test.go`.

**Consumes:** `Searcher`, `ErrQuotaExceeded`, `playlist.Track`/`Playlist`.
**Produces:** `func Resolve(ctx, s Searcher, p *playlist.Playlist, budget *int) (resolved int, quotaHit bool, err error)`.

- [ ] **Step 1: Failing test** — `internal/youtube/resolve_test.go`:

```go
package youtube

import (
	"context"
	"testing"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

// fakeSearcher returns queued results/errors in order.
type fakeSearcher struct {
	ids   []string
	errs  []error
	calls int
}

func (f *fakeSearcher) Search(_ context.Context, _ string) (string, error) {
	i := f.calls
	f.calls++
	var id string
	if i < len(f.ids) {
		id = f.ids[i]
	}
	var err error
	if i < len(f.errs) {
		err = f.errs[i]
	}
	return id, err
}

func pl(ids ...string) *playlist.Playlist {
	p := &playlist.Playlist{}
	for i := range ids {
		p.Tracks = append(p.Tracks, playlist.Track{Title: "t", Artist: "a", YouTubeID: ids[i]})
	}
	return p
}

func TestResolveOnlyFillsEmptyIDs(t *testing.T) {
	p := pl("", "already", "")
	f := &fakeSearcher{ids: []string{"v1", "v2"}}
	n, quota, err := Resolve(context.Background(), f, p, nil)
	if err != nil || quota {
		t.Fatalf("n=%d quota=%v err=%v", n, quota, err)
	}
	if n != 2 || f.calls != 2 {
		t.Errorf("resolved=%d calls=%d, want 2/2", n, f.calls)
	}
	if p.Tracks[0].YouTubeID != "v1" || p.Tracks[1].YouTubeID != "already" || p.Tracks[2].YouTubeID != "v2" {
		t.Errorf("ids = %q", []string{p.Tracks[0].YouTubeID, p.Tracks[1].YouTubeID, p.Tracks[2].YouTubeID})
	}
}

func TestResolveRespectsBudget(t *testing.T) {
	p := pl("", "", "")
	f := &fakeSearcher{ids: []string{"v1", "v2", "v3"}}
	budget := 1
	n, _, _ := Resolve(context.Background(), f, p, &budget)
	if n != 1 || f.calls != 1 {
		t.Errorf("resolved=%d calls=%d, want 1/1", n, f.calls)
	}
	if budget != 0 {
		t.Errorf("budget=%d, want 0", budget)
	}
}

func TestResolveStopsOnQuota(t *testing.T) {
	p := pl("", "", "")
	f := &fakeSearcher{ids: []string{"v1", "", ""}, errs: []error{nil, ErrQuotaExceeded, nil}}
	n, quota, err := Resolve(context.Background(), f, p, nil)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if !quota || n != 1 || f.calls != 2 {
		t.Errorf("quota=%v resolved=%d calls=%d, want true/1/2", quota, n, f.calls)
	}
	if p.Tracks[2].YouTubeID != "" {
		t.Errorf("track after quota should stay empty, got %q", p.Tracks[2].YouTubeID)
	}
}

func TestResolveSkipsSearchErrorAndContinues(t *testing.T) {
	p := pl("", "")
	f := &fakeSearcher{ids: []string{"", "v2"}, errs: []error{errSome(), nil}}
	n, quota, err := Resolve(context.Background(), f, p, nil)
	if err != nil || quota {
		t.Fatalf("n=%d quota=%v err=%v", n, quota, err)
	}
	if n != 1 || p.Tracks[0].YouTubeID != "" || p.Tracks[1].YouTubeID != "v2" {
		t.Errorf("resolved=%d ids=%q", n, []string{p.Tracks[0].YouTubeID, p.Tracks[1].YouTubeID})
	}
}

func errSome() error { return context.DeadlineExceeded }
```

- [ ] **Step 2: Run — expect FAIL** (`Resolve` undefined): `cd internal/youtube && go test ./...`

- [ ] **Step 3: Implement** `internal/youtube/resolve.go`:

```go
package youtube

import (
	"context"
	"errors"
	"strings"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

// Resolve searches for a YouTube video ID for every track in p that lacks one,
// mutating the tracks in place. budget (if non-nil) caps the number of searches
// this call performs. It stops early — returning quotaHit=true — when the API
// reports quota exhaustion, leaving already-resolved IDs in place for the caller
// to persist. A per-track search error is logged by the caller's Searcher (or
// ignored here) and skipped; it does not abort the run.
func Resolve(ctx context.Context, s Searcher, p *playlist.Playlist, budget *int) (resolved int, quotaHit bool, err error) {
	for i := range p.Tracks {
		t := &p.Tracks[i]
		if t.YouTubeID != "" {
			continue
		}
		if budget != nil && *budget <= 0 {
			return resolved, false, nil
		}
		query := strings.TrimSpace(t.Artist + " " + t.Title)
		id, searchErr := s.Search(ctx, query)
		if budget != nil {
			*budget--
		}
		if errors.Is(searchErr, ErrQuotaExceeded) {
			return resolved, true, nil
		}
		if searchErr != nil {
			continue // transient/other error — skip this track, keep going
		}
		if id != "" {
			t.YouTubeID = id
			resolved++
		}
	}
	return resolved, false, nil
}
```

- [ ] **Step 4: Run — expect PASS.** `cd internal/youtube && go test ./...`

- [ ] **Step 5: Commit** — `feat(youtube): Resolve fills missing ids with budget + quota stop`

---

### Task 4: `cmd/resolve.go` + config key

**Files:** `cmd/resolve.go` (new), `cmd/root.go` (config default), `byom-sync.yaml.example`.

**Note:** command wiring is thin (logic lives in `youtube.Resolve`, tested in Task 3); no new cmd unit test is required. Verify via `make build` and a manual `--help`.

- [ ] **Step 1: Implement** `cmd/resolve.go`:

```go
package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/lmorchard/byom-sync/internal/youtube"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	resolveInput string
	resolveLimit int
)

var resolveCmd = &cobra.Command{
	Use:   "resolve",
	Short: "Resolve external IDs for hub tracks (e.g. YouTube video IDs)",
}

var resolveYouTubeCmd = &cobra.Command{
	Use:   "youtube",
	Short: "Search YouTube for tracks missing a youtube_id and store it in the hub",
	Long: `Search the YouTube Data API for each hub track that has no youtube_id yet
and write the resolved video ID back into the YAML. Only missing tracks are
searched, so runs are incremental; --limit caps searches per run, and resolution
stops early (persisting progress) if the daily API quota is exhausted.

Requires youtube_api_key in config (or the YOUTUBE_API_KEY environment variable).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runResolveYouTube(context.Background())
	},
}

func runResolveYouTube(ctx context.Context) error {
	apiKey := viper.GetString("youtube_api_key")
	if apiKey == "" {
		return fmt.Errorf("youtube_api_key is not set (config or YOUTUBE_API_KEY env)")
	}

	input := resolveInput
	if input == "" {
		input = viper.GetString("dir")
	}

	paths, err := hubPaths(input)
	if err != nil {
		return err
	}

	searcher := youtube.HTTPSearcher{APIKey: apiKey}
	var budget *int
	if resolveLimit > 0 {
		budget = &resolveLimit
	}

	total := 0
	stopped := "done"
	for _, path := range paths {
		p, err := playlist.LoadFile(path)
		if err != nil {
			return fmt.Errorf("load %s: %w", path, err)
		}
		n, quota, err := youtube.Resolve(ctx, searcher, &p, budget)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", path, err)
		}
		if n > 0 {
			if err := playlist.SaveFile(path, p); err != nil {
				return fmt.Errorf("save %s: %w", path, err)
			}
		}
		total += n
		log.Infof("resolved %d in %s", n, filepath.Base(path))
		if quota {
			stopped = "quota"
			break
		}
		if budget != nil && *budget <= 0 {
			stopped = "limit"
			break
		}
	}
	log.Warnf("youtube resolve: %d ids resolved; stopped: %s", total, stopped)
	return nil
}

// hubPaths returns the YAML files to process: a single file, or every *.yaml in
// a directory.
func hubPaths(input string) ([]string, error) {
	info, err := os.Stat(input)
	if err != nil {
		return nil, fmt.Errorf("input %s: %w", input, err)
	}
	if !info.IsDir() {
		return []string{input}, nil
	}
	matches, err := filepath.Glob(filepath.Join(input, "*.yaml"))
	if err != nil {
		return nil, err
	}
	return matches, nil
}

func init() {
	rootCmd.AddCommand(resolveCmd)
	resolveCmd.AddCommand(resolveYouTubeCmd)
	resolveYouTubeCmd.Flags().StringVar(&resolveInput, "input", "", "hub YAML file or directory (default: config dir)")
	resolveYouTubeCmd.Flags().IntVar(&resolveLimit, "limit", 0, "max searches this run (0 = unlimited; quota is the backstop)")
}
```

- [ ] **Step 2: Config default.** In `cmd/root.go` `initConfig`, add beside the other defaults:

```go
	viper.SetDefault("youtube_api_key", "")
```

- [ ] **Step 3: Document** in `byom-sync.yaml.example` — add a line:

```yaml
# YouTube Data API key for `resolve youtube` (or set YOUTUBE_API_KEY)
youtube_api_key: ""
```

- [ ] **Step 4: Verify build + help.**

```
make build && ./byom-sync resolve youtube --help
```

Expected: help text lists `--input` and `--limit`.

- [ ] **Step 5: Commit** — `feat(cmd): add 'resolve youtube' command + youtube_api_key config`

---

### Task 5: JSPF export extension

**Files:** `internal/export/jspf.go`, `internal/export/export_test.go`.

- [ ] **Step 1: Failing test** — add to `internal/export/export_test.go`:

```go
func TestJSPFExportEmitsYouTubeExtension(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.jspf.json")
	p := samplePlaylist()
	p.Tracks[0].YouTubeID = "vid42"
	if err := (JSPFExporter{}).Export(p, out, nil); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(out)

	var doc struct {
		Playlist struct {
			Track []struct {
				Extension map[string][]struct {
					Resolved struct {
						YouTube string `json:"youtube"`
					} `json:"resolved"`
				} `json:"extension"`
			} `json:"track"`
		} `json:"playlist"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	ext := doc.Playlist.Track[0].Extension["https://github.com/lmorchard/byom-sync"]
	if len(ext) == 0 || ext[0].Resolved.YouTube != "vid42" {
		t.Errorf("missing/incorrect youtube extension:\n%s", raw)
	}
	// Track without a YouTubeID emits no extension key.
	if len(doc.Playlist.Track[1].Extension) != 0 {
		t.Errorf("track 2 should have no extension, got %v", doc.Playlist.Track[1].Extension)
	}
}
```

- [ ] **Step 2: Run — expect FAIL.** `cd internal/export && go test ./...`

- [ ] **Step 3: Implement** in `internal/export/jspf.go`. Add the namespace const near the top:

```go
// byomExtNS namespaces byom-sync's JSPF track extension (resolved ids, and
// later sync_state). Kept in sync with byom-player's reader.
const byomExtNS = "https://github.com/lmorchard/byom-sync"
```

Add `Extension` to `jspfTrack` (last field):

```go
	Extension map[string][]jspfExt `json:"extension,omitempty"`
```

Add the extension types (below the `jspfTrack` struct):

```go
type jspfExt struct {
	Resolved *jspfResolved `json:"resolved,omitempty"`
}

type jspfResolved struct {
	YouTube string `json:"youtube,omitempty"`
}
```

In the track loop, after setting `jt.Location`, add:

```go
		if t.YouTubeID != "" {
			jt.Extension = map[string][]jspfExt{
				byomExtNS: {{Resolved: &jspfResolved{YouTube: t.YouTubeID}}},
			}
		}
```

- [ ] **Step 4: Run — expect PASS.** `cd internal/export && go test ./...`

- [ ] **Step 5: Full verification + commit.**

```
make format && make lint && make test && make build
git add -A && git commit -m "feat(export): emit resolved.youtube in the JSPF extension"
```

---

## Post-implementation: manual live check

Not automated (spends quota). With a real key in config:

```
byom-sync resolve youtube --input ./playlists --limit 3
byom-sync export jspf --input ./playlists --out ./out
```

Confirm: up to 3 `youtube_id`s appear in the hub YAML, re-running skips them (no new searches), and the exported JSPF carries `resolved.youtube` for those tracks. Verify the ids play in byom-player once part 2 lands.
