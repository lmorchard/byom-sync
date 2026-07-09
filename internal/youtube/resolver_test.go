package youtube

import (
	"context"
	"errors"
	"testing"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

// stubResolver returns a fixed result/error and counts calls.
type stubResolver struct {
	name  string
	res   Result
	err   error
	calls int
}

func (s *stubResolver) Name() string { return s.name }

func (s *stubResolver) Resolve(_ context.Context, _ playlist.Track) (Result, error) {
	s.calls++
	return s.res, s.err
}

// fakeSearcher for SearchResolver tests.
type fakeSearcher struct {
	id  string
	err error
}

func (f fakeSearcher) Search(_ context.Context, _ string) (string, error) { return f.id, f.err }

func TestChainFirstHitWins(t *testing.T) {
	r1 := &stubResolver{name: "odesli", res: Result{VideoID: "v1", Source: "odesli"}}
	r2 := &stubResolver{name: "youtube-search", res: Result{VideoID: "v2"}}
	res, err := NewChain(r1, r2).Resolve(context.Background(), playlist.Track{})
	if err != nil || res.VideoID != "v1" || res.Source != "odesli" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if r2.calls != 0 {
		t.Errorf("second resolver should not run after a hit (calls=%d)", r2.calls)
	}
}

func TestChainMissFallsThrough(t *testing.T) {
	r1 := &stubResolver{name: "odesli", res: Result{}} // clean miss
	r2 := &stubResolver{name: "youtube-search", res: Result{VideoID: "v2", Source: "youtube-search"}}
	res, err := NewChain(r1, r2).Resolve(context.Background(), playlist.Track{})
	if err != nil || res.VideoID != "v2" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if r1.calls != 1 || r2.calls != 1 {
		t.Errorf("calls r1=%d r2=%d, want 1/1", r1.calls, r2.calls)
	}
}

func TestChainDisablesExhaustedResolverButKeepsGoing(t *testing.T) {
	// odesli misses; youtube-search hits but then gets rate-limited on a later
	// call. The chain should disable youtube-search and keep resolving via
	// odesli — not halt the whole run.
	odesli := &stubResolver{name: "odesli", res: Result{VideoID: "od", Source: "odesli"}}
	yt := &stubResolver{name: "youtube-search", err: ErrRateLimited}
	var disabled []string
	c := NewChain(odesli, yt)
	c.OnDisable = func(name string, _ error) { disabled = append(disabled, name) }

	// odesli hits first, so youtube-search never runs and nothing is disabled.
	res, err := c.Resolve(context.Background(), playlist.Track{})
	if err != nil || res.VideoID != "od" {
		t.Fatalf("res=%+v err=%v", res, err)
	}

	// Now make odesli miss so the run reaches youtube-search, which is rate-limited.
	odesli.res = Result{}
	res, err = c.Resolve(context.Background(), playlist.Track{})
	if err != nil { // NOT halted — youtube-search disabled, odesli still active
		t.Fatalf("should not halt while odesli active, got err=%v", err)
	}
	if res.VideoID != "" {
		t.Errorf("this track is a miss, got %q", res.VideoID)
	}
	if len(disabled) != 1 || disabled[0] != "youtube-search" {
		t.Errorf("disabled = %v, want [youtube-search]", disabled)
	}

	// A subsequent track: youtube-search is skipped (disabled), odesli still tried.
	odesli.res = Result{VideoID: "od2", Source: "odesli"}
	res, _ = c.Resolve(context.Background(), playlist.Track{})
	if res.VideoID != "od2" {
		t.Errorf("want od2 from still-active odesli, got %q", res.VideoID)
	}
	if yt.calls != 1 {
		t.Errorf("youtube-search should not be called again once disabled (calls=%d)", yt.calls)
	}
}

func TestChainHaltsWhenAllResolversExhausted(t *testing.T) {
	r1 := &stubResolver{name: "odesli", err: ErrRateLimited}
	r2 := &stubResolver{name: "youtube-search", err: ErrQuotaExceeded}
	_, err := NewChain(r1, r2).Resolve(context.Background(), playlist.Track{})
	if !isStop(err) {
		t.Fatalf("want a stop error once all resolvers exhausted, got %v", err)
	}
}

func TestChainTransientErrorFallsThroughThenReturnsLastErr(t *testing.T) {
	boom := errors.New("boom")
	r1 := &stubResolver{name: "odesli", err: boom}             // transient
	r2 := &stubResolver{name: "youtube-search", res: Result{}} // miss
	res, err := NewChain(r1, r2).Resolve(context.Background(), playlist.Track{})
	if res.VideoID != "" {
		t.Errorf("want no id, got %q", res.VideoID)
	}
	if !errors.Is(err, boom) {
		t.Errorf("want last transient error surfaced, got %v", err)
	}
	if r2.calls != 1 {
		t.Errorf("transient error should fall through (r2 calls=%d)", r2.calls)
	}
}

func TestSearchResolverMapsResults(t *testing.T) {
	tr := playlist.Track{Artist: "Kavinsky", Title: "Nightcall"}

	res, err := (SearchResolver{Searcher: fakeSearcher{id: "vid9"}}).Resolve(context.Background(), tr)
	if err != nil || res.VideoID != "vid9" || res.Source != "youtube-search" {
		t.Errorf("hit: res=%+v err=%v", res, err)
	}

	res, err = (SearchResolver{Searcher: fakeSearcher{id: ""}}).Resolve(context.Background(), tr)
	if err != nil || res.VideoID != "" {
		t.Errorf("miss: res=%+v err=%v", res, err)
	}

	_, err = (SearchResolver{Searcher: fakeSearcher{err: ErrQuotaExceeded}}).Resolve(context.Background(), tr)
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("error should propagate, got %v", err)
	}
}
