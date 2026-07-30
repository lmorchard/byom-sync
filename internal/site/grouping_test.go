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

func TestFeaturedOf(t *testing.T) {
	pl := func(title, updated string, featured bool) *Node {
		n := &Node{
			Name:     title,
			Title:    title,
			Playlist: &playlist.Playlist{Title: title, Featured: featured},
		}
		if updated != "" {
			n.Playlist.DateUpdated, _ = time.Parse(time.RFC3339, updated)
		}
		return n
	}
	// Featured playlists live at the root and nested two directories deep; the
	// list is flat and globally date-ordered regardless of where they're filed.
	root := &Node{IsDir: true, Children: []*Node{
		{Name: "archive", IsDir: true, Children: []*Node{
			pl("deep", "2021-05-01T00:00:00Z", true),
			pl("deep-plain", "2024-01-01T00:00:00Z", false),
			{Name: "deeper", IsDir: true, Children: []*Node{
				pl("deepest", "2026-01-01T00:00:00Z", true),
			}},
		}},
		pl("newest", "2026-07-01T00:00:00Z", true),
		pl("plain", "2026-07-02T00:00:00Z", false),
		pl("undated", "", true),
		pl("zed", "2026-01-01T00:00:00Z", true), // ties with "deepest" on date
	}}

	got := featuredOf(root)
	var titles []string
	for _, n := range got {
		titles = append(titles, n.Title)
	}
	want := []string{"newest", "deepest", "zed", "deep", "undated"}
	if len(titles) != len(want) {
		t.Fatalf("featuredOf titles = %v, want %v", titles, want)
	}
	for i := range want {
		if titles[i] != want[i] {
			t.Fatalf("featuredOf titles = %v, want %v", titles, want)
		}
	}
}

func TestFeaturedOf_NoneFeatured(t *testing.T) {
	root := &Node{IsDir: true, Children: []*Node{
		{Name: "d", IsDir: true, Children: []*Node{
			{Name: "x", Title: "X", Playlist: &playlist.Playlist{Title: "X"}},
		}},
		{Name: "y", Title: "Y", Playlist: &playlist.Playlist{Title: "Y"}},
	}}
	if got := featuredOf(root); len(got) != 0 {
		t.Errorf("featuredOf = %+v, want empty", got)
	}
}
