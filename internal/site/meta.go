package site

import (
	"strings"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

// playlistImage returns the first track image as the page's og:image. Playlist-
// level cover art (a parallel effort) can supersede this once the field lands.
func playlistImage(p *playlist.Playlist) string {
	for _, t := range p.Tracks {
		if t.Image != "" {
			return t.Image
		}
	}
	return ""
}

// firstParagraph returns the first non-empty line of markdown with any leading
// heading marker/space trimmed — a cheap meta-description fallback.
func firstParagraph(md string) string {
	for _, line := range strings.Split(md, "\n") {
		s := strings.TrimSpace(strings.TrimLeft(line, "#"))
		if s != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// canonical joins baseURL and a root-relative urlPath into an absolute URL with
// a trailing slash.
func canonical(baseURL, urlPath string) string {
	b := strings.TrimRight(baseURL, "/")
	if urlPath == "" {
		return b + "/"
	}
	return b + "/" + strings.Trim(urlPath, "/") + "/"
}
