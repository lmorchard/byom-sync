// Package export compiles the local playlist "hub" (YAML) into destination
// "spoke" formats: M3U8, JSPF, and Markdown (with YAML frontmatter).
package export

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

// Exporter renders a single playlist to outputPath. cfg carries format-specific
// options (e.g. "lib_prefix" and "ext" for M3U8, "template" for Markdown).
type Exporter interface {
	Export(p playlist.Playlist, outputPath string, cfg map[string]string) error
}

// Run dispatches input to e. When input is a directory, every playlist YAML
// under it is exported recursively and the hub's structure is mirrored beneath
// out — "00-conceptual/drones.yaml" becomes "<out>/00-conceptual/drones.<ext>".
// Mirroring (rather than flattening) means two playlists with the same
// basename in different folders can't overwrite each other. When input is a
// single file, out is the exact output path.
func Run(e Exporter, ext, input, out string, cfg map[string]string) error {
	info, err := os.Stat(input)
	if err != nil {
		return fmt.Errorf("input %s: %w", input, err)
	}

	if !info.IsDir() {
		p, err := playlist.LoadFile(input)
		if err != nil {
			return fmt.Errorf("load %s: %w", input, err)
		}
		return e.Export(p, out, cfg)
	}

	paths, err := playlist.HubPaths(input)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	for _, path := range paths {
		p, err := playlist.LoadFile(path)
		if err != nil {
			return fmt.Errorf("load %s: %w", path, err)
		}
		rel, err := filepath.Rel(input, path)
		if err != nil {
			return fmt.Errorf("relativize %s: %w", path, err)
		}
		outPath := filepath.Join(out, strings.TrimSuffix(rel, filepath.Ext(rel))+"."+ext)
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return err
		}
		if err := e.Export(p, outPath, cfg); err != nil {
			return fmt.Errorf("export %s: %w", path, err)
		}
	}
	return nil
}
