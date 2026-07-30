package site

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// IndexNode is the nav projection of a Node serialized into site-index.json (no
// track data beyond the summary Meta line). It appears both in the nav tree and
// in the flat featured list. Path is absolute-from-root with leading + trailing
// slashes.
type IndexNode struct {
	Name     string      `json:"name"`
	Title    string      `json:"title"`
	Path     string      `json:"path"`
	IsDir    bool        `json:"isDir"`
	Meta     string      `json:"meta,omitempty"`  // playlist summary line (leaves only)
	Image    string      `json:"image,omitempty"` // resolved cover href (leaves only)
	Year     int         `json:"year,omitempty"`
	Children []IndexNode `json:"children,omitempty"`
}

func toIndexNodes(children []*Node) []IndexNode {
	out := make([]IndexNode, 0, len(children))
	for _, c := range children {
		n := IndexNode{
			Name:     c.Name,
			Title:    c.Title,
			Path:     "/" + c.Path + "/",
			IsDir:    c.IsDir,
			Children: toIndexNodes(c.Children),
		}
		if !c.IsDir {
			n.Meta = playlistMeta(c.Playlist)
			n.Image = coverHref(c.Playlist)
			if !c.Playlist.DateUpdated.IsZero() {
				n.Year = c.Playlist.DateUpdated.Year()
			}
		}
		out = append(out, n)
	}
	return out
}

// SiteIndex is the payload of site-index.json: the flat Featured list plus the
// nav tree. Featured is sorted here rather than client-side because Go has the
// full playlist dates and IndexNode only carries the year.
type SiteIndex struct {
	Featured []IndexNode `json:"featured,omitempty"`
	Children []IndexNode `json:"children"`
}

// WriteIndexJSON writes the featured list and the nav tree (root's children) to
// site-index.json.
func WriteIndexJSON(outDir string, root *Node) error {
	idx := SiteIndex{Children: toIndexNodes(root.Children)}
	if featured := featuredOf(root); len(featured) > 0 {
		idx.Featured = toIndexNodes(featured)
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "site-index.json"), append(data, '\n'), 0o644)
}
