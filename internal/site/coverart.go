package site

import (
	"os"
	"path/filepath"
)

// WriteCoverArt copies every self-hosted cover referenced by a playlist in the
// tree from hubDir into outDir, preserving the hub-relative path. Remote covers
// carry no local path and are skipped; a missing source file is skipped too
// (the page keeps its href — a broken <img> is preferable to aborting the whole
// build over one absent cover). Each source is copied at most once.
func WriteCoverArt(hubDir, outDir string, root *Node) error {
	seen := map[string]bool{}
	return walkPlaylists(root, func(c *Node) error {
		_, local := siteCover(c.Playlist)
		if local == "" || seen[local] {
			return nil
		}
		seen[local] = true
		rel := filepath.FromSlash(local)
		data, err := os.ReadFile(filepath.Join(hubDir, rel))
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		dst := filepath.Join(outDir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
}
