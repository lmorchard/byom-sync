# Notes — reverse-chronological playlists + year headers (#26)

**Branch:** `feat/playlist-chrono` (off `main` e1506f7)
**Date:** 2026-07-10

## What changed

- `tree.go` — playlists sort by `DateUpdated` descending (undated last, ties by
  title); dirs still first + alphabetical. All consumers inherit the order.
- `meta.go` — the metadata line's trailing date is now a `date_created –
  date_updated` range (`dateRange`/`monthYear`), collapsing to one value when the
  months match, showing the present side when only one, omitted when neither.
- `grouping.go` (new) — `dirsOf` / `yearGroupsOf`; `treeList` restructured
  (landing + folder) to render folders first, then per-year `<h2 class="year">`
  groups; undated under a trailing "Undated" header.
- `index.go` + `site-nav.js` — `IndexNode.Year` (guarded against zero-time
  `.Year()==1`); the sidebar renders dirs then year-separated leaves
  (`<li class="nav-year">`).
- `site.css` — `.year` (page) + `.site-nav .nav-year` (sidebar) styles.

Grouping/sort key is `date_updated`; year label = its year. No new deps.

## Verification

- Per-task TDD + reviews; `make lint && make test && make build` clean.
- Real-hub smoke (60 playlists + pages dir): landing year headers descending
  (2026 → 2015), playlists newest-first within each year, metadata ranges
  rendering (e.g. `Apr 2020 – Jul 2026`, and single `Jul 2026` when created and
  updated share a month), and `site-index.json` carries per-leaf `year` for the
  sidebar separators.

## Minor findings deferred (.superpowers/sdd/progress.md)

- `tree_test.go`: Title tie-break branch untested (brief-inherited, Low).

## Follow-ups (out of scope)

- Collapsible year groups in the sidebar; a created-vs-updated sort toggle.
