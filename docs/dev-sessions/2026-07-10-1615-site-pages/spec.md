# Spec — static content pages for `byom-sync site`

**Date:** 2026-07-10
**Status:** Draft (brainstorm complete, pending review)
**Slug:** site-pages
**Issue:** [#21](https://github.com/lmorchard/byom-sync/issues/21)

## Summary

Extend the `byom-sync site` generator to render a directory of standalone
markdown pages (e.g. "about", "colophon", "elsewhere") into HTML and link them
from the site header. These are simple informational pages, distinct from
playlist pages — the second content type the site emits.

## Context

`byom-sync site` (shipped in #19) compiles the playlist hub into landing / folder
/ playlist / embed pages. It has no place for non-playlist prose. The site is a
small publishing site (mixtapes.lmorchard.com) that wants a few static pages.

The generator lives in `internal/site/` (Go, `html/template`, embedded assets,
`goldmark` for markdown, reuses `internal/export.JSPFExporter`).

## Design

### Source & config

- New config key **`site.pages_dir`**, default **`./pages`**.
- Every `*.md` file in that directory becomes one content page.
- If the directory is absent → no pages and no header nav (graceful; the feature
  is opt-in by creating the directory).

### Frontmatter

Each page file starts with a small YAML frontmatter block, parsed with the
`gopkg.in/yaml.v3` already in the project (no new dependency):

```markdown
---
title: About
order: 1
---
Body markdown here…
```

- `title` → the header link label. Fallback: the filename stem when absent.
- `order` → int, default `0`. Header links sort by `(order, title)`.
- The body (everything after the closing `---`) is rendered to HTML via
  `goldmark`, exactly like `index.md` / `README.md` intros.
- A file with no frontmatter is still valid: whole file is the body, title falls
  back to the filename stem, order `0`.

### Output & URLs

- `pages/about.md` → `dist/pages/about/index.html`, served at `/pages/about/`.
- Slug = filename stem, consistent with playlist URLs. The top-level `pages`
  path segment is reserved for content pages; a playlist/folder named `pages`
  would collide with it.

### Header nav

- Pages, sorted by `(order, title)`, render as links in the **site header**.
- Shown on the landing, folder, playlist, and content pages — **not** on the
  chrome-less `/embed/` variant.
- Threaded via a new `SiteMeta.Pages []PageLink` (slug + title), which is already
  part of every page's template data. A shared `siteheader` partial renders the
  title (as a home link) plus the page-nav links, used uniformly by all page
  types.
- Per-page `<head>` metadata: OG title (page title), description (first
  paragraph of the body), canonical (`base_url` + `/slug/`).

### New template

`page.html` — the standard site chrome (`siteheader` with nav + footer) wrapping
the rendered markdown body. No player, no sidebar; single column.

### Visual note

The landing header's title becomes a link (matching the interior pages) so the
shared `siteheader` partial is uniform. Styled to look the same as today.

## Non-goals / deliberately untouched

- **Playlists, `site-index.json`, the sidebar, and RSS** — content pages are
  header-only. They do not appear in the playlist tree or the feed.
- No CMS features (collections, taxonomies, drafts, per-page templates).
- No nested pages directory (flat `*.md` only for v1).

## Testing

- Frontmatter parsing: with frontmatter, without frontmatter, `order` sorting,
  title fallback to filename.
- Page render: markdown → HTML body present; header nav present and correctly
  ordered; OG metadata present.
- Missing `pages_dir` → graceful no-op (no pages, no header nav, no error).
- Header nav also renders on a playlist page; embed pages have none.
- End-to-end `Build` emits the content pages alongside the playlist output.

## Open questions / fast-follows (out of scope)

- Nested/organized pages directory.
- `nav: false` frontmatter to render a page without a header link.
- Active-page highlight in the header nav.
