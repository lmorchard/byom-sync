package export

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

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

func (JSPFExporter) Export(p playlist.Playlist, outputPath string, opts map[string]string) error {
	doc := jspfDoc{Playlist: jspfPlaylist{
		Title:   p.Title,
		Creator: p.Creator,
		Track:   make([]jspfTrack, 0, len(p.Tracks)),
	}}
	if !p.DateCreated.IsZero() {
		doc.Playlist.Date = p.DateCreated.UTC().Format("2006-01-02T15:04:05Z")
	}

	doc.Playlist.Image = playlistImage(p, opts)

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
			Image:    jspfImage(t, opts),
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

// playlistImage returns the playlist's hero image. An explicit playlist-level
// image (p.Image / its downloaded p.ImageFile) wins over the first-track
// fallback: embedded data: URL (embed_art) → deployed local copy (art_base +
// image_file) → the source Image URL. When no explicit hero is set, it falls
// back to the first track with art so a playlist still has cover art.
func playlistImage(p playlist.Playlist, opts map[string]string) string {
	if d, ok := embedDataURL(p.ImageFile, opts); ok {
		return d
	}
	if base := opts["art_base"]; base != "" && p.ImageFile != "" {
		return joinURL(base, p.ImageFile)
	}
	if p.Image != "" {
		return p.Image
	}
	if base := opts["art_base"]; base != "" {
		for _, t := range p.Tracks {
			if t.ImageFile != "" {
				return joinURL(base, t.ImageFile)
			}
		}
	}
	for _, t := range p.Tracks {
		if t.Image != "" {
			return t.Image
		}
	}
	return ""
}

// jspfImage returns the JSPF image for a track: a data: URL embedded from the
// track's downloaded local copy when embed_art is set and a copy exists;
// else, when art_base is set and a local copy exists, art_base + image_file;
// else the track's source Image URL.
func jspfImage(t playlist.Track, opts map[string]string) string {
	if d, ok := embedDataURL(t.ImageFile, opts); ok {
		return d
	}
	if base := opts["art_base"]; base != "" && t.ImageFile != "" {
		return joinURL(base, t.ImageFile)
	}
	return t.Image
}

// embedDataURL reads a downloaded local art copy (art_root + imageFile) and
// returns it as a base64 data: URL. Returns ("", false) when embedding isn't
// requested, no local copy is recorded, or the read fails — callers then fall
// through to a URL.
func embedDataURL(imageFile string, opts map[string]string) (string, bool) {
	if opts["embed_art"] != "true" || imageFile == "" {
		return "", false
	}
	abs := filepath.Join(opts["art_root"], filepath.FromSlash(imageFile))
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", false
	}
	ct := ctypeForExt(filepath.Ext(imageFile))
	return "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(data), true
}

// joinURL joins a base URL and a relative path with exactly one slash.
func joinURL(base, rel string) string {
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(rel, "/")
}

func ctypeForExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return "image/jpeg"
	}
}
