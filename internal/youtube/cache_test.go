package youtube

import (
	"context"
	"testing"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/lmorchard/byom-sync/internal/rcache"
)

// fakeCache is an in-memory Cache for tests.
type fakeCache struct {
	m    map[string]rcache.Entry
	puts int
}

func newFakeCache() *fakeCache { return &fakeCache{m: map[string]rcache.Entry{}} }

func (c *fakeCache) Get(k string) (rcache.Entry, bool)  { e, ok := c.m[k]; return e, ok }
func (c *fakeCache) Put(k string, e rcache.Entry) error { c.m[k] = e; c.puts++; return nil }

// stubResolver (defined in resolver_test.go) returns a fixed result and counts calls.

func track(id string) playlist.Track { return playlist.Track{Artist: "A", Title: "T", ISRC: id} }

func TestResolvePositiveCacheHitSkipsResolver(t *testing.T) {
	cache := newFakeCache()
	cache.m[track("US1").Key()] = rcache.Entry{VideoID: "cached", CheckedAt: time.Now()}
	r := &stubResolver{res: Result{VideoID: "network"}}
	p := playlist.Playlist{Tracks: []playlist.Track{track("US1")}}
	n, _, err := Resolve(context.Background(), r, &p, ResolveOptions{Cache: cache, MissTTL: time.Hour, EmbedTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if r.calls != 0 {
		t.Fatalf("resolver called %d times; want 0 (cache hit)", r.calls)
	}
	if p.Tracks[0].YouTubeID != "cached" || n != 1 {
		t.Fatalf("got id=%q n=%d", p.Tracks[0].YouTubeID, n)
	}
}

func TestResolveFreshMissSkipsResolver(t *testing.T) {
	cache := newFakeCache()
	cache.m[track("US2").Key()] = rcache.Entry{VideoID: "", CheckedAt: time.Now()}
	r := &stubResolver{res: Result{VideoID: "network"}}
	p := playlist.Playlist{Tracks: []playlist.Track{track("US2")}}
	_, _, _ = Resolve(context.Background(), r, &p, ResolveOptions{Cache: cache, MissTTL: time.Hour, EmbedTTL: time.Hour})
	if r.calls != 0 {
		t.Fatalf("resolver called %d times; want 0 (fresh miss)", r.calls)
	}
}

func TestResolveExpiredMissReattemptsAndCaches(t *testing.T) {
	cache := newFakeCache()
	cache.m[track("US3").Key()] = rcache.Entry{VideoID: "", CheckedAt: time.Now().Add(-48 * time.Hour)}
	r := &stubResolver{res: Result{VideoID: "found", Source: "stub"}}
	p := playlist.Playlist{Tracks: []playlist.Track{track("US3")}}
	_, _, _ = Resolve(context.Background(), r, &p, ResolveOptions{Cache: cache, MissTTL: time.Hour, EmbedTTL: time.Hour})
	if r.calls != 1 {
		t.Fatalf("resolver called %d times; want 1 (expired miss)", r.calls)
	}
	if got, _ := cache.Get(track("US3").Key()); got.VideoID != "found" {
		t.Fatalf("cache not updated: %+v", got)
	}
}

func TestResolveMissIsCached(t *testing.T) {
	cache := newFakeCache()
	r := &stubResolver{res: Result{}} // no match
	p := playlist.Playlist{Tracks: []playlist.Track{track("US4")}}
	_, _, _ = Resolve(context.Background(), r, &p, ResolveOptions{Cache: cache, MissTTL: time.Hour, EmbedTTL: time.Hour})
	got, ok := cache.Get(track("US4").Key())
	if !ok || got.VideoID != "" {
		t.Fatalf("miss not cached: ok=%v %+v", ok, got)
	}
}

func TestReresolveEmbedCacheHitSkipsVerify(t *testing.T) {
	cache := newFakeCache()
	tr := track("US5")
	tr.YouTubeID = "vid"
	yes := true
	cache.m[tr.Key()] = rcache.Entry{VideoID: "vid", Embeddable: &yes, CheckedAt: time.Now()}
	verifyCalls := 0
	r := &stubResolver{}
	p := playlist.Playlist{Tracks: []playlist.Track{tr}}
	_, _, _ = Resolve(context.Background(), r, &p, ResolveOptions{
		Cache: cache, MissTTL: time.Hour, EmbedTTL: time.Hour, Reresolve: true,
		Verify: func(_ context.Context, _ string) (bool, error) { verifyCalls++; return true, nil },
	})
	if verifyCalls != 0 {
		t.Fatalf("Verify called %d times; want 0 (fresh embed cache)", verifyCalls)
	}
	if p.Tracks[0].YouTubeID != "vid" {
		t.Fatalf("id changed: %q", p.Tracks[0].YouTubeID)
	}
}

func TestNilCacheUnchangedBehavior(t *testing.T) {
	r := &stubResolver{res: Result{VideoID: "network", Source: "stub"}}
	p := playlist.Playlist{Tracks: []playlist.Track{track("US6")}}
	_, _, _ = Resolve(context.Background(), r, &p, ResolveOptions{}) // no cache
	if r.calls != 1 || p.Tracks[0].YouTubeID != "network" {
		t.Fatalf("nil-cache path changed: calls=%d id=%q", r.calls, p.Tracks[0].YouTubeID)
	}
}
