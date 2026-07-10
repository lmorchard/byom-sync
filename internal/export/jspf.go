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
	Title      string               `json:"title,omitempty"`
	Creator    string               `json:"creator,omitempty"`
	Album      string               `json:"album,omitempty"`
	Duration   int                  `json:"duration,omitempty"`
	Identifier []string             `json:"identifier,omitempty"`
	Location   []string             `json:"location,omitempty"`
	Extension  map[string][]jspfExt `json:"extension,omitempty"`
}

// byomExtNS namespaces byom-sync's JSPF track extension (resolved ids and
// sync_state). Kept in sync with byom-player's reader.
const byomExtNS = "https://github.com/lmorchard/byom-sync"

type jspfExt struct {
	Resolved *jspfResolved `json:"resolved,omitempty"`
	// SpotifyPresent + DateOrphaned mirror playlist.SyncState, emitted only for
	// orphaned tracks (spotify_present:false). A *bool so false is serialized
	// while a present/absent track omits it. byom-player uses this for its orphan
	// indicator and degrades gracefully when absent.
	SpotifyPresent *bool  `json:"spotify_present,omitempty"`
	DateOrphaned   string `json:"date_orphaned,omitempty"`
}

type jspfResolved struct {
	YouTube string `json:"youtube,omitempty"`
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
		// Collect byom-sync extension data into a single element: the resolved id
		// (when present) and sync_state (only when orphaned).
		var ext jspfExt
		hasExt := false
		if t.YouTubeID != "" {
			ext.Resolved = &jspfResolved{YouTube: t.YouTubeID}
			hasExt = true
		}
		if !t.SyncState.SpotifyPresent {
			absent := false
			ext.SpotifyPresent = &absent
			ext.DateOrphaned = t.SyncState.DateOrphaned
			hasExt = true
		}
		if hasExt {
			jt.Extension = map[string][]jspfExt{byomExtNS: {ext}}
		}
		doc.Playlist.Track = append(doc.Playlist.Track, jt)
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(outputPath, append(data, '\n'), 0o644)
}
