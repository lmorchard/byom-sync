package site

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// IndexNode is the nav projection of a Node serialized into site-index.json (no
// track data beyond the summary Meta line). Path is absolute-from-root with
// leading + trailing slashes.
type IndexNode struct {
	Name     string      `json:"name"`
	Title    string      `json:"title"`
	Path     string      `json:"path"`
	IsDir    bool        `json:"isDir"`
	Meta     string      `json:"meta,omitempty"` // playlist summary line (leaves only)
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
			if !c.Playlist.DateUpdated.IsZero() {
				n.Year = c.Playlist.DateUpdated.Year()
			}
		}
		out = append(out, n)
	}
	return out
}

// WriteIndexJSON writes the nav tree (root's children) to site-index.json.
func WriteIndexJSON(outDir string, root *Node) error {
	data, err := json.MarshalIndent(toIndexNodes(root.Children), "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "site-index.json"), append(data, '\n'), 0o644)
}
