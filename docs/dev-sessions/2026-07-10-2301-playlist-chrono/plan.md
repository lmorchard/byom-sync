# Reverse-chronological Playlists + Year Headers — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Order `byom-sync site` playlists reverse-chronologically by `date_updated`, group them under year separator headers (index + sidebar), and show a `date_created – date_updated` range in each playlist's metadata line.

**Architecture:** Sort playlists in the tree walk (`tree.go`) so all consumers inherit the order. Add grouping helpers (`grouping.go`) + template restructure for year headers on the landing/folder pages. Carry a `year` per leaf in `site-index.json` so `site-nav.js` emits the same year separators. Widen `playlistMeta` to a date range.

**Tech Stack:** Go 1.25 · `html/template` · existing `internal/site` package. No new deps.

## Global Constraints

- Module `github.com/lmorchard/byom-sync`; Go `1.25.0`. No cgo, no new dependencies.
- golangci-lint v2, **errcheck strict** (`_ =` for ignored returns); gofumpt (`make format`). Verify with `make lint && make test && make build`.
- Sort/group key is **`Playlist.DateUpdated`**; undated (`IsZero()`) sorts LAST and groups under **"Undated"**. Year label = `DateUpdated.Year()`.
- Metadata range is **`DateCreated – DateUpdated`** (en dash `–`), collapsing to one value when equal; each side `Format("Jan 2006")`.
- Directories stay first at each level, alphabetical by `Name`. Only ordering + index/folder/sidebar rendering + the metadata line change — playlist/embed/content pages, RSS, JSPF are untouched.
- Commit trailer: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.

## File Structure

- Modify: `internal/site/tree.go` (sort), `internal/site/meta.go` (date range), `internal/site/index.go` (`IndexNode.Year`), `internal/site/render.go` (register funcs), `internal/site/templates/landing.html` (`treeList` restructure), `internal/site/assets/site.css` (year header styles), `internal/site/assets/site-nav.js` (year separators).
- Create: `internal/site/grouping.go` (`YearGroup`, `dirsOf`, `yearGroupsOf`) + `internal/site/grouping_test.go`.
- Tests: extend `tree_test.go`, `meta_test.go`, `index_test.go`, `render_test.go`.

Note: `writeFixtureHub` has one playlist at root + one nested (single playlist per level), so the sort change does NOT reorder it — existing ordering assertions stay valid. New ordering behavior is covered by dedicated tests with multiple dated playlists.

---

## Task 1: Reverse-chronological sort in the tree walk

**Files:** Modify `internal/site/tree.go`; extend `internal/site/tree_test.go`.

**Interfaces:** Produces: `buildDir` sorts each node's children dirs-first (alphabetical), then playlists by `DateUpdated` desc (undated last, ties by `Title`).

- [ ] **Step 1: Write the failing test** (append to `tree_test.go`)

```go
func TestBuildTreeReverseChron(t *testing.T) {
	dir := t.TempDir()
	write := func(name, updated string) {
		body := "title: " + name + "\ntracks:\n  - {title: T, artist: A}\n"
		if updated != "" {
			body = "title: " + name + "\ndate_updated: " + updated + "\ntracks:\n  - {title: T, artist: A}\n"
		}
		mustWrite(t, filepath.Join(dir, name+".yaml"), body)
	}
	write("old", "2015-03-01T00:00:00Z")
	write("newest", "2020-06-01T00:00:00Z")
	write("mid", "2018-01-01T00:00:00Z")
	write("undated", "") // no date_updated

	root, err := BuildTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, c := range root.Children {
		order = append(order, c.Name)
	}
	want := []string{"newest", "mid", "old", "undated"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v, want %v", order, want)
	}
}
```

- [ ] **Step 2: Run it — expect FAIL** (currently alphabetical: `mid,newest,old,undated`)

Run: `cd /Users/lorchard/devel/byom-sync-mixtapes-site && go test ./internal/site/ -run TestBuildTreeReverseChron -v`

- [ ] **Step 3: Replace the sort in `tree.go`**

Replace the `sort.SliceStable(node.Children, ...)` block with:

```go
	sort.SliceStable(node.Children, func(i, j int) bool {
		a, b := node.Children[i], node.Children[j]
		if a.IsDir != b.IsDir {
			return a.IsDir // directories first
		}
		if a.IsDir {
			return a.Name < b.Name
		}
		// Playlists: newest DateUpdated first; undated (zero) last; ties by Title.
		au, bu := a.Playlist.DateUpdated, b.Playlist.DateUpdated
		if au.IsZero() != bu.IsZero() {
			return !au.IsZero()
		}
		if !au.Equal(bu) {
			return au.After(bu)
		}
		return a.Title < b.Title
	})
```

- [ ] **Step 4: Run — expect PASS** (also run the full package: `go test ./internal/site/` to confirm existing tests still pass).

- [ ] **Step 5: Commit**

```bash
git add internal/site/tree.go internal/site/tree_test.go
git commit -m "feat(site): sort playlists reverse-chronologically by date_updated

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Date range in the metadata line

**Files:** Modify `internal/site/meta.go`; extend `internal/site/meta_test.go`.

**Interfaces:** Produces: `func dateRange(created, updated time.Time) string`; `playlistMeta` ends with `dateRange(p.DateCreated, p.DateUpdated)` (omitted when empty).

- [ ] **Step 1: Write the failing test** (append to `meta_test.go`; ensure `time` is imported)

```go
func TestDateRange(t *testing.T) {
	feb23 := time.Date(2023, 2, 1, 0, 0, 0, 0, time.UTC)
	jun26 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	feb23b := time.Date(2023, 2, 15, 0, 0, 0, 0, time.UTC) // same month as feb23
	var zero time.Time
	cases := []struct{ c, u time.Time; want string }{
		{feb23, jun26, "Feb 2023 – Jun 2026"},
		{feb23, feb23b, "Feb 2023"}, // same month-year collapses
		{feb23, zero, "Feb 2023"},
		{zero, jun26, "Jun 2026"},
		{zero, zero, ""},
	}
	for _, tc := range cases {
		if got := dateRange(tc.c, tc.u); got != tc.want {
			t.Errorf("dateRange(%v,%v) = %q, want %q", tc.c, tc.u, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run — expect FAIL** (`undefined: dateRange`).

- [ ] **Step 3: Implement in `meta.go`** (add `"time"` to imports)

```go
func monthYear(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("Jan 2006")
}

// dateRange formats a created–updated span as "Feb 2023 – Jun 2026", collapsing
// to a single value when both fall in the same month, and to whichever side is
// present when only one is.
func dateRange(created, updated time.Time) string {
	c, u := monthYear(created), monthYear(updated)
	switch {
	case c == "" && u == "":
		return ""
	case c == "":
		return u
	case u == "" || c == u:
		return c
	default:
		return c + " – " + u
	}
}
```

Then in `playlistMeta`, replace the `if !p.DateCreated.IsZero() { ... }` block with:

```go
	if r := dateRange(p.DateCreated, p.DateUpdated); r != "" {
		parts = append(parts, r)
	}
```

- [ ] **Step 4: Run — expect PASS** (`go test ./internal/site/ -run 'TestDateRange|TestPlaylistMeta'`; the existing `TestPlaylistMeta` still passes — it sets only `DateCreated`, so the range collapses to that single month).

- [ ] **Step 5: Commit**

```bash
git add internal/site/meta.go internal/site/meta_test.go
git commit -m "feat(site): show date_created–date_updated range in playlist metadata

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Year-group helpers + template restructure + CSS

**Files:** Create `internal/site/grouping.go`, `internal/site/grouping_test.go`; modify `internal/site/render.go` (register funcs), `internal/site/templates/landing.html`, `internal/site/assets/site.css`; extend `internal/site/render_test.go`.

**Interfaces:**
- Produces: `type YearGroup struct { Label string; Playlists []*Node }`; `func dirsOf(children []*Node) []*Node`; `func yearGroupsOf(children []*Node) []YearGroup` (consecutive same-year playlists grouped, preserving order; undated → trailing "Undated" group). Registered as template funcs `dirsOf`/`yearGroupsOf`.

- [ ] **Step 1: Write the failing helper test** (`grouping_test.go`)

```go
package site

import (
	"testing"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

func TestYearGroupsOf(t *testing.T) {
	pl := func(updated string) *Node {
		n := &Node{Playlist: &playlist.Playlist{}}
		if updated != "" {
			n.Playlist.DateUpdated, _ = time.Parse(time.RFC3339, updated)
		}
		return n
	}
	children := []*Node{
		{Name: "d", IsDir: true},
		pl("2020-01-01T00:00:00Z"),
		pl("2020-06-01T00:00:00Z"),
		pl("2018-01-01T00:00:00Z"),
		pl(""), // undated
	}
	if d := dirsOf(children); len(d) != 1 || d[0].Name != "d" {
		t.Fatalf("dirsOf = %+v", d)
	}
	groups := yearGroupsOf(children)
	if len(groups) != 3 {
		t.Fatalf("groups = %d, want 3", len(groups))
	}
	if groups[0].Label != "2020" || len(groups[0].Playlists) != 2 {
		t.Errorf("group0 = %s/%d", groups[0].Label, len(groups[0].Playlists))
	}
	if groups[1].Label != "2018" || groups[2].Label != "Undated" {
		t.Errorf("labels = %s, %s", groups[1].Label, groups[2].Label)
	}
}
```

- [ ] **Step 2: Run — expect FAIL** (`undefined: dirsOf`).

- [ ] **Step 3: Implement `grouping.go`**

```go
package site

import "strconv"

// YearGroup is a run of playlists sharing a DateUpdated year (or the undated
// group), for year-separated rendering.
type YearGroup struct {
	Label     string
	Playlists []*Node
}

// dirsOf returns the directory children, in their existing order.
func dirsOf(children []*Node) []*Node {
	var dirs []*Node
	for _, c := range children {
		if c.IsDir {
			dirs = append(dirs, c)
		}
	}
	return dirs
}

// yearGroupsOf splits playlist children into ordered year groups, preserving the
// children's (reverse-chron) order: consecutive same-year playlists share a
// group; undated ones form a trailing "Undated" group.
func yearGroupsOf(children []*Node) []YearGroup {
	var groups []YearGroup
	for _, c := range children {
		if c.IsDir {
			continue
		}
		label := "Undated"
		if !c.Playlist.DateUpdated.IsZero() {
			label = strconv.Itoa(c.Playlist.DateUpdated.Year())
		}
		if len(groups) == 0 || groups[len(groups)-1].Label != label {
			groups = append(groups, YearGroup{Label: label})
		}
		groups[len(groups)-1].Playlists = append(groups[len(groups)-1].Playlists, c)
	}
	return groups
}
```

- [ ] **Step 4: Register the funcs** in `render.go`'s `NewRenderer` FuncMap:

```go
		"dirsOf":       dirsOf,
		"yearGroupsOf": yearGroupsOf,
```

- [ ] **Step 5: Restructure `treeList` in `landing.html`**

Replace the existing `{{define "treeList"}}…{{end}}` block with:

```html
{{define "treeList"}}
{{$children := .}}
{{with dirsOf $children}}
<ul class="tree-list">
{{range .}}
  <li class="dir"><a href="/{{.Path}}/">📁 {{.Title}}</a>{{template "treeList" .Children}}</li>
{{end}}
</ul>
{{end}}
{{range yearGroupsOf $children}}
<h2 class="year">{{.Label}}</h2>
<ul class="tree-list">
{{range .Playlists}}
  <li class="leaf"><a href="/{{.Path}}/">{{.Title}}</a> <span class="meta">— {{playlistMeta .Playlist}}</span></li>
{{end}}
</ul>
{{end}}
{{end}}
```

(`folder.html` and `landing.html` both call `treeList` with a children slice, so both get year grouping. No change needed in those call sites.)

- [ ] **Step 6: Add year-header CSS** to `site.css` (after `.tree-list .meta`):

```css
.year { font-size:.8rem; text-transform:uppercase; letter-spacing:.08em; color:var(--muted); margin:1.6rem 0 .4rem; padding-bottom:.3rem; border-bottom:1px solid #333; }
```

- [ ] **Step 7: Add a render test** (append to `render_test.go`)

```go
func TestRenderYearHeaders(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "index.md"), "# hub\n")
	mustWrite(t, filepath.Join(dir, "a.yaml"), "title: A\ndate_updated: 2020-05-01T00:00:00Z\ntracks:\n  - {title: T, artist: X}\n")
	mustWrite(t, filepath.Join(dir, "b.yaml"), "title: B\ndate_updated: 2018-02-01T00:00:00Z\ntracks:\n  - {title: T, artist: X}\n")
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
	i20, i18 := strings.Index(s, `<h2 class="year">2020</h2>`), strings.Index(s, `<h2 class="year">2018</h2>`)
	if i20 < 0 || i18 < 0 {
		t.Fatal("missing year headers")
	}
	if i20 > i18 {
		t.Error("year headers not in descending order (2020 should precede 2018)")
	}
}
```

- [ ] **Step 8: Run** `go test ./internal/site/ -run 'TestYearGroupsOf|TestRenderYearHeaders|TestRenderSite' -v` — expect PASS (existing `TestRenderSite` still passes; its undated fixture playlist now renders under an "Undated" header, but its link/meta assertions are unaffected). Then `make lint && make test`.

- [ ] **Step 9: Commit**

```bash
git add internal/site/grouping.go internal/site/grouping_test.go internal/site/render.go internal/site/templates/landing.html internal/site/assets/site.css internal/site/render_test.go
git commit -m "feat(site): year-group headers on index + folder pages

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: `IndexNode.Year` + sidebar year separators

**Files:** Modify `internal/site/index.go`, `internal/site/assets/site-nav.js`; extend `internal/site/index_test.go`.

**Interfaces:** Produces: `IndexNode.Year int` (`json:"year,omitempty"`) — the `DateUpdated` year for leaves, `0`/absent when undated or a dir. `site-nav.js` renders dirs then year-separated leaves.

- [ ] **Step 1: Extend the index test** (`index_test.go`)

Add a dated playlist to a temp hub and assert its `Year`. Append:

```go
func TestIndexNodeYear(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.yaml"), "title: A\ndate_updated: 2019-04-01T00:00:00Z\ntracks:\n  - {title: T, artist: X}\n")
	mustWrite(t, filepath.Join(dir, "b.yaml"), "title: B\ntracks:\n  - {title: T, artist: X}\n") // undated
	root, err := BuildTree(dir)
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
	byName := map[string]IndexNode{}
	for _, n := range nodes {
		byName[n.Name] = n
	}
	if byName["a"].Year != 2019 {
		t.Errorf("a.Year = %d, want 2019", byName["a"].Year)
	}
	if byName["b"].Year != 0 {
		t.Errorf("undated b.Year = %d, want 0", byName["b"].Year)
	}
}
```

- [ ] **Step 2: Run — expect FAIL** (`IndexNode.Year` undefined).

- [ ] **Step 3: Add `Year` to `IndexNode` + populate** in `index.go`

Add the field to the struct:
```go
	Year     int         `json:"year,omitempty"`
```
In `toIndexNodes`, inside the loop after building `n` (where the leaf `Meta` is set), add:
```go
		if !c.IsDir && !c.Playlist.DateUpdated.IsZero() {
			n.Year = c.Playlist.DateUpdated.Year()
		}
```

- [ ] **Step 4: Run — expect PASS.**

- [ ] **Step 5: Update `site-nav.js`** — render dirs first, then year-separated leaves. Replace the `render(nodes, here)` method body with:

```js
  render(nodes, here) {
    const dirs = nodes.filter((n) => n.isDir);
    const leaves = nodes.filter((n) => !n.isDir);
    let html = '';
    if (dirs.length) {
      html += `<ul>${dirs.map((n) => {
        const active = n.path === here ? ' aria-current="page"' : '';
        const kids = n.children && n.children.length ? this.render(n.children, here) : '';
        return `<li><a href="${esc(n.path)}"${active}>📁 ${esc(n.title)}</a>${kids}</li>`;
      }).join('')}</ul>`;
    }
    let items = '';
    let lastYear = null;
    for (const n of leaves) {
      const y = n.year || 0;
      if (y !== lastYear) {
        items += `<li class="nav-year">${y ? y : 'Undated'}</li>`;
        lastYear = y;
      }
      const active = n.path === here ? ' aria-current="page"' : '';
      const meta = n.meta ? `<span class="nav-meta">${esc(n.meta)}</span>` : '';
      items += `<li><a href="${esc(n.path)}"${active}>${esc(n.title)}</a>${meta}</li>`;
    }
    if (items) html += `<ul>${items}</ul>`;
    return html;
  }
```

- [ ] **Step 6: Add sidebar year-separator CSS** to `site.css` (after the `.site-nav .nav-meta` rule):

```css
.site-nav .nav-year { list-style:none; font-size:.7rem; text-transform:uppercase; letter-spacing:.06em; color:var(--muted); margin:.9rem 0 .25rem; }
```

- [ ] **Step 7: Verify** `make lint && make test && make build` — all clean.

- [ ] **Step 8: Commit**

```bash
git add internal/site/index.go internal/site/index_test.go internal/site/assets/site-nav.js internal/site/assets/site.css
git commit -m "feat(site): year separators in the sidebar nav (#26)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Real-hub smoke test + notes

**Files:** Create `docs/dev-sessions/2026-07-10-2301-playlist-chrono/notes.md`.

- [ ] **Step 1: Rebuild + serve**

```bash
cd /Users/lorchard/devel/byom-sync-mixtapes-site
go run . site --input /Users/lorchard/devel/byom-sync/playlists --out /tmp/mixtapes-full --base-url https://mixtapes.lmorchard.com --pages /tmp/mixtapes-pages
# landing: year headers descending, playlists reverse-chron, ranges in meta
grep -oE '<h2 class="year">[0-9]+</h2>' /tmp/mixtapes-full/index.html | head
grep -oE '<span class="meta">— [^<]*</span>' /tmp/mixtapes-full/index.html | head -4
pkill -f "http.server 8099"; (cd /tmp/mixtapes-full && python3 -m http.server 8099 --bind 127.0.0.1 &)
```
Confirm in the browser: index year headers in descending order, playlists newest-first within each, metadata shows created–updated ranges, and the sidebar shows matching year separators.

- [ ] **Step 2: Notes** — write `notes.md`: what changed, verification (year order, ranges, sidebar), any deviations, follow-ups (collapsible groups, sort toggle).

- [ ] **Step 3: Commit**

```bash
git add docs/dev-sessions/2026-07-10-2301-playlist-chrono/notes.md
git commit -m "docs(session): playlist-chrono build notes + verification

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Self-Review

**Spec coverage:** reverse-chron sort → Task 1; date range → Task 2; year headers (index/folder) → Task 3; sidebar year separators + `IndexNode.Year` → Task 4; verification → Task 5. ✓

**Placeholder scan:** none — every step has concrete code/commands.

**Type consistency:** `YearGroup`, `dirsOf`, `yearGroupsOf` (grouping.go, used in render.go funcmap + landing.html), `IndexNode.Year` (index.go, index_test, site-nav.js `n.year`), `dateRange`/`monthYear` (meta.go). `Playlist.DateUpdated`/`DateCreated` confirmed present (#24). Sort comparator uses `a.Playlist` only in the both-playlists branch (dirs handled first, so nil `Playlist` on dirs is never dereferenced).

**Ambiguity:** undated = `DateUpdated.IsZero()`, sorts last, label "Undated"; year label = `DateUpdated.Year()`; range collapses when the two months are equal.

## Open questions / fast-follows (out of scope)

- Collapsible year groups; created-vs-updated sort toggle.
