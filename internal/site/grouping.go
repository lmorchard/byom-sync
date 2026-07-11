package site

import "strconv"

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
