// Package export compiles the local playlist "hub" (YAML) into destination
// "spoke" formats: M3U8, JSPF, and Hugo Markdown.
package export

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

// Exporter renders a single playlist to outputPath. cfg carries format-specific
// options (e.g. "lib_prefix" and "ext" for M3U8, "template" for Hugo).
type Exporter interface {
	Export(p playlist.Playlist, outputPath string, cfg map[string]string) error
}

// Run dispatches input to e. When input is a directory, every *.yaml file is
// exported to out (treated as a directory) as "<input-basename>.<ext>". When
// input is a single file, out is the exact output path.
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

	matches, err := filepath.Glob(filepath.Join(input, "*.yaml"))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	for _, path := range matches {
		p, err := playlist.LoadFile(path)
		if err != nil {
			return fmt.Errorf("load %s: %w", path, err)
		}
		base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		outPath := filepath.Join(out, base+"."+ext)
		if err := e.Export(p, outPath, cfg); err != nil {
			return fmt.Errorf("export %s: %w", path, err)
		}
	}
	return nil
}
