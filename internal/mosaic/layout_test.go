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
		{1, []int{0}},            // single, full frame
		{2, []int{0, 1, -1, -1}}, // TL,TR covers; BL,BR black
		{3, []int{0, 1, 2, -1}},  // BR black
		{4, []int{0, 1, 2, 3}},   // full 2x2
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
