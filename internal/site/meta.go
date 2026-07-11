package site

import (
	"fmt"
	"strings"
	"time"

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

func monthYear(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("Jan 2006")
}

// dateRange formats a created–updated span as "Feb 2023 – Jun 2026", collapsing
// to a single value when both fall in the same month, and to whichever side is
// present when only one is.
func dateRange(created, updated time.Time) string {
	c, u := monthYear(created), monthYear(updated)
	switch {
	case c == "" && u == "":
		return ""
	case c == "":
		return u
	case u == "" || c == u:
		return c
	default:
		return c + " – " + u
	}
}

// playlistMeta renders a light one-line summary of a playlist — track count,
// total duration, and month-year — mirroring what byom-player shows, e.g.
// "16 tracks · 1 hr 8 min · Jul 2026". Segments with no data are omitted
// (duration when no track carries duration_ms; month when date_created is zero).
func playlistMeta(p *playlist.Playlist) string {
	parts := []string{trackCount(len(p.Tracks))}

	var totalMS int
	for _, t := range p.Tracks {
		totalMS += t.DurationMS
	}
	if d := humanDuration(totalMS); d != "" {
		parts = append(parts, d)
	}
	if r := dateRange(p.DateCreated, p.DateUpdated); r != "" {
		parts = append(parts, r)
	}
	return strings.Join(parts, " · ")
}

func trackCount(n int) string {
	if n == 1 {
		return "1 track"
	}
	return fmt.Sprintf("%d tracks", n)
}

// humanDuration formats a total of milliseconds as "1 hr 8 min" / "42 min",
// rounded to the nearest minute. Returns "" for a zero/negative total.
func humanDuration(totalMS int) string {
	if totalMS <= 0 {
		return ""
	}
	mins := (totalMS + 30_000) / 60_000
	h, m := mins/60, mins%60
	if h > 0 {
		if m > 0 {
			return fmt.Sprintf("%d hr %d min", h, m)
		}
		return fmt.Sprintf("%d hr", h)
	}
	return fmt.Sprintf("%d min", m)
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
