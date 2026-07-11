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
