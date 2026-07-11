# Spec — Cover-art media cards on the site index + sidebar (UI layer)

## Goal

Show playlist cover art on the generated static site: as media cards on the
index and folder pages, and as small thumbnails in the left sidebar nav on
playlist/folder pages. Render a playlist's `Description` as a blurb on the cards
when present.

This is a UI-only feature built on top of the cover-art **infrastructure that
already exists on `main`** (PRs #29–#33). It supersedes the earlier
`2026-07-11-1137-site-cover-art` spec, which predated that infrastructure and
duplicated it.

## Baseline (what `main` already provides — do NOT rebuild)

- `Playlist.ImageFile` — a self-hosted playlist hero image path (hub-relative,
  e.g. `art/aa/hash.jpg` or `art/mosaic/<hash>.jpg`).
- `GenerateMosaics(hubDir, outDir, root)` runs first in `site.Build` and sets
  `p.ImageFile` **in-memory** for every playlist lacking an explicit cover
  (builds a mosaic from track covers). So by the time `WriteIndexJSON` and
  `RenderSite` run on the same `root`, essentially every playlist with track art
  has an `ImageFile`.
- `CopyArt(hubDir, outDir)` copies the whole `<hub>/art` store into the output;
  mosaics are written directly to `<outDir>/art/mosaic/`. The referenced image
  files therefore already exist in the output — **no per-cover copy step is
  needed**.
- `playlistImage(p, baseURL)` resolves a playlist's hero as an **absolute** URL
  for `og:image` (precedence: `p.ImageFile` → `p.Image` → first track
  `ImageFile` → first track `Image`).
- `tree.go` already skips the `<hub>/art` store when walking the hub.

## Decisions

- **Cover source:** reuse the existing hero resolution. Add a **root-relative**
  resolver `coverHref` (for on-page `<img src>` and the client-fetched
  `site-index.json`), and refactor `playlistImage` to delegate to it (DRY,
  behavior-preserving). Root-relative (not baseURL-absolute) so the images work
  under local preview and any host.
- **Index + folder pages:** responsive multi-column grid of horizontal media
  cards (hero at left, title/meta/blurb at right). Folder pages match the index
  (shared `treeList` template).
- **Sidebar nav:** compact rows with a small hero thumbnail beside each title.
  No blurb.
- **Placeholder:** keep a lightweight placeholder box for the rare playlist with
  no resolvable cover at all (e.g. a native playlist with no track art, so no
  mosaic). `coverHref` returns `""` in that case.
- **No** new art-copy step, **no** `siteCover`/`WriteCoverArt` (that was the
  duplicated infrastructure).

## Design

### 1. `coverHref` resolver + `playlistImage` refactor + template func + IndexNode.Image

`internal/site/meta.go`:

```go
// coverHref resolves a playlist's cover as a root-relative site path (leading
// "/") for a local file, or the remote URL as-is, or "" when none. Precedence
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
```

Refactor `playlistImage(p, baseURL)` to delegate (behavior-preserving — a
root-relative href becomes absolute; a remote URL passes through):

```go
func playlistImage(p *playlist.Playlist, baseURL string) string {
	href := coverHref(p)
	if strings.HasPrefix(href, "/") {
		return strings.TrimRight(baseURL, "/") + href
	}
	return href
}
```

Template func in `render.go` `NewRenderer` FuncMap: `"playlistCover": coverHref`.

`internal/site/index.go`: add `Image string json:"image,omitempty"` to
`IndexNode`; in `toIndexNodes`, for leaves set `n.Image = coverHref(c.Playlist)`.

### 2. Index/folder media cards (`templates/landing.html` + `assets/site.css`)

Replace ONLY the year-group leaf block of `treeList` (directories and year
headers unchanged) with a `.playlist-cards` grid of `.playlist-card` links:

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

CSS: `.playlist-cards` grid (`repeat(auto-fill, minmax(18rem, 1fr))`),
`.playlist-card` row (~84px cover, title/meta/blurb), `.cover.placeholder` fill.

### 3. Sidebar thumbnails (`assets/site-nav.js` + `assets/site.css`)

`site-nav.js` leaf render: add `<img class="nav-cover">` (URL escaped via the
existing `esc()`), wrap title+meta in a `nav-text` span, give the leaf link
class `nav-leaf`. Directory rendering unchanged. Conditional on `n.image`.

CSS: `.site-nav .nav-leaf` (flex row), `.nav-cover` (~2.2rem thumb), `.nav-text`.

## Testing

- `meta_test.go` — `coverHref`: playlist ImageFile (root-relative); playlist
  remote Image; first-track ImageFile preference; first-track remote; empty.
  Existing `playlistImage` tests must still pass unchanged (refactor is
  behavior-preserving).
- `index_test.go` — `IndexNode.Image` populated (root-relative) for a leaf;
  empty for directories.
- `render_test.go` — `.playlist-card` markup present; cover `<img>` for a
  playlist with art; `.cover placeholder` for a cover-less playlist; blurb iff
  `Description` non-empty.
- `assets_test.go` — copied `site-nav.js` contains `nav-cover`.

## Out of scope

- Player / JSPF track art.
- Any change to `CopyArt`, `GenerateMosaics`, or the hero/mosaic model.
- Backfilling `Description` on existing playlists.
