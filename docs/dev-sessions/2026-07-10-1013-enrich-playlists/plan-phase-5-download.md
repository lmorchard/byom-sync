# Phase 5 (PR 1) — Local Art Download Primitives Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Download resolved cover art into a local content-addressed store so it survives URL rot, and let `export jspf` inline that art as `data:` URLs for a self-contained single file. (No `internal/site` changes — that's PR 2.)

**Architecture:** A new `internal/artstore` package fetches an image URL, hashes the bytes (sha256), and writes it to a shared, deduplicated store `<hubroot>/art/<hh>/<hash>.<ext>`, returning the hub-relative path. `resolve art --download` runs a download pass that fills a new `Track.ImageFile` for every track with an `image` URL. The JSPF exporter gains an opt-driven `embed_art` mode that inlines the local copy as a `data:` URL.

**Tech Stack:** Go 1.25 · stdlib `net/http`/`crypto/sha256`/`encoding/base64` · no new deps.

## Global Constraints

- Go 1.25; no cgo; gofumpt; golangci-lint v2 strict errcheck (`_ =` for ignored returns incl. deferred `Body.Close()`).
- Run `make lint && make test && make build` before claiming done; read output.
- Commit trailer: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
- Branch `feat/art-download` (worktree `byom-sync-art`, off `main`). **No `internal/site` changes** (PR 2).
- **WORKING TREE HYGIENE:** work only in `/Users/lorchard/devel/byom-sync-art`; never `git checkout`/`restore`/`stash`/`clean` or `git add .`/`-A`; stage files by explicit path. Confirm `git branch --show-current` == `feat/art-download` before committing.
- HTTP: close every body; handle non-200; base URL / http client injectable for tests.

## File Structure

- `internal/playlist/types.go` — **modify.** Add `Track.ImageFile`.
- `internal/playlist/types_test.go` — **modify.** Round-trip test.
- `internal/artstore/artstore.go` — **create.** `Store`, `Save`.
- `internal/artstore/artstore_test.go` — **create.** httptest-backed.
- `internal/export/jspf.go` — **modify.** `embed_art` opt → inline `data:` from the local copy.
- `internal/export/export_test.go` — **modify.** Embed test.
- `cmd/resolve.go` — **modify.** `--download` flag + download pass in `runResolveArt`.
- `cmd/export.go` — **modify.** `export jspf --embed-art` flag → pass opts.
- `AGENTS.md` — **modify.** Document `--download` + `--embed-art` + the art store.

---

### Task 1: `Track.ImageFile` schema

**Files:** Modify `internal/playlist/types.go`; test `internal/playlist/types_test.go`.

**Interfaces:** Produces `Track.ImageFile string` (yaml `image_file,omitempty`) — a hub-relative path to a downloaded local copy of the cover.

- [ ] **Step 1: Failing test** — add to `types_test.go`:

```go
func TestTrack_ImageFileRoundTrip(t *testing.T) {
	data, err := yaml.Marshal(Track{Title: "T", Artist: "A", Image: "https://x/c.jpg", ImageFile: "art/ab/abcd.jpg"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), "image_file: art/ab/abcd.jpg") {
		t.Errorf("image_file not serialized:\n%s", data)
	}
	// omitempty: absent when unset
	bare, _ := yaml.Marshal(Track{Title: "T", Artist: "A"})
	if strings.Contains(string(bare), "image_file:") {
		t.Errorf("bare track should omit image_file:\n%s", bare)
	}
}
```

- [ ] **Step 2: Run — FAIL** (`Track has no field ImageFile`).
  `go test ./internal/playlist/ -run TestTrack_ImageFileRoundTrip -v`

- [ ] **Step 3: Implement** — in the `Track` struct, after `Image`:

```go
	Image      string    `yaml:"image,omitempty"`
	// ImageFile is a hub-relative path to a downloaded local copy of the cover
	// (from `resolve art --download`); Image stays as the source URL.
	ImageFile string `yaml:"image_file,omitempty"`
```

- [ ] **Step 4: Run — PASS.** `go test ./internal/playlist/ -v`
- [ ] **Step 5: Commit** — `git add internal/playlist/types.go internal/playlist/types_test.go` then commit `feat(playlist): add Track.ImageFile for downloaded local cover art`.

---

### Task 2: `internal/artstore` — content-addressed store

**Files:** Create `internal/artstore/artstore.go`, `internal/artstore/artstore_test.go`.

**Interfaces:**
- `type Store struct { Root string; HTTP *http.Client }`
- `func (s Store) Save(ctx context.Context, url string) (relPath string, err error)` — GETs url, sha256s the bytes, picks an extension from the Content-Type, writes `<Root>/art/<hash[:2]>/<hash>.<ext>` (creating dirs), and returns the hub-relative `art/<hash[:2]>/<hash>.<ext>`. If that file already exists, it does NOT re-write (dedup) but still returns the path. A non-200 response is an error.

**Context:** Content-addressing by image bytes means byte-identical covers (an album's tracks, repeated albums across playlists) collapse to one file. The 2-char shard dir keeps any single directory small. Extension map: `image/jpeg`→`jpg`, `image/png`→`png`, `image/webp`→`webp`, `image/gif`→`gif`; unknown → `jpg` (Spotify/CAA serve JPEG).

- [ ] **Step 1: Failing test** — create `internal/artstore/artstore_test.go`:

```go
package artstore

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSave_WritesContentAddressedFileAndDedups(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("JPEGBYTES"))
	}))
	defer srv.Close()

	root := t.TempDir()
	s := Store{Root: root, HTTP: srv.Client()}

	rel, err := s.Save(context.Background(), srv.URL+"/cover")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !strings.HasPrefix(rel, "art/") || !strings.HasSuffix(rel, ".jpg") {
		t.Errorf("rel path shape: %q", rel)
	}
	// file exists on disk under root
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
		t.Errorf("file not written: %v", err)
	}
	// sharded: art/<2 chars>/<hash>.jpg
	parts := strings.Split(rel, "/")
	if len(parts) != 3 || len(parts[1]) != 2 {
		t.Errorf("expected art/<hh>/<hash>.jpg, got %q", rel)
	}

	// second Save of the same bytes → same path, no re-download
	rel2, err := s.Save(context.Background(), srv.URL+"/cover")
	if err != nil {
		t.Fatalf("Save 2: %v", err)
	}
	if rel2 != rel {
		t.Errorf("dedup: got %q want %q", rel2, rel)
	}
	if hits != 1 {
		t.Errorf("expected 1 network hit (second is a dedup skip), got %d", hits)
	}
}

func TestSave_Non200IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	s := Store{Root: t.TempDir(), HTTP: srv.Client()}
	if _, err := s.Save(context.Background(), srv.URL+"/x"); err == nil {
		t.Fatal("expected error on 404")
	}
}
```

Note on dedup + `hits`: to avoid re-downloading when the file already exists, `Save` must check for an existing file BEFORE fetching — but the path depends on the content hash, which requires the bytes. Resolve this by keying the on-disk existence check on a cheap pre-hash of the URL is NOT reliable. Instead: the dedup-without-refetch guarantee is scoped to a **content hash the caller already has**. Simpler contract for this test: `Save` fetches, hashes, and if the target file exists, skips the write (still 1 network hit per call). Adjust the test's `hits` expectation to `2` (both calls fetch; the second skips only the write). Implement accordingly and make the test assert `hits == 2` with a comment that dedup avoids the disk write, not the fetch. (Callers avoid redundant fetches via the `image_file`-already-set check in Task 3.)

- [ ] **Step 2: Run — FAIL** (package/Save undefined).
  `go test ./internal/artstore/ -v`

- [ ] **Step 3: Implement** — create `internal/artstore/artstore.go`:

```go
// Package artstore downloads cover-art images into a local, content-addressed
// store so playlists survive source-URL rot. Files are named by the sha256 of
// their bytes, so byte-identical covers dedup automatically.
package artstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// Store writes downloaded art under Root/art/. Root is the hub directory.
type Store struct {
	Root string
	HTTP *http.Client
}

// Save fetches url, writes the bytes to art/<hh>/<sha256>.<ext> under Root
// (skipping the write if that file already exists), and returns the hub-relative
// slash path. A non-200 response is an error.
func (s Store) Save(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch %s: status %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	ext := extFor(resp.Header.Get("Content-Type"))
	rel := filepath.ToSlash(filepath.Join("art", hash[:2], hash+"."+ext))
	abs := filepath.Join(s.Root, filepath.FromSlash(rel))

	if _, err := os.Stat(abs); err == nil {
		return rel, nil // already stored (dedup) — skip the write
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(abs, data, 0o644); err != nil {
		return "", err
	}
	return rel, nil
}

// extFor maps a Content-Type to a file extension, defaulting to jpg.
func extFor(contentType string) string {
	switch {
	case contains(contentType, "png"):
		return "png"
	case contains(contentType, "webp"):
		return "webp"
	case contains(contentType, "gif"):
		return "gif"
	default:
		return "jpg"
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

(Prefer `strings.Contains` over the hand-rolled `contains`/`indexOf` — import `strings` and use `strings.Contains(contentType, "png")` etc. The hand-rolled versions are shown only to avoid an unused-import trap; use `strings`.)

- [ ] **Step 4: Run — PASS** (adjust the `hits` assertion to 2 per the Step-1 note). `go test ./internal/artstore/ -v`
- [ ] **Step 5: Commit** — `git add internal/artstore/` then commit `feat(artstore): content-addressed local cover-art store`.

---

### Task 3: `resolve art --download`

**Files:** Modify `cmd/resolve.go`.

**Interfaces:** Consumes `artstore.Store`, existing `runResolveArt`/`hubPaths`/`log`. Produces a `--download` flag + a download pass.

**Context:** After the Spotify + MusicBrainz passes fill `image` URLs (and before/at the final `SaveFile`), a download pass — when `--download` is set — walks the playlist's tracks and, for each with `Image != "" && ImageFile == ""`, calls `store.Save(ctx, t.Image)` and sets `t.ImageFile`. The store `Root` is the hub root (`input` when it's a dir, else the input file's dir). A per-track download error is logged (warn) and skipped, not fatal (mirrors the MusicBrainz pass's resilience). `--download` also works standalone on a re-run: tracks already have `image` URLs, resolve passes no-op, and the download pass fills `image_file`.

- [ ] **Step 1** — add the flag var (near `artNoCache`): `artDownload bool`.

- [ ] **Step 2** — determine the hub root once in `runResolveArt` (after `input` is resolved):

```go
	artRoot := input
	if fi, statErr := os.Stat(input); statErr == nil && !fi.IsDir() {
		artRoot = filepath.Dir(input)
	}
	var store *artstore.Store
	if artDownload {
		store = &artstore.Store{Root: artRoot, HTTP: http.DefaultClient}
	}
```

- [ ] **Step 3** — add the download pass inside the per-playlist loop, right before the final `SaveFile`:

```go
		if store != nil {
			dl := 0
			for i := range p.Tracks {
				t := &p.Tracks[i]
				if t.Image == "" || t.ImageFile != "" {
					continue
				}
				rel, derr := store.Save(ctx, t.Image)
				if derr != nil {
					log.Warnf("  download art: %s - %s: %v", t.Artist, t.Title, derr)
					continue
				}
				t.ImageFile = rel
				dl++
			}
			if dl > 0 {
				log.Infof("%s: downloaded %d cover(s) into %s/art", base, dl, artRoot)
			}
		}
```

- [ ] **Step 4** — register the flag in `init()`:

```go
	resolveArtCmd.Flags().BoolVar(&artDownload, "download", false, "download resolved cover art into a local <hub>/art store and record image_file")
```

Add imports `"net/http"` (if not present) and `"github.com/lmorchard/byom-sync/internal/artstore"` to `cmd/resolve.go`.

- [ ] **Step 5** — build + smoke:
  `make build && ./byom-sync resolve art --help` (shows `--download`); `go test ./cmd/`; `make test`.
- [ ] **Step 6: Commit** — `git add cmd/resolve.go` then commit `feat(resolve): 'resolve art --download' saves art to a local content-addressed store`.

---

### Task 4: `export jspf --embed-art`

**Files:** Modify `internal/export/jspf.go`, `cmd/export.go`; test `internal/export/export_test.go`.

**Interfaces:** The JSPF exporter reads `opts["embed_art"] == "true"` and `opts["art_root"]`. When embedding and a track has `ImageFile`, its JSPF `image` is a `data:<ctype>;base64,<...>` URL built from the local file (`filepath.Join(art_root, ImageFile)`); tracks without an `ImageFile` keep their `image` URL. A read error for a specific file falls back to the track's URL (never fails the whole export). `export jspf --embed-art` passes these opts.

**Context:** The exporter stays network-free — it embeds only already-downloaded local copies (run `resolve art --download` first). Content-type from the file extension (`.jpg`→`image/jpeg`, `.png`→`image/png`, `.webp`→`image/webp`, `.gif`→`image/gif`).

- [ ] **Step 1: Failing test** — add to `internal/export/export_test.go`:

```go
func TestJSPFExport_EmbedArt(t *testing.T) {
	root := t.TempDir()
	// a downloaded local cover
	rel := filepath.Join("art", "ab", "abcd.jpg")
	if err := os.MkdirAll(filepath.Join(root, "art", "ab"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, rel), []byte("JPEGBYTES"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := playlist.Playlist{Title: "T", Tracks: []playlist.Track{
		{Title: "Has Local", Artist: "A", Image: "https://x/c.jpg", ImageFile: "art/ab/abcd.jpg"},
		{Title: "URL Only", Artist: "B", Image: "https://x/d.jpg"},
	}}
	out := filepath.Join(t.TempDir(), "e.jspf")
	err := (JSPFExporter{}).Export(p, out, map[string]string{"embed_art": "true", "art_root": root})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(out)
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("json: %v", err)
	}
	tracks := doc["playlist"].(map[string]any)["track"].([]any)
	t0 := tracks[0].(map[string]any)["image"].(string)
	if !strings.HasPrefix(t0, "data:image/jpeg;base64,") {
		t.Errorf("track with a local copy should embed data URL, got %q", t0)
	}
	t1 := tracks[1].(map[string]any)["image"].(string)
	if t1 != "https://x/d.jpg" {
		t.Errorf("track without a local copy should keep its URL, got %q", t1)
	}
}
```

- [ ] **Step 2: Run — FAIL** (embed not implemented; track image stays the URL).
  `go test ./internal/export/ -run TestJSPFExport_EmbedArt -v`

- [ ] **Step 3: Implement** — in `jspf.go`, replace the track `Image` assignment so it goes through a helper that honors the opts. Where the loop currently sets `jt.Image = t.Image` (from Phase 3), change to:

```go
		jt.Image = jspfImage(t, opts)
```

And update `Export`'s signature use of `opts` (it's already `_ map[string]string`; rename to `opts`). Add the helper:

```go
// jspfImage returns the JSPF image for a track: a data: URL embedded from the
// track's downloaded local copy when embed_art is set and a copy exists, else
// the track's source Image URL.
func jspfImage(t playlist.Track, opts map[string]string) string {
	if opts["embed_art"] == "true" && t.ImageFile != "" {
		abs := filepath.Join(opts["art_root"], filepath.FromSlash(t.ImageFile))
		if data, err := os.ReadFile(abs); err == nil {
			ct := ctypeForExt(filepath.Ext(t.ImageFile))
			return "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(data)
		}
		// read failed → fall through to the URL
	}
	return t.Image
}

func ctypeForExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return "image/jpeg"
	}
}
```

Add imports to `jspf.go`: `"encoding/base64"`, `"path/filepath"`, `"strings"`. Note the existing `Export` param is `_ map[string]string` — rename to `opts` and thread it (and to `playlistImage` if you also want playlist-level embed; for this PR, playlist image can remain URL-based — keep `playlistImage(p)` as-is).

- [ ] **Step 4** — wire the CLI flag in `cmd/export.go`. Add a flag var `exportEmbedArt bool`; register it on the `jspf` subcommand; in the jspf `RunE`, pass opts:

```go
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := map[string]string{}
		if exportEmbedArt {
			opts["embed_art"] = "true"
			opts["art_root"] = artRootOf(exportInput)
		}
		return export.Run(export.JSPFExporter{}, "jspf", exportInput, exportOut, opts)
	},
```

with a helper `artRootOf(input string) string` (input's dir when it's a file, else input) — reuse the same logic as Task 3 (a small shared helper in cmd is fine). Register: `exportJSPFCmd.Flags().BoolVar(&exportEmbedArt, "embed-art", false, "inline downloaded cover art as data: URLs (run 'resolve art --download' first)")`. (Find the jspf subcommand var name in cmd/export.go; if the subcommand is an inline `&cobra.Command{...}` without a package var, promote it to a var so a flag can be attached.)

- [ ] **Step 5** — build + test: `make build && ./byom-sync export jspf --help` (shows `--embed-art`); `go test ./internal/export/ ./cmd/ -v`; `make test`.
- [ ] **Step 6: Commit** — `git add internal/export/jspf.go internal/export/export_test.go cmd/export.go` then commit `feat(export): 'jspf --embed-art' inlines downloaded art as data URLs`.

---

### Task 5: Document in AGENTS.md

**Files:** Modify `AGENTS.md`.

- [ ] **Step 1** — extend the **Cover art** convention bullet: `resolve art --download` saves resolved art into a shared, content-addressed `<hub>/art/<hh>/<hash>.<ext>` store (dedup by image bytes) and records `Track.ImageFile` (hub-relative; `Image` stays the source URL). `export jspf --embed-art` inlines those local copies as `data:` URLs for a self-contained file (run `--download` first; network-free). Note `internal/artstore`.
- [ ] **Step 2** — add `internal/artstore/` to the Layout section (one line: content-addressed art download store).
- [ ] **Step 3** — `git diff AGENTS.md` (only intended edits); commit `docs(agents): document local art download + embed-art`.

---

### Final verification

- [ ] `make lint && make test && make build` — all green.
- [ ] Live (manual): `./byom-sync resolve art --download --input playlists/<small>.yaml` fills `image_file` and writes files under `playlists/art/`; `./byom-sync export jspf --embed-art --input playlists/<small>.yaml --out /tmp/x.jspf` produces data: URLs.

## Self-Review

**Coverage:** `Track.ImageFile` (T1); content-addressed store w/ dedup (T2); `resolve art --download` pass (T3); `export jspf --embed-art` (T4); docs (T5). No `internal/site` changes (PR 2). Matches the agreed design: shared content-addressed store `art/<hh>/<hash>.<ext>`, keep `image` URL + add `image_file`, embed for the single-file goal.

**Placeholder scan:** none — complete code. Two implementer notes flagged inline: the `contains`/`indexOf` hand-roll should be `strings.Contains` (avoid the unused-import trap by importing `strings`); the `Save` dedup skips the write not the fetch (test `hits == 2`).

**Type consistency:** `artstore.Store.Save` returns the hub-relative path stored in `Track.ImageFile` (T1) and read back by `jspfImage` via `art_root`+`ImageFile` (T4). The `opts` map keys (`embed_art`, `art_root`) are set in `cmd/export.go` (T4) and read in `jspf.go` (T4). `--download` (T3) and `--embed-art` (T4) are independent flags on different commands.
