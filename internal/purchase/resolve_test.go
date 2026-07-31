package purchase

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/lmorchard/byom-sync/internal/rcache"
)

// fakeSource records calls and returns a canned answer per album. name defaults
// to "fake"; set it when a test needs two distinct sources.
type fakeSource struct {
	name    string
	byAlbum map[string]string
	calls   int
	err     error
}

func (f *fakeSource) Name() string {
	if f.name == "" {
		return "fake"
	}
	return f.name
}

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

	filled, _, err := Resolve(context.Background(), src, p, Options{})
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
	if _, _, err := Resolve(context.Background(), src, p, Options{}); err != nil {
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

	filled, _, err := Resolve(context.Background(), src, p, Options{Cache: cache})
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

	if _, _, err := Resolve(context.Background(), src, p, Options{Cache: cache, MissTTL: time.Hour}); err != nil {
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

	if _, _, err := Resolve(context.Background(), src, p, Options{Cache: cache, MissTTL: time.Hour}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if src.calls != 1 {
		t.Errorf("calls = %d, want 1 — an expired miss should be retried", src.calls)
	}
}

func TestResolveRecordsMiss(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{trackOf("A", "Unknown", "T")}}
	cache := mapCache{}
	if _, _, err := Resolve(context.Background(), &fakeSource{}, p, Options{Cache: cache}); err != nil {
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
	if _, _, err := Resolve(context.Background(), src, p, Options{Budget: &budget}); err != nil {
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
	if _, _, err := Resolve(context.Background(), src, p, Options{
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
	if _, _, err := Resolve(context.Background(), src, p, Options{}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if src.calls != 2 {
		t.Errorf("calls = %d, want 2 — albumless tracks are distinct lookups", src.calls)
	}
}

// The cascade rests on this: a tier-1 miss must not suppress tier 2. Cache keys
// are source-scoped, so the same album under a different source name is a
// different row.
func TestResolveSourcesKeepIndependentCacheKeys(t *testing.T) {
	q := Query{Artist: "A", Album: "B"}
	// Tier 1 already looked and found nothing, recently.
	cache := mapCache{q.CacheKey("tier1"): {Source: "tier1", CheckedAt: time.Now()}}

	// Tier 1 re-runs: the fresh miss suppresses the lookup, as intended.
	p1 := &playlist.Playlist{Tracks: []playlist.Track{trackOf("A", "B", "T")}}
	first := &fakeSource{name: "tier1", byAlbum: map[string]string{"B": "https://tier1/b"}}
	if _, _, err := Resolve(context.Background(), first, p1, Options{Cache: cache, MissTTL: time.Hour}); err != nil {
		t.Fatalf("Resolve tier1: %v", err)
	}
	if first.calls != 0 {
		t.Errorf("tier1 calls = %d, want 0 — its own fresh miss should suppress the retry", first.calls)
	}

	// Tier 2 must still get its turn at the very same album.
	p2 := &playlist.Playlist{Tracks: []playlist.Track{trackOf("A", "B", "T")}}
	second := &fakeSource{name: "tier2", byAlbum: map[string]string{"B": "https://tier2/b"}}
	filled, _, err := Resolve(context.Background(), second, p2, Options{Cache: cache, MissTTL: time.Hour})
	if err != nil {
		t.Fatalf("Resolve tier2: %v", err)
	}
	if second.calls != 1 {
		t.Errorf("tier2 calls = %d, want 1 — a tier-1 cached miss must not block tier 2", second.calls)
	}
	if filled != 1 || p2.Tracks[0].PurchaseURL != "https://tier2/b" {
		t.Errorf("filled=%d url=%q, want 1 and tier2's URL", filled, p2.Tracks[0].PurchaseURL)
	}
	// And tier 1's row must survive tier 2 writing its own.
	if e, ok := cache[q.CacheKey("tier1")]; !ok || e.Source != "tier1" {
		t.Errorf("tier1 cache row = %+v (ok=%v), want it untouched", e, ok)
	}
}

// The rate floor is a property of the source, not of one playlist file. The
// caller runs Resolve once per file, so a shared Tier is what keeps the floor
// holding across the file boundary — otherwise the first lookup in every file
// goes out unpaced.
func TestResolvePacesAcrossCalls(t *testing.T) {
	const pace = 120 * time.Millisecond
	tier := &Tier{}

	run := func(album string) time.Duration {
		p := &playlist.Playlist{Tracks: []playlist.Track{trackOf("A", album, "T")}}
		start := time.Now()
		if _, _, err := Resolve(context.Background(), &fakeSource{}, p, Options{Pace: pace, Tier: tier}); err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		return time.Since(start)
	}

	// First file: nothing has been requested yet, so no wait.
	if d := run("One"); d > pace/2 {
		t.Errorf("first call took %v, want no pacing delay", d)
	}
	// Second file: its first lookup must still respect the floor. Allow a
	// little slack for timer granularity.
	if d := run("Two"); d < pace-20*time.Millisecond {
		t.Errorf("second call took %v, want at least ~%v — the floor must hold across files", d, pace)
	}
}

// Without a shared Tier, each call is independently paced — the behavior the
// caller must not rely on, pinned here so the distinction stays visible.
func TestResolvePacingIsCallLocalWithoutATier(t *testing.T) {
	const pace = 120 * time.Millisecond
	for _, album := range []string{"One", "Two"} {
		p := &playlist.Playlist{Tracks: []playlist.Track{trackOf("A", album, "T")}}
		start := time.Now()
		if _, _, err := Resolve(context.Background(), &fakeSource{}, p, Options{Pace: pace}); err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if d := time.Since(start); d > pace/2 {
			t.Errorf("call for %q took %v, want no delay without a shared Tier", album, d)
		}
	}
}

// A source that has started refusing us must end the tier, not receive
// thousands more requests.
func TestResolveStopsOnSustainedErrors(t *testing.T) {
	tracks := make([]playlist.Track, 0, maxConsecutiveErrors*2)
	for i := 0; i < maxConsecutiveErrors*2; i++ {
		tracks = append(tracks, trackOf("A", fmt.Sprintf("Album %d", i), "T"))
	}
	p := &playlist.Playlist{Tracks: tracks}
	src := &fakeSource{err: errors.New("403 forbidden")}

	_, stopped, err := Resolve(context.Background(), src, p, Options{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if stopped != StopErrors {
		t.Errorf("stopped = %q, want %q", stopped, StopErrors)
	}
	if src.calls != maxConsecutiveErrors {
		t.Errorf("calls = %d, want %d — the streak must end the pass", src.calls, maxConsecutiveErrors)
	}
}

// One success clears the streak, so intermittent failures never accumulate into
// a false abort.
func TestResolveErrorStreakResetsOnSuccess(t *testing.T) {
	tracks := make([]playlist.Track, 0, maxConsecutiveErrors*3)
	for i := 0; i < maxConsecutiveErrors*3; i++ {
		tracks = append(tracks, trackOf("A", fmt.Sprintf("Album %d", i), "T"))
	}
	p := &playlist.Playlist{Tracks: tracks}

	// Fail, fail, succeed, repeat: never maxConsecutiveErrors in a row.
	src := &flakySource{failEvery: 3}
	_, stopped, err := Resolve(context.Background(), src, p, Options{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if stopped != "" {
		t.Errorf("stopped = %q, want empty — intermittent errors must not abort", stopped)
	}
	if src.calls != len(tracks) {
		t.Errorf("calls = %d, want %d — every album should have been attempted", src.calls, len(tracks))
	}
}

// flakySource fails unless the call index is a multiple of failEvery.
type flakySource struct {
	failEvery int
	calls     int
}

func (*flakySource) Name() string { return "flaky" }

func (f *flakySource) Lookup(_ context.Context, q Query) (Result, error) {
	f.calls++
	if f.calls%f.failEvery != 0 {
		return Result{}, errors.New("transient")
	}
	return Result{URL: "https://ok/" + q.Album, Kind: q.Kind(), Score: 1}, nil
}

// The tier ends where the streak completes, but everything filled before it
// stays filled — a stop is not a rollback.
func TestResolveKeepsFillsBeforeAnErrorStop(t *testing.T) {
	tracks := []playlist.Track{trackOf("A", "Good", "T")}
	for i := 0; i < maxConsecutiveErrors; i++ {
		tracks = append(tracks, trackOf("A", fmt.Sprintf("Bad %d", i), "T"))
	}
	p := &playlist.Playlist{Tracks: tracks}
	src := &failAfterFirst{url: "https://ok/good"}

	filled, stopped, err := Resolve(context.Background(), src, p, Options{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if stopped != StopErrors {
		t.Errorf("stopped = %q, want %q", stopped, StopErrors)
	}
	if filled != 1 || p.Tracks[0].PurchaseURL != "https://ok/good" {
		t.Errorf("filled=%d url=%q, want the pre-stop fill preserved", filled, p.Tracks[0].PurchaseURL)
	}
}

// failAfterFirst answers the first lookup and errors on every one after it.
type failAfterFirst struct {
	url   string
	calls int
}

func (*failAfterFirst) Name() string { return "failafterfirst" }

func (f *failAfterFirst) Lookup(_ context.Context, q Query) (Result, error) {
	f.calls++
	if f.calls == 1 {
		return Result{URL: f.url, Kind: q.Kind(), Score: 1}, nil
	}
	return Result{}, errors.New("refused")
}
