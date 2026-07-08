package export

import (
	"encoding/json"
	"os"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

// JSPFExporter marshals a playlist into a JSPF (JSON) document suitable for web
// components. Track identifiers use the "urn:isrc:<isrc>" form; durations are
// emitted in seconds per the JSPF spec.
type JSPFExporter struct{}

type jspfDoc struct {
	Playlist jspfPlaylist `json:"playlist"`
}

type jspfPlaylist struct {
	Title   string      `json:"title,omitempty"`
	Creator string      `json:"creator,omitempty"`
	Date    string      `json:"date,omitempty"`
	Track   []jspfTrack `json:"track"`
}

type jspfTrack struct {
	Title      string   `json:"title,omitempty"`
	Creator    string   `json:"creator,omitempty"`
	Album      string   `json:"album,omitempty"`
	Duration   int      `json:"duration,omitempty"`
	Identifier []string `json:"identifier,omitempty"`
	Location   []string `json:"location,omitempty"`
}

func (JSPFExporter) Export(p playlist.Playlist, outputPath string, _ map[string]string) error {
	doc := jspfDoc{Playlist: jspfPlaylist{
		Title:   p.Title,
		Creator: p.Creator,
		Track:   make([]jspfTrack, 0, len(p.Tracks)),
	}}
	if !p.DateCreated.IsZero() {
		doc.Playlist.Date = p.DateCreated.UTC().Format("2006-01-02T15:04:05Z")
	}

	for _, t := range p.Tracks {
		jt := jspfTrack{
			Title:    t.Title,
			Creator:  t.Artist,
			Album:    t.Album,
			Duration: (t.DurationMS + 500) / 1000, // round to nearest second
		}
		if t.ISRC != "" {
			jt.Identifier = []string{"urn:isrc:" + t.ISRC}
		}
		if t.SpotifyURL != "" {
			jt.Location = []string{t.SpotifyURL}
		}
		doc.Playlist.Track = append(doc.Playlist.Track, jt)
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(outputPath, append(data, '\n'), 0o644)
}
