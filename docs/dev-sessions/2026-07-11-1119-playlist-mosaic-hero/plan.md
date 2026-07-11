# Playlist mosaic hero image — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When a playlist has no explicit hero image, generate a representative mosaic of its most-featured album covers at site-build time and use it as the hero for `og:image` and the site JSPF (reusing #31's `Playlist.ImageFile` pipeline).

**Architecture:** A pure, filesystem-free `internal/mosaic` package (select ranked distinct covers → plan a 2×2/fractal layout → render JPEG → content-hash name). A thin `internal/site/mosaic.go` reads cover bytes from the hub store, generates the mosaic into `<out>/art/mosaic/`, and sets the in-memory `Playlist.ImageFile`. Wired into `site.Build` after `BuildTree`, before `WriteJSPF`/`RenderSite`.

**Tech Stack:** Go 1.25; stdlib `image`/`image/{jpeg,png,gif}`; `golang.org/x/image/{draw,webp}` (new direct dep); existing `internal/playlist`, `internal/site`.

**Spec:** `docs/dev-sessions/2026-07-11-1119-playlist-mosaic-hero/spec.md` · **Issue:** #32

## Global Constraints

- **Reuse #31, do not extend the hub schema.** No new YAML fields. The mosaic path lives only on the in-memory `*playlist.Playlist` during a build. An explicit hero (`p.Image != "" || p.ImageFile != ""`) always wins and is never modified.
- **Site-build-time only, network-free.** Only tracks with a downloaded cover (`Track.ImageFile != ""`) are eligible tiles. `resolve art`/`export jspf`/byom-player unchanged.
- **Deterministic.** Selection order, layout, and the content-hash filename must be stable across builds for identical input. Filename = `art/mosaic/<sha256(layoutVersion + ordered cover paths)>.jpg`.
- **Never fail the build on bad art.** An unreadable/undecodable cover becomes a black tile.
- **Output:** square JPEG, black `#000` background, JPEG quality 88. Geometry constants: canvas 1200, padding 20, gap 20, sub-gap 10.
- Per-task: TDD (failing test first), run `gofmt`/`go vet`, `make lint` clean, commit at the end of each task.

## File Structure

- Create `internal/mosaic/layout.go` — geometry types + `Plan(n) Layout` (pure).
- Create `internal/mosaic/select.go` — `Cover`, `Select(p) []Cover`.
- Create `internal/mosaic/render.go` — `Render(l, covers) ([]byte, error)` + decoders.
- Create `internal/mosaic/hash.go` — `Name(coverPaths) string`.
- Create `internal/mosaic/*_test.go` — per-file tests.
- Create `internal/site/mosaic.go` — `GenerateMosaics(hubDir, outDir, root)`.
- Create `internal/site/mosaic_test.go`.
- Modify `internal/site/site.go` — call `GenerateMosaics` in `Build`.
- Modify `go.mod`/`go.sum` — promote `golang.org/x/image` to a direct dep (Task 3).

---

### Task 1: Layout planning (`internal/mosaic/layout.go`)

Pure geometry: given a usable cover count, produce the slot rectangles and which ranked cover (or black) fills each. This is the core logic; no image or fs deps.

**Files:**
- Create: `internal/mosaic/layout.go`
- Test: `internal/mosaic/layout_test.go`

**Interfaces:**
- Produces: `type Rect struct{ X, Y, W, H int }`; `type Slot struct{ Rect Rect; CoverIndex int }` (`CoverIndex == -1` ⇒ black); `type Layout struct{ Canvas int; Slots []Slot }`; `func Plan(n int) Layout`. Geometry constants `Canvas, Padding, Gap, SubGap`.

- [ ] **Step 1: Write the failing test**

```go
package mosaic

import "testing"

// coverIdxs returns the CoverIndex of each slot, in slot order.
func coverIdxs(l Layout) []int {
	out := make([]int, len(l.Slots))
	for i, s := range l.Slots {
		out[i] = s.CoverIndex
	}
	return out
}

func eq(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestPlan_SparseAndFull(t *testing.T) {
	cases := []struct {
		n    int
		want []int // CoverIndex per slot, in order
	}{
		{1, []int{0}},                                  // single, full frame
		{2, []int{0, 1, -1, -1}},                       // TL,TR covers; BL,BR black
		{3, []int{0, 1, 2, -1}},                        // BR black
		{4, []int{0, 1, 2, 3}},                         // full 2x2
	}
	for _, tc := range cases {
		if got := coverIdxs(Plan(tc.n)); !eq(got, tc.want) {
			t.Errorf("Plan(%d) idxs = %v, want %v", tc.n, got, tc.want)
		}
	}
	// n==1 fills the whole canvas.
	if s := Plan(1).Slots[0].Rect; s != (Rect{0, 0, Canvas, Canvas}) {
		t.Errorf("single-cover rect = %+v, want full canvas", s)
	}
}

func TestPlan_Fractal(t *testing.T) {
	// 5–7 → 3 whole quadrants + 1 subdivided (4 sub-slots) = 7 slots.
	l5 := Plan(5)
	if len(l5.Slots) != 7 {
		t.Fatalf("Plan(5) slots = %d, want 7", len(l5.Slots))
	}
	// whole: 0,1,2 ; sub: 3,4 covers then 5,6 -> black
	if got, want := coverIdxs(l5), []int{0, 1, 2, 3, 4, -1, -1}; !eq(got, want) {
		t.Errorf("Plan(5) idxs = %v, want %v", got, want)
	}
	// 8–10 → 2 whole + 2 subdivided = 2 + 8 = 10 slots.
	if got := len(Plan(8).Slots); got != 10 {
		t.Errorf("Plan(8) slots = %d, want 10", got)
	}
	// 11–13 → 1 whole + 3 subdivided = 1 + 12 = 13 slots.
	l13 := Plan(13)
	if len(l13.Slots) != 13 {
		t.Fatalf("Plan(13) slots = %d, want 13", len(l13.Slots))
	}
	if got, want := coverIdxs(l13), []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}; !eq(got, want) {
		t.Errorf("Plan(13) idxs = %v, want %v", got, want)
	}
	// >13 caps at 13 cover slots; no CoverIndex >= 13.
	for _, s := range Plan(20).Slots {
		if s.CoverIndex >= 13 {
			t.Errorf("Plan(20) referenced cover %d, want <=12 (capped)", s.CoverIndex)
		}
	}
}

func TestPlan_Geometry(t *testing.T) {
	l := Plan(4)
	q := (Canvas - 2*Padding - Gap) / 2
	want := []Rect{
		{Padding, Padding, q, q},
		{Padding + q + Gap, Padding, q, q},
		{Padding, Padding + q + Gap, q, q},
		{Padding + q + Gap, Padding + q + Gap, q, q},
	}
	for i, w := range want {
		if l.Slots[i].Rect != w {
			t.Errorf("quadrant %d rect = %+v, want %+v", i, l.Slots[i].Rect, w)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd internal/mosaic && go test ./... -run TestPlan`
Expected: FAIL (undefined: Plan / Rect / Slot / Layout).

- [ ] **Step 3: Write minimal implementation**

```go
// Package mosaic composites a representative cover-art mosaic for a playlist
// from its most-featured album covers. All functions are pure/deterministic and
// filesystem-free; the caller supplies cover bytes and writes the result.
package mosaic

// Geometry of the generated square mosaic, in pixels.
const (
	Canvas  = 1200 // output width == height
	Padding = 20   // outer margin around the 2x2
	Gap     = 20   // gutter between quadrants
	SubGap  = 10   // gutter between sub-tiles inside a subdivided quadrant
)

// Rect is an axis-aligned rectangle in canvas pixel space.
type Rect struct{ X, Y, W, H int }

// Slot is one tile position. CoverIndex is an index into the ranked cover list,
// or -1 for a black (empty) tile.
type Slot struct {
	Rect       Rect
	CoverIndex int
}

// Layout is the full placement plan for a mosaic of n usable covers.
type Layout struct {
	Canvas int
	Slots  []Slot
}

// subdivCount returns how many of the four quadrants subdivide, given n covers.
func subdivCount(n int) int {
	switch {
	case n <= 4:
		return 0
	case n <= 7:
		return 1
	case n <= 10:
		return 2
	default:
		return 3
	}
}

// quadrants returns the four quadrant rects in reading order (TL, TR, BL, BR).
func quadrants() [4]Rect {
	q := (Canvas - 2*Padding - Gap) / 2
	x0, x1 := Padding, Padding+q+Gap
	return [4]Rect{
		{x0, x0, q, q},
		{x1, x0, q, q},
		{x0, x1, q, q},
		{x1, x1, q, q},
	}
}

// subTiles splits a quadrant rect into its four sub-tile rects (TL, TR, BL, BR).
func subTiles(r Rect) [4]Rect {
	s := (r.W - SubGap) / 2
	x0, y0 := r.X, r.Y
	x1, y1 := r.X+s+SubGap, r.Y+s+SubGap
	return [4]Rect{
		{x0, y0, s, s},
		{x1, y0, s, s},
		{x0, y1, s, s},
		{x1, y1, s, s},
	}
}

// Plan produces the placement for n usable covers. n<=0 yields an empty Layout
// (the caller then generates no mosaic). n==1 is a single full-frame cover.
func Plan(n int) Layout {
	l := Layout{Canvas: Canvas}
	if n <= 0 {
		return l
	}
	if n == 1 {
		l.Slots = []Slot{{Rect{0, 0, Canvas, Canvas}, 0}}
		return l
	}
	qs := quadrants()
	s := subdivCount(n)
	whole := 4 - s
	idx := 0
	// assign returns CoverIndex idx (consuming it) or -1 once covers run out.
	assign := func() int {
		if idx < n && idx < 13 {
			i := idx
			idx++
			return i
		}
		idx++
		return -1
	}
	for q := 0; q < 4; q++ {
		if q < whole {
			l.Slots = append(l.Slots, Slot{qs[q], assign()})
			continue
		}
		for _, sr := range subTiles(qs[q]) {
			l.Slots = append(l.Slots, Slot{sr, assign()})
		}
	}
	return l
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd internal/mosaic && go test ./... -run TestPlan -v`
Expected: PASS (all three tests).

- [ ] **Step 5: Commit**

```bash
git add internal/mosaic/layout.go internal/mosaic/layout_test.go
git commit -m "feat(mosaic): pure 2x2/fractal layout planner"
```

---

### Task 2: Cover selection (`internal/mosaic/select.go`)

Rank a playlist's distinct downloaded covers by how many tracks use them.

**Files:**
- Create: `internal/mosaic/select.go`
- Test: `internal/mosaic/select_test.go`

**Interfaces:**
- Consumes: `playlist.Playlist`/`playlist.Track` (`Track.ImageFile string`).
- Produces: `type Cover struct{ ImageFile string; Count int }`; `func Select(p playlist.Playlist) []Cover` (ranked: count desc, then first-appearance asc; excludes tracks with empty `ImageFile`).

- [ ] **Step 1: Write the failing test**

```go
package mosaic

import (
	"testing"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

func TestSelect_RanksByCountThenFirstAppearance(t *testing.T) {
	p := playlist.Playlist{Tracks: []playlist.Track{
		{Title: "1", ImageFile: "art/a.jpg"}, // A (first seen idx 0)
		{Title: "2", ImageFile: "art/b.jpg"}, // B (idx 1)
		{Title: "3", ImageFile: "art/a.jpg"}, // A again -> count 2
		{Title: "4"},                         // no downloaded cover -> excluded
		{Title: "5", ImageFile: "art/c.jpg"}, // C (idx 4)
		{Title: "6", ImageFile: "art/b.jpg"}, // B again -> count 2
	}}
	got := Select(p)
	// A(2, first@0) and B(2, first@1) tie on count; A wins the tiebreak.
	// C(1) last.
	want := []Cover{
		{ImageFile: "art/a.jpg", Count: 2},
		{ImageFile: "art/b.jpg", Count: 2},
		{ImageFile: "art/c.jpg", Count: 1},
	}
	if len(got) != len(want) {
		t.Fatalf("Select len = %d (%v), want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("rank %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestSelect_NoDownloadedCovers(t *testing.T) {
	p := playlist.Playlist{Tracks: []playlist.Track{{Title: "x"}, {Title: "y"}}}
	if got := Select(p); len(got) != 0 {
		t.Errorf("Select with no image_files = %v, want empty", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd internal/mosaic && go test ./... -run TestSelect`
Expected: FAIL (undefined: Select / Cover).

- [ ] **Step 3: Write minimal implementation**

```go
package mosaic

import (
	"sort"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

// Cover is a distinct album cover and how many tracks reference it.
type Cover struct {
	ImageFile string
	Count     int
}

// Select returns the playlist's distinct downloaded covers ranked by track
// count (desc), tie-broken by first appearance in the tracklist (asc). Tracks
// without a downloaded cover (empty ImageFile) are ignored. Deterministic.
func Select(p playlist.Playlist) []Cover {
	type agg struct {
		count int
		first int
	}
	seen := map[string]*agg{}
	order := []string{}
	for i, t := range p.Tracks {
		if t.ImageFile == "" {
			continue
		}
		a := seen[t.ImageFile]
		if a == nil {
			a = &agg{first: i}
			seen[t.ImageFile] = a
			order = append(order, t.ImageFile)
		}
		a.count++
	}
	out := make([]Cover, 0, len(order))
	for _, f := range order {
		out = append(out, Cover{ImageFile: f, Count: seen[f].count})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return seen[out[i].ImageFile].first < seen[out[j].ImageFile].first
	})
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd internal/mosaic && go test ./... -run TestSelect -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mosaic/select.go internal/mosaic/select_test.go
git commit -m "feat(mosaic): most-featured-albums cover selection"
```

---

### Task 3: Rendering (`internal/mosaic/render.go`)

Composite ranked cover bytes onto a black canvas per a `Layout`, center-cropping and scaling each tile; encode JPEG. Adds the `golang.org/x/image` direct dep.

**Files:**
- Create: `internal/mosaic/render.go`
- Test: `internal/mosaic/render_test.go`
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Consumes: `Layout` (Task 1).
- Produces: `func Render(l Layout, covers [][]byte) ([]byte, error)` — `covers` is index-aligned to `Slot.CoverIndex`; output is a JPEG of `l.Canvas × l.Canvas`.

- [ ] **Step 1: Add the dependency**

Run: `go get golang.org/x/image@latest`
Expected: `go.mod` now lists `golang.org/x/image` as a direct require.

- [ ] **Step 2: Write the failing test**

```go
package mosaic

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// solidPNG returns PNG bytes of a c-colored w×h image.
func solidPNG(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestRender_ProducesSquareJPEG(t *testing.T) {
	covers := [][]byte{
		solidPNG(t, 300, 200, color.RGBA{200, 0, 0, 255}), // non-square → center-cropped
		solidPNG(t, 100, 100, color.RGBA{0, 200, 0, 255}),
		solidPNG(t, 100, 100, color.RGBA{0, 0, 200, 255}),
		solidPNG(t, 100, 100, color.RGBA{200, 200, 0, 255}),
	}
	out, err := Render(Plan(4), covers)
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("output not a JPEG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != Canvas || b.Dy() != Canvas {
		t.Errorf("output %dx%d, want %dx%d", b.Dx(), b.Dy(), Canvas, Canvas)
	}
}

func TestRender_CorruptTileBecomesBlackNotError(t *testing.T) {
	covers := [][]byte{
		[]byte("not an image"),
		solidPNG(t, 100, 100, color.RGBA{0, 200, 0, 255}),
	}
	// n==2 → 2x2 with covers 0,1 and black 2,3; cover 0 fails to decode → black.
	if _, err := Render(Plan(2), covers); err != nil {
		t.Errorf("Render must not fail on a corrupt tile: %v", err)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd internal/mosaic && go test ./... -run TestRender`
Expected: FAIL (undefined: Render).

- [ ] **Step 4: Write minimal implementation**

```go
package mosaic

import (
	"bytes"
	"image"
	"image/draw"
	"image/jpeg" // named: provides Encode and registers the JPEG decoder

	// Decoders for the other formats artstore may have saved.
	_ "image/gif"
	_ "image/png"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const jpegQuality = 88

// Render composites the ranked cover bytes onto a black square canvas per l and
// returns JPEG bytes. covers is index-aligned to Slot.CoverIndex. A slot whose
// cover is missing, out of range, or undecodable is left black — Render never
// fails on bad art (only on JPEG encoding, which shouldn't happen for RGBA).
func Render(l Layout, covers [][]byte) ([]byte, error) {
	dst := image.NewRGBA(image.Rect(0, 0, l.Canvas, l.Canvas))
	// image.Black is a predefined *image.Uniform, usable directly as the src.
	draw.Draw(dst, dst.Bounds(), image.Black, image.Point{}, draw.Src)

	for _, s := range l.Slots {
		if s.CoverIndex < 0 || s.CoverIndex >= len(covers) {
			continue // black
		}
		src, _, err := image.Decode(bytes.NewReader(covers[s.CoverIndex]))
		if err != nil {
			continue // black
		}
		sq := centerSquare(src)
		r := image.Rect(s.Rect.X, s.Rect.Y, s.Rect.X+s.Rect.W, s.Rect.Y+s.Rect.H)
		xdraw.CatmullRom.Scale(dst, r, sq, sq.Bounds(), xdraw.Over, nil)
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// centerSquare returns the largest centered square sub-image of src.
func centerSquare(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	side := w
	if h < side {
		side = h
	}
	x0 := b.Min.X + (w-side)/2
	y0 := b.Min.Y + (h-side)/2
	crop := image.Rect(x0, y0, x0+side, y0+side)
	if si, ok := src.(interface {
		SubImage(image.Rectangle) image.Image
	}); ok {
		return si.SubImage(crop)
	}
	// Fallback: copy the crop region into a fresh RGBA.
	out := image.NewRGBA(image.Rect(0, 0, side, side))
	draw.Draw(out, out.Bounds(), src, crop.Min, draw.Src)
	return out
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd internal/mosaic && go test ./... -run TestRender -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/mosaic/render.go internal/mosaic/render_test.go go.mod go.sum
git commit -m "feat(mosaic): composite+scale tiles into a JPEG (x/image draw+webp)"
```

---

### Task 4: Content-hash filename (`internal/mosaic/hash.go`)

**Files:**
- Create: `internal/mosaic/hash.go`
- Test: `internal/mosaic/hash_test.go`

**Interfaces:**
- Produces: `func Name(coverPaths []string) string` → `"<hex>.jpg"`, deterministic; folds in a private `layoutVersion` so a layout change invalidates old URLs.

- [ ] **Step 1: Write the failing test**

```go
package mosaic

import (
	"strings"
	"testing"
)

func TestName_DeterministicAndInputSensitive(t *testing.T) {
	a := Name([]string{"art/a.jpg", "art/b.jpg"})
	if a != Name([]string{"art/a.jpg", "art/b.jpg"}) {
		t.Error("Name must be deterministic for identical input")
	}
	if !strings.HasSuffix(a, ".jpg") {
		t.Errorf("Name = %q, want .jpg suffix", a)
	}
	if a == Name([]string{"art/b.jpg", "art/a.jpg"}) {
		t.Error("order must change the hash")
	}
	if a == Name([]string{"art/a.jpg"}) {
		t.Error("different inputs must differ")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd internal/mosaic && go test ./... -run TestName`
Expected: FAIL (undefined: Name).

- [ ] **Step 3: Write minimal implementation**

```go
package mosaic

import (
	"crypto/sha256"
	"encoding/hex"
)

// layoutVersion is folded into every mosaic hash so changing the layout logic
// invalidates cached URLs. Bump when Plan's geometry/rules change.
const layoutVersion = "v1"

// Name returns the content-addressed filename ("<sha256>.jpg") for a mosaic
// built from coverPaths in order. Deterministic; order-sensitive.
func Name(coverPaths []string) string {
	h := sha256.New()
	h.Write([]byte(layoutVersion))
	for _, p := range coverPaths {
		h.Write([]byte{0})
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil)) + ".jpg"
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd internal/mosaic && go test ./... -run TestName -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mosaic/hash.go internal/mosaic/hash_test.go
git commit -m "feat(mosaic): content-hash mosaic filename"
```

---

### Task 5: Site integration (`internal/site/mosaic.go` + `site.go`)

Generate mosaics during the site build for playlists lacking an explicit hero, and set the in-memory `ImageFile` so the existing #31 precedence carries it into the JSPF and og:image.

**Files:**
- Create: `internal/site/mosaic.go`
- Test: `internal/site/mosaic_test.go`
- Modify: `internal/site/site.go`

**Interfaces:**
- Consumes: `mosaic.Select`/`Plan`/`Render`/`Name` (Tasks 1–4); `*Node` (`Node.Playlist *playlist.Playlist`); `walkPlaylists` (existing, `internal/site/paths.go`).
- Produces: `func GenerateMosaics(hubDir, outDir string, root *Node) error`.

- [ ] **Step 1: Write the failing test**

```go
package site

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

// writePNG writes a tiny solid PNG to <hub>/<rel> so Select/Render have bytes.
func writePNG(t *testing.T, hub, rel string) {
	t.Helper()
	p := filepath.Join(hub, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	// 1x1 PNG (valid, decodable).
	png := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xde, 0x00, 0x00, 0x00,
		0x0c, 0x49, 0x44, 0x41, 0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x00, 0x03, 0x00, 0x01, 0x00, 0x18, 0xdd, 0x8d, 0xb0, 0x00, 0x00,
		0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
	}
	if err := os.WriteFile(p, png, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateMosaics(t *testing.T) {
	hub, out := t.TempDir(), t.TempDir()
	writePNG(t, hub, "art/aa/a.jpg")
	writePNG(t, hub, "art/bb/b.jpg")

	needs := &playlist.Playlist{Title: "Needs", Tracks: []playlist.Track{
		{Title: "1", ImageFile: "art/aa/a.jpg"},
		{Title: "2", ImageFile: "art/bb/b.jpg"},
	}}
	explicit := &playlist.Playlist{Title: "Explicit", Image: "https://x/hero.jpg",
		Tracks: []playlist.Track{{Title: "1", ImageFile: "art/aa/a.jpg"}}}
	bare := &playlist.Playlist{Title: "Bare", Tracks: []playlist.Track{{Title: "1"}}}

	root := &Node{IsDir: true, Children: []*Node{
		{Playlist: needs}, {Playlist: explicit}, {Playlist: bare},
	}}

	if err := GenerateMosaics(hub, out, root); err != nil {
		t.Fatal(err)
	}

	// Playlist with covers → ImageFile set to an art/mosaic/*.jpg that exists.
	if needs.ImageFile == "" {
		t.Fatal("expected a mosaic ImageFile for the covered playlist")
	}
	if filepath.Dir(needs.ImageFile) != "art/mosaic" {
		t.Errorf("mosaic path = %q, want under art/mosaic/", needs.ImageFile)
	}
	if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(needs.ImageFile))); err != nil {
		t.Errorf("mosaic file not written: %v", err)
	}
	// Explicit hero untouched.
	if explicit.ImageFile != "" {
		t.Errorf("explicit-hero playlist must not get a mosaic: %q", explicit.ImageFile)
	}
	// No downloaded covers → untouched.
	if bare.ImageFile != "" {
		t.Errorf("cover-less playlist must not get a mosaic: %q", bare.ImageFile)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd internal/site && go test ./... -run TestGenerateMosaics`
Expected: FAIL (undefined: GenerateMosaics).

- [ ] **Step 3: Write minimal implementation**

```go
package site

import (
	"os"
	"path/filepath"

	"github.com/lmorchard/byom-sync/internal/mosaic"
)

// GenerateMosaics builds a representative cover mosaic for each playlist that
// has no explicit hero image, writes it to <outDir>/art/mosaic/<hash>.jpg, and
// sets the in-memory Playlist.ImageFile so the existing hero precedence carries
// it into the JSPF and og:image. Source cover bytes are read from the hub art
// store (<hubDir>/<image_file>), so this does not depend on CopyArt. Playlists
// with an explicit hero, or with no downloaded covers, are left untouched.
func GenerateMosaics(hubDir, outDir string, root *Node) error {
	return walkPlaylists(root, func(n *Node) error {
		p := n.Playlist
		if p.Image != "" || p.ImageFile != "" {
			return nil // explicit hero wins
		}
		var paths []string
		var covers [][]byte
		for _, c := range mosaic.Select(*p) {
			b, err := os.ReadFile(filepath.Join(hubDir, filepath.FromSlash(c.ImageFile)))
			if err != nil {
				continue // unreadable cover → drop from the tile set
			}
			paths = append(paths, c.ImageFile)
			covers = append(covers, b)
		}
		if len(covers) == 0 {
			return nil // nothing to composite
		}
		rel := filepath.ToSlash(filepath.Join("art", "mosaic", mosaic.Name(paths)))
		data, err := mosaic.Render(mosaic.Plan(len(covers)), covers)
		if err != nil {
			return err
		}
		dst := filepath.Join(outDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return err
		}
		p.ImageFile = rel
		return nil
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd internal/site && go test ./... -run TestGenerateMosaics -v`
Expected: PASS.

- [ ] **Step 5: Wire into `site.Build`**

In `internal/site/site.go`, immediately after the `BuildTree` block and before `WriteJSPF`, add:

```go
	if err := GenerateMosaics(opts.HubDir, opts.OutDir, root); err != nil {
		return err
	}
```

Context (the existing lines it slots between):

```go
	root, err := BuildTree(opts.HubDir)
	if err != nil {
		return err
	}
	// ... existing pages/collision/renderer setup stays as-is ...
	if err := GenerateMosaics(opts.HubDir, opts.OutDir, root); err != nil {
		return err
	}
	if err := WriteJSPF(opts.OutDir, root, opts.Site.BaseURL); err != nil {
		return err
	}
```

(Place the `GenerateMosaics` call anywhere after `BuildTree` and before `WriteJSPF`/`RenderSite`; `opts.OutDir` is already `MkdirAll`ed at the top of `Build`.)

- [ ] **Step 6: Run the full suite**

Run: `go test ./... && go vet ./... && make lint`
Expected: all pass, 0 lint issues.

- [ ] **Step 7: Commit**

```bash
git add internal/site/mosaic.go internal/site/mosaic_test.go internal/site/site.go
git commit -m "feat(site): generate mosaic hero for playlists lacking an explicit image (#32)"
```

---

## Post-implementation

- **Feel pass:** with the real corpus, build the site and eyeball the mosaics; tune `Padding`/`Gap`/`SubGap`/`Canvas` (and bump `layoutVersion`) in the visual companion before finalizing. Structural correctness is locked by tests; only the geometry constants are subjective.
- **Manual verify:** run a site build against the current hub, confirm playlists without an explicit hero now show a mosaic in both `og:image` (page `<head>`) and the embedded player, and that #31 explicit heroes are unchanged.
