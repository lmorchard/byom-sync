# Site Cover-Art Cards Implementation Plan (UI layer)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render playlist cover art as media cards on the index/folder pages and as thumbnails in the sidebar nav, reusing `main`'s existing hero/mosaic/CopyArt infrastructure.

**Architecture:** A root-relative `coverHref` resolver (which `playlistImage` is refactored to delegate to) feeds three consumers: the `playlistCover` template func (cards), the `IndexNode.Image` JSON field (sidebar), and — via delegation — the existing `og:image`. No new art-copy step: `main`'s `GenerateMosaics` + `CopyArt` already place every hero in the output before rendering.

**Tech Stack:** Go 1.25, `html/template`, vanilla JS custom element (`byom-site-nav`), CSS. Standard-library `testing`.

## Global Constraints

- Formatting via `gofumpt` (run `make format`); lint via golangci-lint v2 (`make lint`); errcheck strict (`_ =` for ignored returns).
- Verify with `make lint && make test && make build`.
- Commit trailer (verbatim): `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
- Branch: `feat/site-cover-cards` (already created off `origin/main`).
- Scope: `internal/site/` only. Do NOT modify `art.go`/`CopyArt`, `mosaic.go`/`GenerateMosaics`, the hero/mosaic model, or the player/JSPF path.
- The `playlistImage` refactor MUST be behavior-preserving — the existing `TestMetaHelpers` and `TestPlaylistImage_PrefersDeployedLocalArt` cases must pass unchanged.

---

### Task 1: `coverHref` resolver + `playlistImage` delegation + template func + `IndexNode.Image`

**Files:**
- Modify: `internal/site/meta.go` (add `coverHref`, refactor `playlistImage`)
- Modify: `internal/site/render.go` (register `playlistCover` func)
- Modify: `internal/site/index.go` (`IndexNode.Image` + populate)
- Test: `internal/site/meta_test.go`, `internal/site/index_test.go`

**Interfaces:**
- Produces:
  - `func coverHref(p *playlist.Playlist) string` — root-relative site path (leading `/`) for a local hero/track file, remote URL as-is, or `""`.
  - `playlistImage(p, baseURL)` unchanged externally (now delegates to `coverHref`).
  - Template func `playlistCover` → `coverHref`.
  - `IndexNode.Image string` (JSON `image,omitempty`), populated from `coverHref` for leaves.

- [ ] **Step 1: Write the failing tests**

Add to `internal/site/meta_test.go`:

```go
func TestCoverHref(t *testing.T) {
	cases := []struct {
		name string
		p    *playlist.Playlist
		want string
	}{
		{"playlist hero file → root-relative", &playlist.Playlist{ImageFile: "art/aa/x.jpg"}, "/art/aa/x.jpg"},
		{"playlist remote image passthrough", &playlist.Playlist{Image: "http://img/pl.jpg"}, "http://img/pl.jpg"},
		{"first track local beats earlier remote", &playlist.Playlist{Tracks: []playlist.Track{{Image: "http://img/0.jpg"}, {ImageFile: "art/bb/y.jpg"}}}, "/art/bb/y.jpg"},
		{"first track remote fallback", &playlist.Playlist{Tracks: []playlist.Track{{}, {Image: "http://img/2.jpg"}}}, "http://img/2.jpg"},
		{"nothing", &playlist.Playlist{Tracks: []playlist.Track{{}}}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := coverHref(tc.p); got != tc.want {
				t.Errorf("coverHref = %q, want %q", got, tc.want)
			}
		})
	}
}
```

Add to `internal/site/index_test.go` (fixture leaf `2014-top-songs` has a remote track image and no `image_file`; `WriteIndexJSON` does not run `GenerateMosaics`, so its cover resolves to that remote URL):

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

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/site/ -run 'TestCoverHref|TestIndexNodeImage'`
Expected: FAIL — `undefined: coverHref`; `nodes[1].Image undefined`.

- [ ] **Step 3: Implement**

In `internal/site/meta.go`, add `coverHref` and refactor `playlistImage`. The current `playlistImage` body (with its inline `abs` closure and precedence chain) is replaced by delegation:

```go
// coverHref resolves a playlist's cover as a root-relative site path (leading
// "/") for a local file, the remote URL as-is, or "" when none. Precedence
// matches playlistImage: playlist hero, then first track. GenerateMosaics
// populates ImageFile for cover-less playlists before rendering, so this is
// almost always the (mosaic or explicit) hero.
func coverHref(p *playlist.Playlist) string {
	if p.ImageFile != "" {
		return "/" + p.ImageFile
	}
	if p.Image != "" {
		return p.Image
	}
	for _, t := range p.Tracks {
		if t.ImageFile != "" {
			return "/" + t.ImageFile
		}
	}
	for _, t := range p.Tracks {
		if t.Image != "" {
			return t.Image
		}
	}
	return ""
}

// playlistImage returns the playlist cover as an absolute URL for og:image,
// prefixing the deployed baseURL onto a root-relative local path and passing a
// remote URL through unchanged.
func playlistImage(p *playlist.Playlist, baseURL string) string {
	href := coverHref(p)
	if strings.HasPrefix(href, "/") {
		return strings.TrimRight(baseURL, "/") + href
	}
	return href
}
```

Keep the `strings` import (still used). Register the func in `internal/site/render.go`'s `NewRenderer` FuncMap:

```go
		"playlistCover": coverHref,
```

In `internal/site/index.go`, add to `IndexNode` (after `Meta`):

```go
	Image    string      `json:"image,omitempty"` // resolved cover href (leaves only)
```

and in `toIndexNodes`, inside the `if !c.IsDir {` block:

```go
		n.Image = coverHref(c.Playlist)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/site/ -run 'TestCoverHref|TestIndexNodeImage|TestMetaHelpers|TestPlaylistImage'`
Expected: PASS — including the pre-existing `TestMetaHelpers` and `TestPlaylistImage_PrefersDeployedLocalArt` (refactor is behavior-preserving).

- [ ] **Step 5: Format, lint, commit**

Run: `make format && make lint && make test` (all clean).

```bash
git add internal/site/meta.go internal/site/render.go internal/site/index.go internal/site/meta_test.go internal/site/index_test.go
git commit -m "feat(site): coverHref resolver + cover href in site-index.json

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Index/folder media cards (template + CSS)

**Files:**
- Modify: `internal/site/templates/landing.html`
- Modify: `internal/site/assets/site.css`
- Test: `internal/site/render_test.go`

**Interfaces:**
- Consumes: `playlistCover` template func (Task 1), `playlistMeta`, `dirsOf`, `yearGroupsOf`, `Node.Playlist.Description`.

- [ ] **Step 1: Adjust/add the failing tests**

In `internal/site/render_test.go`, `TestRenderSite`: the leaf assertion changes from the old list markup. Find and replace:

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

(`read` is defined at the top of `TestRenderSite` — line ~34 — and `read("synthpop/index.html")` is already used later in the same test, so both are in scope.)

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
	if strings.Count(s, `class="blurb"`) != 1 {
		t.Errorf("expected exactly one blurb, got %d", strings.Count(s, `class="blurb"`))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/site/ -run 'TestRenderSite|TestRenderCardBlurb'`
Expected: FAIL — old markup absent / `playlist-card` not found.

- [ ] **Step 3: Update the template**

In `internal/site/templates/landing.html`, replace ONLY the year-group leaf block (the directory `tree-list` block above it and the `<h2 class="year">` header stay unchanged):

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

with:

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

- [ ] **Step 5: Run tests, format, lint, commit**

Run: `go test ./internal/site/ -run 'TestRenderSite|TestRenderCardBlurb|TestRenderYearHeaders'` → PASS.
Then `make format && make lint && make test` (all clean).

```bash
git add internal/site/templates/landing.html internal/site/assets/site.css internal/site/render_test.go
git commit -m "feat(site): render playlists as cover-art media cards

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Sidebar cover thumbnails

**Files:**
- Modify: `internal/site/assets/site-nav.js`
- Modify: `internal/site/assets/site.css`
- Test: `internal/site/assets_test.go`

**Interfaces:**
- Consumes: `IndexNode.Image` → JSON `image` (Task 1).

- [ ] **Step 1: Write the failing test**

In `internal/site/assets_test.go`, in `TestWriteAssetsAndCNAME`, after the existing `site-nav.js` content check add:

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

- [ ] **Step 5: Run test, format, lint, commit**

Run: `go test ./internal/site/ -run TestWriteAssetsAndCNAME` → PASS.
Then `make format && make lint && make test` (all clean).

```bash
git add internal/site/assets/site-nav.js internal/site/assets/site.css internal/site/assets_test.go
git commit -m "feat(site): cover thumbnails in sidebar nav

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Full verification + manual eyeball

**Files:** none (verification only).

- [ ] **Step 1: Full gate** — `make lint && make test && make build` → all pass.

- [ ] **Step 2: Build the real site and inspect**

```bash
./byom-sync site --input ./playlists --out ./tmp/site-preview --base-url http://localhost:8899
```

Confirm:
- `grep -c 'playlist-card' ./tmp/site-preview/index.html` > 0.
- Cover `src`s are `/art/...` (self-hosted; mosaics live under `/art/mosaic/`) — and every referenced `/art/...` file exists in the output.
- `grep -c 'nav-cover' ./tmp/site-preview/assets/site-nav.js` == 1.
- The `<hub>/art` store is NOT rendered as a playlist folder (main's #30 fix).
- Serve (`python3 -m http.server 8899` in `./tmp/site-preview`) and eyeball the landing cards + a playlist page's sidebar thumbnails.

- [ ] **Step 3: Clean up** — `rm -rf ./tmp/site-preview`.

---

## Notes for the implementer

- `writeFixtureHub(t)` / `mustWrite` / `testSite()` live in the package — reuse them.
- Render tests call `BuildTree` + `RenderSite` directly (NOT `site.Build`), so `GenerateMosaics` does not run in them; the fixture's cover resolution falls to the first track's remote image, matching the assertions above.
- Directories keep their existing `tree-list` rendering in `landing.html`; only playlist leaves become cards.
- Do not touch `art.go`, `mosaic.go`, or their tests.
