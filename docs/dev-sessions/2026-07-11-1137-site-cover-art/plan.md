# Site Cover Art Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show playlist cover art on the generated static site — media cards on index/folder pages and thumbnails in the sidebar nav — preferring self-hosted local copies with a remote-URL fallback.

**Architecture:** A single pure resolver (`siteCover`) decides each playlist's cover reference and whether it is self-hosted. The server-rendered index/folder template consumes it via a template func; the client-rendered sidebar consumes it via a new `site-index.json` field; and `site.Build` copies the referenced local files into the output.

**Tech Stack:** Go 1.25, `html/template`, vanilla JS custom element (`byom-site-nav`), CSS. Tests are standard-library `testing`.

## Global Constraints

- Formatting via `gofumpt`; lint via golangci-lint v2. **errcheck is strict** — use `_ =` for intentionally-ignored returns.
- Verify with `make lint && make test && make build`.
- Commit trailer: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
- Work happens on branch `feat/site-cover-art`.
- Scope: `internal/site/` only. No player/JSPF changes, no hub-schema changes.

---

### Task 1: `siteCover` resolver + `playlistCover` template func

**Files:**
- Modify: `internal/site/meta.go`
- Modify: `internal/site/render.go` (register template func in `NewRenderer`)
- Test: `internal/site/meta_test.go`

**Interfaces:**
- Consumes: `playlist.Playlist`, `playlist.Track` (fields `Image`, `ImageFile`).
- Produces:
  - `func siteCover(p *playlist.Playlist) (href, local string)` — `href` is the site reference (root-relative `/art/...` when self-hosted, else remote URL, else `""`); `local` is the hub-relative source path to copy when self-hosted (else `""`).
  - Template func `playlistCover` → `func(*playlist.Playlist) string` returning `href`.

- [ ] **Step 1: Write the failing test**

Add to `internal/site/meta_test.go`:

```go
func TestSiteCover(t *testing.T) {
	cases := []struct {
		name             string
		p                *playlist.Playlist
		wantHref, wantLo string
	}{
		{
			name:     "playlist-level image is remote",
			p:        &playlist.Playlist{Image: "http://img/pl.jpg", Tracks: []playlist.Track{{ImageFile: "art/aa/x.jpg"}}},
			wantHref: "http://img/pl.jpg", wantLo: "",
		},
		{
			name:     "prefers first track local file over an earlier remote",
			p:        &playlist.Playlist{Tracks: []playlist.Track{{Image: "http://img/0.jpg"}, {ImageFile: "art/bb/y.jpg"}}},
			wantHref: "/art/bb/y.jpg", wantLo: "art/bb/y.jpg",
		},
		{
			name:     "falls back to first remote when no local exists",
			p:        &playlist.Playlist{Tracks: []playlist.Track{{}, {Image: "http://img/2.jpg"}}},
			wantHref: "http://img/2.jpg", wantLo: "",
		},
		{
			name:     "nothing available",
			p:        &playlist.Playlist{Tracks: []playlist.Track{{}}},
			wantHref: "", wantLo: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			href, lo := siteCover(tc.p)
			if href != tc.wantHref || lo != tc.wantLo {
				t.Errorf("siteCover = (%q,%q), want (%q,%q)", href, lo, tc.wantHref, tc.wantLo)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/site/ -run TestSiteCover`
Expected: FAIL — `undefined: siteCover`.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/site/meta.go` (below `playlistImage`):

```go
// siteCover resolves how a playlist's cover is referenced on the site. It
// prefers a downloaded local copy (returned as a root-relative /art/... href
// plus the hub-relative source path to copy) and falls back to a remote URL.
// href is what pages/JSON link to; local is non-empty only when the cover is
// self-hosted and must be copied into the output.
func siteCover(p *playlist.Playlist) (href, local string) {
	if p.Image != "" {
		return p.Image, ""
	}
	for _, t := range p.Tracks {
		if t.ImageFile != "" {
			return "/" + t.ImageFile, t.ImageFile
		}
	}
	for _, t := range p.Tracks {
		if t.Image != "" {
			return t.Image, ""
		}
	}
	return "", ""
}
```

Register the template func in `internal/site/render.go` inside the `funcs` map in `NewRenderer`:

```go
		"playlistCover": func(p *playlist.Playlist) string { href, _ := siteCover(p); return href },
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/site/ -run TestSiteCover`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/site/meta.go internal/site/render.go internal/site/meta_test.go
git commit -m "feat(site): siteCover resolver (self-host, remote fallback)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: `IndexNode.Image` in `site-index.json`

**Files:**
- Modify: `internal/site/index.go`
- Test: `internal/site/index_test.go`

**Interfaces:**
- Consumes: `siteCover` (Task 1).
- Produces: `IndexNode.Image string` (JSON key `image`), populated for leaves.

- [ ] **Step 1: Write the failing test**

Add to `internal/site/index_test.go` (the fixture leaf `2014-top-songs` has a track with `image: 'http://img/1.jpg'` and no `image_file`, so its cover is that remote URL):

```go
func TestIndexNodeImage(t *testing.T) {
	root, err := BuildTree(writeFixtureHub(t))
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := WriteIndexJSON(out, root); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(out, "site-index.json"))
	var nodes []IndexNode
	if err := json.Unmarshal(data, &nodes); err != nil {
		t.Fatal(err)
	}
	// nodes[1] is the top-level leaf 2014-top-songs (dir "synthpop" sorts first).
	if nodes[1].Name != "2014-top-songs" {
		t.Fatalf("expected 2014-top-songs leaf, got %q", nodes[1].Name)
	}
	if nodes[1].Image != "http://img/1.jpg" {
		t.Errorf("leaf Image = %q, want http://img/1.jpg", nodes[1].Image)
	}
	if nodes[0].Image != "" {
		t.Errorf("directory Image = %q, want empty", nodes[0].Image)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/site/ -run TestIndexNodeImage`
Expected: FAIL — `nodes[1].Image undefined` (compile error).

- [ ] **Step 3: Write minimal implementation**

In `internal/site/index.go`, add the field to `IndexNode` (after `Meta`):

```go
	Image    string      `json:"image,omitempty"` // resolved cover href (leaves only)
```

In `toIndexNodes`, inside the `if !c.IsDir {` block, after the `Meta` assignment:

```go
		if href, _ := siteCover(c.Playlist); href != "" {
			n.Image = href
		}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/site/ -run TestIndexNodeImage`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/site/index.go internal/site/index_test.go
git commit -m "feat(site): carry cover href in site-index.json

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Copy referenced art into the output (`WriteCoverArt`)

**Files:**
- Create: `internal/site/coverart.go`
- Modify: `internal/site/site.go` (wire into `Build`)
- Test: `internal/site/coverart_test.go`

**Interfaces:**
- Consumes: `siteCover` (Task 1), `Node` tree, `Options.HubDir`/`Options.OutDir`.
- Produces: `func WriteCoverArt(hubDir, outDir string, root *Node) error`.

- [ ] **Step 1: Write the failing test**

Create `internal/site/coverart_test.go`:

```go
package site

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

func TestWriteCoverArt(t *testing.T) {
	hub := t.TempDir()
	// A real local art file that a playlist references via image_file.
	srcRel := filepath.FromSlash("art/aa/hash.jpg")
	if err := os.MkdirAll(filepath.Join(hub, "art", "aa"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hub, srcRel), []byte("JPEGDATA"), 0o644); err != nil {
		t.Fatal(err)
	}

	root := &Node{IsDir: true, Children: []*Node{
		{Playlist: &playlist.Playlist{Tracks: []playlist.Track{{ImageFile: "art/aa/hash.jpg"}}}},              // local → copied
		{Playlist: &playlist.Playlist{Tracks: []playlist.Track{{Image: "http://img/x.jpg"}}}},                 // remote → skipped
		{Playlist: &playlist.Playlist{Tracks: []playlist.Track{{ImageFile: "art/zz/missing.jpg"}}}},           // missing source → skipped
		{IsDir: true, Children: []*Node{{Playlist: &playlist.Playlist{Tracks: []playlist.Track{{ImageFile: "art/aa/hash.jpg"}}}}}}, // dup in subdir → copied once
	}}

	out := t.TempDir()
	if err := WriteCoverArt(hub, out, root); err != nil {
		t.Fatalf("WriteCoverArt: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(out, srcRel))
	if err != nil {
		t.Fatalf("copied art missing: %v", err)
	}
	if string(got) != "JPEGDATA" {
		t.Errorf("copied bytes = %q", got)
	}
	if _, err := os.Stat(filepath.Join(out, "art", "zz", "missing.jpg")); !os.IsNotExist(err) {
		t.Errorf("missing source should not be created, err=%v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/site/ -run TestWriteCoverArt`
Expected: FAIL — `undefined: WriteCoverArt`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/site/coverart.go`:

```go
package site

import (
	"os"
	"path/filepath"
)

// WriteCoverArt copies every self-hosted cover referenced by a playlist in the
// tree from hubDir into outDir, preserving the hub-relative path. Remote covers
// carry no local path and are skipped; a missing source file is skipped too
// (the page keeps its href — a broken <img> is preferable to aborting the whole
// build over one absent cover). Each source is copied at most once.
func WriteCoverArt(hubDir, outDir string, root *Node) error {
	seen := map[string]bool{}
	var walk func(n *Node) error
	walk = func(n *Node) error {
		for _, c := range n.Children {
			if c.IsDir {
				if err := walk(c); err != nil {
					return err
				}
				continue
			}
			_, local := siteCover(c.Playlist)
			if local == "" || seen[local] {
				continue
			}
			seen[local] = true
			rel := filepath.FromSlash(local)
			data, err := os.ReadFile(filepath.Join(hubDir, rel))
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return err
			}
			dst := filepath.Join(outDir, rel)
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(dst, data, 0o644); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(root)
}
```

Wire into `internal/site/site.go` `Build`, immediately after the `WriteIndexJSON` call:

```go
	if err := WriteCoverArt(opts.HubDir, opts.OutDir, root); err != nil {
		return err
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/site/ -run TestWriteCoverArt`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/site/coverart.go internal/site/coverart_test.go internal/site/site.go
git commit -m "feat(site): copy referenced local cover art into output

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Index/folder media cards (template + CSS)

**Files:**
- Modify: `internal/site/templates/landing.html`
- Modify: `internal/site/assets/site.css`
- Test: `internal/site/render_test.go`

**Interfaces:**
- Consumes: `playlistCover` template func (Task 1), `playlistMeta`, `dirsOf`, `yearGroupsOf`, `Node.Playlist.Description`.
- Produces: `.playlist-cards` grid of `.playlist-card` links on landing and folder pages.

- [ ] **Step 1: Write/adjust the failing tests**

In `internal/site/render_test.go`, `TestRenderSite`: the leaf metadata assertion changes because leaves are no longer `<li>… — meta`. Replace:

```go
	if !strings.Contains(landing, `class="meta">— 1 track`) {
		t.Error("landing tree missing per-playlist metadata line")
	}
```

with:

```go
	// Playlists render as media cards; the top-level leaf carries a remote cover.
	if !strings.Contains(landing, `class="playlist-card"`) {
		t.Error("landing missing playlist cards")
	}
	if !strings.Contains(landing, `<img class="cover" src="http://img/1.jpg"`) {
		t.Error("landing card missing cover image")
	}
	if !strings.Contains(landing, `class="meta">1 track`) {
		t.Error("landing card missing metadata line")
	}
	// The synthpop child (bleep-bloop-bop) has no cover → placeholder box.
	folderPage := read("synthpop/index.html")
	if !strings.Contains(folderPage, `class="cover placeholder"`) {
		t.Error("cover-less playlist should render a placeholder box")
	}
```

Add a focused blurb test:

```go
func TestRenderCardBlurb(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "index.md"), "# hub\n")
	mustWrite(t, filepath.Join(dir, "a.yaml"),
		"title: A\ndescription: A short blurb.\ntracks:\n  - {title: T, artist: X}\n")
	mustWrite(t, filepath.Join(dir, "b.yaml"),
		"title: B\ntracks:\n  - {title: T, artist: X}\n")
	root, err := BuildTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewRenderer(testSite())
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := r.RenderSite(out, root); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(out, "index.html"))
	s := string(b)
	if !strings.Contains(s, `class="blurb">A short blurb.`) {
		t.Error("playlist with description should render a blurb")
	}
	// Playlist B has no description → no stray empty blurb span.
	if strings.Count(s, `class="blurb"`) != 1 {
		t.Errorf("expected exactly one blurb, got %d", strings.Count(s, `class="blurb"`))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/site/ -run 'TestRenderSite|TestRenderCardBlurb'`
Expected: FAIL — old markup absent / `playlist-card` not found.

- [ ] **Step 3: Update the template**

In `internal/site/templates/landing.html`, replace ONLY the year-group leaf block (the directory `tree-list` block above it stays unchanged). Change:

```html
{{range yearGroupsOf $children}}
<h2 class="year">{{.Label}}</h2>
<ul class="tree-list">
{{range .Playlists}}
  <li class="leaf"><a href="/{{.Path}}/">{{.Title}}</a> <span class="meta">— {{playlistMeta .Playlist}}</span></li>
{{end}}
</ul>
{{end}}
```

to:

```html
{{range yearGroupsOf $children}}
<h2 class="year">{{.Label}}</h2>
<div class="playlist-cards">
{{range .Playlists}}
  <a class="playlist-card" href="/{{.Path}}/">
    {{with playlistCover .Playlist}}<img class="cover" src="{{.}}" alt="" loading="lazy">{{else}}<span class="cover placeholder"></span>{{end}}
    <span class="body"><span class="title">{{.Title}}</span><span class="meta">{{playlistMeta .Playlist}}</span>{{if .Playlist.Description}}<span class="blurb">{{.Playlist.Description}}</span>{{end}}</span>
  </a>
{{end}}
</div>
{{end}}
```

- [ ] **Step 4: Add the CSS**

Append to `internal/site/assets/site.css`:

```css
.playlist-cards { display:grid; grid-template-columns:repeat(auto-fill,minmax(18rem,1fr)); gap:1rem; margin:.4rem 0 0; }
.playlist-card { display:flex; gap:.75rem; align-items:flex-start; padding:.55rem; border-radius:8px; text-decoration:none; color:var(--fg); background:#1c1c1c; }
.playlist-card:hover { background:#232323; }
.playlist-card .cover { width:5.25rem; height:5.25rem; border-radius:6px; object-fit:cover; flex:none; background:#222; }
.playlist-card .cover.placeholder { background:linear-gradient(135deg,#2a2a2a,#1a1a1a); }
.playlist-card .body { display:flex; flex-direction:column; min-width:0; }
.playlist-card .title { font-weight:600; }
.playlist-card .meta { color:var(--muted); font-size:.82rem; margin-top:.15rem; }
.playlist-card .blurb { color:#c2c2c2; font-size:.85rem; margin-top:.35rem; }
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/site/ -run 'TestRenderSite|TestRenderCardBlurb|TestRenderYearHeaders'`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/site/templates/landing.html internal/site/assets/site.css internal/site/render_test.go
git commit -m "feat(site): render playlists as cover-art media cards

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Sidebar cover thumbnails

**Files:**
- Modify: `internal/site/assets/site-nav.js`
- Modify: `internal/site/assets/site.css`
- Test: `internal/site/assets_test.go`

**Interfaces:**
- Consumes: `IndexNode.Image` → JSON `image` (Task 2).
- Produces: `<img class="nav-cover">` beside each leaf title in `byom-site-nav`.

- [ ] **Step 1: Write the failing test**

In `internal/site/assets_test.go`, add to `TestWriteAssetsAndCNAME` after the existing `site-nav.js` content check:

```go
	if !strings.Contains(string(js), "nav-cover") {
		t.Error("site-nav.js missing cover thumbnail rendering")
	}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/site/ -run TestWriteAssetsAndCNAME`
Expected: FAIL — `nav-cover` not found.

- [ ] **Step 3: Update `site-nav.js`**

In `internal/site/assets/site-nav.js`, in the leaf loop inside `render()`, replace:

```js
      const active = n.path === here ? ' aria-current="page"' : '';
      const meta = n.meta ? `<span class="nav-meta">${esc(n.meta)}</span>` : '';
      items += `<li><a href="${esc(n.path)}"${active}>${esc(n.title)}</a>${meta}</li>`;
```

with:

```js
      const active = n.path === here ? ' aria-current="page"' : '';
      const meta = n.meta ? `<span class="nav-meta">${esc(n.meta)}</span>` : '';
      const cover = n.image ? `<img class="nav-cover" src="${esc(n.image)}" alt="" loading="lazy">` : '';
      items += `<li><a class="nav-leaf" href="${esc(n.path)}"${active}>${cover}<span class="nav-text">${esc(n.title)}${meta}</span></a></li>`;
```

- [ ] **Step 4: Add the CSS**

Append to `internal/site/assets/site.css`:

```css
.site-nav .nav-leaf { display:flex; gap:.5rem; align-items:center; }
.site-nav .nav-cover { width:2.2rem; height:2.2rem; border-radius:4px; object-fit:cover; flex:none; background:#222; }
.site-nav .nav-text { display:flex; flex-direction:column; min-width:0; }
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/site/ -run TestWriteAssetsAndCNAME`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/site/assets/site-nav.js internal/site/assets/site.css internal/site/assets_test.go
git commit -m "feat(site): cover thumbnails in sidebar nav

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Full verification + manual eyeball

**Files:** none (verification only).

- [ ] **Step 1: Full gate**

Run: `make lint && make test && make build`
Expected: all pass, no errcheck/gofumpt findings.

- [ ] **Step 2: Build the real site and inspect**

Run (uses the local hub in `./playlists`; base URL is only used for canonical/OG):

```bash
./byom-sync site --input ./playlists --out ./tmp/site-preview --base-url https://mix.test
```

Expected: exits 0. Then confirm:
- `ls ./tmp/site-preview/art` shows copied cover files.
- `grep -c 'playlist-card' ./tmp/site-preview/index.html` is > 0.
- `grep -o 'class="cover"[^>]*' ./tmp/site-preview/index.html | head` shows `/art/...` (self-hosted) and/or remote `http` srcs.
- Open `./tmp/site-preview/index.html` in a browser: cards show covers with placeholders where art is absent; open a playlist page and confirm sidebar thumbnails render.

- [ ] **Step 3: Clean up the preview**

Run: `rm -rf ./tmp/site-preview`

---

## Notes for the implementer

- `internal/site/tree_test.go` provides `writeFixtureHub(t)` and `mustWrite(t, path, body)` — reuse them; don't invent new fixtures unless a test needs specific data (Task 4's blurb test does).
- The fixture's `2014-top-songs` leaf has a remote track image and no `image_file`, so it exercises the remote-fallback branch; `bleep-bloop-bop` has no image at all, exercising the placeholder branch.
- Directories keep their existing `tree-list` rendering in `landing.html`; only playlist leaves become cards.
- `siteCover` is deliberately filesystem-free (pure). The only code that touches disk is `WriteCoverArt`, which skips missing sources.
