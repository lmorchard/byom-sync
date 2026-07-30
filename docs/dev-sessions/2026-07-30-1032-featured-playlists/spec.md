# Spec: featured playlists

## Goal

Let a playlist mark itself as featured. Featured playlists appear in a dedicated
list at the top of the site's main index page and at the top of the sidebar nav.

## Decisions

- **Additive, not exclusive.** A featured playlist appears in the Featured list
  *and* keeps its normal position in the year groups and nav tree. Nothing
  disappears from the regular listing.
- **Whole tree.** Any playlist anywhere in the hub can be featured, including
  ones inside subdirectories. The Featured list is flat.
- **Boolean flag, date order.** `featured: true`. The Featured list sorts
  newest-`date_updated`-first, matching every other listing on the site. No
  hand-ordering; add it later if the need shows up.
- **Presentation only.** Exporters (jspf, m3u8, markdown) ignore `featured`.

## Data model

One new field on `playlist.Playlist`:

```go
// Featured promotes this playlist into the site's Featured list (index page
// and sidebar nav). Presentation only; exporters ignore it.
Featured bool `yaml:"featured,omitempty"`
```

`omitempty` keeps it out of files that don't use it.

## Collection and ordering

`featuredOf(root *Node) []*Node` in `internal/site/grouping.go` walks the whole
tree recursively and returns the leaves whose playlist is featured, ordered:

1. newest `DateUpdated` first,
2. undated (zero `DateUpdated`) last,
3. ties broken by `Title`.

That comparator already exists inline in `buildDir`'s sort. Extract it as
`playlistNodeLess(a, b *Node) bool` and call it from both places, so the
Featured order and the per-folder order cannot drift apart.

## Index page

`landing.html` gains a Featured section above the folder list and the year
groups:

```
{{with featuredOf .Root}}
<h2 class="year featured">Featured</h2>
<div class="playlist-cards">…cards…</div>
{{end}}
```

- The card markup currently lives inline in `treeList`. Extract it as
  `{{define "playlistCard"}}` and use it from both call sites — one card
  definition, not two.
- `with` means no featured playlists renders no heading and no empty grid.
- Reusing the existing `.year` and `.playlist-cards` rules needs no new CSS. The
  extra `featured` class is a hook for accenting the heading later.
- `featuredOf` is registered in the `template.FuncMap` in `render.go`.

Folder pages (`folder.html`) do **not** get a Featured section; only the main
index page does.

## site-index.json

The file changes from a top-level array to an object:

```go
type SiteIndex struct {
    Featured []IndexNode `json:"featured,omitempty"`
    Children []IndexNode `json:"children"`
}
```

- `Featured` holds flat leaf `IndexNode`s carrying their absolute paths,
  pre-sorted in Go where the full dates are available. The sidebar renderer does
  no sorting of its own.
- `IndexNode` itself is unchanged.
- `Children` remains the root's children, exactly as before.

This is a breaking change to the JSON shape. It is acceptable because the
generator and `site-nav.js` ship together and both regenerate on every build.

## Sidebar nav

`site-nav.js` renders a Featured group at the top of the nav, above the folders
and the year groups:

```html
<li class="nav-year nav-featured">Featured</li>
```

followed by the usual `nav-leaf` rows (cover, title, meta, `aria-current` when
it is the current page). No new CSS required; `.nav-featured` is a styling hook.

The fetch handler tolerates both shapes:

```js
const index = Array.isArray(data) ? { children: data } : data;
```

so a browser-cached older `site-index.json` degrades to "no Featured group"
rather than a blank nav.

`folder.html` renders the same `<byom-site-nav>`, so the Featured group appears
in the sidebar on folder pages as well as playlist detail pages. That is
intended: the nav looks the same everywhere it appears.

## Testing

Test-driven, in the existing test files:

- `internal/playlist/types_test.go` — `featured` YAML round-trips; absent field
  means `false`; `false` is omitted on save.
- `internal/site/grouping_test.go` — `featuredOf` finds featured leaves nested in
  subdirectories, skips unfeatured ones, orders newest-first with undated last
  and title-broken ties, and returns empty for a tree with none.
- `internal/site/index_test.go` — `site-index.json` marshals to the object shape;
  `featured` is present and ordered when there are featured playlists and omitted
  when there are none; `children` matches the previous content.
- `internal/site/render_test.go` — the landing page contains the Featured heading
  and the featured playlist's link when one is featured, and contains neither
  when none is.

## Verification

`make lint && make test && make build`, reading the output.

## Docs

Note the flag in `AGENTS.md` (the `internal/site/` bullet and a line in the
conventions section) and in `README.md` where site generation is described.

## Out of scope

- Hand-ordering the Featured list.
- Featured sections on folder pages.
- Any exporter or feed change.
- Config to rename the "Featured" heading.
