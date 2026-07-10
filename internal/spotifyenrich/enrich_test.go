package spotifyenrich

import (
	"context"
	"testing"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/lmorchard/byom-sync/internal/rcache"
)

// fakeSearcher returns canned candidates per query and records GetByID calls.
type fakeSearcher struct {
	byTitle map[string][]Candidate
	byID    map[string]Candidate
	calls   int
}

func (f *fakeSearcher) Search(_ context.Context, t playlist.Track) ([]Candidate, error) {
	f.calls++
	return f.byTitle[t.Title], nil
}

func (f *fakeSearcher) GetByID(_ context.Context, id string) (Candidate, error) {
	f.calls++
	return f.byID[id], nil
}

func TestEnrich_ConfidentFillsOnlyEmpty(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{{Title: "Nightcall", Artist: "Kavinsky"}}}
	s := &fakeSearcher{byTitle: map[string][]Candidate{
		"Nightcall": {{SpotifyID: "sid", ISRC: "FR123", Title: "Nightcall", Artist: "Kavinsky", Album: "Nightcall", SpotifyURL: "url", Image: "img", DurationMS: 258000}},
	}}
	n, err := Enrich(context.Background(), s, p, Options{})
	if err != nil || n != 1 {
		t.Fatalf("Enrich: n=%d err=%v", n, err)
	}
	got := p.Tracks[0]
	if got.SpotifyID != "sid" || got.ISRC != "FR123" || got.DurationMS != 258000 || got.Image != "img" {
		t.Errorf("technical fields not filled: %+v", got)
	}
	if got.Title != "Nightcall" || got.Artist != "Kavinsky" {
		t.Errorf("authored text should be preserved: %+v", got)
	}
	if len(got.EnrichCandidates) != 0 {
		t.Errorf("candidates should be empty on confident match: %+v", got.EnrichCandidates)
	}
}

func TestEnrich_PreservesSetTechnicalField(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{{Title: "Nightcall", Artist: "Kavinsky", Album: "My Album"}}}
	s := &fakeSearcher{byTitle: map[string][]Candidate{
		"Nightcall": {{SpotifyID: "sid", Title: "Nightcall", Artist: "Kavinsky", Album: "Spotify Album"}},
	}}
	_, _ = Enrich(context.Background(), s, p, Options{})
	if p.Tracks[0].Album != "My Album" {
		t.Errorf("set album should be preserved: %q", p.Tracks[0].Album)
	}
}

func TestEnrich_Canonicalize(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{{Title: "come together", Artist: "beatles"}}}
	s := &fakeSearcher{byTitle: map[string][]Candidate{
		"come together": {{SpotifyID: "sid", Title: "Come Together", Artist: "The Beatles", Album: "Abbey Road"}},
	}}
	_, _ = Enrich(context.Background(), s, p, Options{Canonicalize: true})
	if p.Tracks[0].Title != "Come Together" || p.Tracks[0].Artist != "The Beatles" {
		t.Errorf("canonicalize should overwrite text: %+v", p.Tracks[0])
	}
}

func TestEnrich_AmbiguousWritesCandidates(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{{Title: "Song", Artist: "X"}}}
	s := &fakeSearcher{byTitle: map[string][]Candidate{
		"Song": {
			{SpotifyID: "a", Title: "Totally Different", Artist: "Nobody"},
			{SpotifyID: "b", Title: "Another", Artist: "Someone"},
		},
	}}
	n, _ := Enrich(context.Background(), s, p, Options{MaxCandidates: 5})
	if n != 0 {
		t.Fatalf("ambiguous should not count as enriched: n=%d", n)
	}
	got := p.Tracks[0]
	if got.SpotifyID != "" {
		t.Errorf("ambiguous track should not get a spotify_id: %q", got.SpotifyID)
	}
	if len(got.EnrichCandidates) != 2 || got.EnrichCandidates[0].SpotifyID == "" || got.EnrichCandidates[0].Score == 0 {
		t.Errorf("candidates should be written with scores: %+v", got.EnrichCandidates)
	}
}

func TestEnrich_Miss(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{{Title: "Nope", Artist: "X"}}}
	s := &fakeSearcher{byTitle: map[string][]Candidate{}}
	var kinds []EventKind
	n, _ := Enrich(context.Background(), s, p, Options{Report: func(e Event) { kinds = append(kinds, e.Kind) }})
	if n != 0 || len(kinds) != 1 || kinds[0] != KindMiss {
		t.Fatalf("expected one miss event: n=%d kinds=%v", n, kinds)
	}
}

func TestEnrich_PickByEditing(t *testing.T) {
	// user copied candidate 'b' up to spotify_id; candidates still present
	p := &playlist.Playlist{Tracks: []playlist.Track{{
		Title: "Song", Artist: "X", SpotifyID: "b",
		EnrichCandidates: []playlist.EnrichCandidate{{SpotifyID: "a"}, {SpotifyID: "b"}},
	}}}
	s := &fakeSearcher{byID: map[string]Candidate{
		"b": {SpotifyID: "b", ISRC: "FR9", Title: "Song", Artist: "X", Album: "Real", SpotifyURL: "u", DurationMS: 100000},
	}}
	var kinds []EventKind
	n, _ := Enrich(context.Background(), s, p, Options{Report: func(e Event) { kinds = append(kinds, e.Kind) }})
	got := p.Tracks[0]
	if n != 1 || len(kinds) != 1 || kinds[0] != KindPicked {
		t.Fatalf("expected one picked event: n=%d kinds=%v", n, kinds)
	}
	if got.SpotifyID != "b" || got.ISRC != "FR9" || got.Album != "Real" {
		t.Errorf("pick should fill from the chosen id: %+v", got)
	}
	if len(got.EnrichCandidates) != 0 {
		t.Errorf("candidates should be cleared after pick: %+v", got.EnrichCandidates)
	}
}

func TestEnrich_SkipsAlreadyEnriched(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{{Title: "T", Artist: "A", SpotifyID: "done"}}}
	s := &fakeSearcher{}
	_, _ = Enrich(context.Background(), s, p, Options{})
	if s.calls != 0 {
		t.Errorf("already-enriched track should not be searched: calls=%d", s.calls)
	}
}

func TestEnrich_BudgetCaps(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{
		{Title: "One", Artist: "A"}, {Title: "Two", Artist: "B"},
	}}
	s := &fakeSearcher{byTitle: map[string][]Candidate{
		"One": {{SpotifyID: "1", Title: "One", Artist: "A"}},
		"Two": {{SpotifyID: "2", Title: "Two", Artist: "B"}},
	}}
	budget := 1
	_, _ = Enrich(context.Background(), s, p, Options{Budget: &budget})
	if s.calls != 1 {
		t.Errorf("budget=1 should attempt one track: calls=%d", s.calls)
	}
}

// fakeCache implements the Cache interface.
type fakeCache struct {
	m map[string]rcache.EnrichEntry
}

func (c *fakeCache) GetEnrich(key string) (rcache.EnrichEntry, bool)  { e, ok := c.m[key]; return e, ok }
func (c *fakeCache) PutEnrich(key string, e rcache.EnrichEntry) error { c.m[key] = e; return nil }

func TestEnrich_CachePositiveShortCircuits(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{{Title: "Nightcall", Artist: "Kavinsky"}}}
	key := p.Tracks[0].Key()
	cache := &fakeCache{m: map[string]rcache.EnrichEntry{
		key: {SpotifyID: "cached", ISRC: "FR1", Title: "Nightcall", Artist: "Kavinsky", CheckedAt: time.Now()},
	}}
	s := &fakeSearcher{}
	n, _ := Enrich(context.Background(), s, p, Options{Cache: cache})
	if n != 1 || s.calls != 0 {
		t.Fatalf("cache hit should avoid search: n=%d calls=%d", n, s.calls)
	}
	if p.Tracks[0].SpotifyID != "cached" {
		t.Errorf("cache positive should fill the track: %+v", p.Tracks[0])
	}
}
