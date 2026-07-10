package playlist

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// ParseText reads a plain-text track list into a native Playlist.
//
// Each track line is "{artist} - {title}", split on the first " - "; any further
// " - " stays in the title. Lines beginning with '#' are metadata/comments:
// "# title: X" and "# creator: X" (case-insensitive key) set the playlist's
// Title/Creator, and every other '#' line is an ignored comment. Blank lines are
// skipped. A non-blank, non-'#' line with no " - " separator (or with an empty
// artist or title) is skipped and its original text returned in warnings.
//
// The result is a native playlist (no SpotifyID); each track carries only Artist
// and Title. Title/Creator are populated only from headers here — callers layer
// flag overrides and filename defaults on top.
func ParseText(r io.Reader) (Playlist, []string, error) {
	var (
		p        Playlist
		warnings []string
	)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024) // tolerate long lines

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			body := strings.TrimSpace(strings.TrimPrefix(line, "#"))
			if key, val, ok := strings.Cut(body, ":"); ok {
				switch strings.ToLower(strings.TrimSpace(key)) {
				case "title":
					p.Title = strings.TrimSpace(val)
				case "creator":
					p.Creator = strings.TrimSpace(val)
				}
			}
			continue
		}

		artist, title, ok := strings.Cut(line, " - ")
		artist, title = strings.TrimSpace(artist), strings.TrimSpace(title)
		if !ok || artist == "" || title == "" {
			warnings = append(warnings, line)
			continue
		}
		p.Tracks = append(p.Tracks, Track{Artist: artist, Title: title})
	}
	if err := sc.Err(); err != nil {
		return Playlist{}, nil, fmt.Errorf("read: %w", err)
	}
	return p, warnings, nil
}
