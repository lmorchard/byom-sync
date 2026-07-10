package coverart

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/lmorchard/byom-sync/internal/rcache"
)

type fakeArter struct {
	byTitle map[string]Result
	err     error
	calls   int
}

func (f *fakeArter) Resolve(_ context.Context, t playlist.Track) (Result, error) {
	f.calls++
	if f.err != nil {
		return Result{}, f.err
	}
	return f.byTitle[t.Title], nil
}

func TestResolve_FillsMissingArt(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{{Title: "Song", Artist: "A"}}}
	a := &fakeArter{byTitle: map[string]Result{"Song": {ImageURL: "https://img", Source: "musicbrainz-recording"}}}
	n, err := Resolve(context.Background(), a, p, Options{})
	if err != nil || n != 1 {
		t.Fatalf("Resolve: n=%d err=%v", n, err)
	}
	if p.Tracks[0].Image != "https://img" {
		t.Errorf("image not filled: %+v", p.Tracks[0])
	}
}

func TestResolve_SkipsTracksWithImage(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{{Title: "Song", Artist: "A", Image: "existing"}}}
	a := &fakeArter{}
	_, _ = Resolve(context.Background(), a, p, Options{})
	if a.calls != 0 {
		t.Errorf("track with an image should be skipped: calls=%d", a.calls)
	}
}

func TestResolve_Miss(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{{Title: "Nope", Artist: "A"}}}
	a := &fakeArter{byTitle: map[string]Result{}} // empty Result = miss
	var kinds []EventKind
	n, _ := Resolve(context.Background(), a, p, Options{Report: func(e Event) { kinds = append(kinds, e.Kind) }})
	if n != 0 || len(kinds) != 1 || kinds[0] != KindMiss {
		t.Fatalf("expected one miss: n=%d kinds=%v", n, kinds)
	}
}

func TestResolve_ErrorReportedNotFatal(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{{Title: "Song", Artist: "A"}}}
	a := &fakeArter{err: errors.New("boom")}
	var kinds []EventKind
	n, err := Resolve(context.Background(), a, p, Options{Report: func(e Event) { kinds = append(kinds, e.Kind) }})
	if err != nil || n != 0 || len(kinds) != 1 || kinds[0] != KindError {
		t.Fatalf("expected one error event, non-fatal: n=%d err=%v kinds=%v", n, err, kinds)
	}
}

func TestResolve_BudgetCaps(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{{Title: "One", Artist: "A"}, {Title: "Two", Artist: "B"}}}
	a := &fakeArter{byTitle: map[string]Result{"One": {ImageURL: "1"}, "Two": {ImageURL: "2"}}}
	budget := 1
	_, _ = Resolve(context.Background(), a, p, Options{Budget: &budget})
	if a.calls != 1 {
		t.Errorf("budget=1 should attempt one track: calls=%d", a.calls)
	}
}

type fakeCache struct{ m map[string]rcache.ArtEntry }

func (c *fakeCache) GetArt(k string) (rcache.ArtEntry, bool)  { e, ok := c.m[k]; return e, ok }
func (c *fakeCache) PutArt(k string, e rcache.ArtEntry) error { c.m[k] = e; return nil }

func TestResolve_CachePositiveShortCircuits(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{{Title: "Song", Artist: "A"}}}
	key := p.Tracks[0].Key()
	cache := &fakeCache{m: map[string]rcache.ArtEntry{key: {ImageURL: "cached", CheckedAt: time.Now()}}}
	a := &fakeArter{}
	n, _ := Resolve(context.Background(), a, p, Options{Cache: cache})
	if n != 1 || a.calls != 0 || p.Tracks[0].Image != "cached" {
		t.Fatalf("cache hit should fill without a lookup: n=%d calls=%d image=%q", n, a.calls, p.Tracks[0].Image)
	}
}

func TestResolve_CacheFreshMissSkips(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	p := &playlist.Playlist{Tracks: []playlist.Track{{Title: "Nope", Artist: "A"}}}
	key := p.Tracks[0].Key()
	cache := &fakeCache{m: map[string]rcache.ArtEntry{key: {ImageURL: "", CheckedAt: now}}}
	a := &fakeArter{}
	n, _ := Resolve(context.Background(), a, p, Options{
		Cache: cache, MissTTL: 24 * time.Hour, Now: func() time.Time { return now },
	})
	if n != 0 || a.calls != 0 {
		t.Fatalf("fresh miss should skip lookup: n=%d calls=%d", n, a.calls)
	}
}
