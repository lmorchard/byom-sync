package purchase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/lmorchard/byom-sync/internal/rcache"
)

// fakeSource records calls and returns a canned answer per album.
type fakeSource struct {
	byAlbum map[string]string
	calls   int
	err     error
}

func (*fakeSource) Name() string { return "fake" }

func (f *fakeSource) Lookup(_ context.Context, q Query) (Result, error) {
	f.calls++
	if f.err != nil {
		return Result{}, f.err
	}
	if u, ok := f.byAlbum[q.Album]; ok {
		return Result{URL: u, Kind: KindAlbum, Score: 1.0}, nil
	}
	return Result{Kind: q.Kind()}, nil
}

type mapCache map[string]rcache.PurchaseEntry

func (m mapCache) GetPurchase(k string) (rcache.PurchaseEntry, bool) { e, ok := m[k]; return e, ok }
func (m mapCache) PutPurchase(k string, e rcache.PurchaseEntry) error {
	m[k] = e
	return nil
}

func trackOf(artist, album, title string) playlist.Track {
	return playlist.Track{Artist: artist, Album: album, Title: title}
}

// One lookup must fill every track on the same album.
func TestResolveFansOutAcrossAlbum(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{
		trackOf("Beach House", "Once Twice Melody", "Superstar"),
		trackOf("Beach House", "Once Twice Melody", "Pink Funeral"),
		trackOf("Beach House", "Once Twice Melody", "Runaway"),
	}}
	src := &fakeSource{byAlbum: map[string]string{
		"Once Twice Melody": "https://beachhouse.bandcamp.com/album/once-twice-melody",
	}}

	filled, err := Resolve(context.Background(), src, p, Options{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if filled != 3 {
		t.Errorf("filled = %d, want 3", filled)
	}
	if src.calls != 1 {
		t.Errorf("calls = %d, want 1 — album lookups must fan out", src.calls)
	}
	for i, tr := range p.Tracks {
		if tr.PurchaseURL == "" {
			t.Errorf("track %d has no purchase_url", i)
		}
	}
}

func TestResolveSkipsAlreadyResolved(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{
		{Artist: "A", Album: "B", Title: "T", PurchaseURL: "https://existing"},
	}}
	src := &fakeSource{}
	if _, err := Resolve(context.Background(), src, p, Options{}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if src.calls != 0 {
		t.Errorf("calls = %d, want 0 — an already-resolved track is how later tiers know to skip", src.calls)
	}
	if p.Tracks[0].PurchaseURL != "https://existing" {
		t.Error("existing purchase_url must not be overwritten")
	}
}

func TestResolveUsesCacheHit(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{trackOf("A", "B", "T")}}
	src := &fakeSource{}
	q := Query{Artist: "A", Album: "B"}
	cache := mapCache{q.CacheKey("fake"): {URL: "https://cached", Source: "fake", CheckedAt: time.Now()}}

	filled, err := Resolve(context.Background(), src, p, Options{Cache: cache})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if filled != 1 || p.Tracks[0].PurchaseURL != "https://cached" {
		t.Errorf("filled=%d url=%q, want 1 and the cached URL", filled, p.Tracks[0].PurchaseURL)
	}
	if src.calls != 0 {
		t.Errorf("calls = %d, want 0 on a cache hit", src.calls)
	}
}

func TestResolveHonorsFreshMiss(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{trackOf("A", "B", "T")}}
	src := &fakeSource{}
	q := Query{Artist: "A", Album: "B"}
	cache := mapCache{q.CacheKey("fake"): {CheckedAt: time.Now()}}

	if _, err := Resolve(context.Background(), src, p, Options{Cache: cache, MissTTL: time.Hour}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if src.calls != 0 {
		t.Errorf("calls = %d, want 0 — a fresh miss should be skipped", src.calls)
	}
}

func TestResolveRetriesExpiredMiss(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{trackOf("A", "B", "T")}}
	src := &fakeSource{}
	q := Query{Artist: "A", Album: "B"}
	cache := mapCache{q.CacheKey("fake"): {CheckedAt: time.Now().Add(-48 * time.Hour)}}

	if _, err := Resolve(context.Background(), src, p, Options{Cache: cache, MissTTL: time.Hour}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if src.calls != 1 {
		t.Errorf("calls = %d, want 1 — an expired miss should be retried", src.calls)
	}
}

func TestResolveRecordsMiss(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{trackOf("A", "Unknown", "T")}}
	cache := mapCache{}
	if _, err := Resolve(context.Background(), &fakeSource{}, p, Options{Cache: cache}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	e, ok := cache[Query{Artist: "A", Album: "Unknown"}.CacheKey("fake")]
	if !ok || e.URL != "" {
		t.Errorf("expected a negative cache entry, got %+v (ok=%v)", e, ok)
	}
}

func TestResolveBudgetStops(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{
		trackOf("A", "One", "T"), trackOf("B", "Two", "T"), trackOf("C", "Three", "T"),
	}}
	budget := 2
	src := &fakeSource{}
	if _, err := Resolve(context.Background(), src, p, Options{Budget: &budget}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if src.calls != 2 {
		t.Errorf("calls = %d, want 2 — budget must cap network attempts", src.calls)
	}
}

// A per-album failure is reported and skipped; the pass continues.
func TestResolveContinuesPastError(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{trackOf("A", "One", "T"), trackOf("B", "Two", "T")}}
	src := &fakeSource{err: errors.New("boom")}
	var errs int
	if _, err := Resolve(context.Background(), src, p, Options{
		Report: func(e Event) {
			if e.Kind == KindError {
				errs++
			}
		},
	}); err != nil {
		t.Fatalf("Resolve should not abort on per-album errors: %v", err)
	}
	if errs != 2 {
		t.Errorf("error events = %d, want 2", errs)
	}
}

// Albumless tracks resolve per-track and must not collide with each other.
func TestResolveAlbumlessTracks(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{
		trackOf("A", "", "First"), trackOf("A", "", "Second"),
	}}
	src := &fakeSource{}
	if _, err := Resolve(context.Background(), src, p, Options{}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if src.calls != 2 {
		t.Errorf("calls = %d, want 2 — albumless tracks are distinct lookups", src.calls)
	}
}
