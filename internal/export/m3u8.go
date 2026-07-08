package export

import (
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

// M3U8Exporter writes an extended M3U8 playlist for local media servers
// (Navidrome, Mopidy, etc.). Track file paths are constructed as
// "{lib_prefix}/{Artist}/{Album}/{Title}.{ext}" — they are emitted verbatim and
// not verified against the filesystem. All tracks are included, orphans and all.
type M3U8Exporter struct{}

func (M3U8Exporter) Export(p playlist.Playlist, outputPath string, cfg map[string]string) error {
	ext := cfg["ext"]
	if ext == "" {
		ext = "flac"
	}
	prefix := cfg["lib_prefix"]

	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	for _, t := range p.Tracks {
		if t.Title == "" {
			continue
		}
		seconds := t.DurationMS / 1000
		fmt.Fprintf(&b, "#EXTINF:%d,%s - %s\n", seconds, t.Artist, t.Title)
		// Use path (not filepath) so paths use "/" regardless of host OS — these
		// are paths on the target media server, not the local machine.
		b.WriteString(path.Join(prefix, t.Artist, t.Album, t.Title) + "." + ext + "\n")
	}

	return os.WriteFile(outputPath, []byte(b.String()), 0o644)
}
