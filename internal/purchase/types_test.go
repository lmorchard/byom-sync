package purchase

import (
	"net/http"
	"testing"
)

func TestQueryKind(t *testing.T) {
	if got := (Query{Artist: "A", Album: "B"}).Kind(); got != KindAlbum {
		t.Errorf("Kind() = %q, want %q", got, KindAlbum)
	}
	if got := (Query{Artist: "A", Title: "T"}).Kind(); got != KindTrack {
		t.Errorf("Kind() = %q, want %q", got, KindTrack)
	}
}

func TestQueryCacheKeyIsSourceScoped(t *testing.T) {
	q := Query{Artist: "Beach House", Album: "Once Twice Melody"}
	if q.CacheKey("bandcamp") == q.CacheKey("itunes") {
		t.Error("cache keys must differ per source so each tier owns its key space")
	}
}

func TestQueryCacheKeyNormalizes(t *testing.T) {
	a := Query{Artist: "Beach House", Album: "Once Twice Melody"}
	b := Query{Artist: "  beach   house ", Album: "Once Twice Melody!"}
	if a.CacheKey("bandcamp") != b.CacheKey("bandcamp") {
		t.Error("cosmetically different but equivalent queries must share a cache key")
	}
}

// Album-scoped and track-scoped queries for the same artist must not collide.
func TestQueryCacheKeyScopeSeparation(t *testing.T) {
	album := Query{Artist: "Beach House", Album: "Superstar"}
	track := Query{Artist: "Beach House", Title: "Superstar"}
	if album.CacheKey("bandcamp") == track.CacheKey("bandcamp") {
		t.Error("album and track scopes must not share a cache key")
	}
}

func TestScore(t *testing.T) {
	q := Query{Artist: "Amanda Palmer", Album: "Theatre Is Evil"}
	exact := Score(q, "Amanda Palmer", "Theatre Is Evil")
	if exact < Threshold {
		t.Errorf("exact match scored %v, want >= %v", exact, Threshold)
	}
	// A fuller credit than we asked for should still pass — measured at 1.00.
	fuller := Score(q, "Amanda Palmer & The Grand Theft Orchestra", "Theatre Is Evil")
	if fuller < Threshold {
		t.Errorf("fuller artist credit scored %v, want >= %v", fuller, Threshold)
	}
	// Score alone does NOT reject a same-artist wrong album — see
	// TestScoreAboveThresholdButAcceptRejects, where that is the point.
}

// Real rejections from the measured run; each must stay below threshold.
func TestScoreRejectsMeasuredWrongAlbums(t *testing.T) {
	for _, tc := range []struct {
		q             Query
		artist, album string
	}{
		{Query{Artist: "Ride", Album: "Peace Sign"}, "Various Artists", "Classical Music for Zodiac Signs"},
		{Query{Artist: "Rob Zombie", Album: "Hellbilly Deluxe"}, "Rob Zombie", "The Sinister Urge"},
		{Query{Artist: "Sara Lov", Album: "I Already Love You"}, "Eddie Cochran", "Summertime Blues - EP"},
	} {
		if got := Score(tc.q, tc.artist, tc.album); got >= Threshold {
			t.Errorf("Score(%q/%q vs %q/%q) = %v, want < %v",
				tc.q.Artist, tc.q.Album, tc.artist, tc.album, got, Threshold)
		}
	}
}

// Pins the regression this task exists to prevent: at equal weights, the
// combined Score for a real same-artist wrong album ("Piano Is Evil" for a
// "Theatre Is Evil" query) clears Threshold on its own. SubjectFloor — via
// Accept — is what actually rejects it. If a future reader "simplifies" the
// floor away as redundant with Threshold, this test catches it.
func TestScoreAboveThresholdButAcceptRejects(t *testing.T) {
	q := Query{Artist: "Amanda Palmer", Album: "Theatre Is Evil"}
	score := Score(q, "Amanda Palmer", "Piano Is Evil")
	if score < Threshold {
		t.Fatalf("expected the combined score to clear Threshold on its own (that's the bug this test documents), got %v", score)
	}
	if _, ok := Accept(q, "Amanda Palmer", "Piano Is Evil"); ok {
		t.Error("Accept must still reject Piano Is Evil despite the combined score clearing Threshold")
	}
}

func TestAccept(t *testing.T) {
	q := Query{Artist: "Amanda Palmer", Album: "Theatre Is Evil"}
	if _, ok := Accept(q, "Amanda Palmer", "Theatre Is Evil"); !ok {
		t.Error("exact match should be accepted")
	}
	// A fuller credit than we asked for should still pass — measured at 1.00.
	if _, ok := Accept(q, "Amanda Palmer & The Grand Theft Orchestra", "Theatre Is Evil"); !ok {
		t.Error("fuller artist credit should still be accepted")
	}

	// Real rejections from the measured run; SubjectFloor must reject each
	// even though a same-artist match makes the combined Score deceptive.
	for _, tc := range []struct {
		q             Query
		artist, album string
	}{
		{Query{Artist: "Amanda Palmer", Album: "Theatre Is Evil"}, "Amanda Palmer", "Piano Is Evil"},
		{Query{Artist: "Rob Zombie", Album: "Hellbilly Deluxe"}, "Rob Zombie", "The Sinister Urge"},
		{Query{Artist: "Ride", Album: "Peace Sign"}, "Various Artists", "Classical Music for Zodiac Signs"},
	} {
		if _, ok := Accept(tc.q, tc.artist, tc.album); ok {
			t.Errorf("Accept(%q/%q vs %q/%q) = true, want false",
				tc.q.Artist, tc.q.Album, tc.artist, tc.album)
		}
	}
}

// Every source must default to a client with a timeout: http.DefaultClient has
// none, and one hung connection would stall a multi-hour pass indefinitely.
func TestSourcesDefaultToATimeoutClient(t *testing.T) {
	for name, got := range map[string]*http.Client{
		"bandcamp": NewBandcamp(nil, "").client,
		"itunes":   NewITunes(nil, "").client,
		"discogs":  NewDiscogs(nil, "", "").client,
	} {
		if got == http.DefaultClient {
			t.Errorf("%s: uses http.DefaultClient, which has no timeout", name)
		}
		if got.Timeout <= 0 {
			t.Errorf("%s: client timeout = %v, want > 0", name, got.Timeout)
		}
	}
}
