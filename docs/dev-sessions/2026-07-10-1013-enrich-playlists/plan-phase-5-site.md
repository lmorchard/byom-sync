# Phase 5 (PR 2) — Publish Cover Art on the Static Site Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Make the static site serve downloaded cover art: copy the hub's `art/` store into the site output, and reference `base_url + image_file` in the per-playlist JSPF (via the exporter) and the Open Graph image — so art survives source-URL rot on the published site. Builds on PR 1 (`Track.ImageFile`, `internal/artstore`, `resolve art --download`), now in `main`.

**Architecture:** The JSPF exporter gains an opt-driven `art_base` mode: when set, a track's (and the playlist's) JSPF `image` becomes `art_base + image_file` for tracks with a downloaded local copy (else the source URL). The `site` builder copies `<hub>/art/` into `<out>/art/` and calls the exporter with `art_base = base_url`; the OG image (`meta.playlistImage`) is likewise upgraded to `base_url + image_file` when a local copy exists.

**Tech Stack:** Go 1.25 · stdlib · no new deps.

## Global Constraints

- Go 1.25; no cgo; gofumpt; golangci-lint v2 strict errcheck.
- Run `make lint && make test && make build`; read output.
- Commit trailer: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
- Branch `feat/art-site` (worktree `byom-sync-art-site`, off `main`). This DOES touch `internal/site` (accepted — conflicts with concurrent site branches reconciled at merge).
- **WORKING TREE HYGIENE:** work only in `/Users/lorchard/devel/byom-sync-art-site`; never `git checkout`/`restore`/`stash`/`clean` or `git add .`/`-A`; stage by explicit path; confirm `git branch --show-current` == `feat/art-site` before committing.

## File Structure

- `internal/export/jspf.go` — **modify.** `jspfImage` + playlist image honor `opts["art_base"]`.
- `internal/export/export_test.go` — **modify.** art_base test.
- `internal/site/paths.go` — **modify.** `WriteJSPF` takes a base URL, passes `art_base` opt.
- `internal/site/assets.go` (or new `internal/site/art.go`) — **create/modify.** `CopyArt(hubDir, outDir)`.
- `internal/site/meta.go` — **modify.** `playlistImage` honors `base_url + image_file`.
- `internal/site/render.go` — **modify.** pass base URL to `playlistImage`.
- `internal/site/site.go` — **modify.** call `CopyArt`; pass base URL to `WriteJSPF`.
- `internal/site/*_test.go` — **modify/create.** CopyArt + OG + JSPF art tests.
- `AGENTS.md` — **modify.** Document site art publishing.

---

### Task 1: JSPF exporter `art_base` option

**Files:** Modify `internal/export/jspf.go`; test `internal/export/export_test.go`.

**Interfaces:** The exporter reads `opts["art_base"]`. When set and a track has `ImageFile`, its JSPF `image` is `art_base + "/" + image_file`; otherwise the track's source `Image` URL. `embed_art` (PR 1) still takes precedence when both are set. The playlist-level image gets the same treatment.

**Context:** `jspfImage(t, opts)` (from PR 1) currently handles `embed_art` then returns `t.Image`. Add an `art_base` branch between them. `playlistImage(p)` (in jspf.go, playlist-level) must also honor `art_base` — refactor it to take `opts` and, when `art_base` is set, return `art_base + "/" + <first track's image_file>` (preferring a track with a local copy), else its current URL behavior.

- [ ] **Step 1: Failing test** — add to `internal/export/export_test.go`:

```go
func TestJSPFExport_ArtBase(t *testing.T) {
	p := playlist.Playlist{Title: "T", Tracks: []playlist.Track{
		{Title: "Local", Artist: "A", Image: "https://x/c.jpg", ImageFile: "art/ab/abcd.jpg"},
		{Title: "URLOnly", Artist: "B", Image: "https://x/d.jpg"},
	}}
	out := filepath.Join(t.TempDir(), "b.jspf")
	if err := (JSPFExporter{}).Export(p, out, map[string]string{"art_base": "https://site.example"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(out)
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("json: %v", err)
	}
	pl := doc["playlist"].(map[string]any)
	if pl["image"] != "https://site.example/art/ab/abcd.jpg" {
		t.Errorf("playlist image should use art_base+image_file: %v", pl["image"])
	}
	tracks := pl["track"].([]any)
	if got := tracks[0].(map[string]any)["image"]; got != "https://site.example/art/ab/abcd.jpg" {
		t.Errorf("track w/ local copy → art_base URL, got %v", got)
	}
	if got := tracks[1].(map[string]any)["image"]; got != "https://x/d.jpg" {
		t.Errorf("track w/o local copy → source URL, got %v", got)
	}
}
```

- [ ] **Step 2: Run — FAIL.** `go test ./internal/export/ -run TestJSPFExport_ArtBase -v`

- [ ] **Step 3: Implement** — in `jspf.go`, extend `jspfImage`:

```go
func jspfImage(t playlist.Track, opts map[string]string) string {
	if opts["embed_art"] == "true" && t.ImageFile != "" {
		abs := filepath.Join(opts["art_root"], filepath.FromSlash(t.ImageFile))
		if data, err := os.ReadFile(abs); err == nil {
			ct := ctypeForExt(filepath.Ext(t.ImageFile))
			return "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(data)
		}
	}
	if base := opts["art_base"]; base != "" && t.ImageFile != "" {
		return joinURL(base, t.ImageFile)
	}
	return t.Image
}

// joinURL joins a base URL and a relative path with exactly one slash.
func joinURL(base, rel string) string {
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(rel, "/")
}
```

Refactor `playlistImage` to take opts (update its call site in `Export` — currently `doc.Playlist.Image = playlistImage(p)`):

```go
func playlistImage(p playlist.Playlist, opts map[string]string) string {
	if base := opts["art_base"]; base != "" {
		for _, t := range p.Tracks {
			if t.ImageFile != "" {
				return joinURL(base, t.ImageFile)
			}
		}
	}
	if p.Image != "" {
		return p.Image
	}
	for _, t := range p.Tracks {
		if t.Image != "" {
			return t.Image
		}
	}
	return ""
}
```

(`strings` is already imported from PR 1's `ctypeForExt`. Verify.)

- [ ] **Step 4: Run — PASS** (new + existing JSPF tests, incl. embed + back-compat). `go test ./internal/export/ -v`
- [ ] **Step 5: Commit** — `git add internal/export/jspf.go internal/export/export_test.go` → `feat(export): JSPF art_base option references deployed local art`.

---

### Task 2: `site` copies the hub art store into output

**Files:** Create `internal/site/art.go`; test `internal/site/art_test.go`; modify `internal/site/site.go`.

**Interfaces:** `func CopyArt(hubDir, outDir string) error` — copies `<hubDir>/art/` recursively into `<outDir>/art/`. A missing `<hubDir>/art` is a no-op (nil). Called from `Build`.

**Context:** Mirror `WriteAssets`'s walk/copy, but source from the hub filesystem (`os.DirFS(hubDir)` or `filepath.WalkDir(filepath.Join(hubDir,"art"), ...)`) rather than the embedded FS. `Build` (site.go) has `opts.HubDir` and `opts.OutDir`; add the call alongside `WriteAssets`.

- [ ] **Step 1: Failing test** — create `internal/site/art_test.go`:

```go
package site

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyArt(t *testing.T) {
	hub := t.TempDir()
	if err := os.MkdirAll(filepath.Join(hub, "art", "ab"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hub, "art", "ab", "abcd.jpg"), []byte("IMG"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := CopyArt(hub, out); err != nil {
		t.Fatalf("CopyArt: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(out, "art", "ab", "abcd.jpg"))
	if err != nil || string(got) != "IMG" {
		t.Errorf("art not copied: %v / %q", err, got)
	}
}

func TestCopyArt_NoArtDirIsNoop(t *testing.T) {
	if err := CopyArt(t.TempDir(), t.TempDir()); err != nil {
		t.Errorf("missing art dir should be a no-op, got %v", err)
	}
}
```

- [ ] **Step 2: Run — FAIL** (`undefined: CopyArt`). `go test ./internal/site/ -run TestCopyArt -v`

- [ ] **Step 3: Implement** — create `internal/site/art.go`:

```go
package site

import (
	"io/fs"
	"os"
	"path/filepath"
)

// CopyArt copies the hub's downloaded cover-art store (<hubDir>/art) into
// <outDir>/art so the static site can serve it. A missing art dir is a no-op.
func CopyArt(hubDir, outDir string) error {
	src := filepath.Join(hubDir, "art")
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(hubDir, p)
		if err != nil {
			return err
		}
		dst := filepath.Join(outDir, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
}
```

- [ ] **Step 4** — call it in `Build` (site.go), after `WriteAssets`:

```go
	if err := WriteAssets(opts.OutDir); err != nil {
		return err
	}
	if err := CopyArt(opts.HubDir, opts.OutDir); err != nil {
		return err
	}
```

- [ ] **Step 5: Run — PASS.** `go test ./internal/site/ -run TestCopyArt -v`; `make test`.
- [ ] **Step 6: Commit** — `git add internal/site/art.go internal/site/art_test.go internal/site/site.go` → `feat(site): copy the hub art store into the site output`.

---

### Task 3: `site` references deployed art (JSPF + OG image)

**Files:** Modify `internal/site/paths.go`, `internal/site/site.go`, `internal/site/meta.go`, `internal/site/render.go`; test `internal/site/*_test.go`.

**Interfaces:** `WriteJSPF(outDir string, root *Node, baseURL string)` — passes `map[string]string{"art_base": baseURL}` to the exporter. `playlistImage(p, baseURL)` — returns `canonical(baseURL, image_file)` for the first track with a local copy, else the first track's `Image` URL.

**Context:** The site's per-playlist JSPF (loaded by byom-player) and the page OG image should both point at the deployed art (`base_url + image_file`) for downloaded tracks, and keep the source URL otherwise. `canonical(baseURL, urlPath)` (meta.go) already joins a base + path into an absolute URL — reuse it for OG. `WriteJSPF` currently passes `nil` opts; give it the base URL and pass `art_base`.

- [ ] **Step 1: Failing tests** — add to the appropriate `internal/site/*_test.go`:

```go
func TestPlaylistImage_PrefersDeployedLocalArt(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{
		{Title: "A", Image: "https://x/c.jpg", ImageFile: "art/ab/abcd.jpg"},
	}}
	if got := playlistImage(p, "https://site.example"); got != "https://site.example/art/ab/abcd.jpg" {
		t.Errorf("OG image should use base+image_file: %q", got)
	}
	// no local copy → source URL
	q := &playlist.Playlist{Tracks: []playlist.Track{{Title: "B", Image: "https://x/d.jpg"}}}
	if got := playlistImage(q, "https://site.example"); got != "https://x/d.jpg" {
		t.Errorf("no local copy → source URL: %q", got)
	}
}
```

(If a `WriteJSPF` test exists, update its call to the new signature; otherwise the compile + Task-1 exporter test cover the JSPF art_base path.)

- [ ] **Step 2: Run — FAIL** (signature mismatch / wrong value). `go test ./internal/site/ -run TestPlaylistImage -v`

- [ ] **Step 3: Implement**

`meta.go` — `playlistImage` takes a base URL and prefers deployed local art:

```go
func playlistImage(p *playlist.Playlist, baseURL string) string {
	for _, t := range p.Tracks {
		if t.ImageFile != "" {
			return canonical(baseURL, t.ImageFile)
		}
	}
	for _, t := range p.Tracks {
		if t.Image != "" {
			return t.Image
		}
	}
	return ""
}
```

`render.go` — update the call (line ~196): `Image: playlistImage(node.Playlist, r.Site.BaseURL)`.

`paths.go` — `WriteJSPF` takes `baseURL` and passes `art_base`:

```go
func WriteJSPF(outDir string, root *Node, baseURL string) error {
	return walkPlaylists(root, func(n *Node) error {
		dir := pageDir(outDir, n)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		return export.JSPFExporter{}.Export(*n.Playlist, filepath.Join(dir, "playlist.jspf.json"),
			map[string]string{"art_base": baseURL})
	})
}
```

`site.go` — update the call: `if err := WriteJSPF(opts.OutDir, root, opts.Site.BaseURL); err != nil {`.

- [ ] **Step 4: Run — PASS** (new OG test + all existing site tests, updated call compiles). `go test ./internal/site/ -v`; `make test`.
- [ ] **Step 5: Commit** — `git add internal/site/meta.go internal/site/render.go internal/site/paths.go internal/site/site.go internal/site/*_test.go` → `feat(site): reference deployed local art in JSPF + OG image`.

---

### Task 4: Document site art publishing

**Files:** Modify `AGENTS.md`.

- [ ] **Step 1** — extend the site's coverage note / Cover art bullet: `site` copies the hub `<hub>/art` store into the build output and references `base_url + image_file` in each `playlist.jspf.json` (via the exporter's `art_base` opt) and the OG image, so downloaded art is served from the site and survives source-URL rot. Tracks without a local copy keep their source URL.
- [ ] **Step 2** — `git diff AGENTS.md`; commit `docs(agents): document site cover-art publishing`.

---

### Final verification

- [ ] `make lint && make test && make build` — all green.
- [ ] Live (manual): after `resolve art --download` on a hub, `byom-sync site` produces `<out>/art/...` files and `playlist.jspf.json` with `image` pointing at `base_url/art/...` for downloaded tracks.

## Self-Review

**Coverage:** exporter `art_base` (T1); site copies art store (T2); site references deployed art in JSPF + OG (T3); docs (T4). Depends only on PR 1 (in `main`). Matches the agreed design (shared store served by the site, `base_url + image_file` references).

**Placeholder scan:** none — complete code. `strings` import in jspf.go is from PR 1 (`ctypeForExt`); verify before adding.

**Type consistency:** `jspfImage`/`playlistImage` (export) both read `opts["art_base"]` (T1) set by `WriteJSPF` (T3). `joinURL` (export) and `canonical` (site) both do base+rel joining in their packages. `WriteJSPF` new signature `(outDir, root, baseURL)` updated at its lone call in `site.go` (T3). `CopyArt(hubDir, outDir)` (T2) called in `Build` with `opts.HubDir`/`opts.OutDir`. `playlistImage(p, baseURL)` (site) updated at its `render.go` call (T3).
