# Notes — byom-sync site generator (mixtapes-site)

**Branch:** `feat/mixtapes-site` (off `origin/main` @ 7017f2c)
**Date:** 2026-07-10

## What was built

A `byom-sync site` subcommand that compiles the playlist hub into a static
publishing site (first target: mixtapes.lmorchard.com). New `internal/site/`
package:

- `tree.go` — recursive hub walk → `Node` tree (dirs first, then playlists;
  filename-stem slugs; `index.md`/`README.md` intros).
- `paths.go` — output-path derivation + per-playlist `playlist.jspf.json` via the
  existing `export.JSPFExporter` (no new serialization).
- `index.go` — `site-index.json` nav data.
- `meta.go` — OG image (first track image), description, canonical URL helpers.
- `render.go` + `templates/` — landing / folder / playlist / embed pages via
  embedded `html/template`; player-owned provider config; OG/Twitter meta +
  `<noscript>` tracklist; `goldmark` for markdown intros.
- `assets.go` + `assets/` — `site.css` + `site-nav.js` (`<byom-site-nav>` web
  component, fetches `site-index.json`, highlights current path); CNAME.
- `feed.go` — RSS (newest-first by `date_created`) via `gorilla/feeds`.
- `site.go` — `Build` orchestrator.
- `cmd/site.go` + `site:` Viper config block + `.github/workflows/
  example-site-deploy.yml.example`.

New deps: `github.com/yuin/goldmark`, `github.com/gorilla/feeds` (both pure Go).

## Deviations from the plan

1. **`playlist.Track.Image` added to a shared package (accepted by Les).** The
   plan's OG-image feature needs `Track.Image`, which is not on `origin/main`
   (it's on `feat/spotify-enrich`/`feat/spotify-marker`). The identical additive
   field was added here, waiving the "no existing-package edits" constraint for
   this one field. Whichever branch merges to `main` second resolves a trivial
   one-line conflict.
2. **Breadcrumb home crumb bug fixed (review-caught, Important).** Plan code
   hardcoded the home crumb label `"mixtapes"`; changed to use the configured
   `Site.Title`, and the test now uses a distinct title to guard it.
3. **Example workflow renamed to `.yml.example` (review-caught, Medium).** As a
   `.yml` in byom-sync's own `.github/workflows/`, it would auto-run on push to
   `main` and attempt an unwanted Pages deploy. Renamed so GitHub won't register
   it; matches the repo's `byom-sync.yaml.example` convention.

## Minor findings deferred (see `.superpowers/sdd/progress.md`)

- `tree.go`: intro-file read swallows non-ENOENT errors as empty `IntroMD`.
- `render.go`: unused `markdown` template FuncMap entry; `renderMarkdown`
  swallows goldmark errors (effectively never errors on a bytes.Buffer).
- `assets_test.go`: `WriteCNAME` empty/hostless no-op branches untested.
- `feed.go`/`assets.go`: no `MkdirAll` (mitigated — `Build` creates OutDir first).
- `cmd/site_test.go`: first `cmd` test to mutate the global Viper singleton.

## Verification

- `make lint && make test && make build` — clean throughout (per-task + Task 9).
- Real-hub smoke test: built a fixture hub (`/tmp/mixtapes-hub`) with two
  subdirectories (`synthpop/`, `by-year/`), `index.md`, and a `README.md`, from
  copies of real playlists. Output tree, `site-index.json` (nested, dirs-first,
  filename-stem slugs, playlist titles), `CNAME`, and `feed.xml` all correct.
- Live browser check (Playwright, served over HTTP): on the deepest playlist
  page, `<byom-player>` upgraded from jsDelivr (shadow DOM rendered),
  `<byom-site-nav>` populated all 8 nested links from `site-index.json` with
  correct `aria-current` on the active page, breadcrumb home = site title. Embed
  page is chrome-less. The only console errors are third-party YouTube iframe
  cross-site-cookie/Feature-Policy noise from the `youtube` provider booting —
  none from the generated site.

## Follow-ups (fast-follows, out of scope)

- `sitemap.xml` (deferred; low priority).
- Optional explicit `published:` field if `date_created` feed ordering
  disappoints (all items currently share the sync "first seen" date).
- Playlist-level cover art: prefer it in `playlistImage` once the field lands.
- `sync` creating/maintaining hub subdirectories.
- `--preview`/local mode relaxing the `base_url` requirement.
