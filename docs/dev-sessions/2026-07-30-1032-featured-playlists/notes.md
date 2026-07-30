# Notes: featured playlists

## What shipped

`featured: true` on a playlist promotes it into a flat `Featured` list at the top
of the landing page and at the top of the sidebar nav. Promotion is additive: the
playlist keeps its normal position in the year groups, folder listings, and nav
tree.

Commits on `feat/featured-playlists`:

| Commit | What |
|---|---|
| `f840472` | spec |
| `1b897fd` | plan |
| `f476531` | `Playlist.Featured` field |
| `a9f2a99` | `featuredOf` + `playlistNodeLess`, `buildDir` routed through the shared comparator |
| `7f466a9` | `playlistCard` extraction + Featured section on the landing page |
| `d0b835f` | `SiteIndex` object shape for `site-index.json` |
| `e763cc2` | sidebar Featured group, shared `leaf` row, shape tolerance, `centerActive` fix |
| `6de7e1e` | AGENTS.md + README.md |
| `88cb693` | rule under the sidebar Featured group |

## Deviations from the plan

**Added a CSS rule** (`88cb693`), against the plan's "add no new CSS" constraint.
Les looked at the smoke-test screenshot and read the folder tree beneath the
Featured group as *nested inside* it — the group had no visual terminator. Two
rules on `.nav-featured-list` fix it (bottom border matching `.year`'s, plus
zeroing the first label's top margin). The data was already flat; this was purely
a reading problem, and worth knowing that a flat group followed by a nested tree
needs an explicit break.

**One test assertion was wrong, not the code.** `TestWriteIndexJSON_Featured`
asserted `Meta == "1 track"`, but `playlistMeta` appends a date when
`date_updated` is set, so dated fixtures get `1 track · Feb 2026`. Changed to a
prefix check. The existing fixture hub has undated playlists, which is why the
older tests get the bare string.

## Things worth remembering

- **`site-index.json` is now an object**, not an array:
  `{"featured": [...], "children": [...]}`. `site-nav.js` tolerates the old array
  shape (`Array.isArray(data) ? { children: data } : data`) so a browser-cached
  older file degrades to "no Featured group" rather than a blank nav. Verified by
  actually swapping an array-shaped file into the build and reloading.
- **Duplicate `aria-current="page"`** is a real consequence of additive
  promotion: a featured playlist's own page marks it current in both the featured
  group and the tree. `centerActive()` grabbed the first match and would scroll to
  the top of the nav, so it now prefers
  `ul:not(.nav-featured-list) a[aria-current="page"]`.
- **The landing page already recursed into subfolders** before this change —
  `treeList` renders nested playlists' cards under their folder. So a nested
  featured playlist legitimately appears twice on the landing page (featured card
  + folder recursion). Not a regression.
- **Assets are `go:embed`-ed.** Editing `site.css` or `site-nav.js` and re-running
  a previously-built `./byom-sync` silently serves the old asset. `make build`
  first. Cost a confusing `grep -c` returning 0.
- **`byom-sync site` flags are `--input` / `--out` / `--pages` / `--base-url`**
  (not `--dir`), and `--base-url` is required.

## Process note

The Bash tool's cwd persists across calls, and a `cd /tmp/...` mid-session left a
later `git add` running in the *primary* checkout on `main` instead of the
worktree. It staged nothing (edits had gone to the worktree via absolute paths)
and failed safely, but the lesson holds: after any `cd` outside the worktree,
`cd` back and confirm `git branch --show-current` before touching git.

## Verified

`make lint` (0 issues), `make test`, `make build` after every task. Real-browser
smoke test with a scratch hub at three depths: landing Featured section ordered
and ahead of the year groups; sidebar Featured group flat, ruled off, current page
highlighted, auto-scroll targeting the tree occurrence; array-shape fallback
renders the full tree. Scratch hub and server cleaned up.

## Not done (deliberately)

- Hand-ordering the featured list (`featured: 1` ranks) — YAGNI; date order for
  now.
- Featured sections on folder pages (folder pages do get the sidebar group, since
  they share `<byom-site-nav>`).
- Any exporter or feed change — `featured` is presentation-only.
- Config to rename the "Featured" heading.
