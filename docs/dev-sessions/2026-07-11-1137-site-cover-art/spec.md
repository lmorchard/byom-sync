# Spec — Cover art on the site index + sidebar nav

## Goal

Show playlist cover art on the generated static site: as media cards on the
index and folder pages, and as small thumbnails in the left global nav on
playlist/folder pages. Render a playlist's description as a blurb on the cards
when it has one (none do yet, so this is conditional and forward-looking).

Scope is limited to the site generator (`internal/site/`). No new commands, no
config, no hub-schema changes. Track art *inside* the player (via JSPF) is out
of scope for this arc.

## Decisions

- **Image source: self-host, remote fallback.** Prefer a downloaded local copy
  from the hub's content-addressed `art/` store; fall back to the remote image
  URL when no local copy exists. Only covers actually referenced by the site get
  copied into the output — not the whole store.
- **Index + folder pages** use a responsive multi-column grid of horizontal
  media cards (bigger cover at left, text at right). Folder pages match the
  index exactly (they already share the `treeList` template).
- **Sidebar nav** uses compact rows with a small cover thumbnail beside each
  playlist title. No blurb (no room).
- Player/JSPF track art is unchanged.

## Design

### 1. Cover resolution — `siteCover` (pure helper, `meta.go`)

```go
// siteCover returns how a playlist's cover should be referenced on the site.
// href is what pages/JSON link to; local is the hub-relative source path to
// copy into the output when the cover is self-hosted (empty otherwise).
func siteCover(p *playlist.Playlist) (href, local string)
```

Resolution order:
1. `p.Image != ""` → `href = p.Image` (remote; playlists carry no local copy at
   the playlist level), `local = ""`.
2. First track with `ImageFile != ""` → `href = "/" + ImageFile` (root-relative
   `/art/<hh>/<hash>.ext`), `local = ImageFile` (hub-relative source to copy).
3. First track with `Image != ""` → `href = Image` (remote), `local = ""`.
4. None → `("", "")`.

A thin template func `playlistCover(p) string` returns just `href`.

`playlistImage` (existing og:image helper) is left as-is; it stays remote-only
for meta tags.

### 2. Self-hosting the art — `WriteCoverArt` (new, `assets.go` or `coverart.go`)

In `site.Build`, after `BuildTree`, walk the tree, collect the set of non-empty
`local` paths from `siteCover` over every playlist node, and copy each from
`HubDir` into `OutDir` preserving its relative path (`art/<hh>/<hash>.ext`).

- Deduplicate paths (the content-addressed store already dedupes bytes; multiple
  playlists may share a first-track cover).
- A missing source file is skipped silently (leaves a broken `<img>` rather than
  aborting the build). This shouldn't happen after a normal
  `resolve art --download` pipeline; noted as a known edge.

### 3. Index + folder pages (`templates/landing.html`, `treeList`)

Playlist leaves change from `<li>` items to a responsive grid:

```html
<div class="playlist-cards">
  {{range .Playlists}}
  <a class="playlist-card" href="/{{.Path}}/">
    {{with playlistCover .Playlist}}
      <img class="cover" src="{{.}}" alt="" loading="lazy">
    {{else}}
      <span class="cover placeholder"></span>
    {{end}}
    <span class="body">
      <span class="title">{{.Title}}</span>
      <span class="meta">{{playlistMeta .Playlist}}</span>
      {{if .Playlist.Description}}<span class="blurb">{{.Playlist.Description}}</span>{{end}}
    </span>
  </a>
  {{end}}
</div>
```

- Grid: `repeat(auto-fill, minmax(300px, 1fr))` → 3 cols wide, reflowing to 2/1.
- Cover ~84px square; placeholder box keeps alignment when a playlist has no art.
- Year headers (`<h2 class="year">`) still precede each group, spanning the row.
- Directory children keep their existing simple `tree-list` rendering.

### 4. Sidebar nav (`index.go`, `assets/site-nav.js`, `assets/site.css`)

- `IndexNode` gains `Image string json:"image,omitempty"`, populated from
  `siteCover(...).href` for leaves in `toIndexNodes`.
- `site-nav.js` renders a small cover `<img class="nav-cover">` beside each
  leaf's title (compact "Option A" row). Escapes/handles the empty case.
- New `.site-nav .nav-cover` CSS (~40px, flush left, rounded).

### 5. Styling (`assets/site.css`)

- `.playlist-cards` grid + `.playlist-card` row (cover / body / title / meta /
  blurb), matching the approved mockup.
- `.playlist-card .cover.placeholder` neutral fill.
- Sidebar `.nav-cover` thumbnail.

## Testing

- `meta_test.go` — `siteCover`: playlist-level image; first-track local-file
  preference (href root-relative + local path returned); remote fallback; empty.
- `index_test.go` — `Image` populated in the `site-index.json` projection for a
  leaf; absent/empty handled.
- Art-copy test — `WriteCoverArt` copies a referenced local file into the output
  at the preserved relative path; skips a missing source; ignores remote-only.
- `render_test.go` — `playlist-card` markup present; blurb rendered iff
  `Description` non-empty; placeholder when no cover.

## Out of scope

- Player / JSPF track art.
- Playlist-level cover art field on the hub schema.
- Backfilling `Description` on existing playlists.
