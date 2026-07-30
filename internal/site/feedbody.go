package site

import (
	"html"
	"strconv"
	"strings"

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
