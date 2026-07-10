package spotifyenrich

import (
	"testing"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

func TestScore_ExactMatchIsHigh(t *testing.T) {
	tr := playlist.Track{Title: "Nightcall", Artist: "Kavinsky", Album: "Nightcall"}
	c := Candidate{Title: "Nightcall", Artist: "Kavinsky", Album: "Nightcall"}
	if s := Score(tr, c); s < 0.99 {
		t.Errorf("exact match should score ~1.0, got %v", s)
	}
}

func TestScore_MinorVariationClearsThreshold(t *testing.T) {
	// authored loosely; Spotify has fuller strings. Containment-aware similarity
	// should let this clear the auto-accept threshold.
	tr := playlist.Track{Title: "Come Together", Artist: "Beatles"}
	c := Candidate{Title: "Come Together - Remastered 2019", Artist: "The Beatles", Album: "Abbey Road"}
	if s := Score(tr, c); s < DefaultThreshold {
		t.Errorf("close match should clear the auto-accept threshold, got %v", s)
	}
}

func TestScore_WrongTrackIsLow(t *testing.T) {
	tr := playlist.Track{Title: "Nightcall", Artist: "Kavinsky"}
	c := Candidate{Title: "Bohemian Rhapsody", Artist: "Queen"}
	if s := Score(tr, c); s >= DefaultThreshold {
		t.Errorf("wrong track should be below threshold, got %v", s)
	}
}

func TestSim(t *testing.T) {
	if got := sim("nightcall", "nightcall"); got != 1.0 {
		t.Errorf("identical sim: got %v want 1.0", got)
	}
	if got := sim("", ""); got != 1.0 {
		t.Errorf("empty/empty sim: got %v want 1.0", got)
	}
	if got := sim("abc", ""); got != 0.0 {
		t.Errorf("something vs empty: got %v want 0.0", got)
	}
	// containment: the shorter string matching a contiguous run of the longer
	// scores 1.0 — the property that fixes remaster suffixes / "The" prefixes.
	if got := sim("beatles", "the beatles"); got != 1.0 {
		t.Errorf("containment (prefix word): got %v want 1.0", got)
	}
	if got := sim("come together", "come together remastered 2019"); got != 1.0 {
		t.Errorf("containment (suffix): got %v want 1.0", got)
	}
	// typos still degrade gracefully.
	if got := sim("kitten", "sitting"); got < 0.6 || got > 0.7 {
		t.Errorf("kitten/sitting ratio out of expected band: got %v", got)
	}
}

func TestSim_ShortTokenNoOverMatch(t *testing.T) {
	// a short pattern ("go") is a trivial substring of many longer strings;
	// containment must not fire for it, or wrong tracks auto-accept.
	if got := sim("go", "going home"); got >= 0.5 {
		t.Errorf("short pattern should not over-match via containment: got %v", got)
	}
}

func TestScore_ShortTokenWrongTrackRejected(t *testing.T) {
	tr := playlist.Track{Title: "Go", Artist: "Cat"}
	c := Candidate{Title: "Going Home", Artist: "Cat Stevens"}
	if s := Score(tr, c); s >= DefaultThreshold {
		t.Errorf("short-token mismatch should stay below threshold, got %v", s)
	}
}
