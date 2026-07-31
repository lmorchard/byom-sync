package purchase

import (
	"regexp"
	"strings"
)

// editionSuffix matches trailing edition/remaster noise that store catalogues
// generally don't carry, in either the " - Foo Edition" or "(Foo Edition)" form.
// The dash form allows arbitrary text (e.g. a year) between the dash and the
// recognized keyword, since catalogues write both "- Deluxe Edition" and
// "- 2011 Remaster".
var editionSuffix = regexp.MustCompile(
	`(?i)\s*[-–]\s*.*\b(deluxe|remaster(ed)?|expanded|anniversary|special|edited)\b.*$` +
		`|\s*\((deluxe|remaster(ed)?|expanded|anniversary|special|edited|feat\.?)[^)]*\)\s*$`,
)

// trailingParen matches any trailing parenthetical, e.g. "(feat. X)".
var trailingParen = regexp.MustCompile(`\s*\([^)]*\)\s*$`)

// FirstArtist returns the first name from Spotify's comma-joined artist credit.
// The stores match a single primary artist; "Cavedoll, Tim Phillips" finds
// nothing while "Cavedoll" finds the record. Measured to rescue real misses.
func FirstArtist(s string) string {
	if i := strings.IndexByte(s, ','); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// CleanAlbum strips trailing parentheticals and edition markers. It never
// returns empty for a non-empty input — a title that is entirely parenthetical
// is left alone rather than erased.
func CleanAlbum(s string) string {
	out := strings.TrimSpace(editionSuffix.ReplaceAllString(s, ""))
	if out == "" {
		out = strings.TrimSpace(trailingParen.ReplaceAllString(s, ""))
	}
	if out == "" {
		return strings.TrimSpace(s)
	}
	return out
}
