package site

import (
	"os"
	"path/filepath"

	"github.com/lmorchard/byom-sync/internal/mosaic"
)

// GenerateMosaics builds a representative cover mosaic for each playlist that
// has no explicit hero image, writes it to <outDir>/art/mosaic/<hash>.jpg, and
// sets the in-memory Playlist.ImageFile so the existing hero precedence carries
// it into the JSPF and og:image. Source cover bytes are read from the hub art
// store (<hubDir>/<image_file>), so this does not depend on CopyArt. Playlists
// with an explicit hero, or with no downloaded covers, are left untouched.
func GenerateMosaics(hubDir, outDir string, root *Node) error {
	return walkPlaylists(root, func(n *Node) error {
		p := n.Playlist
		if p.Image != "" || p.ImageFile != "" {
			return nil // explicit hero wins
		}
		var paths []string
		var covers [][]byte
		for _, c := range mosaic.Select(*p) {
			b, err := os.ReadFile(filepath.Join(hubDir, filepath.FromSlash(c.ImageFile)))
			if err != nil {
				continue // unreadable cover → drop from the tile set
			}
			paths = append(paths, c.ImageFile)
			covers = append(covers, b)
		}
		if len(covers) == 0 {
			return nil // nothing to composite
		}
		rel := filepath.ToSlash(filepath.Join("art", "mosaic", mosaic.Name(paths)))
		data, err := mosaic.Render(mosaic.Plan(len(covers)), covers)
		if err != nil {
			return err
		}
		dst := filepath.Join(outDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return err
		}
		p.ImageFile = rel
		return nil
	})
}
