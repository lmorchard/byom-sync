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
