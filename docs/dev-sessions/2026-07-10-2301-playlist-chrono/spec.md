# Spec — reverse-chronological playlists + year headers

**Date:** 2026-07-10
**Status:** Draft (brainstorm complete, pending review)
**Slug:** playlist-chrono
**Issue:** [#26](https://github.com/lmorchard/byom-sync/issues/26)

## Summary

Make the `byom-sync site` index and sidebar chronological, now that playlists
carry real dates (#24):

1. Playlists ordered **reverse-chronologically by `date_updated`** (newest first).
2. **Year separator headers** grouping playlists by their `date_updated` year, on
   both the landing/folder pages and the sidebar nav.
3. The per-playlist metadata line shows a **`date_created` – `date_updated` range**
   (e.g. `Feb 2023 – Jun 2026`).

## Context

`internal/site/` sorts each directory's children dirs-first then alphabetically
(`tree.go`), and `playlistMeta` (`meta.go`) currently ends with the
`date_created` month. Playlist dates (`playlist.Playlist`, all `time.Time`):

- `DateCreated` — earliest track `added_at` (start of curation).
- `DateUpdated` — latest track `added_at` (most recent curation).
- `DateImported` — first-seen; the fallback both above use when no track has a
  parseable `added_at`.

## Design

### Ordering (in the tree walk)

Change `buildDir`'s sort (`tree.go`) so that within each directory's children:

1. Directories first (unchanged), alphabetical by `Name`.
2. Then playlists by `DateUpdated` **descending** (newest first).
3. Undated playlists (`DateUpdated.IsZero()`) sort **last**.
4. Ties broken by `Title`.

Because ordering lives in `BuildTree`, the landing page, folder pages, and
`site-index.json` (→ sidebar) all inherit it.

### Year grouping (presentation)

At each directory level, render **folders first** (as folder entries), then the
level's playlists split into **year groups**, each preceded by a separator
header (the `DateUpdated` year, descending). Undated playlists fall under a
trailing **"Undated"** header.

- Two template helpers over a level's `[]*Node`:
  - `dirsOf(children)` → the directory children.
  - `yearGroupsOf(children)` → ordered `[]YearGroup{ Label string; Playlists []*Node }`
    where `Label` is the year (e.g. `"2016"`) or `"Undated"`, in the children's
    existing (already reverse-chron) order.
- `landing.html` / `folder.html` iterate `dirsOf` (recursing `treeList` for each)
  then `yearGroupsOf` (a `<h2 class="year">` header + its playlist list).
- Nested folders recurse through the same helpers.

### Sidebar (`site-nav.js` + `site-index.json`)

- Add `year int` to `IndexNode` for playlist leaves (the `DateUpdated` year; `0`
  when undated). `index.go` populates it.
- `site-nav.js`: while rendering a level's children, emit dirs as today, then
  walk the playlist leaves inserting a year-separator element whenever the year
  changes (data is already ordered). `0` renders as "Undated".

### Metadata line (date range)

Replace the single `date_created` month in `playlistMeta` (`meta.go`) with a
range built from both dates:

- Both present and **same month-year** → one value (`Dec 2014`).
- Both present, different → `Feb 2023 – Jun 2026` (`created – updated`, en dash).
- Only one present → that one.
- Neither → omit the segment.

A helper `dateRange(created, updated time.Time) string` formats it; each side via
`Format("Jan 2006")`.

## Non-goals / untouched

- Playlist / embed / content pages, RSS, JSPF — unchanged.
- No new config or dependencies.

## Testing

- `tree.go`: sort orders playlists by `DateUpdated` desc, undated last, dirs
  still first + alphabetical.
- `meta.go`: `dateRange` — same-month collapse, distinct range, single-date, and
  empty cases; `playlistMeta` includes the range.
- `index.go`: `IndexNode.Year` set for leaves (0 when undated), absent/zero for
  dirs.
- Rendering: landing/folder show `<h2 class="year">` headers in descending order
  with playlists grouped under the right year; an "Undated" group when present.
- `site-nav.js` behavior is manual/JS (verified in the browser smoke test).

## Open questions / fast-follows (out of scope)

- Collapsible year groups in the sidebar.
- A "sort by created vs updated" toggle.
