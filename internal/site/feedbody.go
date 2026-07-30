package site

import (
	"html"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gorilla/feeds"
	"github.com/lmorchard/byom-sync/internal/playlist"
)

// trackLink returns the best playback URL for a track: its YouTube watch URL
// when a youtube_id is present, otherwise its Spotify URL, otherwise "".
//
// A spotify_url is only trusted when it is https. Hub data is generally our own,
// but a published feed is the wrong place to discover otherwise.
func trackLink(t playlist.Track) string {
	if t.YouTubeID != "" {
		return "https://www.youtube.com/watch?v=" + t.YouTubeID
	}
	if strings.HasPrefix(t.SpotifyURL, "https://") {
		return t.SpotifyURL
	}
	return ""
}

// trackThumb returns an absolute URL for a track's locally stored cover art, or
// "" when the track has only a remote image or none at all. Local-only on
// purpose: the content-addressed art store exists so the site and feed survive
// source-URL rot, and a hotlinked CDN thumbnail would defeat that.
func trackThumb(t playlist.Track, baseURL string) string {
	if t.ImageFile == "" {
		return ""
	}
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(t.ImageFile, "/")
}

// trackRow renders one <li> for a track: an optional thumbnail followed by
// "Artist — Title", wrapped in a playback link when one is available.
//
// The thumbnail carries width/height attributes *and* an inline style because
// feed readers sanitize aggressively — inline attributes survive where a <style>
// block does not.
func trackRow(t playlist.Track, baseURL string) string {
	label := html.EscapeString(t.Title)
	if t.Artist != "" {
		label = html.EscapeString(t.Artist) + " — " + label
	}

	var inner strings.Builder
	if thumb := trackThumb(t, baseURL); thumb != "" {
		inner.WriteString(`<img src="` + html.EscapeString(thumb) +
			`" alt="" width="48" height="48" ` +
			`style="vertical-align:middle;margin-right:8px">`)
	}
	inner.WriteString(label)

	if href := trackLink(t); href != "" {
		return `<li><a href="` + html.EscapeString(href) + `">` + inner.String() + `</a></li>`
	}
	return `<li>` + inner.String() + `</li>`
}

// itemHTML builds the RSS item body for one playlist: its cover, its own prose,
// a meta line, and the opening tracks as playback links. Each piece is omitted
// when the underlying data is absent.
//
// The result is used for both <description> and <content:encoded>. Many readers
// render only the former, which is exactly where a missing tracklist would
// defeat the point, so both carry the same HTML.
func itemHTML(n *Node, site SiteMeta) string {
	p := n.Playlist
	var b strings.Builder

	if cover := playlistImage(p, site.BaseURL); cover != "" {
		b.WriteString(`<p><img src="` + html.EscapeString(cover) +
			`" alt="` + html.EscapeString(n.Title) + ` cover" width="300"></p>`)
	}
	// plainText decodes the HTML entities Spotify serves; EscapeString then
	// re-encodes exactly once for output.
	if desc := strings.TrimSpace(plainText(p.Description)); desc != "" {
		b.WriteString(`<p>` + html.EscapeString(desc) + `</p>`)
	}
	if meta := playlistMeta(p); meta != "" {
		b.WriteString(`<p>` + html.EscapeString(meta) + `</p>`)
	}

	shown := p.Tracks
	if site.FeedTrackLimit > 0 && len(shown) > site.FeedTrackLimit {
		shown = shown[:site.FeedTrackLimit]
	}
	if len(shown) > 0 {
		b.WriteString(`<ol>`)
		for _, t := range shown {
			b.WriteString(trackRow(t, site.BaseURL))
		}
		b.WriteString(`</ol>`)
	}
	if rest := len(p.Tracks) - len(shown); rest > 0 {
		b.WriteString(`<p><a href="` + html.EscapeString(canonical(site.BaseURL, n.Path)) +
			`">…and ` + strconv.Itoa(rest) + ` more →</a></p>`)
	}
	return b.String()
}

// imageTypes maps the cover-art extensions the art store produces to their MIME
// types. This is an explicit table rather than a call to mime.TypeByExtension
// because that function consults system files (e.g. /etc/apache2/mime.types on
// macOS), so its answers vary by machine — and it will happily report
// application/octet-stream for a non-image, which is not something to advertise
// as a cover.
var imageTypes = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".webp": "image/webp",
	".gif":  "image/gif",
	".avif": "image/avif",
}

// localCoverPath returns the site-relative path of the playlist's cover when a
// downloaded local copy exists. It mirrors coverHref's precedence — playlist
// hero first, then the first track with local art — but ignores remote URLs.
func localCoverPath(p *playlist.Playlist) string {
	if p.ImageFile != "" {
		return strings.TrimLeft(p.ImageFile, "/")
	}
	for _, t := range p.Tracks {
		if t.ImageFile != "" {
			return strings.TrimLeft(t.ImageFile, "/")
		}
	}
	return ""
}

// coverEnclosure returns an RSS enclosure for the playlist's cover, or nil when
// one cannot be produced honestly.
//
// gorilla/feeds only emits an enclosure when both Type and Length are set, and
// Length is a byte count. That is knowable for a local file — GenerateMosaics and
// CopyArt both run before WriteFeed, so the file is already in outDir — but not
// for a remote URL without a network request, and the site build stays offline.
// A remote-only cover therefore gets no enclosure; the body's <img> still shows
// it.
func coverEnclosure(p *playlist.Playlist, site SiteMeta, outDir string) *feeds.Enclosure {
	rel := localCoverPath(p)
	if rel == "" {
		return nil
	}
	ctype, ok := imageTypes[strings.ToLower(filepath.Ext(rel))]
	if !ok {
		return nil
	}
	fi, err := os.Stat(filepath.Join(outDir, filepath.FromSlash(rel)))
	if err != nil || fi.Size() == 0 {
		return nil
	}
	return &feeds.Enclosure{
		Url:    strings.TrimRight(site.BaseURL, "/") + "/" + rel,
		Type:   ctype,
		Length: strconv.FormatInt(fi.Size(), 10),
	}
}
