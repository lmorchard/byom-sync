# byom-sync Static Site Generator — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `byom-sync site` subcommand that compiles the playlist hub into a static mini-site — one page per playlist embedding `<byom-player>`, organized by a tree mirroring hub subdirectories, plus a shared nav, Open Graph metadata, and an RSS feed.

**Architecture:** A new `internal/site` package does the work: a recursive walk builds a `Node` tree from the hub; a renderer emits per-playlist JSPF (reusing `export.JSPFExporter`) + HTML pages from embedded `html/template` files; a shared `site-index.json` + embedded `<byom-site-nav>` vanilla web component power interior-page navigation; `gorilla/feeds` produces RSS; `goldmark` renders `index.md`/`README.md`. A thin `cmd/site.go` reads a `site:` config block (Viper) and calls `site.Build`.

**Tech Stack:** Go 1.25 · Cobra · Viper · `html/template` + `//go:embed` · `github.com/yuin/goldmark` (markdown→HTML) · `github.com/gorilla/feeds` (RSS) · reuses `internal/playlist` + `internal/export`.

## Global Constraints

- Module path: `github.com/lmorchard/byom-sync`; Go `1.25.0`.
- No cgo. New deps must be pure Go (`goldmark`, `gorilla/feeds` both are).
- `tsconfig`-style rule N/A (Go repo). Lint: golangci-lint v2 (**errcheck strict** — use `_ =` for intentionally-ignored returns like `fmt.Fprintln`, `w.Write`).
- Formatting: `gofumpt` (via `make format`). Verify with `make lint && make test && make build`.
- Do **not** modify `playlist.Load`, `export.Run`, or `sync`/`export` behavior. The recursive walk is site-local.
- Commit trailer on every commit: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
- Provider config is owned by the player; the generator only sets `provider`, `providers`, and host attributes on the `<byom-player>` element.
- URL slug for a playlist = its YAML filename stem (not the title).

---

## File Structure

**Create:**
- `internal/site/tree.go` — `Node` type + `BuildTree(hubDir)` recursive walk.
- `internal/site/paths.go` — output-path derivation + `WriteJSPF` per playlist.
- `internal/site/index.go` — `WriteIndexJSON` (nav-only tree → `site-index.json`).
- `internal/site/meta.go` — metadata helpers (`playlistDesc`, `playlistImage`, `canonical`).
- `internal/site/render.go` — template data structs + `Renderer` + page emission.
- `internal/site/feed.go` — `WriteFeed` (RSS via gorilla/feeds).
- `internal/site/assets.go` — copy embedded static assets + `CNAME`.
- `internal/site/site.go` — `Options` struct + `Build(opts)` orchestrator.
- `internal/site/templates.go` — `embed.FS` for `templates/` + `assets/`.
- `internal/site/templates/partials.html` — `head`, `nav`, `footer`, `player` partials.
- `internal/site/templates/landing.html`
- `internal/site/templates/folder.html`
- `internal/site/templates/playlist.html`
- `internal/site/templates/embed.html`
- `internal/site/assets/site.css`
- `internal/site/assets/site-nav.js` — the `<byom-site-nav>` component.
- `cmd/site.go` — Cobra command wiring.
- `.github/workflows/example-site-deploy.yml` — example content-repo deploy workflow.
- Per-file `_test.go` alongside each Go source.

**Modify:**
- `cmd/root.go:74-85` — add `site.*` Viper defaults.
- `AGENTS.md` — document the `site` subcommand under Commands/Layout.

---

## Task 1: Tree model + recursive hub walk

**Files:**
- Create: `internal/site/tree.go`
- Test: `internal/site/tree_test.go`

**Interfaces:**
- Produces:
  - `type Node struct { Name, Title, Path string; IsDir bool; IntroMD string; Playlist *playlist.Playlist; Children []*Node }`
  - `func BuildTree(hubDir string) (*Node, error)` — returns the root node (`IsDir: true`, `Path: ""`). Root's `IntroMD` comes from `index.md`; each subdirectory node's `IntroMD` comes from its `README.md`. Playlist leaves carry the loaded `*playlist.Playlist`; `Name` is the filename stem, `Title` is `Playlist.Title`, `Path` is the slash-joined slug path from root. Children are sorted: directories first, then playlists, each alphabetically by `Name`.

- [ ] **Step 1: Write the failing test**

```go
package site

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFixtureHub(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "index.md"), "# mixtapes\n\nWelcome.\n")
	mustWrite(t, filepath.Join(dir, "2014-top-songs.yaml"),
		"spotify_id: abc\ntitle: 2014 Top Songs\ncreator: les\ntracks:\n  - {title: T1, artist: A1, image: 'http://img/1.jpg'}\n")
	sp := filepath.Join(dir, "synthpop")
	if err := os.MkdirAll(sp, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(sp, "README.md"), "Synthpop picks.\n")
	mustWrite(t, filepath.Join(sp, "bleep-bloop-bop.yaml"),
		"title: Bleep Bloop Bop\ncreator: les\ntracks:\n  - {title: T2, artist: A2}\n")
	return dir
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildTree(t *testing.T) {
	root, err := BuildTree(writeFixtureHub(t))
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}
	if !root.IsDir || root.Path != "" {
		t.Fatalf("root: IsDir=%v Path=%q", root.IsDir, root.Path)
	}
	if root.IntroMD == "" {
		t.Error("root IntroMD should come from index.md")
	}
	// Directories sort before playlists.
	if len(root.Children) != 2 {
		t.Fatalf("root children = %d, want 2", len(root.Children))
	}
	if root.Children[0].Name != "synthpop" || !root.Children[0].IsDir {
		t.Errorf("first child = %q (dir=%v), want synthpop dir", root.Children[0].Name, root.Children[0].IsDir)
	}
	leaf := root.Children[1]
	if leaf.Name != "2014-top-songs" || leaf.Path != "2014-top-songs" || leaf.Title != "2014 Top Songs" {
		t.Errorf("leaf = %+v", leaf)
	}
	if leaf.Playlist == nil || len(leaf.Playlist.Tracks) != 1 {
		t.Error("leaf should carry loaded playlist")
	}
	nested := root.Children[0].Children[0]
	if nested.Path != "synthpop/bleep-bloop-bop" {
		t.Errorf("nested path = %q", nested.Path)
	}
	if root.Children[0].IntroMD == "" {
		t.Error("synthpop IntroMD should come from README.md")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/lorchard/devel/byom-sync-mixtapes-site && go test ./internal/site/ -run TestBuildTree -v`
Expected: FAIL — `undefined: BuildTree` (package doesn't compile).

- [ ] **Step 3: Write the implementation**

```go
// Package site compiles the playlist hub into a static, navigable web site:
// one page per playlist embedding <byom-player>, a tree mirroring the hub's
// subdirectories, a shared nav index, Open Graph metadata, and an RSS feed.
package site

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

// Node is one entry in the site tree: either a directory (IsDir) with Children,
// or a playlist leaf carrying its loaded Playlist. Path is the slash-joined slug
// path from the hub root ("" for the root itself).
type Node struct {
	Name     string
	Title    string
	Path     string
	IsDir    bool
	IntroMD  string // raw markdown from index.md (root) or README.md (subdirs)
	Playlist *playlist.Playlist
	Children []*Node
}

// BuildTree walks hubDir recursively and returns the root node.
func BuildTree(hubDir string) (*Node, error) {
	return buildDir(hubDir, "", "")
}

func buildDir(fsDir, urlPath, name string) (*Node, error) {
	node := &Node{Name: name, Title: name, Path: urlPath, IsDir: true}

	introName := "README.md"
	if urlPath == "" {
		introName = "index.md"
	}
	if data, err := os.ReadFile(filepath.Join(fsDir, introName)); err == nil {
		node.IntroMD = string(data)
	}

	entries, err := os.ReadDir(fsDir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			child, err := buildDir(
				filepath.Join(fsDir, e.Name()),
				path.Join(urlPath, e.Name()),
				e.Name(),
			)
			if err != nil {
				return nil, err
			}
			node.Children = append(node.Children, child)
			continue
		}
		if !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".yaml")
		p, err := playlist.LoadFile(filepath.Join(fsDir, e.Name()))
		if err != nil {
			return nil, err
		}
		node.Children = append(node.Children, &Node{
			Name:     stem,
			Title:    p.Title,
			Path:     path.Join(urlPath, stem),
			Playlist: &p,
		})
	}

	sort.SliceStable(node.Children, func(i, j int) bool {
		a, b := node.Children[i], node.Children[j]
		if a.IsDir != b.IsDir {
			return a.IsDir // directories first
		}
		return a.Name < b.Name
	})
	return node, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/site/ -run TestBuildTree -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/site/tree.go internal/site/tree_test.go
git commit -m "feat(site): recursive hub walk into a Node tree

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Output paths + per-playlist JSPF emission

**Files:**
- Create: `internal/site/paths.go`
- Test: `internal/site/paths_test.go`

**Interfaces:**
- Consumes: `Node`, `BuildTree` (Task 1); `export.JSPFExporter` (existing).
- Produces:
  - `func pageDir(outDir string, n *Node) string` — the on-disk directory for a node's `index.html` (`outDir` for root, else `outDir/<Path>`).
  - `func walkPlaylists(root *Node, fn func(*Node) error) error` — depth-first visit of every playlist leaf.
  - `func WriteJSPF(outDir string, root *Node) error` — writes `<pageDir>/playlist.jspf.json` for each leaf via `export.JSPFExporter`.

- [ ] **Step 1: Write the failing test**

```go
package site

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteJSPF(t *testing.T) {
	root, err := BuildTree(writeFixtureHub(t))
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := WriteJSPF(out, root); err != nil {
		t.Fatalf("WriteJSPF: %v", err)
	}
	for _, rel := range []string{
		"2014-top-songs/playlist.jspf.json",
		"synthpop/bleep-bloop-bop/playlist.jspf.json",
	} {
		data, err := os.ReadFile(filepath.Join(out, rel))
		if err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Errorf("%s not valid JSON: %v", rel, err)
		}
		if _, ok := doc["playlist"]; !ok {
			t.Errorf("%s missing playlist key", rel)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/site/ -run TestWriteJSPF -v`
Expected: FAIL — `undefined: WriteJSPF`.

- [ ] **Step 3: Write the implementation**

```go
package site

import (
	"os"
	"path/filepath"

	"github.com/lmorchard/byom-sync/internal/export"
)

// pageDir returns the on-disk directory that holds a node's index.html.
func pageDir(outDir string, n *Node) string {
	if n.Path == "" {
		return outDir
	}
	return filepath.Join(outDir, filepath.FromSlash(n.Path))
}

// walkPlaylists visits every playlist leaf in the tree, depth-first.
func walkPlaylists(root *Node, fn func(*Node) error) error {
	for _, c := range root.Children {
		if c.IsDir {
			if err := walkPlaylists(c, fn); err != nil {
				return err
			}
			continue
		}
		if err := fn(c); err != nil {
			return err
		}
	}
	return nil
}

// WriteJSPF writes a playlist.jspf.json next to each playlist page, reusing the
// tested JSPF exporter.
func WriteJSPF(outDir string, root *Node) error {
	return walkPlaylists(root, func(n *Node) error {
		dir := pageDir(outDir, n)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		return export.JSPFExporter{}.Export(*n.Playlist, filepath.Join(dir, "playlist.jspf.json"), nil)
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/site/ -run TestWriteJSPF -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/site/paths.go internal/site/paths_test.go
git commit -m "feat(site): emit per-playlist JSPF via the existing exporter

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: `site-index.json` navigation data

**Files:**
- Create: `internal/site/index.go`
- Test: `internal/site/index_test.go`

**Interfaces:**
- Consumes: `Node`, `pageDir` helper.
- Produces:
  - `type IndexNode struct { Name, Title, Path string `json:"…"`; IsDir bool `json:"isDir"`; Children []IndexNode `json:"children,omitempty"` }`
  - `func WriteIndexJSON(outDir string, root *Node) error` — writes `<outDir>/site-index.json` containing the nav-only tree (no track data). URLs in `Path` are absolute-from-root with a leading `/` and trailing `/` (e.g. `/synthpop/bleep-bloop-bop/`); root's children only (root itself is implicit).

- [ ] **Step 1: Write the failing test**

```go
package site

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteIndexJSON(t *testing.T) {
	root, err := BuildTree(writeFixtureHub(t))
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := WriteIndexJSON(out, root); err != nil {
		t.Fatalf("WriteIndexJSON: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(out, "site-index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var nodes []IndexNode
	if err := json.Unmarshal(data, &nodes); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(nodes) != 2 || !nodes[0].IsDir || nodes[0].Name != "synthpop" {
		t.Fatalf("top-level nodes = %+v", nodes)
	}
	child := nodes[0].Children[0]
	if child.Path != "/synthpop/bleep-bloop-bop/" {
		t.Errorf("nested path = %q, want /synthpop/bleep-bloop-bop/", child.Path)
	}
	if nodes[1].Path != "/2014-top-songs/" {
		t.Errorf("leaf path = %q", nodes[1].Path)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/site/ -run TestWriteIndexJSON -v`
Expected: FAIL — `undefined: IndexNode`.

- [ ] **Step 3: Write the implementation**

```go
package site

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// IndexNode is the nav-only projection of a Node serialized into site-index.json
// (no track data). Path is absolute-from-root with leading + trailing slashes.
type IndexNode struct {
	Name     string      `json:"name"`
	Title    string      `json:"title"`
	Path     string      `json:"path"`
	IsDir    bool        `json:"isDir"`
	Children []IndexNode `json:"children,omitempty"`
}

func toIndexNodes(children []*Node) []IndexNode {
	out := make([]IndexNode, 0, len(children))
	for _, c := range children {
		out = append(out, IndexNode{
			Name:     c.Name,
			Title:    c.Title,
			Path:     "/" + c.Path + "/",
			IsDir:    c.IsDir,
			Children: toIndexNodes(c.Children),
		})
	}
	return out
}

// WriteIndexJSON writes the nav tree (root's children) to site-index.json.
func WriteIndexJSON(outDir string, root *Node) error {
	data, err := json.MarshalIndent(toIndexNodes(root.Children), "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "site-index.json"), append(data, '\n'), 0o644)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/site/ -run TestWriteIndexJSON -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/site/index.go internal/site/index_test.go
git commit -m "feat(site): emit site-index.json nav data

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Metadata helpers

**Files:**
- Create: `internal/site/meta.go`
- Test: `internal/site/meta_test.go`

**Interfaces:**
- Consumes: `playlist.Playlist`, `Node`.
- Produces:
  - `func playlistImage(p *playlist.Playlist) string` — first non-empty track `Image`, else `""`. (Playlist-level cover art is a future field; documented extension point.)
  - `func firstParagraph(md string) string` — first non-empty line/paragraph of markdown, stripped of leading `#`/whitespace (folder/description fallback for meta description).
  - `func canonical(baseURL, urlPath string) string` — `baseURL` + `/` + `urlPath` + `/`, normalized to no double slashes; root → `baseURL + "/"`.

- [ ] **Step 1: Write the failing test**

```go
package site

import (
	"testing"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

func TestMetaHelpers(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{
		{Title: "A"}, {Title: "B", Image: "http://img/b.jpg"},
	}}
	if got := playlistImage(p); got != "http://img/b.jpg" {
		t.Errorf("playlistImage = %q", got)
	}
	if got := playlistImage(&playlist.Playlist{}); got != "" {
		t.Errorf("empty playlistImage = %q, want empty", got)
	}
	if got := firstParagraph("# Heading\n\nBody text here.\n"); got != "Heading" {
		t.Errorf("firstParagraph = %q", got)
	}
	if got := canonical("https://x.test", "a/b"); got != "https://x.test/a/b/" {
		t.Errorf("canonical = %q", got)
	}
	if got := canonical("https://x.test/", ""); got != "https://x.test/" {
		t.Errorf("root canonical = %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/site/ -run TestMetaHelpers -v`
Expected: FAIL — undefined helpers.

- [ ] **Step 3: Write the implementation**

```go
package site

import (
	"strings"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

// playlistImage returns the first track image as the page's og:image. Playlist-
// level cover art (a parallel effort) can supersede this once the field lands.
func playlistImage(p *playlist.Playlist) string {
	for _, t := range p.Tracks {
		if t.Image != "" {
			return t.Image
		}
	}
	return ""
}

// firstParagraph returns the first non-empty line of markdown with any leading
// heading marker/space trimmed — a cheap meta-description fallback.
func firstParagraph(md string) string {
	for _, line := range strings.Split(md, "\n") {
		s := strings.TrimSpace(strings.TrimLeft(line, "#"))
		if s != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// canonical joins baseURL and a root-relative urlPath into an absolute URL with
// a trailing slash.
func canonical(baseURL, urlPath string) string {
	b := strings.TrimRight(baseURL, "/")
	if urlPath == "" {
		return b + "/"
	}
	return b + "/" + strings.Trim(urlPath, "/") + "/"
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/site/ -run TestMetaHelpers -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/site/meta.go internal/site/meta_test.go
git commit -m "feat(site): metadata helpers (og:image, description, canonical)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Templates, embedded assets, and page rendering

**Files:**
- Create: `internal/site/templates.go`, `internal/site/render.go`
- Create: `internal/site/templates/partials.html`, `landing.html`, `folder.html`, `playlist.html`, `embed.html`
- Create: `internal/site/assets/site.css`, `internal/site/assets/site-nav.js`
- Test: `internal/site/render_test.go`
- Add deps: `github.com/yuin/goldmark`

**Interfaces:**
- Consumes: `Node`, `pageDir`, `walkPlaylists`, meta helpers.
- Produces:
  - `type SiteMeta struct { Title, BaseURL, PlayerSrc, Provider, YouTubeSearchEndpoint, SpotifyClientID string; Providers []string }`
  - `type Crumb struct { Label, Href string }`
  - `type Renderer struct { Site SiteMeta; tmpl *template.Template }`
  - `func NewRenderer(site SiteMeta) (*Renderer, error)` — parses embedded templates with funcs `{markdown, providersCSV}`.
  - `func (r *Renderer) RenderSite(outDir string, root *Node) error` — renders landing + every folder and playlist page (+ each playlist's `embed/index.html`).
  - Player attributes emitted: `provider`, `providers` (CSV, when set), `youtube-search-endpoint`, `spotify-client-id` (when set), `src="playlist.jspf.json"`.

- [ ] **Step 1: Add goldmark**

Run: `go get github.com/yuin/goldmark@latest`
Expected: `go.mod`/`go.sum` updated.

- [ ] **Step 2: Write the embedded template + asset files**

Create `internal/site/templates/partials.html`:

```html
{{define "head"}}
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}} · {{.Site.Title}}</title>
{{if .Desc}}<meta name="description" content="{{.Desc}}">{{end}}
<link rel="canonical" href="{{.Canonical}}">
<meta property="og:type" content="website">
<meta property="og:site_name" content="{{.Site.Title}}">
<meta property="og:title" content="{{.Title}}">
{{if .Desc}}<meta property="og:description" content="{{.Desc}}">{{end}}
<meta property="og:url" content="{{.Canonical}}">
{{if .Image}}<meta property="og:image" content="{{.Image}}">{{end}}
<meta name="twitter:card" content="{{if .Image}}summary_large_image{{else}}summary{{end}}">
<link rel="stylesheet" href="/assets/site.css">
<link rel="alternate" type="application/rss+xml" title="{{.Site.Title}}" href="/feed.xml">
{{end}}

{{define "crumbs"}}
<nav class="crumbs">{{range $i, $c := .Crumbs}}{{if $i}} / {{end}}{{if $c.Href}}<a href="{{$c.Href}}">{{$c.Label}}</a>{{else}}<span>{{$c.Label}}</span>{{end}}{{end}}</nav>
{{end}}

{{define "footer"}}
<footer class="site-footer">built with <a href="https://github.com/lmorchard/byom-sync">byom-sync</a> · <a href="/feed.xml">RSS</a></footer>
{{end}}

{{define "player"}}
<byom-player provider="{{.Site.Provider}}"{{if .Site.Providers}} providers="{{providersCSV .Site.Providers}}"{{end}}{{if .Site.YouTubeSearchEndpoint}} youtube-search-endpoint="{{.Site.YouTubeSearchEndpoint}}"{{end}}{{if .Site.SpotifyClientID}} spotify-client-id="{{.Site.SpotifyClientID}}"{{end}} src="playlist.jspf.json"></byom-player>
<script type="module" src="{{.Site.PlayerSrc}}"></script>
{{end}}
```

Create `internal/site/templates/landing.html`:

```html
<!doctype html>
<html lang="en">
<head>{{template "head" .}}</head>
<body class="landing">
<header class="site-header"><h1>{{.Site.Title}}</h1></header>
<main>
{{if .Intro}}<section class="intro">{{.Intro}}</section>{{end}}
<section class="tree">{{template "treeList" .Root.Children}}</section>
</main>
{{template "footer" .}}
</body>
</html>

{{define "treeList"}}
<ul class="tree-list">
{{range .}}
  <li class="{{if .IsDir}}dir{{else}}leaf{{end}}">
    <a href="/{{.Path}}/">{{if .IsDir}}📁 {{end}}{{.Title}}</a>
    {{if .IsDir}}{{template "treeList" .Children}}{{end}}
  </li>
{{end}}
</ul>
{{end}}
```

Create `internal/site/templates/folder.html`:

```html
<!doctype html>
<html lang="en">
<head>{{template "head" .}}</head>
<body class="folder">
<header class="site-header"><a href="/">{{.Site.Title}}</a></header>
<byom-site-nav></byom-site-nav>
<script type="module" src="/assets/site-nav.js"></script>
<main>
{{template "crumbs" .}}
<h1>{{.Title}}</h1>
{{if .Intro}}<section class="intro">{{.Intro}}</section>{{end}}
<section class="tree">{{template "treeList" .Node.Children}}</section>
</main>
{{template "footer" .}}
</body>
</html>
```

(Note: `folder.html` reuses `treeList` defined in `landing.html`; both are parsed into the same template set.)

Create `internal/site/templates/playlist.html`:

```html
<!doctype html>
<html lang="en">
<head>{{template "head" .}}</head>
<body class="playlist">
<header class="site-header"><a href="/">{{.Site.Title}}</a></header>
<byom-site-nav></byom-site-nav>
<script type="module" src="/assets/site-nav.js"></script>
<main>
{{template "crumbs" .}}
{{template "player" .}}
<noscript>
  <h1>{{.Title}}</h1>
  <ol class="tracklist">{{range .Playlist.Tracks}}<li>{{.Title}} — {{.Artist}}</li>{{end}}</ol>
</noscript>
</main>
{{template "footer" .}}
</body>
</html>
```

Create `internal/site/templates/embed.html`:

```html
<!doctype html>
<html lang="en">
<head>{{template "head" .}}</head>
<body class="embed">
{{template "player" .}}
<a class="embed-attribution" href="{{.Canonical}}" target="_blank" rel="noopener">open on {{.Site.Title}} ↗</a>
</body>
</html>
```

Create `internal/site/assets/site.css`:

```css
:root { color-scheme: dark; --bg:#111; --fg:#eee; --muted:#888; --accent:#a48cff; }
* { box-sizing: border-box; }
body { margin:0; padding:1.5rem; background:var(--bg); color:var(--fg); font-family:system-ui,sans-serif; line-height:1.5; max-width:52rem; margin-inline:auto; }
a { color:var(--accent); }
.site-header h1, .site-header a { font-size:1.4rem; font-weight:700; text-decoration:none; }
.crumbs { color:var(--muted); font-size:.85rem; margin:.5rem 0 1rem; }
.tree-list { list-style:none; padding-left:1rem; }
.tree-list > li { margin:.25rem 0; }
.intro { opacity:.9; }
.site-footer { margin-top:2rem; color:var(--muted); font-size:.8rem; }
.tracklist { color:var(--muted); }
byom-player { display:block; max-width:36rem; margin:1rem 0; }
byom-site-nav { display:block; margin:1rem 0; }
body.embed { padding:.5rem; }
.embed-attribution { display:block; font-size:.75rem; color:var(--muted); margin-top:.4rem; }
```

Create `internal/site/assets/site-nav.js`:

```js
// <byom-site-nav> — renders the shared site navigation from /site-index.json,
// highlighting the current page. Kept dependency-free and self-contained so the
// byom-sync generator can emit it as a static asset (no JS build pipeline).
class ByomSiteNav extends HTMLElement {
  async connectedCallback() {
    try {
      const res = await fetch('/site-index.json');
      const nodes = await res.json();
      const here = location.pathname;
      this.innerHTML = `<nav class="site-nav">${this.render(nodes, here)}</nav>`;
    } catch (e) {
      this.innerHTML = '';
    }
  }
  render(nodes, here) {
    return `<ul>${nodes.map((n) => {
      const active = n.path === here ? ' aria-current="page"' : '';
      const label = (n.isDir ? '📁 ' : '') + n.title;
      const kids = n.children && n.children.length ? this.render(n.children, here) : '';
      return `<li><a href="${n.path}"${active}>${label}</a>${kids}</li>`;
    }).join('')}</ul>`;
  }
}
customElements.define('byom-site-nav', ByomSiteNav);
```

- [ ] **Step 3: Write the failing test**

```go
package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testSite() SiteMeta {
	return SiteMeta{
		Title: "mixtapes", BaseURL: "https://mix.test",
		PlayerSrc: "https://cdn.example/byom-player.js",
		Provider:  "youtube", Providers: []string{"youtube", "spotify"},
	}
}

func TestRenderSite(t *testing.T) {
	root, err := BuildTree(writeFixtureHub(t))
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewRenderer(testSite())
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := r.RenderSite(out, root); err != nil {
		t.Fatalf("RenderSite: %v", err)
	}
	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(out, rel))
		if err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
		return string(b)
	}
	landing := read("index.html")
	if !strings.Contains(landing, "Welcome") || !strings.Contains(landing, "/synthpop/") {
		t.Error("landing missing intro or tree link")
	}
	pl := read("synthpop/bleep-bloop-bop/index.html")
	if !strings.Contains(pl, `<byom-player`) || !strings.Contains(pl, `src="playlist.jspf.json"`) {
		t.Error("playlist page missing player tag")
	}
	if !strings.Contains(pl, `provider="youtube"`) || !strings.Contains(pl, `providers="youtube,spotify"`) {
		t.Error("player missing provider config")
	}
	if !strings.Contains(pl, `<byom-site-nav>`) {
		t.Error("playlist page missing nav component")
	}
	if !strings.Contains(pl, `property="og:title"`) {
		t.Error("playlist page missing OG tags")
	}
	embed := read("synthpop/bleep-bloop-bop/embed/index.html")
	if !strings.Contains(embed, "<byom-player") || strings.Contains(embed, "<byom-site-nav>") {
		t.Error("embed should have player but no site nav")
	}
	folder := read("synthpop/index.html")
	if !strings.Contains(folder, "Synthpop picks") {
		t.Error("folder page missing README intro")
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `go test ./internal/site/ -run TestRenderSite -v`
Expected: FAIL — `undefined: NewRenderer`.

- [ ] **Step 5: Write `templates.go`**

```go
package site

import "embed"

//go:embed templates/*.html assets/*
var embedded embed.FS
```

- [ ] **Step 6: Write `render.go`**

```go
package site

import (
	"bytes"
	"html/template"
	"os"
	"path/filepath"
	"strings"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/yuin/goldmark"
)

// SiteMeta carries site-wide settings baked into every page.
type SiteMeta struct {
	Title                 string
	BaseURL               string
	PlayerSrc             string
	Provider              string
	Providers             []string
	YouTubeSearchEndpoint string
	SpotifyClientID       string
}

// Crumb is one breadcrumb link (Href empty → plain text, i.e. current page).
type Crumb struct {
	Label string
	Href  string
}

// pageData is the base data shared by every page template.
type pageData struct {
	Site      SiteMeta
	Title     string
	Desc      string
	Image     string
	Canonical string
	Crumbs    []Crumb
}

type landingData struct {
	pageData
	Intro template.HTML
	Root  *Node
}

type folderData struct {
	pageData
	Intro template.HTML
	Node  *Node
}

type playlistData struct {
	pageData
	Playlist *playlist.Playlist
}

// Renderer holds the parsed template set and site settings.
type Renderer struct {
	Site SiteMeta
	tmpl *template.Template
}

// NewRenderer parses the embedded templates.
func NewRenderer(site SiteMeta) (*Renderer, error) {
	funcs := template.FuncMap{
		"markdown":     renderMarkdown,
		"providersCSV": func(p []string) string { return strings.Join(p, ",") },
	}
	tmpl, err := template.New("site").Funcs(funcs).ParseFS(embedded, "templates/*.html")
	if err != nil {
		return nil, err
	}
	return &Renderer{Site: site, tmpl: tmpl}, nil
}

func renderMarkdown(md string) template.HTML {
	var buf bytes.Buffer
	if err := goldmark.Convert([]byte(md), &buf); err != nil {
		return ""
	}
	return template.HTML(buf.String()) // #nosec G203 — source is our own hub files
}

// RenderSite renders the landing page plus every folder and playlist page.
func (r *Renderer) RenderSite(outDir string, root *Node) error {
	if err := r.renderLanding(outDir, root); err != nil {
		return err
	}
	return r.renderChildren(outDir, root, nil)
}

func (r *Renderer) renderLanding(outDir string, root *Node) error {
	data := landingData{
		pageData: pageData{
			Site:      r.Site,
			Title:     r.Site.Title,
			Desc:      firstParagraph(root.IntroMD),
			Canonical: canonical(r.Site.BaseURL, ""),
		},
		Intro: renderMarkdown(root.IntroMD),
		Root:  root,
	}
	return r.write(filepath.Join(outDir, "index.html"), "landing.html", data)
}

func (r *Renderer) renderChildren(outDir string, node *Node, crumbs []Crumb) error {
	for _, c := range node.Children {
		trail := append(append([]Crumb{}, crumbs...), Crumb{Label: c.Title, Href: "/" + c.Path + "/"})
		if c.IsDir {
			if err := r.renderFolder(outDir, c, withCurrentLast(trail)); err != nil {
				return err
			}
			if err := r.renderChildren(outDir, c, trail); err != nil {
				return err
			}
			continue
		}
		if err := r.renderPlaylist(outDir, c, withCurrentLast(trail)); err != nil {
			return err
		}
	}
	return nil
}

// withCurrentLast strips the href from the final crumb (the current page).
func withCurrentLast(crumbs []Crumb) []Crumb {
	out := append([]Crumb{{Label: "mixtapes", Href: "/"}}, crumbs...)
	out[len(out)-1].Href = ""
	return out
}

func (r *Renderer) renderFolder(outDir string, node *Node, crumbs []Crumb) error {
	dir := pageDir(outDir, node)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data := folderData{
		pageData: pageData{
			Site:      r.Site,
			Title:     node.Title,
			Desc:      firstParagraph(node.IntroMD),
			Canonical: canonical(r.Site.BaseURL, node.Path),
			Crumbs:    crumbs,
		},
		Intro: renderMarkdown(node.IntroMD),
		Node:  node,
	}
	return r.write(filepath.Join(dir, "index.html"), "folder.html", data)
}

func (r *Renderer) renderPlaylist(outDir string, node *Node, crumbs []Crumb) error {
	dir := pageDir(outDir, node)
	if err := os.MkdirAll(filepath.Join(dir, "embed"), 0o755); err != nil {
		return err
	}
	base := pageData{
		Site:      r.Site,
		Title:     node.Title,
		Desc:      node.Playlist.Description,
		Image:     playlistImage(node.Playlist),
		Canonical: canonical(r.Site.BaseURL, node.Path),
		Crumbs:    crumbs,
	}
	data := playlistData{pageData: base, Playlist: node.Playlist}
	if err := r.write(filepath.Join(dir, "index.html"), "playlist.html", data); err != nil {
		return err
	}
	return r.write(filepath.Join(dir, "embed", "index.html"), "embed.html", data)
}

func (r *Renderer) write(path, tmplName string, data any) error {
	var buf bytes.Buffer
	if err := r.tmpl.ExecuteTemplate(&buf, tmplName, data); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
```

- [ ] **Step 7: Run test to verify it passes**

Run: `go test ./internal/site/ -run TestRenderSite -v`
Expected: PASS. (If `providers` CSV assertion fails, confirm `providersCSV` func and the `partials.html` player block.)

- [ ] **Step 8: Commit**

```bash
git add internal/site/templates.go internal/site/render.go internal/site/templates internal/site/assets internal/site/render_test.go go.mod go.sum
git commit -m "feat(site): render landing/folder/playlist/embed pages + assets

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: Static assets + CNAME emission

**Files:**
- Create: `internal/site/assets.go`
- Test: `internal/site/assets_test.go`

**Interfaces:**
- Consumes: embedded `assets/` FS (Task 5), `SiteMeta.BaseURL`.
- Produces:
  - `func WriteAssets(outDir string) error` — copies embedded `assets/*` to `<outDir>/assets/`.
  - `func WriteCNAME(outDir, baseURL string) error` — writes `<outDir>/CNAME` with the host from `baseURL` (no-op when `baseURL` empty or has no host).

- [ ] **Step 1: Write the failing test**

```go
package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAssetsAndCNAME(t *testing.T) {
	out := t.TempDir()
	if err := WriteAssets(out); err != nil {
		t.Fatalf("WriteAssets: %v", err)
	}
	js, err := os.ReadFile(filepath.Join(out, "assets", "site-nav.js"))
	if err != nil {
		t.Fatalf("site-nav.js: %v", err)
	}
	if !strings.Contains(string(js), "customElements.define('byom-site-nav'") {
		t.Error("site-nav.js missing component registration")
	}
	if _, err := os.Stat(filepath.Join(out, "assets", "site.css")); err != nil {
		t.Errorf("site.css missing: %v", err)
	}
	if err := WriteCNAME(out, "https://mixtapes.lmorchard.com"); err != nil {
		t.Fatal(err)
	}
	cname, _ := os.ReadFile(filepath.Join(out, "CNAME"))
	if strings.TrimSpace(string(cname)) != "mixtapes.lmorchard.com" {
		t.Errorf("CNAME = %q", cname)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/site/ -run TestWriteAssetsAndCNAME -v`
Expected: FAIL — `undefined: WriteAssets`.

- [ ] **Step 3: Write the implementation**

```go
package site

import (
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
)

// WriteAssets copies the embedded assets/ directory into outDir/assets/.
func WriteAssets(outDir string) error {
	return fs.WalkDir(embedded, "assets", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dst := filepath.Join(outDir, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		data, err := embedded.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
}

// WriteCNAME writes a GitHub Pages CNAME file with the host from baseURL. It is
// a no-op when baseURL is empty or has no host.
func WriteCNAME(outDir, baseURL string) error {
	if baseURL == "" {
		return nil
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return nil
	}
	return os.WriteFile(filepath.Join(outDir, "CNAME"), []byte(u.Host+"\n"), 0o644)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/site/ -run TestWriteAssetsAndCNAME -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/site/assets.go internal/site/assets_test.go
git commit -m "feat(site): copy static assets + emit CNAME

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: RSS feed

**Files:**
- Create: `internal/site/feed.go`
- Test: `internal/site/feed_test.go`
- Add deps: `github.com/gorilla/feeds`

**Interfaces:**
- Consumes: `Node`, `walkPlaylists`, `SiteMeta`, `canonical`.
- Produces:
  - `func WriteFeed(outDir string, site SiteMeta, root *Node) error` — writes `<outDir>/feed.xml`. Items = playlists, newest first by `Playlist.DateCreated`; each item `Title`, `Link` (absolute page URL via `canonical`), `Description` (playlist `Description`), `Created` (`DateCreated`).

- [ ] **Step 1: Add gorilla/feeds**

Run: `go get github.com/gorilla/feeds@latest`
Expected: `go.mod`/`go.sum` updated.

- [ ] **Step 2: Write the failing test**

```go
package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

func TestWriteFeed(t *testing.T) {
	older := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	root := &Node{IsDir: true, Children: []*Node{
		{Name: "old", Title: "Old", Path: "old", Playlist: &playlist.Playlist{Title: "Old", DateCreated: older}},
		{Name: "new", Title: "New", Path: "new", Playlist: &playlist.Playlist{Title: "New", DateCreated: newer}},
	}}
	out := t.TempDir()
	if err := WriteFeed(out, testSite(), root); err != nil {
		t.Fatalf("WriteFeed: %v", err)
	}
	xml, err := os.ReadFile(filepath.Join(out, "feed.xml"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(xml)
	if !strings.Contains(s, "https://mix.test/new/") {
		t.Error("feed missing absolute item link")
	}
	// Newest first: "New" item appears before "Old".
	if strings.Index(s, "<title>New</title>") > strings.Index(s, "<title>Old</title>") {
		t.Error("feed items not newest-first")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/site/ -run TestWriteFeed -v`
Expected: FAIL — `undefined: WriteFeed`.

- [ ] **Step 4: Write the implementation**

```go
package site

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/gorilla/feeds"
)

// WriteFeed writes an RSS feed of playlists, newest first by DateCreated.
func WriteFeed(outDir string, site SiteMeta, root *Node) error {
	var items []*feeds.Item
	err := walkPlaylists(root, func(n *Node) error {
		items = append(items, &feeds.Item{
			Title:       n.Title,
			Link:        &feeds.Link{Href: canonical(site.BaseURL, n.Path)},
			Description: n.Playlist.Description,
			Created:     n.Playlist.DateCreated,
		})
		return nil
	})
	if err != nil {
		return err
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Created.After(items[j].Created)
	})

	feed := &feeds.Feed{
		Title:       site.Title,
		Link:        &feeds.Link{Href: canonical(site.BaseURL, "")},
		Description: site.Title + " — playlists",
		Items:       items,
	}
	rss, err := feed.ToRss()
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "feed.xml"), []byte(rss), 0o644)
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/site/ -run TestWriteFeed -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/site/feed.go internal/site/feed_test.go go.mod go.sum
git commit -m "feat(site): RSS feed of playlists (newest first)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 8: `Build` orchestrator

**Files:**
- Create: `internal/site/site.go`
- Test: `internal/site/site_test.go`

**Interfaces:**
- Consumes: all prior functions.
- Produces:
  - `type Options struct { HubDir, OutDir string; Site SiteMeta }`
  - `func Build(opts Options) error` — clears/creates `OutDir`, `BuildTree`, then `WriteJSPF`, `WriteIndexJSON`, `RenderSite`, `WriteAssets`, `WriteCNAME`, `WriteFeed`.

- [ ] **Step 1: Write the failing test**

```go
package site

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildEndToEnd(t *testing.T) {
	out := t.TempDir()
	err := Build(Options{
		HubDir: writeFixtureHub(t),
		OutDir: out,
		Site:   testSite(),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, rel := range []string{
		"index.html",
		"site-index.json",
		"feed.xml",
		"CNAME",
		"assets/site.css",
		"assets/site-nav.js",
		"2014-top-songs/index.html",
		"2014-top-songs/playlist.jspf.json",
		"2014-top-songs/embed/index.html",
		"synthpop/index.html",
		"synthpop/bleep-bloop-bop/index.html",
		"synthpop/bleep-bloop-bop/playlist.jspf.json",
	} {
		if _, err := os.Stat(filepath.Join(out, rel)); err != nil {
			t.Errorf("missing output %s: %v", rel, err)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/site/ -run TestBuildEndToEnd -v`
Expected: FAIL — `undefined: Build`.

- [ ] **Step 3: Write the implementation**

```go
package site

import "os"

// Options configures a site build.
type Options struct {
	HubDir string
	OutDir string
	Site   SiteMeta
}

// Build compiles the hub at opts.HubDir into a static site at opts.OutDir.
func Build(opts Options) error {
	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return err
	}
	root, err := BuildTree(opts.HubDir)
	if err != nil {
		return err
	}
	r, err := NewRenderer(opts.Site)
	if err != nil {
		return err
	}
	if err := WriteJSPF(opts.OutDir, root); err != nil {
		return err
	}
	if err := WriteIndexJSON(opts.OutDir, root); err != nil {
		return err
	}
	if err := r.RenderSite(opts.OutDir, root); err != nil {
		return err
	}
	if err := WriteAssets(opts.OutDir); err != nil {
		return err
	}
	if err := WriteCNAME(opts.OutDir, opts.Site.BaseURL); err != nil {
		return err
	}
	return WriteFeed(opts.OutDir, opts.Site, root)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/site/ -run TestBuildEndToEnd -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/site/site.go internal/site/site_test.go
git commit -m "feat(site): Build orchestrator wiring the full pipeline

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 9: `site` command + config + docs + example workflow

**Files:**
- Create: `cmd/site.go`
- Modify: `cmd/root.go` (add `site.*` Viper defaults after line 85)
- Create: `.github/workflows/example-site-deploy.yml`
- Modify: `AGENTS.md`
- Test: `cmd/site_test.go`

**Interfaces:**
- Consumes: `site.Build`, `site.Options`, `site.SiteMeta`; Viper keys `dir`, `site.*`.
- Produces: `byom-sync site` command. Flags: `--input` (default Viper `dir`), `--out` (default `site.out_dir`), `--base-url` (default `site.base_url`). Errors if the resolved base URL is empty.

- [ ] **Step 1: Add Viper defaults**

In `cmd/root.go`, after the existing `viper.SetDefault(...)` block (line 85), add:

```go
	viper.SetDefault("site.title", "mixtapes")
	viper.SetDefault("site.out_dir", "./dist")
	viper.SetDefault("site.provider", "youtube")
	viper.SetDefault("site.player_src", "https://cdn.jsdelivr.net/gh/lmorchard/byom-player@dist/byom-player.js")
```

- [ ] **Step 2: Write the failing test**

```go
package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestSiteCommandBuilds(t *testing.T) {
	hub := t.TempDir()
	if err := os.WriteFile(filepath.Join(hub, "x.yaml"),
		[]byte("title: X\ncreator: me\ntracks:\n  - {title: T, artist: A}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()

	viper.Reset()
	viper.Set("dir", hub)
	viper.Set("site.out_dir", out)
	viper.Set("site.base_url", "https://x.test")
	viper.Set("site.title", "mixtapes")
	viper.Set("site.player_src", "https://cdn/p.js")
	viper.Set("site.provider", "youtube")

	if err := runSite(nil, nil); err != nil {
		t.Fatalf("runSite: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "index.html")); err != nil {
		t.Errorf("no index.html: %v", err)
	}
}

func TestSiteCommandRequiresBaseURL(t *testing.T) {
	viper.Reset()
	viper.Set("dir", t.TempDir())
	viper.Set("site.out_dir", t.TempDir())
	// no base_url
	if err := runSite(nil, nil); err == nil {
		t.Error("expected error when base_url is empty")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./cmd/ -run TestSiteCommand -v`
Expected: FAIL — `undefined: runSite`.

- [ ] **Step 4: Write `cmd/site.go`**

```go
package cmd

import (
	"fmt"

	"github.com/lmorchard/byom-sync/internal/site"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	siteInput   string
	siteOut     string
	siteBaseURL string
)

var siteCmd = &cobra.Command{
	Use:   "site",
	Short: "Compile the playlist hub into a static web site",
	Long: `Compile the local playlist "hub" (YAML) into a navigable static site:
one page per playlist embedding <byom-player>, a tree mirroring hub
subdirectories, a shared nav, Open Graph metadata, and an RSS feed.

Configure defaults under a "site:" block in byom-sync.yaml. --base-url (or
site.base_url) is required.`,
	RunE: runSite,
}

func runSite(_ *cobra.Command, _ []string) error {
	hub := siteInput
	if hub == "" {
		hub = viper.GetString("dir")
	}
	out := siteOut
	if out == "" {
		out = viper.GetString("site.out_dir")
	}
	baseURL := siteBaseURL
	if baseURL == "" {
		baseURL = viper.GetString("site.base_url")
	}
	if baseURL == "" {
		return fmt.Errorf("site: base_url is required (set site.base_url or pass --base-url)")
	}

	return site.Build(site.Options{
		HubDir: hub,
		OutDir: out,
		Site: site.SiteMeta{
			Title:                 viper.GetString("site.title"),
			BaseURL:               baseURL,
			PlayerSrc:             viper.GetString("site.player_src"),
			Provider:              viper.GetString("site.provider"),
			Providers:             viper.GetStringSlice("site.providers"),
			YouTubeSearchEndpoint: viper.GetString("site.youtube_search_endpoint"),
			SpotifyClientID:       viper.GetString("site.spotify_client_id"),
		},
	})
}

func init() {
	rootCmd.AddCommand(siteCmd)
	siteCmd.Flags().StringVar(&siteInput, "input", "", "hub directory (default: config `dir`)")
	siteCmd.Flags().StringVar(&siteOut, "out", "", "output directory (default: config `site.out_dir`)")
	siteCmd.Flags().StringVar(&siteBaseURL, "base-url", "", "site base URL (default: config `site.base_url`)")
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./cmd/ -run TestSiteCommand -v`
Expected: PASS (both).
Note: `runSite` reads package-level flag vars; the tests set Viper only, leaving flags empty, so the Viper fallbacks are exercised. Reset flag vars if a prior test set them: not needed here (tests don't set them).

- [ ] **Step 6: Write the example deploy workflow**

Create `.github/workflows/example-site-deploy.yml`:

```yaml
# Example workflow for a CONTENT repo (your playlist hub) — not byom-sync itself.
# Copy into your hub repo, adjust paths, and enable GitHub Pages (source: Actions).
name: Deploy mixtapes site
on:
  push:
    branches: [main]
  workflow_dispatch:
permissions:
  contents: read
  pages: write
  id-token: write
jobs:
  build-deploy:
    runs-on: ubuntu-latest
    environment:
      name: github-pages
      url: ${{ steps.deploy.outputs.page_url }}
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v6
        with:
          go-version: '1.25'
      - name: Install byom-sync
        run: go install github.com/lmorchard/byom-sync@latest
      - name: Build site
        run: byom-sync site --input ./playlists --out ./dist --base-url https://mixtapes.lmorchard.com
      - uses: actions/upload-pages-artifact@v3
        with:
          path: ./dist
      - id: deploy
        uses: actions/deploy-pages@v4
```

- [ ] **Step 7: Update AGENTS.md**

Add to the Commands section a line for `site`, and to Layout a bullet:
`internal/site/` — the static site generator (`byom-sync site`): recursive hub walk → per-playlist JSPF + HTML pages embedding `<byom-player>`, `site-index.json` + `<byom-site-nav>`, OG metadata, RSS. Reuses `export.JSPFExporter`.

- [ ] **Step 8: Full verification**

Run: `make lint && make test && make build`
Expected: all pass, no lint findings.

- [ ] **Step 9: Commit**

```bash
git add cmd/site.go cmd/root.go cmd/site_test.go .github/workflows/example-site-deploy.yml AGENTS.md
git commit -m "feat(site): byom-sync site command + config + example deploy workflow

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 10: Real-hub smoke test + notes

**Files:**
- Modify: `docs/dev-sessions/2026-07-10-1349-mixtapes-site/notes.md` (create)

**Interfaces:** none (manual verification).

- [ ] **Step 1: Build against the real hub**

Run:
```bash
cd /Users/lorchard/devel/byom-sync-mixtapes-site
go run . site --input ./playlists --out /tmp/mixtapes-dist --base-url https://mixtapes.lmorchard.com
find /tmp/mixtapes-dist -maxdepth 2 -type f | sort
```
Expected: `index.html`, `site-index.json`, `feed.xml`, `CNAME`, `assets/`, and per-playlist dirs with `index.html` + `playlist.jspf.json` + `embed/index.html`.

- [ ] **Step 2: Serve + eyeball with the real player**

Run: `cd /tmp/mixtapes-dist && python3 -m http.server 8099`
Open `http://localhost:8099/` — confirm the tree renders, a playlist page mounts `<byom-player>` from the CDN, the sidebar populates from `site-index.json`, and `/…/embed/` shows the chrome-less player. (Provider playback needs real credentials via the player's ⚙ panel — availability of playback is out of scope; the mount + JSPF load are what we verify.)

- [ ] **Step 3: Capture notes**

Write `notes.md` with: what was built, any deviations from the spec, follow-ups (sitemap.xml, explicit `published:` field, `sync` subdirectory management, playlist-level cover art wiring once the field lands), and the verification results.

- [ ] **Step 4: Commit**

```bash
git add docs/dev-sessions/2026-07-10-1349-mixtapes-site/notes.md
git commit -m "docs(session): mixtapes-site build notes + verification

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Self-Review

**Spec coverage:**
- Placement (`byom-sync site` subcommand) → Task 9. ✓
- Recursive walk / tree → Task 1. ✓
- Output layout + filename-stem slugs → Tasks 1–2 (`Name` = stem, `Path`). ✓
- Per-playlist JSPF via existing exporter → Task 2. ✓
- `site:` config block → Task 9 (defaults in root.go, read in site.go). ✓
- Page types (base/landing/folder/playlist/embed) + templating → Task 5. ✓
- OG/Twitter metadata + `<noscript>` → Task 5 (`head` partial, playlist template). ✓
- Provider wiring (provider/providers/host attrs; player-owned) → Task 5 (`player` partial). ✓
- Player CDN loading (`player_src`) → Task 5 + Task 9 default. ✓
- `site-index.json` + `<byom-site-nav>` + breadcrumb floor → Tasks 3, 5. ✓
- RSS via gorilla/feeds → Task 7. ✓
- Assets (site.css, site-nav.js), CNAME → Tasks 5–6. ✓
- Deploy example workflow → Task 9. ✓
- Testing → each task's tests + Task 10 smoke. ✓

**Placeholder scan:** No TBD/TODO; every code step has complete code. ✓

**Type consistency:** `Node`, `IndexNode`, `SiteMeta`, `Crumb`, `pageData`/`landingData`/`folderData`/`playlistData`, `Options`, and function names (`BuildTree`, `WriteJSPF`, `WriteIndexJSON`, `NewRenderer`, `RenderSite`, `WriteAssets`, `WriteCNAME`, `WriteFeed`, `Build`, `runSite`) are consistent across tasks. `pageDir`/`walkPlaylists` defined in Task 2 and reused in 3/5/7. ✓

**Note on template coupling:** `treeList` is defined in `landing.html` and reused by `folder.html`; both are parsed into one set via `ParseFS`, so the definition is available. Verified by the folder-page assertion in Task 5's test.

## Open questions / fast-follows (not in scope)

- `sitemap.xml` (deferred; low priority).
- Explicit `published:` field if `date_created` feed ordering disappoints.
- `sync` creating/maintaining hub subdirectories.
- Playlist-level cover art field → swap `playlistImage` to prefer it once it exists in `playlist.Playlist`.
- `--preview`/local mode that relaxes the `base_url` requirement.
