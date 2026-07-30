package match

import "testing"

func TestNorm(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"The Beatles", "the beatles"},
		{"Sgt. Pepper's!", "sgt pepper s"},
		{"  Spaced   Out  ", "spaced out"},
		{"", ""},
	} {
		if got := Norm(tc.in); got != tc.want {
			t.Errorf("Norm(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSim(t *testing.T) {
	for _, tc := range []struct {
		name, a, b string
		min, max   float64
	}{
		{"identical", "come together", "come together", 1.0, 1.0},
		{"containment", "come together", "come together remastered 2019", 1.0, 1.0},
		{"both empty", "", "", 1.0, 1.0},
		{"one empty", "abc", "", 0.0, 0.0},
		{"unrelated", "hellbilly deluxe", "the sinister urge", 0.0, 0.6},
		{"short pattern strict", "go", "going home", 0.0, 0.5},
	} {
		got := Sim(tc.a, tc.b)
		if got < tc.min || got > tc.max {
			t.Errorf("%s: Sim(%q,%q) = %v, want in [%v,%v]", tc.name, tc.a, tc.b, got, tc.min, tc.max)
		}
	}
}
