# Featured Playlists Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A playlist with `featured: true` appears in a Featured list at the top of the site's main index page and at the top of the sidebar nav.

**Architecture:** One boolean field on `playlist.Playlist`. The site generator collects featured leaves from the whole hub tree into one flat, date-sorted list; the landing page renders them as normal playlist cards under a `Featured` heading, and `site-index.json` grows a pre-sorted `featured` array that `<byom-site-nav>` renders as a group above the folders and year groups. Featuring is additive — a featured playlist keeps its normal position everywhere else.

**Tech Stack:** Go 1.25 · `html/template` · `gopkg.in/yaml.v3` · dependency-free vanilla-JS custom element (no JS build pipeline, no JS test harness).

## Global Constraints

- Work in the existing worktree `/Users/lorchard/devel/byom-sync/.claude/worktrees/featured-playlists` on branch `feat/featured-playlists`. Never run git checkout/restore/stash in the shared primary checkout.
- Formatting via `gofumpt` (`make format`); lint via golangci-lint v2 (`make lint`). **errcheck is strict** — use `_ =` for intentionally-ignored returns.
- The YAML key is exactly `featured`. The heading text is exactly `Featured`. The JSON keys are exactly `featured` and `children`.
- Featuring is presentation-only: no exporter (`internal/export`), feed, or mosaic code reads `Featured`.
- Add no new CSS rules. Reuse the existing `.year`, `.playlist-cards`, `.nav-year`, and `.nav-leaf` rules; new class names are styling hooks only.
- Verification at the end of every task: `make lint && make test && make build`, reading the output.
- Commit trailer on every commit: `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`.

## File Structure

| File | Change | Responsibility |
|---|---|---|
| `internal/playlist/types.go` | Modify | `Playlist.Featured` field |
| `internal/playlist/types_test.go` | Modify | YAML round-trip / omitempty coverage |
| `internal/site/grouping.go` | Modify | `playlistNodeLess`, `featuredOf` — collection + ordering |
| `internal/site/tree.go` | Modify | `buildDir` sort delegates to `playlistNodeLess` |
| `internal/site/grouping_test.go` | Modify | `featuredOf` recursion + ordering tests |
| `internal/site/templates/landing.html` | Modify | `playlistCard` define + Featured section |
| `internal/site/render.go` | Modify | register `featuredOf` in the FuncMap |
| `internal/site/render_test.go` | Modify | landing page contains/omits the Featured section |
| `internal/site/index.go` | Modify | `SiteIndex` object shape for `site-index.json` |
| `internal/site/index_test.go` | Modify | new JSON shape; existing tests migrated |
| `internal/site/assets/site-nav.js` | Modify | Featured group + shared leaf row + shape tolerance |
| `AGENTS.md`, `README.md` | Modify | document the flag |

---

### Task 1: `featured` field on Playlist

**Files:**
- Modify: `internal/playlist/types.go` (the `Playlist` struct, after `ImageFile`)
- Test: `internal/playlist/types_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `playlist.Playlist.Featured bool` with YAML key `featured` and `omitempty`. Every later task reads this field.

- [ ] **Step 1: Write the failing test**

Append to `internal/playlist/types_test.go`:

```go
func TestPlaylist_FeaturedYAML(t *testing.T) {
	// Featured round-trips through YAML.
	var p Playlist
	if err := yaml.Unmarshal([]byte("title: T\nfeatured: true\ntracks: []\n"), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !p.Featured {
		t.Error("featured: true did not unmarshal to Featured == true")
	}

	// An absent key means not featured (the common case for every existing file).
	var plain Playlist
	if err := yaml.Unmarshal([]byte("title: T\ntracks: []\n"), &plain); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if plain.Featured {
		t.Error("absent featured key should leave Featured == false")
	}

	// omitempty keeps the key out of the files that don't use it.
	data, err := yaml.Marshal(plain)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "featured") {
		t.Errorf("unfeatured playlist should not serialize a featured key:\n%s", data)
	}

	data, err = yaml.Marshal(Playlist{Title: "T", Featured: true})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), "featured: true") {
		t.Errorf("featured playlist should serialize featured: true:\n%s", data)
	}
}
```

`strings` and `yaml` are already imported in this file.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/playlist/ -run TestPlaylist_FeaturedYAML -v`
Expected: FAIL — compile error, `p.Featured` undefined.

- [ ] **Step 3: Write minimal implementation**

In `internal/playlist/types.go`, add to the `Playlist` struct immediately after the `ImageFile` field and before the `DateImported` field:

```go
	// Featured promotes this playlist into the site's Featured list (the top of
	// the index page and of the sidebar nav). It is additive: a featured
	// playlist also keeps its normal position in the year groups and nav tree.
	// Presentation only — exporters ignore it.
	Featured bool `yaml:"featured,omitempty"`
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/playlist/ -v`
Expected: PASS, including the pre-existing `TestPlaylist_YAMLRoundTrip`.

- [ ] **Step 5: Verify and commit**

```bash
make lint && make test && make build
git add internal/playlist/types.go internal/playlist/types_test.go
git commit -m "feat(playlist): add featured flag

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: collect and order featured playlists

**Files:**
- Modify: `internal/site/grouping.go` (add two functions)
- Modify: `internal/site/tree.go` (`buildDir`'s `sort.SliceStable` comparator, ~lines 88-101)
- Test: `internal/site/grouping_test.go`

**Interfaces:**
- Consumes: `playlist.Playlist.Featured` from Task 1.
- Produces:
  - `playlistNodeLess(a, b *Node) bool` — orders two playlist leaf nodes: newest `DateUpdated` first, undated last, ties by `Title`. Panics on directory nodes (they have no `Playlist`); callers must filter.
  - `featuredOf(root *Node) []*Node` — flat, ordered list of featured playlist leaves anywhere under `root`. Returns `nil` when there are none, so `{{with}}` in a template treats it as empty. Used by Task 3 (template func) and Task 4 (`WriteIndexJSON`).

- [ ] **Step 1: Write the failing test**

Append to `internal/site/grouping_test.go`:

```go
func TestFeaturedOf(t *testing.T) {
	pl := func(title, updated string, featured bool) *Node {
		n := &Node{
			Name:     title,
			Title:    title,
			Playlist: &playlist.Playlist{Title: title, Featured: featured},
		}
		if updated != "" {
			n.Playlist.DateUpdated, _ = time.Parse(time.RFC3339, updated)
		}
		return n
	}
	// Featured playlists live at the root and nested two directories deep; the
	// list is flat and globally date-ordered regardless of where they're filed.
	root := &Node{IsDir: true, Children: []*Node{
		{Name: "archive", IsDir: true, Children: []*Node{
			pl("deep", "2021-05-01T00:00:00Z", true),
			pl("deep-plain", "2024-01-01T00:00:00Z", false),
			{Name: "deeper", IsDir: true, Children: []*Node{
				pl("deepest", "2026-01-01T00:00:00Z", true),
			}},
		}},
		pl("newest", "2026-07-01T00:00:00Z", true),
		pl("plain", "2026-07-02T00:00:00Z", false),
		pl("undated", "", true),
		pl("zed", "2026-01-01T00:00:00Z", true), // ties with "deepest" on date
	}}

	got := featuredOf(root)
	var titles []string
	for _, n := range got {
		titles = append(titles, n.Title)
	}
	want := []string{"newest", "deepest", "zed", "deep", "undated"}
	if len(titles) != len(want) {
		t.Fatalf("featuredOf titles = %v, want %v", titles, want)
	}
	for i := range want {
		if titles[i] != want[i] {
			t.Fatalf("featuredOf titles = %v, want %v", titles, want)
		}
	}
}

func TestFeaturedOf_NoneFeatured(t *testing.T) {
	root := &Node{IsDir: true, Children: []*Node{
		{Name: "d", IsDir: true, Children: []*Node{
			{Name: "x", Title: "X", Playlist: &playlist.Playlist{Title: "X"}},
		}},
		{Name: "y", Title: "Y", Playlist: &playlist.Playlist{Title: "Y"}},
	}}
	if got := featuredOf(root); len(got) != 0 {
		t.Errorf("featuredOf = %+v, want empty", got)
	}
}
```

The expected order is worth reading carefully: `newest` (2026-07-01) → `deepest` (2026-01-01, title "deepest") → `zed` (2026-01-01, tie broken by title) → `deep` (2021-05-01) → `undated` (zero date, last).

`playlist` and `time` are already imported in this file.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/site/ -run TestFeaturedOf -v`
Expected: FAIL — compile error, `featuredOf` undefined.

- [ ] **Step 3: Add the two functions**

In `internal/site/grouping.go`, add the `sort` import and append:

```go
// playlistNodeLess orders playlist leaf nodes: newest DateUpdated first, undated
// last, ties broken by Title. Shared by the per-folder sort in buildDir and by
// featuredOf so the two orderings cannot drift apart. Callers must pass leaves —
// directory nodes carry no Playlist.
func playlistNodeLess(a, b *Node) bool {
	au, bu := a.Playlist.DateUpdated, b.Playlist.DateUpdated
	if au.IsZero() != bu.IsZero() {
		return !au.IsZero()
	}
	if !au.Equal(bu) {
		return au.After(bu)
	}
	return a.Title < b.Title
}

// featuredOf collects every featured playlist leaf anywhere under root into one
// flat list in Featured order. Featuring is independent of where a playlist is
// filed in the hub, so this walks the whole tree; the result is nil when nothing
// is featured, which templates can test with {{with}}.
func featuredOf(root *Node) []*Node {
	var out []*Node
	var walk func(*Node)
	walk = func(n *Node) {
		for _, c := range n.Children {
			if c.IsDir {
				walk(c)
				continue
			}
			if c.Playlist.Featured {
				out = append(out, c)
			}
		}
	}
	walk(root)
	sort.SliceStable(out, func(i, j int) bool { return playlistNodeLess(out[i], out[j]) })
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/site/ -run TestFeaturedOf -v`
Expected: PASS (both tests).

- [ ] **Step 5: Route the existing folder sort through the shared comparator**

In `internal/site/tree.go`, replace the tail of `buildDir`'s comparator — the block that currently reads:

```go
		// Playlists: newest DateUpdated first; undated (zero) last; ties by Title.
		au, bu := a.Playlist.DateUpdated, b.Playlist.DateUpdated
		if au.IsZero() != bu.IsZero() {
			return !au.IsZero()
		}
		if !au.Equal(bu) {
			return au.After(bu)
		}
		return a.Title < b.Title
```

with:

```go
		// Playlists: newest DateUpdated first; undated (zero) last; ties by Title.
		return playlistNodeLess(a, b)
```

Leave the directories-first and directories-by-name branches above it untouched.

- [ ] **Step 6: Run the package tests to confirm the refactor changed no behavior**

Run: `go test ./internal/site/ -v`
Expected: PASS — every pre-existing test, especially the tree and index ordering tests.

- [ ] **Step 7: Verify and commit**

```bash
make lint && make test && make build
git add internal/site/grouping.go internal/site/grouping_test.go internal/site/tree.go
git commit -m "feat(site): collect featured playlists from the whole hub tree

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Featured section on the index page

**Files:**
- Modify: `internal/site/templates/landing.html`
- Modify: `internal/site/render.go` (the `template.FuncMap` at ~line 73)
- Test: `internal/site/render_test.go`

**Interfaces:**
- Consumes: `featuredOf(root *Node) []*Node` from Task 2.
- Produces: a `{{define "playlistCard"}}` template taking a `*Node`, used by both the Featured section and `treeList`. A `featuredOf` template function.

- [ ] **Step 1: Write the failing test**

Append to `internal/site/render_test.go`:

```go
func TestRenderLanding_FeaturedSection(t *testing.T) {
	hub := writeFixtureHub(t)
	// Feature the nested playlist, to prove the section is not limited to the
	// hub root, and give it a description so the card blurb renders.
	mustWrite(t, filepath.Join(hub, "synthpop", "bleep-bloop-bop.yaml"),
		"title: Bleep Bloop Bop\ncreator: les\nfeatured: true\ndescription: Bleeps and bloops.\ntracks:\n  - {title: T2, artist: A2}\n")
	root, err := BuildTree(hub)
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
	b, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	landing := string(b)
	if !strings.Contains(landing, `class="year featured">Featured`) {
		t.Error("landing missing Featured heading")
	}
	// The featured card links to the nested playlist and renders as a normal card.
	if !strings.Contains(landing, `class="playlist-card" href="/synthpop/bleep-bloop-bop/"`) {
		t.Error("landing Featured section missing the featured playlist card")
	}
	// Featuring is additive: the playlist still appears under its folder, and the
	// unfeatured root playlist is untouched in its year group.
	if !strings.Contains(landing, `href="/synthpop/"`) {
		t.Error("landing lost the folder listing")
	}
	if !strings.Contains(landing, `href="/2014-top-songs/"`) {
		t.Error("landing lost the unfeatured playlist")
	}
	// The Featured heading precedes the year groups.
	if strings.Index(landing, "Featured") > strings.Index(landing, `class="tree"`) {
		t.Error("Featured section should come before the tree section")
	}
}

func TestRenderLanding_NoFeatured(t *testing.T) {
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
	b, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	landing := string(b)
	if strings.Contains(landing, "Featured") {
		t.Error("landing should render no Featured heading when nothing is featured")
	}
	// The regular listing is unaffected.
	if !strings.Contains(landing, `class="playlist-card"`) {
		t.Error("landing lost its playlist cards")
	}
}
```

`os`, `filepath`, and `strings` are already imported in this file.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/site/ -run TestRenderLanding -v`
Expected: FAIL — `TestRenderLanding_FeaturedSection` reports the missing Featured heading (`TestRenderLanding_NoFeatured` passes already, which is the point of having it).

- [ ] **Step 3: Extract the card into its own template**

In `internal/site/templates/landing.html`, replace the body of the `{{range yearGroupsOf $children}}` cards loop so it calls a shared define. The `treeList` define becomes:

```
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
<div class="playlist-cards">
{{range .Playlists}}{{template "playlistCard" .}}{{end}}
</div>
{{end}}
{{end}}

{{define "playlistCard"}}
  <a class="playlist-card" href="/{{.Path}}/">
    {{with playlistCover .Playlist}}<img class="cover" src="{{.}}" alt="" loading="lazy">{{else}}<span class="cover placeholder"></span>{{end}}
    <span class="body"><span class="title">{{.Title}}</span><span class="meta">{{playlistMeta .Playlist}}</span>{{if .Playlist.Description}}<span class="blurb">{{plainText .Playlist.Description}}</span>{{end}}</span>
  </a>
{{end}}
```

- [ ] **Step 4: Add the Featured section to the landing `<main>`**

In the same file, replace:

```
<main>
{{if .Intro}}<section class="intro">{{.Intro}}</section>{{end}}
<section class="tree">{{template "treeList" .Root.Children}}</section>
</main>
```

with:

```
<main>
{{if .Intro}}<section class="intro">{{.Intro}}</section>{{end}}
{{with featuredOf .Root}}
<h2 class="year featured">Featured</h2>
<div class="playlist-cards">
{{range .}}{{template "playlistCard" .}}{{end}}
</div>
{{end}}
<section class="tree">{{template "treeList" .Root.Children}}</section>
</main>
```

`{{with}}` on an empty/nil slice renders nothing, so an unfeatured hub gets no heading and no empty grid. Only `landing.html` changes — `folder.html` keeps rendering `treeList` alone and gets no Featured section.

- [ ] **Step 5: Register the template function**

In `internal/site/render.go`, add to the `template.FuncMap`:

```go
		"featuredOf":    featuredOf,
```

Keep the existing gofumpt alignment of the map's values.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/site/ -v`
Expected: PASS — both new landing tests plus the pre-existing `TestRenderSite` (the card extraction must not change the emitted card markup).

- [ ] **Step 7: Verify and commit**

```bash
make lint && make test && make build
git add internal/site/templates/landing.html internal/site/render.go internal/site/render_test.go
git commit -m "feat(site): render a Featured section on the index page

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: `site-index.json` gains a `featured` array

**Files:**
- Modify: `internal/site/index.go`
- Test: `internal/site/index_test.go` (one new test; three existing tests migrate to the new shape)

**Interfaces:**
- Consumes: `featuredOf` from Task 2, `toIndexNodes` (existing).
- Produces: exported type `SiteIndex{ Featured []IndexNode; Children []IndexNode }` with JSON keys `featured` (omitted when empty) and `children`. `WriteIndexJSON(outDir string, root *Node) error` keeps its signature and now writes an object rather than an array. Task 5's JS consumes this shape.

- [ ] **Step 1: Migrate the three existing tests to the object shape**

All three tests in `internal/site/index_test.go` currently unmarshal into `[]IndexNode` and will fail against an object. In `TestWriteIndexJSON`, `TestIndexNodeImage`, and `TestIndexNodeYear`, replace each

```go
	var nodes []IndexNode
	if err := json.Unmarshal(data, &nodes); err != nil {
```

with

```go
	var idx SiteIndex
	if err := json.Unmarshal(data, &idx); err != nil {
```

and add, immediately after each unmarshal's error handling:

```go
	nodes := idx.Children
```

That keeps every existing assertion below it untouched. In `TestIndexNodeYear` the loop `for _, n := range nodes` also keeps working unchanged.

Additionally, in `TestWriteIndexJSON` add an assertion that the fixture hub (nothing featured) omits the key:

```go
	if len(idx.Featured) != 0 {
		t.Errorf("Featured = %+v, want empty for a hub with nothing featured", idx.Featured)
	}
	if strings.Contains(string(data), `"featured"`) {
		t.Errorf("unfeatured hub should omit the featured key:\n%s", data)
	}
```

Add `"strings"` to that file's imports.

- [ ] **Step 2: Write the failing test for the featured array**

Append to `internal/site/index_test.go`:

```go
func TestWriteIndexJSON_Featured(t *testing.T) {
	dir := t.TempDir()
	// Two featured playlists — one nested, one at the root — plus one unfeatured.
	mustWrite(t, filepath.Join(dir, "newer.yaml"),
		"title: Newer\nfeatured: true\ndate_updated: 2026-02-01T00:00:00Z\ntracks:\n  - {title: T, artist: X, image: 'http://img/n.jpg'}\n")
	mustWrite(t, filepath.Join(dir, "plain.yaml"),
		"title: Plain\ndate_updated: 2026-03-01T00:00:00Z\ntracks:\n  - {title: T, artist: X}\n")
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(sub, "older.yaml"),
		"title: Older\nfeatured: true\ndate_updated: 2025-01-01T00:00:00Z\ntracks:\n  - {title: T, artist: X}\n")

	root, err := BuildTree(dir)
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
	var idx SiteIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Flat, newest-first, absolute paths, and the nav fields the sidebar needs.
	if len(idx.Featured) != 2 {
		t.Fatalf("Featured = %+v, want 2 entries", idx.Featured)
	}
	if idx.Featured[0].Title != "Newer" || idx.Featured[1].Title != "Older" {
		t.Errorf("Featured order = %q, %q; want Newer, Older", idx.Featured[0].Title, idx.Featured[1].Title)
	}
	if idx.Featured[1].Path != "/sub/older/" {
		t.Errorf("nested featured Path = %q, want /sub/older/", idx.Featured[1].Path)
	}
	if idx.Featured[0].Meta != "1 track" || idx.Featured[0].Image != "http://img/n.jpg" {
		t.Errorf("featured entry missing nav fields: %+v", idx.Featured[0])
	}
	if idx.Featured[0].Year != 2026 {
		t.Errorf("featured Year = %d, want 2026", idx.Featured[0].Year)
	}

	// Featuring is additive: the tree still holds everything — the "sub"
	// directory plus the two root leaves.
	if len(idx.Children) != 3 {
		t.Errorf("Children = %d entries, want 3 (sub dir + 2 root leaves)", len(idx.Children))
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/site/ -run TestWriteIndexJSON -v`
Expected: FAIL — compile error, `SiteIndex` undefined.

- [ ] **Step 4: Implement `SiteIndex`**

In `internal/site/index.go`, add after the `IndexNode` type and change `WriteIndexJSON`:

```go
// SiteIndex is the payload of site-index.json: the flat Featured list plus the
// nav tree. Featured is sorted here rather than client-side because Go has the
// full playlist dates and IndexNode only carries the year.
type SiteIndex struct {
	Featured []IndexNode `json:"featured,omitempty"`
	Children []IndexNode `json:"children"`
}

// WriteIndexJSON writes the featured list and the nav tree (root's children) to
// site-index.json.
func WriteIndexJSON(outDir string, root *Node) error {
	idx := SiteIndex{Children: toIndexNodes(root.Children)}
	if featured := featuredOf(root); len(featured) > 0 {
		idx.Featured = toIndexNodes(featured)
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "site-index.json"), append(data, '\n'), 0o644)
}
```

The `len(featured) > 0` guard matters: `toIndexNodes` returns an empty non-nil slice, which `omitempty` still drops for a slice — the guard just keeps the intent explicit and cheap. Featured leaves have no children, so their `children` keys stay omitted.

Also update the `IndexNode` doc comment's first line to mention it is used for both the tree and the featured list:

```go
// IndexNode is the nav projection of a Node serialized into site-index.json (no
// track data beyond the summary Meta line). It appears both in the nav tree and
// in the flat featured list. Path is absolute-from-root with leading + trailing
// slashes.
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/site/ -v`
Expected: PASS — all four index tests plus the rest of the package.

- [ ] **Step 6: Verify and commit**

```bash
make lint && make test && make build
git add internal/site/index.go internal/site/index_test.go
git commit -m "feat(site): emit a featured array in site-index.json

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Featured group in the sidebar nav

**Files:**
- Modify: `internal/site/assets/site-nav.js`

**Interfaces:**
- Consumes: the `SiteIndex` JSON shape from Task 4 (`featured`, `children`).
- Produces: no Go API. `ByomSiteNav` gains `leaf(node, here)` (one nav row) and `renderFeatured(nodes, here)`.

**Note on testing:** this file has no automated test — there is no JS test harness in the repo and adding one is out of scope. Its correctness is verified by the real-browser smoke test in Task 6. Keep the change small and readable.

- [ ] **Step 1: Extract the nav row into a shared method**

In `internal/site/assets/site-nav.js`, add a `leaf` method to the class (place it just above `render`):

```js
  // One nav row — shared by the featured group and the year-grouped tree.
  leaf(n, here) {
    const active = n.path === here ? ' aria-current="page"' : '';
    const meta = n.meta ? `<span class="nav-meta">${esc(n.meta)}</span>` : '';
    const cover = n.image ? `<img class="nav-cover" src="${esc(n.image)}" alt="" loading="lazy">` : '';
    return `<li><a class="nav-leaf" href="${esc(n.path)}"${active}>${cover}<span class="nav-text">${esc(n.title)}${meta}</span></a></li>`;
  }
```

Then in `render`, replace the three lines that build a row inside the `for (const n of leaves)` loop:

```js
      const active = n.path === here ? ' aria-current="page"' : '';
      const meta = n.meta ? `<span class="nav-meta">${esc(n.meta)}</span>` : '';
      const cover = n.image ? `<img class="nav-cover" src="${esc(n.image)}" alt="" loading="lazy">` : '';
      items += `<li><a class="nav-leaf" href="${esc(n.path)}"${active}>${cover}<span class="nav-text">${esc(n.title)}${meta}</span></a></li>`;
```

with:

```js
      items += this.leaf(n, here);
```

Leave the `nav-year` label logic above it untouched.

- [ ] **Step 2: Add the featured group renderer**

Add another method next to `leaf`:

```js
  // The featured group sits at the top of the nav, above the folders and year
  // groups. The list arrives pre-sorted from site-index.json.
  renderFeatured(nodes, here) {
    if (!nodes || !nodes.length) return '';
    const items = nodes.map((n) => this.leaf(n, here)).join('');
    return `<ul class="nav-featured-list"><li class="nav-year nav-featured">Featured</li>${items}</ul>`;
  }
```

- [ ] **Step 3: Consume the new JSON shape**

In `connectedCallback`, replace:

```js
      const res = await fetch('/site-index.json');
      const nodes = await res.json();
      const here = location.pathname;
      this.innerHTML = `<nav class="site-nav">${this.render(nodes, here)}</nav>`;
```

with:

```js
      const res = await fetch('/site-index.json');
      const data = await res.json();
      // Tolerate the older top-level-array shape (e.g. a stale cached file): the
      // nav still renders, minus the featured group.
      const index = Array.isArray(data) ? { children: data } : data;
      const here = location.pathname;
      this.innerHTML = `<nav class="site-nav">${this.renderFeatured(index.featured, here)}${this.render(index.children || [], here)}</nav>`;
```

- [ ] **Step 4: Keep auto-centering on the tree occurrence**

A featured playlist appears twice in the nav, so the current page can carry `aria-current="page"` in both the featured group and the tree. `centerActive` currently grabs the first match, which would center on the featured row at the very top. In `centerActive`, replace:

```js
    const active = this.querySelector('a[aria-current="page"]');
```

with:

```js
    // The current page may appear twice (featured + tree); center on the tree
    // occurrence, falling back to whatever exists.
    const active =
      this.querySelector('ul:not(.nav-featured-list) a[aria-current="page"]') ||
      this.querySelector('a[aria-current="page"]');
```

- [ ] **Step 5: Check the asset is syntactically valid**

Run: `node --check internal/site/assets/site-nav.js`
Expected: no output (exit 0). If `node` is unavailable, load the built site in a browser in Task 6 and confirm the console is clean — a syntax error there blanks the nav entirely.

- [ ] **Step 6: Verify and commit**

```bash
make lint && make test && make build
git add internal/site/assets/site-nav.js
git commit -m "feat(site): show a Featured group at the top of the sidebar nav

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: end-to-end smoke test and docs

**Files:**
- Modify: `AGENTS.md` (the `internal/site/` layout bullet and the Conventions section)
- Modify: `README.md` (the site-generation section, near line 263)

**Interfaces:**
- Consumes: everything above. Produces nothing new.

- [ ] **Step 1: Build a real site with a featured playlist**

From the worktree root, using a scratch hub so no real hub file is edited:

```bash
mkdir -p /tmp/byom-featured/sub
printf 'title: Featured One\nfeatured: true\ndate_updated: 2026-02-01T00:00:00Z\ndescription: A featured mix.\ntracks:\n  - {title: T1, artist: A1}\n' > /tmp/byom-featured/one.yaml
printf 'title: Plain Two\ndate_updated: 2026-03-01T00:00:00Z\ntracks:\n  - {title: T2, artist: A2}\n' > /tmp/byom-featured/two.yaml
printf 'title: Nested Featured\nfeatured: true\ndate_updated: 2025-01-01T00:00:00Z\ntracks:\n  - {title: T3, artist: A3}\n' > /tmp/byom-featured/sub/three.yaml
./byom-sync site --dir /tmp/byom-featured --out /tmp/byom-featured-build
```

If those `site` flags differ, run `./byom-sync site --help` and use the actual flag names.

- [ ] **Step 2: Confirm the generated output**

```bash
cat /tmp/byom-featured-build/site-index.json
grep -c 'class="year featured">Featured' /tmp/byom-featured-build/index.html
```

Expected: `site-index.json` has a `featured` array with `Featured One` first and `Nested Featured` second (path `/sub/three/`), plus the full `children` tree; the grep prints `1`.

- [ ] **Step 3: Look at it in a browser**

Serve the build and open the landing page and a playlist page:

```bash
python3 -m http.server 8099 --directory /tmp/byom-featured-build
```

Confirm, with the browser console open:
- the landing page shows a `Featured` heading with two cards above the folder list and year groups;
- both featured playlists also still appear in their normal year group / folder;
- a playlist detail page's sidebar shows a `Featured` group at the top with both entries, above the folders and year labels;
- navigating to `/one/` highlights it in the nav and the sidebar scrolls to the tree occurrence, not the featured row;
- no console errors.

- [ ] **Step 4: Document the flag in AGENTS.md**

In the `internal/site/` bullet under **Layout**, append to the existing sentence about the index and nav:

```
  A playlist with `featured: true` is additionally promoted into a flat
  `Featured` list at the top of the index page and of the sidebar nav
  (`featuredOf` walks the whole tree); it keeps its normal position in the year
  groups and nav tree as well. `site-index.json` is an object —
  `{"featured": [...], "children": [...]}` — with the featured list pre-sorted
  server-side.
```

And add a bullet to **Conventions & gotchas**:

```
- **Featured playlists:** `featured: true` at the top level of a playlist file is
  a site-presentation flag — additive promotion into the Featured list on the
  index page and sidebar nav, ordered newest `date_updated` first with undated
  last. It works at any depth in the hub. Exporters ignore it. Ordering is shared
  with the per-folder sort via `site.playlistNodeLess`, so change both at once.
```

- [ ] **Step 5: Document the flag in README.md**

In the site section near line 263 (which lists `site-index.json`, `feed.xml`, and `CNAME`), add a short paragraph after it:

```markdown
Set `featured: true` on a playlist to promote it: featured playlists appear in a
`Featured` list at the top of the index page and at the top of the sidebar nav on
every playlist and folder page, newest first. They also keep their usual place in
the year groups and folder listings, and the flag works at any depth in the hub.
```

- [ ] **Step 6: Final verification and commit**

```bash
make lint && make test && make build
git add AGENTS.md README.md
git commit -m "docs: document the featured playlist flag

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 7: Clean up the scratch hub**

```bash
rm -rf /tmp/byom-featured /tmp/byom-featured-build
```

---

## Self-Review

**Spec coverage:**

| Spec section | Task |
|---|---|
| Additive, not exclusive | Tasks 3, 4 (assertions), Task 6 browser check |
| Whole tree | Task 2 (`featuredOf` recursion test) |
| Boolean flag, date order | Tasks 1, 2 |
| Presentation only | Global Constraints; no exporter touched |
| Data model | Task 1 |
| Collection and ordering, `playlistNodeLess` extraction | Task 2 |
| Index page, `playlistCard` extraction, FuncMap | Task 3 |
| No Featured section on folder pages | Task 3 Step 4 (only `landing.html` changes) |
| `site-index.json` object shape | Task 4 |
| Sidebar nav group + shape tolerance | Task 5 |
| Testing (4 test files) | Tasks 1-4 |
| Verification | every task's final step |
| Docs | Task 6 |

**Not in the spec, added here:** the `centerActive` fix (Task 5 Step 4). The spec's duplicate-`aria-current` consequence follows from additive featuring, and without this the sidebar would auto-scroll to the top of the nav on every featured playlist's page. Flagged rather than silently omitted.

**Type consistency:** `Playlist.Featured` (Task 1) → `featuredOf` / `playlistNodeLess` (Task 2) → `featuredOf` template func (Task 3) → `SiteIndex.Featured` / `SiteIndex.Children` (Task 4) → `index.featured` / `index.children` JSON keys and `leaf` / `renderFeatured` methods (Task 5). Names match across tasks.
