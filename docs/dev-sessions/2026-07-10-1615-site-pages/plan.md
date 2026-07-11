# Site Content Pages — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render a directory of markdown content pages (about/colophon/etc.) to HTML and link them from the `byom-sync site` header.

**Architecture:** A new `internal/site/pages.go` loads `*.md` from `site.pages_dir`, parses YAML frontmatter (`title`, `order`), and renders bodies with the existing `goldmark`. `Build` sets the sorted page list on `SiteMeta.Pages`; a shared `siteheader` partial renders those links into every non-embed page; a new `page.html` template renders each content page. Playlists, `site-index.json`, the sidebar, and RSS are untouched.

**Tech Stack:** Go 1.25 · `html/template` · `gopkg.in/yaml.v3` (frontmatter) · `goldmark` (already present) · reuses `internal/site` helpers (`renderMarkdown`, `firstParagraph`, `canonical`, `write`).

## Global Constraints

- Module `github.com/lmorchard/byom-sync`; Go `1.25.0`. No cgo; no new dependencies (yaml + goldmark already present).
- golangci-lint v2, **errcheck strict** — `_ =` for intentionally-ignored returns.
- gofumpt formatting (`make format`); verify with `make lint && make test && make build`.
- Commit trailer on every commit: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
- Content pages are header-only: do NOT modify playlist walk, `site-index.json`, `site-nav.js`, or `feed.go`.
- Page slug = markdown filename stem; page URL = `/slug/`.
- Missing `pages_dir` is a graceful no-op (no pages, no header nav, no error).

---

## File Structure

**Create:**
- `internal/site/pages.go` — `PageLink`, `ContentPage`, `parseFrontmatter`, `LoadPages`, `pageLinks`.
- `internal/site/pages_test.go`
- `internal/site/templates/page.html` — content-page template.

**Modify:**
- `internal/site/render.go` — add `Pages []PageLink` to `SiteMeta`; add `contentPageData` + `Renderer.RenderPages`.
- `internal/site/templates/partials.html` — add `siteheader` partial.
- `internal/site/templates/landing.html`, `folder.html`, `playlist.html` — use `{{template "siteheader" .}}`.
- `internal/site/assets/site.css` — header layout + `.site-title` / `.page-nav` styles.
- `internal/site/site.go` — `Options.PagesDir`; load pages, set `Site.Pages`, call `RenderPages`.
- `cmd/site.go` — read `site.pages_dir` (+ `--pages` flag) into `Options.PagesDir`.
- `cmd/root.go` — `viper.SetDefault("site.pages_dir", "./pages")`.
- `AGENTS.md` — note content pages in the `internal/site/` bullet.
- `internal/site/render_test.go` — header-nav assertions.

---

## Task 1: Frontmatter parsing + content-page loading

**Files:**
- Create: `internal/site/pages.go`, `internal/site/pages_test.go`

**Interfaces:**
- Produces:
  - `type PageLink struct { Title, Href string }`
  - `type ContentPage struct { Slug, Title string; Order int; Desc string; Body template.HTML }`
  - `func parseFrontmatter(raw string) (title string, order int, body string)` — strips a leading `---\n…\n---\n` YAML block; returns `"",0,raw` when absent/malformed.
  - `func LoadPages(dir string) ([]ContentPage, error)` — one ContentPage per `*.md`, body rendered via `renderMarkdown`, `Desc` via `firstParagraph` of the raw body, `Title` falling back to the filename stem, sorted by `(Order, Title)`. Missing dir → `nil, nil`.
  - `func pageLinks(pages []ContentPage) []PageLink` — `{Title, Href:"/"+Slug+"/"}` in the same order.

- [ ] **Step 1: Write the failing test**

```go
package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFrontmatter(t *testing.T) {
	title, order, body := parseFrontmatter("---\ntitle: About\norder: 2\n---\nHello *world*.\n")
	if title != "About" || order != 2 || strings.TrimSpace(body) != "Hello *world*." {
		t.Fatalf("got (%q, %d, %q)", title, order, body)
	}
	// No frontmatter: whole input is the body.
	title, order, body = parseFrontmatter("# Just body\n")
	if title != "" || order != 0 || body != "# Just body\n" {
		t.Fatalf("no-fm got (%q, %d, %q)", title, order, body)
	}
}

func TestLoadPages(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "about.md"),
		[]byte("---\ntitle: About\norder: 2\n---\nAbout **me**.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "colophon.md"),
		[]byte("---\ntitle: Colophon\norder: 1\n---\nBuilt with care.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "elsewhere.md"),
		[]byte("Find me around.\n"), 0o644); err != nil { // no frontmatter
		t.Fatal(err)
	}

	pages, err := LoadPages(dir)
	if err != nil {
		t.Fatalf("LoadPages: %v", err)
	}
	if len(pages) != 3 {
		t.Fatalf("got %d pages", len(pages))
	}
	// Sorted by (order, title): Colophon(1), About(2), elsewhere(order 0)… wait order 0 sorts first.
	// elsewhere has order 0 → first; then Colophon(1); then About(2).
	if pages[0].Slug != "elsewhere" || pages[1].Title != "Colophon" || pages[2].Title != "About" {
		t.Fatalf("order = [%s, %s, %s]", pages[0].Slug, pages[1].Title, pages[2].Title)
	}
	if pages[0].Title != "elsewhere" {
		t.Errorf("title fallback = %q, want filename stem", pages[0].Title)
	}
	if !strings.Contains(string(pages[2].Body), "<strong>me</strong>") {
		t.Errorf("body not rendered to HTML: %q", pages[2].Body)
	}
	if pages[2].Desc != "About me." && pages[2].Desc != "About **me**." {
		t.Errorf("desc = %q", pages[2].Desc)
	}

	// Missing dir → no pages, no error.
	empty, err := LoadPages(filepath.Join(dir, "nope"))
	if err != nil || len(empty) != 0 {
		t.Errorf("missing dir: got %d pages, err %v", len(empty), err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/lorchard/devel/byom-sync-mixtapes-site && go test ./internal/site/ -run 'TestParseFrontmatter|TestLoadPages' -v`
Expected: FAIL — `undefined: parseFrontmatter` / `LoadPages`.

- [ ] **Step 3: Write the implementation**

```go
package site

import (
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// PageLink is a content page's entry in the site header nav.
type PageLink struct {
	Title string
	Href  string
}

// ContentPage is a render-ready standalone markdown page.
type ContentPage struct {
	Slug  string
	Title string
	Order int
	Desc  string
	Body  template.HTML
}

// parseFrontmatter strips a leading "---\n…\n---\n" YAML block, returning its
// title/order and the remaining body. Absent or malformed frontmatter yields
// ("", 0, raw).
func parseFrontmatter(raw string) (title string, order int, body string) {
	if !strings.HasPrefix(raw, "---\n") {
		return "", 0, raw
	}
	rest := raw[4:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return "", 0, raw
	}
	var meta struct {
		Title string `yaml:"title"`
		Order int    `yaml:"order"`
	}
	_ = yaml.Unmarshal([]byte(rest[:idx]), &meta) // best-effort
	body = strings.TrimPrefix(rest[idx+4:], "\n")
	return meta.Title, meta.Order, body
}

// LoadPages reads every *.md in dir into a sorted slice of ContentPages. A
// missing directory yields (nil, nil) — content pages are opt-in.
func LoadPages(dir string) ([]ContentPage, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return nil, err
	}
	pages := make([]ContentPage, 0, len(matches))
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		title, order, body := parseFrontmatter(string(data))
		slug := strings.TrimSuffix(filepath.Base(path), ".md")
		if title == "" {
			title = slug
		}
		pages = append(pages, ContentPage{
			Slug:  slug,
			Title: title,
			Order: order,
			Desc:  firstParagraph(body),
			Body:  renderMarkdown(body),
		})
	}
	sort.SliceStable(pages, func(i, j int) bool {
		if pages[i].Order != pages[j].Order {
			return pages[i].Order < pages[j].Order
		}
		return pages[i].Title < pages[j].Title
	})
	return pages, nil
}

// pageLinks projects loaded pages into header nav links, preserving order.
func pageLinks(pages []ContentPage) []PageLink {
	links := make([]PageLink, 0, len(pages))
	for _, p := range pages {
		links = append(links, PageLink{Title: p.Title, Href: "/" + p.Slug + "/"})
	}
	return links
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/site/ -run 'TestParseFrontmatter|TestLoadPages' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/site/pages.go internal/site/pages_test.go
git commit -m "feat(site): load markdown content pages with frontmatter

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: `siteheader` partial + `SiteMeta.Pages` + header refactor + CSS

**Files:**
- Modify: `internal/site/render.go`, `internal/site/templates/partials.html`, `internal/site/templates/landing.html`, `internal/site/templates/folder.html`, `internal/site/templates/playlist.html`, `internal/site/assets/site.css`, `internal/site/render_test.go`

**Interfaces:**
- Consumes: `PageLink` (Task 1).
- Produces: `SiteMeta.Pages []PageLink` — rendered by the shared `siteheader` partial into landing/folder/playlist headers (not embed).

- [ ] **Step 1: Add the `Pages` field to `SiteMeta`**

In `internal/site/render.go`, add to the `SiteMeta` struct (after `SpotifyClientID`):

```go
	Pages                 []PageLink
```

- [ ] **Step 2: Add the `siteheader` partial**

In `internal/site/templates/partials.html`, add:

```html
{{define "siteheader"}}
<header class="site-header">
<a class="site-title" href="/">{{.Site.Title}}</a>
{{if .Site.Pages}}<nav class="page-nav">{{range .Site.Pages}}<a href="{{.Href}}">{{.Title}}</a>{{end}}</nav>{{end}}
</header>
{{end}}
```

- [ ] **Step 3: Use the partial in the three page templates**

In `landing.html`, replace `<header class="site-header"><h1>{{.Site.Title}}</h1></header>` with:
```html
{{template "siteheader" .}}
```
In `folder.html` and `playlist.html`, replace `<header class="site-header"><a href="/">{{.Site.Title}}</a></header>` with:
```html
{{template "siteheader" .}}
```
(Leave `embed.html` unchanged — it has no header.)

- [ ] **Step 4: Update the header CSS**

In `internal/site/assets/site.css`, replace the line
`.site-header h1, .site-header a { font-size:1.4rem; font-weight:700; text-decoration:none; }`
with:
```css
.site-header { display:flex; align-items:baseline; gap:1.5rem; flex-wrap:wrap; }
.site-title { font-size:1.4rem; font-weight:700; text-decoration:none; }
.page-nav { display:flex; gap:1rem; }
.page-nav a { font-size:.95rem; }
```

- [ ] **Step 5: Write the failing test (header nav assertions)**

In `internal/site/render_test.go`, change `TestRenderSite` to render with pages set. Replace the renderer construction:
```go
	r, err := NewRenderer(testSite())
```
with:
```go
	site := testSite()
	site.Pages = []PageLink{{Title: "About", Href: "/about/"}, {Title: "Colophon", Href: "/colophon/"}}
	r, err := NewRenderer(site)
```
Then add, after the existing playlist-page assertions:
```go
	// Header nav: content-page links appear, in order, on interior + landing.
	if i := strings.Index(pl, `href="/about/"`); i < 0 || strings.Index(pl, `href="/colophon/"`) < i {
		t.Error("playlist header missing content-page nav in order")
	}
	if !strings.Contains(landing, `<nav class="page-nav">`) || !strings.Contains(landing, `href="/about/"`) {
		t.Error("landing header missing content-page nav")
	}
	if strings.Contains(embed, `class="page-nav"`) {
		t.Error("embed page must not carry the header nav")
	}
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/site/ -run TestRenderSite -v`
Expected: FAIL first if written before the template edits; PASS after Steps 1–4 are in place. Confirm PASS.

- [ ] **Step 7: Verify formatting/lint**

Run: `make format && make lint`
Expected: clean.

- [ ] **Step 8: Commit**

```bash
git add internal/site/render.go internal/site/templates internal/site/assets/site.css internal/site/render_test.go
git commit -m "feat(site): shared header partial with content-page nav

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: `page.html` template + `Renderer.RenderPages`

**Files:**
- Create: `internal/site/templates/page.html`
- Modify: `internal/site/render.go`, `internal/site/render_test.go`

**Interfaces:**
- Consumes: `ContentPage`, `SiteMeta.Pages`, `canonical`, `write`.
- Produces: `func (r *Renderer) RenderPages(outDir string, pages []ContentPage) error` — writes `<outDir>/<slug>/index.html` per page via `page.html`.

- [ ] **Step 1: Create `internal/site/templates/page.html`**

```html
<!doctype html>
<html lang="en">
<head>{{template "head" .}}</head>
<body class="page">
{{template "siteheader" .}}
<main class="page-body">{{.Body}}</main>
{{template "footer" .}}
</body>
</html>
```

- [ ] **Step 2: Write the failing test**

In `internal/site/render_test.go` add:
```go
func TestRenderPages(t *testing.T) {
	site := testSite()
	site.Pages = []PageLink{{Title: "About", Href: "/about/"}}
	r, err := NewRenderer(site)
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	pages := []ContentPage{{
		Slug: "about", Title: "About", Desc: "Who I am.",
		Body: template.HTML("<p>Hello <strong>world</strong>.</p>"),
	}}
	if err := r.RenderPages(out, pages); err != nil {
		t.Fatalf("RenderPages: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(out, "about", "index.html"))
	if err != nil {
		t.Fatalf("about page: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, "<strong>world</strong>") {
		t.Error("page body not rendered")
	}
	if !strings.Contains(s, `<nav class="page-nav">`) {
		t.Error("page missing header nav")
	}
	if !strings.Contains(s, `property="og:title" content="About"`) {
		t.Error("page missing OG title")
	}
	if !strings.Contains(s, `href="https://mix.test/about/"`) {
		t.Error("page missing canonical URL")
	}
}
```
(Add `"html/template"` to the test file imports if not present.)

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/site/ -run TestRenderPages -v`
Expected: FAIL — `r.RenderPages undefined`.

- [ ] **Step 4: Implement `RenderPages`**

In `internal/site/render.go`, add the data struct (near the other `*Data` types):
```go
type contentPageData struct {
	pageData
	Body template.HTML
}
```
And the method (near `RenderSite`):
```go
// RenderPages writes one HTML page per content page at <outDir>/<slug>/index.html.
func (r *Renderer) RenderPages(outDir string, pages []ContentPage) error {
	for _, p := range pages {
		dir := filepath.Join(outDir, p.Slug)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		data := contentPageData{
			pageData: pageData{
				Site:      r.Site,
				Title:     p.Title,
				Desc:      p.Desc,
				Canonical: canonical(r.Site.BaseURL, p.Slug),
			},
			Body: p.Body,
		}
		if err := r.write(filepath.Join(dir, "index.html"), "page.html", data); err != nil {
			return err
		}
	}
	return nil
}
```
(`os`, `path/filepath`, `html/template` are already imported in render.go.)

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/site/ -run TestRenderPages -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/site/render.go internal/site/templates/page.html internal/site/render_test.go
git commit -m "feat(site): render standalone content pages

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: `Build` wiring + `site` command + config + docs + e2e

**Files:**
- Modify: `internal/site/site.go`, `internal/site/site_test.go`, `cmd/site.go`, `cmd/root.go`, `AGENTS.md`

**Interfaces:**
- Consumes: `LoadPages`, `pageLinks`, `Renderer.RenderPages` (Tasks 1, 3), `SiteMeta.Pages` (Task 2).
- Produces: `Options.PagesDir string`; `Build` loads pages, sets `Site.Pages`, and renders them.

- [ ] **Step 1: Wire `Build` (internal/site/site.go)**

Add `PagesDir string` to `Options` (after `OutDir`). In `Build`, after `BuildTree` and before creating the renderer:
```go
	pages, err := LoadPages(opts.PagesDir)
	if err != nil {
		return err
	}
	opts.Site.Pages = pageLinks(pages)
```
The renderer is created from `opts.Site` (now carrying `Pages`). After `RenderSite(...)` and before the final `WriteFeed`, add:
```go
	if err := r.RenderPages(opts.OutDir, pages); err != nil {
		return err
	}
```

- [ ] **Step 2: Write the failing e2e test**

In `internal/site/site_test.go`, extend `TestBuildEndToEnd`: before calling `Build`, create a pages dir and pass it; after, assert the page + header nav. Add near the top of the test:
```go
	pagesDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(pagesDir, "about.md"),
		[]byte("---\ntitle: About\norder: 1\n---\nHello.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
```
Add `PagesDir: pagesDir` to the `Build(Options{...})` call. Add to the asserted output-path list:
```go
		"about/index.html",
```
And after the existing file-existence loop:
```go
	pl, err := os.ReadFile(filepath.Join(out, "2014-top-songs", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(pl), `href="/about/"`) {
		t.Error("playlist page header should link the content page")
	}
```
(Ensure `strings` is imported in site_test.go.)

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/site/ -run TestBuildEndToEnd -v`
Expected: FAIL — `Options.PagesDir` unknown / `about/index.html` missing (before Step 1 is complete) or missing header link.

- [ ] **Step 4: Add config default + command wiring**

In `cmd/root.go`, after the other `site.*` defaults, add:
```go
	viper.SetDefault("site.pages_dir", "./pages")
```
In `cmd/site.go`, add a package-level flag var `sitePages string`, register it in `init()`:
```go
	siteCmd.Flags().StringVar(&sitePages, "pages", "", "content-pages directory (default: config `site.pages_dir`)")
```
and in `runSite`, resolve it and pass it through:
```go
	pagesDir := sitePages
	if pagesDir == "" {
		pagesDir = viper.GetString("site.pages_dir")
	}
```
Add `PagesDir: pagesDir,` to the `site.Options{...}` literal.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/site/ ./cmd/ -v 2>&1 | tail -20`
Expected: PASS (e2e + existing cmd tests).

- [ ] **Step 6: Update AGENTS.md**

In the `internal/site/` Layout bullet, append a sentence:
`Content pages (`site.pages_dir`, default `./pages`): `*.md` with YAML frontmatter (`title`/`order`) → `/slug/` pages linked in the header.`

- [ ] **Step 7: Full verification**

Run: `make lint && make test && make build`
Expected: all pass, 0 lint findings.

- [ ] **Step 8: Commit**

```bash
git add internal/site/site.go internal/site/site_test.go cmd/site.go cmd/root.go AGENTS.md
git commit -m "feat(site): wire content pages into build + site command (#21)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Real-hub smoke test + notes

**Files:**
- Create: `docs/dev-sessions/2026-07-10-1615-site-pages/notes.md`

- [ ] **Step 1: Build with a pages dir**

```bash
cd /Users/lorchard/devel/byom-sync-mixtapes-site
mkdir -p /tmp/mixtapes-pages
printf -- '---\ntitle: About\norder: 1\n---\nA little site of mixtapes.\n' > /tmp/mixtapes-pages/about.md
printf -- '---\ntitle: Colophon\norder: 2\n---\nBuilt with byom-sync.\n' > /tmp/mixtapes-pages/colophon.md
go run . site --input /Users/lorchard/devel/byom-sync/playlists --out /tmp/mixtapes-full \
  --base-url https://mixtapes.lmorchard.com --pages /tmp/mixtapes-pages
find /tmp/mixtapes-full -maxdepth 2 -name index.html | grep -E '/(about|colophon)/' 
```
Expected: `about/index.html` and `colophon/index.html` exist; a playlist page's header links them in order.

- [ ] **Step 2: Notes**

Write `notes.md`: what was built, any deviations, verification results, follow-ups (nested pages dir, `nav:false`, active-page highlight).

- [ ] **Step 3: Commit**

```bash
git add docs/dev-sessions/2026-07-10-1615-site-pages/notes.md
git commit -m "docs(session): site-pages build notes + verification

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Self-Review

**Spec coverage:**
- `site.pages_dir` + default → Task 4. ✓
- Frontmatter (title/order, filename fallback, no-frontmatter) → Task 1. ✓
- `/slug/` output → Tasks 3 (RenderPages), 1 (slug). ✓
- Header nav on non-embed pages, ordered → Task 2. ✓
- New `page.html` → Task 3. ✓
- Per-page OG metadata → Task 3. ✓
- Graceful missing dir → Task 1 (LoadPages nil), Task 4 (Build passes empty through). ✓
- Playlists/site-index/sidebar/RSS untouched → no task modifies them; constraint enforced. ✓
- Testing (parse, load, render, header nav, embed exclusion, e2e) → Tasks 1–4. ✓

**Placeholder scan:** none — every step has concrete code/commands.

**Type consistency:** `PageLink`, `ContentPage`, `parseFrontmatter`, `LoadPages`, `pageLinks`, `SiteMeta.Pages`, `contentPageData`, `RenderPages`, `Options.PagesDir` are used consistently across tasks. `renderMarkdown`/`firstParagraph`/`canonical`/`write` are pre-existing in the package.

**Ambiguity check:** sort is `(Order asc, Title asc)`; order default 0 sorts first (documented in the Task 1 test). Slug from filename stem. Missing dir → `nil, nil`.

## Open questions / fast-follows (out of scope)

- Nested/organized pages directory.
- `nav: false` frontmatter (render a page without a header link).
- Active-page highlight in the header nav.
