# Playlist mosaic hero image — spec

**Date:** 2026-07-11
**Branch:** `feat/playlist-mosaic-hero` (off `origin/main`, builds on #31)

## Goal

When a playlist has no explicit hero image, generate a **representative mosaic**
of its most-featured album covers at **site-build time**, and use it as the
playlist's hero everywhere the site touches: the `og:image`/social card and the
per-playlist site JSPF (so the embedded byom-player shows it too).

## Motivation

Today the no-explicit-image fallback (from #31) is "the first track's cover" —
arbitrary (why track 1?). A mosaic of the albums the playlist actually draws on
is more representative and looks intentional. We want it to be a real raster
image (not a client-side CSS grid) specifically so it can be the `og:image`.

## Key decisions (from brainstorming)

- **Raster, generated in byom-sync** (not a client-side player grid), so it can
  serve as `og:image`.
- **Generated at site-build time**, not `resolve art` time. It's a *derived*
  artifact (depends on the tracklist), so building it fresh each site build
  sidesteps all staleness / clobber / orphan-GC bookkeeping and keeps derived
  data out of the hub YAML. Content-hash filename gives stable, cacheable URLs.
- **Selection = most-featured albums.** Rank distinct covers by how many tracks
  use them (desc), tie-broken by first appearance in the tracklist (asc, for
  determinism). Dedup is free: the art store is content-addressed, so two tracks
  from the same album share the same `image_file`.
- **Layout = 2×2 with gaps**, extended **fractally** (depth-capped treemap):
  dominant albums keep full quadrants; the long tail packs into subdivided
  quadrants (each a 2×2 of sub-covers). One level of subdivision only.
- **Reuses #31's pipeline.** Populating the in-memory `Playlist.ImageFile` with
  the mosaic path makes it flow through the existing hero precedence in
  `export/jspf.go` (`art_base` + `image_file`) and `site/meta.go` (og:image)
  with no new precedence code.

## Selection algorithm

Input: a playlist's tracks (in memory, from the hub YAML).

1. Consider only tracks with a **downloaded** cover (`Track.ImageFile != ""`) —
   site build is network-free and needs local bytes to composite. Tracks with
   only a remote `Image` URL are ignored for tile purposes.
2. Group by `ImageFile` (identical album cover ⇒ identical content-addressed
   path). For each distinct cover record: track count, and the index of its
   first appearance.
3. Rank distinct covers by count desc, then first-appearance index asc.
4. The ranked list drives layout (below). Let `D = len(ranked)`.

## Layout

Canvas: **square JPEG**, black (`#000`) background, uniform gaps.

- `D == 0` → **no mosaic**; leave the hero to #31's existing fallback (first
  track's `image_file`/`Image`, possibly empty). Do not set a mosaic.
- `D == 1` → **single cover**, full frame (no grid, no gaps).
- `D == 2` → 2×2: covers in the first 2 quadrants (reading order TL, TR),
  remaining quadrants **black**.
- `D == 3` → 2×2: covers in TL, TR, BL; BR **black**.
- `D == 4` → full 2×2 (ranks 1–4 in TL, TR, BL, BR).
- `D >= 5` → **fractal**. `s` = number of subdivided quadrants:
  - `5–7` → `s = 1`; `8–10` → `s = 2`; `11–13` → `s = 3`; `>13` → `s = 3` (cap;
    covers beyond 13 are omitted).
  - The first `4 - s` quadrants (reading order) are **whole**, holding the
    top-ranked covers. The last `s` quadrants are **subdivided** 2×2 panes,
    filled in reading order with the next-ranked covers. A sub-pane that runs
    out of covers fills its remaining sub-cells **black** (same punt as sparse).

Quadrant fill order (whole and sub-cells): TL, TR, BL, BR.

### Concrete dimensions (defaults; tunable in the feel pass)

- Canvas 1200×1200, outer padding 20px, gap 20px between quadrants.
- Quadrant = `(1200 - 2*20 - 20) / 2 = 570` px.
- Subdivided pane: inner gap 10px; sub-tile = `(570 - 10) / 2 = 280` px.
- JPEG quality 88.

These are locked structurally; exact gap/padding/size get one feel pass in the
visual companion during implementation before we finalize.

## Tile rendering

- Source covers may be jpeg/png/gif/webp (whatever `artstore` saved). Register
  decoders: stdlib `image/jpeg`, `image/png`, `image/gif`, plus
  `golang.org/x/image/webp` (decode-only). Output is always JPEG.
- Each source is **center-cropped to a square** then scaled to the target tile
  size with `golang.org/x/image/draw` (`draw.CatmullRom`) for quality.
- A cover that fails to open/decode is treated as a **black** tile (never fails
  the whole build). If that drops usable covers below the count that chose the
  layout, the black-punt rules already cover the gap.

## Content-hash & caching

- Filename: `art/mosaic/<hash>.jpg`, where `<hash>` = sha256 over the ordered
  list of selected cover paths (which embed their own content hashes) plus a
  **layout version** constant. Deterministic ⇒ stable URL across builds ⇒
  browser-cacheable; changing the tracklist/covers changes the hash.
- No persistent build cache initially — compositing a handful of small JPEGs is
  milliseconds. If a build over the full corpus ever gets slow, add a cache
  keyed by that hash. (Documented as a deferred optimization, not silent.)

## Dependencies

- Promote `golang.org/x/image` to a **direct** dependency (`draw` + `webp`). It's
  already transitively present; `go get golang.org/x/image@latest`.

## Code structure

- **`internal/mosaic/`** (new, pure/testable, no site or filesystem-walk deps):
  - `select.go` — `Select(p playlist.Playlist) []Cover` (ranked distinct covers;
    `Cover{ ImageFile string; Count int }`).
  - `layout.go` — `Plan(n int) Layout` (whole/subdivided quadrant plan + tile
    rects for a given canvas size). Pure; unit-tested against the D-thresholds.
  - `render.go` — `Render(plan Layout, coverBytes [][]byte, opts Options)
    ([]byte, error)` composites to JPEG. Takes bytes, not paths, to stay
    filesystem-free and testable.
  - `hash.go` — `Name(coverPaths []string) string` → `<hash>.jpg`.
- **`internal/site/mosaic.go`** (integration glue):
  - `GenerateMosaics(hubDir, outDir string, root *Node) error` — walk playlist
    nodes; **skip** any with an explicit hero (`p.Image != "" || p.ImageFile !=
    ""`); run `Select`, read cover bytes from `<hubDir>/<image_file>`, `Plan`,
    `Render`, write `<outDir>/art/mosaic/<hash>.jpg`, and set the in-memory
    `node.Playlist.ImageFile = "art/mosaic/<hash>.jpg"`.
  - Reads source covers from the **hub** store (`<hubDir>/art/...`), so it does
    not depend on `CopyArt` having run; writes into `<outDir>/art/mosaic/`.
- **`internal/site/site.go`** — call `GenerateMosaics` in `Build` **after
  `BuildTree` and before `WriteJSPF`/`RenderSite`**, so the in-memory
  `ImageFile` is set when the JSPF and og:image are emitted. `CopyArt` (which
  `MkdirAll`s `<out>/art`) is unaffected and does not clobber `mosaic/`.

## What is explicitly NOT changing

- **Hub YAML**: no new fields; the mosaic path lives only in the in-memory tree
  during a build. An explicit `Playlist.Image`/`ImageFile` always wins and is
  never touched.
- **byom-player**: already renders JSPF playlist `image`; no change.
- **`resolve art` / `export jspf`**: unchanged. The mosaic is a site-build
  concern; standalone exports keep #31's behavior.

## Testing

- `mosaic.Select`: ranking by count desc; first-appearance tiebreak; dedup by
  `ImageFile`; tracks without `ImageFile` excluded.
- `mosaic.Plan`: the D→layout table (0/1/2/3/4/5/7/8/10/11/13/14) — quadrant
  count, which are whole vs subdivided, black-cell padding at the tail.
- `mosaic.Render`: given N synthetic tile images, output decodes as a JPEG of
  the expected square dimensions; a deliberately-corrupt tile yields a black
  cell rather than an error.
- `mosaic.Name`: deterministic for the same ordered inputs; differs when inputs
  change; stable across runs.
- `site.GenerateMosaics`: playlist with ≥1 downloaded cover gets
  `ImageFile` set to an `art/mosaic/*.jpg` that exists on disk; a playlist with
  an explicit hero is left untouched; a playlist with no downloaded covers is
  left untouched.

## Success criteria

- Site build of the real corpus produces mosaic heroes for playlists lacking an
  explicit image; `og:image` and the embedded player both show them.
- Explicit hero images (from #31) are unaffected.
- Full `go test ./...`, `go vet`, `make lint` clean.
