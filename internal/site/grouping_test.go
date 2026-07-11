package site

import (
	"testing"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

func TestYearGroupsOf(t *testing.T) {
	pl := func(updated string) *Node {
		n := &Node{Playlist: &playlist.Playlist{}}
		if updated != "" {
			n.Playlist.DateUpdated, _ = time.Parse(time.RFC3339, updated)
		}
		return n
	}
	children := []*Node{
		{Name: "d", IsDir: true},
		pl("2020-01-01T00:00:00Z"),
		pl("2020-06-01T00:00:00Z"),
		pl("2018-01-01T00:00:00Z"),
		pl(""), // undated
	}
	if d := dirsOf(children); len(d) != 1 || d[0].Name != "d" {
		t.Fatalf("dirsOf = %+v", d)
	}
	groups := yearGroupsOf(children)
	if len(groups) != 3 {
		t.Fatalf("groups = %d, want 3", len(groups))
	}
	if groups[0].Label != "2020" || len(groups[0].Playlists) != 2 {
		t.Errorf("group0 = %s/%d", groups[0].Label, len(groups[0].Playlists))
	}
	if groups[1].Label != "2018" || groups[2].Label != "Undated" {
		t.Errorf("labels = %s, %s", groups[1].Label, groups[2].Label)
	}
}
