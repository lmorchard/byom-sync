package purchase

import "testing"

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
	// The real iTunes failure: a different album by the right artist.
	wrong := Score(q, "Amanda Palmer", "Piano Is Evil")
	if wrong >= Threshold {
		t.Errorf("wrong album scored %v, want < %v", wrong, Threshold)
	}
	// A fuller credit than we asked for should still pass — measured at 1.00.
	fuller := Score(q, "Amanda Palmer & The Grand Theft Orchestra", "Theatre Is Evil")
	if fuller < Threshold {
		t.Errorf("fuller artist credit scored %v, want >= %v", fuller, Threshold)
	}
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
