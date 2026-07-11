package export

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

// JSPFExporter marshals a playlist into a JSPF (JSON) document suitable for web
// components. Track identifiers use the "urn:isrc:<isrc>" form when an ISRC is
// known, else a synthesized "urn:byom:<hash>" so every track is addressable;
// durations are emitted in seconds per the JSPF spec.
type JSPFExporter struct{}

type jspfDoc struct {
	Playlist jspfPlaylist `json:"playlist"`
}

type jspfPlaylist struct {
	Title     string                       `json:"title,omitempty"`
	Creator   string                       `json:"creator,omitempty"`
	Date      string                       `json:"date,omitempty"`
	Image     string                       `json:"image,omitempty"`
	Extension map[string][]jspfPlaylistExt `json:"extension,omitempty"`
	Track     []jspfTrack                  `json:"track"`
}

type jspfTrack struct {
	Title      string               `json:"title,omitempty"`
	Creator    string               `json:"creator,omitempty"`
	Album      string               `json:"album,omitempty"`
	Image      string               `json:"image,omitempty"`
	Duration   int                  `json:"duration,omitempty"`
	Identifier []string             `json:"identifier,omitempty"`
	Location   []string             `json:"location,omitempty"`
	Extension  map[string][]jspfExt `json:"extension,omitempty"`
}

// byomExtNS namespaces byom-sync's JSPF track extension (resolved ids and
// sync_state). Kept in sync with byom-player's reader.
const byomExtNS = "https://github.com/lmorchard/byom-sync"

// jspfPlaylistExt carries byom-sync's playlist-level dates that JSPF has no
// native slot for. date_created maps to the standard playlist "date"; these two
// are emitted under the byom namespace only when non-zero. byom-player may read
// them for display and degrades gracefully when absent.
type jspfPlaylistExt struct {
	DateUpdated  string `json:"date_updated,omitempty"`
	DateImported string `json:"date_imported,omitempty"`
}

type jspfExt struct {
	Resolved *jspfResolved `json:"resolved,omitempty"`
	// SpotifyPresent + DateOrphaned mirror playlist.SyncState, emitted only for
	// orphaned tracks (spotify_present:false) of Spotify-sourced playlists —
	// native playlists never emit it. A *bool so false is serialized while a
	// present/absent track omits it. byom-player uses this for its orphan
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

	doc.Playlist.Image = playlistImage(p)

	var pext jspfPlaylistExt
	if !p.DateUpdated.IsZero() {
		pext.DateUpdated = p.DateUpdated.UTC().Format("2006-01-02T15:04:05Z")
	}
	if !p.DateImported.IsZero() {
		pext.DateImported = p.DateImported.UTC().Format("2006-01-02T15:04:05Z")
	}
	if pext.DateUpdated != "" || pext.DateImported != "" {
		doc.Playlist.Extension = map[string][]jspfPlaylistExt{byomExtNS: {pext}}
	}

	for _, t := range p.Tracks {
		jt := jspfTrack{
			Title:    t.Title,
			Creator:  t.Artist,
			Album:    t.Album,
			Image:    t.Image,
			Duration: (t.DurationMS + 500) / 1000, // round to nearest second
		}
		if t.ISRC != "" {
			jt.Identifier = []string{"urn:isrc:" + t.ISRC}
		} else {
			jt.Identifier = []string{"urn:byom:" + byomID(t)}
		}
		if t.SpotifyURL != "" {
			jt.Location = []string{t.SpotifyURL}
		}
		// Collect byom-sync extension data into a single element: the resolved id
		// (when present) and sync_state (only when orphaned, Spotify-sourced only).
		var ext jspfExt
		hasExt := false
		if t.YouTubeID != "" {
			ext.Resolved = &jspfResolved{YouTube: t.YouTubeID}
			hasExt = true
		}
		if p.Source() == playlist.SourceSpotify && !t.SyncState.SpotifyPresent {
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

// byomID is a stable hash of a track's normalized artist+title+album, used as
// the "urn:byom:<hash>" identifier for tracks without an ISRC so they remain
// addressable (e.g. by byom-player). Not cryptographic — just a content id.
func byomID(t playlist.Track) string {
	sum := sha1.Sum([]byte(t.ContentKey()))
	return hex.EncodeToString(sum[:])
}

// playlistImage returns the playlist's own image, or the first track's image as
// a fallback so a playlist still has cover art when none was set explicitly.
func playlistImage(p playlist.Playlist) string {
	if p.Image != "" {
		return p.Image
	}
	for _, t := range p.Tracks {
		if t.Image != "" {
			return t.Image
		}
	}
	return ""
}
