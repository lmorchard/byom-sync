package playlist

import (
	"strings"
	"testing"
)

func TestParseText(t *testing.T) {
	in := `# title: Dark Electro
# creator: Les
# just a comment

Perikkles - Threshold
Fragments Of Passion - All I Wanna Be (2026)
Malformed line without separator
Artist - Title - With Extra Dash
   Padded - Spaces
Artist -
`
	p, warnings, err := ParseText(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ParseText: %v", err)
	}

	if p.Title != "Dark Electro" || p.Creator != "Les" {
		t.Errorf("headers: title=%q creator=%q", p.Title, p.Creator)
	}
	if p.SpotifyID != "" || !p.IsNative() {
		t.Errorf("import should produce a native playlist, got spotify_id=%q", p.SpotifyID)
	}

	if len(p.Tracks) != 4 {
		t.Fatalf("tracks: got %d want 4: %+v", len(p.Tracks), p.Tracks)
	}
	if p.Tracks[0].Artist != "Perikkles" || p.Tracks[0].Title != "Threshold" {
		t.Errorf("track 0: %+v", p.Tracks[0])
	}
	if p.Tracks[1].Title != "All I Wanna Be (2026)" {
		t.Errorf("track 1 (parens preserved): %+v", p.Tracks[1])
	}
	// split on the FIRST " - "; extra separators stay in the title
	if p.Tracks[2].Artist != "Artist" || p.Tracks[2].Title != "Title - With Extra Dash" {
		t.Errorf("track 2 (first-split): %+v", p.Tracks[2])
	}
	// surrounding whitespace trimmed on both fields
	if p.Tracks[3].Artist != "Padded" || p.Tracks[3].Title != "Spaces" {
		t.Errorf("track 3 (trimmed): %+v", p.Tracks[3])
	}

	// "Malformed line without separator" (no " - ") and "Artist -" (empty title)
	// are both skipped with warnings.
	if len(warnings) != 2 {
		t.Fatalf("warnings: got %d want 2: %+v", len(warnings), warnings)
	}
	if warnings[0] != "Malformed line without separator" {
		t.Errorf("warning 0: %q", warnings[0])
	}
	if warnings[1] != "Artist -" {
		t.Errorf("warning 1: %q", warnings[1])
	}
}

func TestParseText_NoHeaders(t *testing.T) {
	p, warnings, err := ParseText(strings.NewReader("A - One\nB - Two\n"))
	if err != nil {
		t.Fatalf("ParseText: %v", err)
	}
	if p.Title != "" || p.Creator != "" {
		t.Errorf("no headers should leave title/creator empty: %q/%q", p.Title, p.Creator)
	}
	if len(p.Tracks) != 2 || len(warnings) != 0 {
		t.Errorf("tracks=%d warnings=%d", len(p.Tracks), len(warnings))
	}
}
