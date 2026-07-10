# Spec — `byom-sync site` static site generator

**Date:** 2026-07-10
**Status:** Draft (brainstorm complete, pending review)
**Slug:** mixtapes-site

## Summary

Add a `byom-sync site` subcommand that compiles the playlist hub into a
navigable static mini-site: one page per playlist, each embedding a
`<byom-player>`, organized by a tree that mirrors the hub's directory structure.
The first target is **mixtapes.lmorchard.com** — a content-publishing site Les
links to, and embeds from, his blog.

## Context

- **byom-sync** owns the hub: a directory of playlist YAML files, a `playlist`
  package (`Load`/`LoadFile`/`Slug`), and an `export` package with a tested
  `JSPFExporter` that already emits exactly what `<byom-player>` consumes.
- **byom-player** is a framework-agnostic web component distributed via jsDelivr.
  It already provides: a built-in multi-playlist picker (`<byom-playlist>`
  children), a settings panel (⚙) where a visitor chooses a provider and enters
  credentials (persisted to `localStorage`), and host-deployment attributes
  (`provider`, `providers`, `youtube-search-endpoint`, `spotify-client-id`,
  `theme`, `no-settings`, `src`).
- The site generator is a new **exporter/subcommand inside byom-sync**, not a
  separate repo. It reuses the playlist loader and the JSPF exporter; it stays in
  Go with no new JS build pipeline.

## Audience & the provider question

The site is **public and browsable, with playback provider configurable** and a
zero-infra default. Crucially, the layered-provider behavior is **already solved
by the player**: the generator only sets a default `provider`, an optional
`providers` list for the picker, and host deployment attributes. Visitors switch
providers and enter credentials in the player's own settings panel. The generator
does not build a provider switcher.

## Non-goals / scope boundaries

- **No changes to `sync`/`export`/`Load`.** The recursive walk lives in the new
  `site` package so existing single-dir behavior is untouched. Making `sync`
  itself create/maintain hub subdirectories is a **separate future change**; for
  now, playlists are hand-filed into subfolders and the site mirrors them.
- **No provider-switcher UI** — the player owns that.
- **No live deployment of mixtapes.lmorchard.com** in this session. We ship the
  command + an example GitHub Actions workflow; wiring the real site (against
  Les's content) and deploying is Les's to run.
- **Static tracklist rendering is deferred to the player** — pages stay thin.

## Design

### Placement

New subcommand `byom-sync site [--config …]` and a new `internal/site/` package.
Follows the existing Cobra command + `internal/*` package conventions.

### Recursive walk & tree model

A new recursive traversal of the hub builds a tree:

- Each directory is a node; each `*.yaml` is a playlist leaf.
- `index.md` at the hub root supplies landing prose.
- `README.md` in any subdirectory supplies that folder's intro blurb.
- The same tree data is serialized to `site-index.json` (see Navigation).

`playlist.Load` (single-dir glob) is left as-is; the walk is site-local.

### Output layout & URL scheme

Directory-style URLs that mirror the hub. **URL slug = the YAML filename stem**
(author-controlled and stable; titles change). Given:

```
hub/
  index.md
  2014-top-songs.yaml
  synthpop/
    README.md
    bleep-bloop-bop.yaml
```

emit:

```
dist/
  index.html                              # landing: prose + static tree
  site-index.json                         # nav data (regenerated each build)
  assets/site.css                         # embedded, init-overridable
  assets/site-nav.js                      # embedded <byom-site-nav> component
  CNAME
  feed.xml                                # RSS
  2014-top-songs/
    index.html                            # playlist page
    playlist.jspf.json                    # produced by JSPFExporter
    embed/index.html                      # chrome-less iframe variant
  synthpop/
    index.html                            # folder index: README blurb + listing
    bleep-bloop-bop/
      index.html
      playlist.jspf.json
      embed/index.html
```

Per-playlist JSPF is written next to its page and produced by reusing the
existing `JSPFExporter` — no new serialization.

### Config (`site:` block in `byom-sync.yaml`)

```yaml
site:
  base_url: https://mixtapes.lmorchard.com   # OG tags, canonical URLs, CNAME, feed
  title: mixtapes
  out_dir: ./dist
  provider: youtube                          # default the pages boot with
  providers: [youtube, spotify]              # optional: offered in the picker
  youtube_search_endpoint: https://…         # host attrs baked into every page
  spotify_client_id: …
  player_src: https://cdn.jsdelivr.net/gh/lmorchard/byom-player@dist/byom-player.js
```

Everything except `base_url` has a sensible default. `player_src` defaults to the
pinned jsDelivr dist URL and is overridable (e.g. for a local player build).

### Page types & templating

Go `html/template`, embedded defaults, `init`-overridable — mirroring the
existing markdown-exporter template convention (`internal/templates`).

- **`base.html`** — `<head>` (title, description, canonical, Open Graph +
  Twitter card, cover image), site header/footer, the
  `<script type="module" src="{player_src}">` tag.
- **`landing.html`** — `index.md` prose + a **static** top-level tree (crawlable;
  browsing is the home page's job) with nested-folder previews (each folder shows
  a few of its playlists + a "+N more" link to the folder page).
- **`folder.html`** — `README.md` blurb + that folder's playlists + previews of
  subfolders.
- **`playlist.html`** — static breadcrumb + `<byom-site-nav>` sidebar +
  `<byom-player provider=… src="playlist.jspf.json" …>`. Cover, description, and
  tracklist come from the player.
- **`embed.html`** — chrome-less: full-bleed `<byom-player>`, nothing else. This
  is what a blog `<iframe>` points at. Includes a small "open on {title} ↗"
  attribution link back to the full page.
- **`feed.xml`** — RSS (see below).

### Metadata

Per-page `<head>` carries Open Graph + Twitter Card tags for good link previews
on Les's blog and in chats:

- **title** — playlist/folder title.
- **description** — playlist `description`/annotation, or folder README excerpt.
- **image** — playlist cover art when present, else the first track's `image`.
- **canonical** — `base_url` + path.

A `<noscript>` track list on playlist pages is the no-JS/search floor.

### Navigation (`site-index.json` + `<byom-site-nav>`)

Motivation: keep global-nav changes from rewriting every page, enabling
incremental publishing (add a playlist → redeploy `site-index.json` + the new
page; existing pages pick up the new nav on next load).

- The build emits `site-index.json` (the full tree) and a tiny **vanilla web
  component** as an embedded static asset, `assets/site-nav.js` (~a screen of
  code): `customElements.define('byom-site-nav', …)`, fetches the JSON, renders
  the tree, highlights the current path from `location.pathname`.
- Interior pages (playlist + folder) include `<byom-site-nav>` + the script.
- **Progressive-enhancement floor:** each page keeps a **static breadcrumb**
  derived from its own path (stable across global-nav edits, no rebuild needed,
  works before JS). The browsable sidebar is the enhancement on top.
- **Landing page tree stays static** (crawlable home/index).
- The component lives in the byom-sync site package (embedded, hand-written
  vanilla JS) — byom-sync has no JS build, so a static file stays self-contained
  and avoids stretching byom-player's scope.

Trade-off (accepted): the sidebar is JS-dependent. Content is always reachable
(static breadcrumb + fully static landing tree); only the convenience sidebar
needs JS.

### RSS

Use `github.com/gorilla/feeds` (clean; RSS/Atom/JSON) rather than hand-rolled
XML. Items = playlists, newest first by `date_created`, each linking to its page
(`base_url` + path) with title + description.

Caveat: `date_created` is "first synced," not true authoring date — acceptable
for v1. An optional explicit `published:` field is an easy later add.

### Assets

`site.css` (site chrome only — the player styles itself and rides byom-player's
visual-design work) and `site-nav.js`, both embedded and `init`-overridable.

## Deploy

Two distinct things; only the first is this session's job:

1. **The `byom-sync site` command** — builds `dist/` from a hub. Delivered here:
   command, templates, embedded assets, tests, docs.
2. **The live mixtapes.lmorchard.com deployment** — runs against Les's content
   hub (wherever that lives, likely a separate repo). We include an **example
   GitHub Actions workflow** (install byom-sync → `byom-sync site` → deploy
   `dist/` to Pages, with `CNAME`). Wiring and running the real site is Les's;
   no deployment happens in this session.

## Testing

Go tests (`make test`/`make lint`/`make build`), following byom-sync conventions:

- Recursive walk / tree building from a fixture hub (nesting, `index.md`,
  `README.md`).
- Slug + output path derivation (filename stems, nested paths, embed paths).
- Template rendering against the fixture hub (pages exist, key metadata present).
- JSPF emission per playlist (reuses the exporter's already-tested path).
- RSS feed (item count, ordering, links).

## Open questions / fast-follows

- **`sitemap.xml`** — deferred; low concern per Les. Easy later add.
- **Explicit `published:` field** — if `date_created` ordering in the feed proves
  wrong, add an optional authoring date.
- **`sync` managing hub subdirectories** — separate future change; out of scope.
- **Playlist-level cover art** — being added in a parallel effort; the generator
  reads playlist cover when available, else falls back to first-track `image`.
