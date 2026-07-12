package site

import (
	"io/fs"
	"os"
	"path/filepath"
)

// CopyArt copies the hub's downloaded cover-art store (<hubDir>/art) into
// <outDir>/art so the static site can serve it. A missing art dir is a no-op.
func CopyArt(hubDir, outDir string) error {
	src := filepath.Join(hubDir, "art")
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(hubDir, p)
		if err != nil {
			return err
		}
		dst := filepath.Join(outDir, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		// Content-addressed store: a file already present at the destination is
		// byte-identical, so skip it (fast incremental rebuilds). The size check
		// guards against a truncated prior copy.
		if fi, statErr := os.Stat(dst); statErr == nil {
			if si, infoErr := d.Info(); infoErr == nil && fi.Size() == si.Size() {
				return nil
			}
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
}
