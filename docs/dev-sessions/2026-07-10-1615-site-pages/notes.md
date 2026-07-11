# Notes — site content pages (#21)

**Branch:** `feat/site-pages` (off `main` c00fd47)
**Date:** 2026-07-10

## What was built

`byom-sync site` now renders a directory of standalone markdown pages and links
them from the site header.

- `internal/site/pages.go` — `PageLink`, `ContentPage`, `parseFrontmatter`
  (leading `---` YAML block), `LoadPages` (glob `*.md`, render body via goldmark,
  desc via `firstParagraph`, sort by `(order, title)`, title falls back to
  filename; missing dir → no pages), `pageLinks`.
- `render.go` — `SiteMeta.Pages`, `contentPageData`, `Renderer.RenderPages`
  (writes `<out>/<slug>/index.html`).
- `templates/partials.html` — shared `siteheader` partial (title home-link +
  page nav); used by `landing`/`folder`/`playlist`/`page` (not `embed`).
- `templates/page.html` — content-page template (chrome + rendered body).
- `assets/site.css` — header flex layout, `.site-title` / `.page-nav` scoping.
- `site.go` — `Options.PagesDir`; `Build` loads pages, sets `Site.Pages`, calls
  `RenderPages`.
- `cmd/site.go` + `cmd/root.go` — `--pages` flag + `site.pages_dir` config
  (default `./pages`).
- `AGENTS.md` — documented under the `internal/site/` bullet.

No new dependencies (reused `goldmark` + `yaml.v3`).

## Deviations / notes

- A `chore(playlist)` commit re-aligned `Track` struct tags per gofumpt —
  fallout from the #19 merge resolution, whitespace only.
- `pageLinks` was defined in Task 1 but not consumed until Task 4's `Build`, so
  `make lint` reported `pageLinks is unused` through Tasks 2–3 (expected,
  sequencing); it cleared in Task 4 and lint is fully clean at the branch tip.

## Verification

- Per-task: TDD (RED/GREEN) + task reviews; `make lint && make test && make
  build` clean at Task 4 (0 lint findings).
- Real-hub smoke test: built the 60-playlist hub with a pages dir
  (`about.md` order 1, `colophon.md` order 2, `elsewhere.md` no frontmatter).
  Output: `about/`, `colophon/`, `elsewhere/` pages emitted; header nav on a
  playlist page ordered `elsewhere` (order 0), `About`, `Colophon`; the about
  page rendered its markdown body with `og:title` + header nav. `elsewhere`
  shows its filename as the label (documented no-frontmatter fallback).

## Minor findings deferred (see .superpowers/sdd/progress.md)

- `LoadPages` returns empty non-nil slice (not nil) on missing dir; `LoadPages("")`
  globs CWD. Harmless in practice (default `pages_dir` is `./pages`).
- CRLF frontmatter + malformed-inner-YAML degrade silently, untested.

## Follow-ups (out of scope)

- Nested/organized pages directory.
- `nav: false` frontmatter (render a page without a header link).
- Active-page highlight in the header nav.
