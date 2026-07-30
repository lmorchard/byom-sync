package site

import (
	"sort"
	"strconv"
)

// YearGroup is a run of playlists sharing a DateUpdated year (or the undated
// group), for year-separated rendering.
type YearGroup struct {
	Label     string
	Playlists []*Node
}

// dirsOf returns the directory children, in their existing order.
func dirsOf(children []*Node) []*Node {
	var dirs []*Node
	for _, c := range children {
		if c.IsDir {
			dirs = append(dirs, c)
		}
	}
	return dirs
}

// yearGroupsOf splits playlist children into ordered year groups, preserving the
// children's (reverse-chron) order: consecutive same-year playlists share a
// group; undated ones form a trailing "Undated" group.
func yearGroupsOf(children []*Node) []YearGroup {
	var groups []YearGroup
	for _, c := range children {
		if c.IsDir {
			continue
		}
		label := "Undated"
		if !c.Playlist.DateUpdated.IsZero() {
			label = strconv.Itoa(c.Playlist.DateUpdated.Year())
		}
		if len(groups) == 0 || groups[len(groups)-1].Label != label {
			groups = append(groups, YearGroup{Label: label})
		}
		groups[len(groups)-1].Playlists = append(groups[len(groups)-1].Playlists, c)
	}
	return groups
}

// playlistNodeLess orders playlist leaf nodes: newest DateUpdated first, undated
// last, ties broken by Title. Shared by the per-folder sort in buildDir and by
// featuredOf so the two orderings cannot drift apart. Callers must pass leaves —
// directory nodes carry no Playlist.
func playlistNodeLess(a, b *Node) bool {
	au, bu := a.Playlist.DateUpdated, b.Playlist.DateUpdated
	if au.IsZero() != bu.IsZero() {
		return !au.IsZero()
	}
	if !au.Equal(bu) {
		return au.After(bu)
	}
	return a.Title < b.Title
}

// featuredOf collects every featured playlist leaf anywhere under root into one
// flat list in Featured order. Featuring is independent of where a playlist is
// filed in the hub, so this walks the whole tree; the result is nil when nothing
// is featured, which templates can test with {{with}}.
func featuredOf(root *Node) []*Node {
	var out []*Node
	var walk func(*Node)
	walk = func(n *Node) {
		for _, c := range n.Children {
			if c.IsDir {
				walk(c)
				continue
			}
			if c.Playlist.Featured {
				out = append(out, c)
			}
		}
	}
	walk(root)
	sort.SliceStable(out, func(i, j int) bool { return playlistNodeLess(out[i], out[j]) })
	return out
}
