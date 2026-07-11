# Design mockups — mosaic hero image

Visual artifacts from the brainstorming/feel-pass sessions that shaped this
feature. Originally authored as brainstorming-companion content fragments; the
`.html` files carry their own inline `<style>` for the mosaic previews, so the
grids render when opened directly in a browser (the companion frame added extra
page chrome/typography that isn't reproduced here).

- **`mosaic-density.html`** — the base-look decision: 2×2 seamless vs. 2×2 with
  gaps vs. 3×3. Outcome: **2×2 with gaps** ("the gaps feel more representative").
  Tiles are stand-in images (picsum.photos).
- **`mosaic-fractal.html`** — how the layout scales to longer playlists: 5–7
  (1 subdivided quadrant), 8–10 (2), 11–13 (3). Outcome: **fractal included from
  the start**; dominant albums keep full quadrants, the tail packs into
  subdivided ones. Tiles are stand-ins (picsum.photos).
- **`real-mosaics.html`** (+ `m1.jpg`–`m6.jpg`) — the feel pass against six real
  mosaics generated from the actual corpus, at the shipped geometry (20px
  gap/padding, black mat). Outcome: **approved as-is, no constant changes.**

See `../spec.md` and `../plan.md` for the full design and implementation, and
issue #32 for the captured decisions.
