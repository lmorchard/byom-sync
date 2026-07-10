package site

import (
	"os"
	"path/filepath"

	"github.com/lmorchard/byom-sync/internal/export"
)

// pageDir returns the on-disk directory that holds a node's index.html.
func pageDir(outDir string, n *Node) string {
	if n.Path == "" {
		return outDir
	}
	return filepath.Join(outDir, filepath.FromSlash(n.Path))
}

// walkPlaylists visits every playlist leaf in the tree, depth-first.
func walkPlaylists(root *Node, fn func(*Node) error) error {
	for _, c := range root.Children {
		if c.IsDir {
			if err := walkPlaylists(c, fn); err != nil {
				return err
			}
			continue
		}
		if err := fn(c); err != nil {
			return err
		}
	}
	return nil
}

// WriteJSPF writes a playlist.jspf.json next to each playlist page, reusing the
// tested JSPF exporter.
func WriteJSPF(outDir string, root *Node) error {
	return walkPlaylists(root, func(n *Node) error {
		dir := pageDir(outDir, n)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		return export.JSPFExporter{}.Export(*n.Playlist, filepath.Join(dir, "playlist.jspf.json"), nil)
	})
}
