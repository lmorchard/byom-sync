package site

import (
	"os"
	"path/filepath"

	"github.com/lmorchard/byom-sync/internal/mosaic"
)

// GenerateMosaics builds a representative cover mosaic for each playlist that
// has no explicit hero image, writes it to <outDir>/art/mosaic/<slug>.jpg, and
// sets the in-memory Playlist.ImageFile so the existing hero precedence carries
// it into the JSPF and og:image. The mosaic filename is the playlist's slug path
// (predictable + stable across rebuilds). Generation is skipped when the mosaic
// already exists and is at least as new as the playlist's source YAML, so
// unchanged playlists don't pay the cover-read + render cost. Playlists with an
// explicit hero, or with no downloaded covers, are left untouched.
func GenerateMosaics(hubDir, outDir string, root *Node) error {
	return walkPlaylists(root, func(n *Node) error {
		p := n.Playlist
		if p.Image != "" || p.ImageFile != "" {
			return nil // explicit hero wins
		}
		selected := mosaic.Select(*p)
		if len(selected) == 0 {
			return nil // no covers to composite
		}

		rel := filepath.ToSlash(filepath.Join("art", "mosaic", n.Path+".jpg"))
		dst := filepath.Join(outDir, filepath.FromSlash(rel))
		src := filepath.Join(hubDir, filepath.FromSlash(n.Path)+".yaml")

		// Skip regeneration when the mosaic is present and no older than its
		// source playlist YAML.
		if mosaicFresh(dst, src) {
			p.ImageFile = rel
			return nil
		}

		var covers [][]byte
		for _, c := range selected {
			b, err := os.ReadFile(filepath.Join(hubDir, filepath.FromSlash(c.ImageFile)))
			if err != nil {
				continue // unreadable cover → drop from the tile set
			}
			covers = append(covers, b)
		}
		if len(covers) == 0 {
			return nil
		}
		data, err := mosaic.Render(mosaic.Plan(len(covers)), covers)
		if err != nil {
			return err
		}
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

// mosaicFresh reports whether the mosaic at dst exists and is at least as new as
// the source playlist YAML at src. A missing dst, or an unstattable src, counts
// as not-fresh (regenerate).
func mosaicFresh(dst, src string) bool {
	di, err := os.Stat(dst)
	if err != nil {
		return false
	}
	si, err := os.Stat(src)
	if err != nil {
		return false
	}
	return !di.ModTime().Before(si.ModTime())
}
