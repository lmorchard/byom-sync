package youtube

import (
	"context"
	"errors"
	"testing"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

// fakeResolver returns queued results/errors in order.
type fakeResolver struct {
	results []Result
	errs    []error
	calls   int
}

func (f *fakeResolver) Name() string { return "fake" }

func (f *fakeResolver) Resolve(_ context.Context, _ playlist.Track) (Result, error) {
	i := f.calls
	f.calls++
	var res Result
	if i < len(f.results) {
		res = f.results[i]
	}
	var err error
	if i < len(f.errs) {
		err = f.errs[i]
	}
	return res, err
}

func hit(id string) Result { return Result{VideoID: id, Source: "fake"} }

func pl(ids ...string) *playlist.Playlist {
	p := &playlist.Playlist{}
	for i := range ids {
		p.Tracks = append(p.Tracks, playlist.Track{Title: "t", Artist: "a", YouTubeID: ids[i]})
	}
	return p
}

func TestResolveOnlyFillsEmptyIDs(t *testing.T) {
	p := pl("", "already", "")
	f := &fakeResolver{results: []Result{hit("v1"), hit("v2")}}
	n, stopped, err := Resolve(context.Background(), f, p, ResolveOptions{})
	if err != nil || stopped != "" {
		t.Fatalf("n=%d stopped=%q err=%v", n, stopped, err)
	}
	if n != 2 || f.calls != 2 {
		t.Errorf("resolved=%d calls=%d, want 2/2", n, f.calls)
	}
	if p.Tracks[0].YouTubeID != "v1" || p.Tracks[1].YouTubeID != "already" || p.Tracks[2].YouTubeID != "v2" {
		t.Errorf("ids = %q", []string{p.Tracks[0].YouTubeID, p.Tracks[1].YouTubeID, p.Tracks[2].YouTubeID})
	}
}

func TestResolveRespectsBudget(t *testing.T) {
	p := pl("", "", "")
	f := &fakeResolver{results: []Result{hit("v1"), hit("v2"), hit("v3")}}
	budget := 1
	n, _, _ := Resolve(context.Background(), f, p, ResolveOptions{Budget: &budget})
	if n != 1 || f.calls != 1 {
		t.Errorf("resolved=%d calls=%d, want 1/1", n, f.calls)
	}
	if budget != 0 {
		t.Errorf("budget=%d, want 0", budget)
	}
}

func TestResolveStopsOnQuota(t *testing.T) {
	p := pl("", "", "")
	f := &fakeResolver{results: []Result{hit("v1"), {}, {}}, errs: []error{nil, ErrQuotaExceeded, nil}}
	n, stopped, err := Resolve(context.Background(), f, p, ResolveOptions{})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if stopped != StopQuota || n != 1 || f.calls != 2 {
		t.Errorf("stopped=%q resolved=%d calls=%d, want quota/1/2", stopped, n, f.calls)
	}
	if p.Tracks[2].YouTubeID != "" {
		t.Errorf("track after quota should stay empty, got %q", p.Tracks[2].YouTubeID)
	}
}

func TestResolveStopsOnRateLimit(t *testing.T) {
	p := pl("", "", "")
	f := &fakeResolver{results: []Result{hit("v1"), {}, {}}, errs: []error{nil, ErrRateLimited, nil}}
	n, stopped, err := Resolve(context.Background(), f, p, ResolveOptions{})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if stopped != StopRateLimit || n != 1 || f.calls != 2 {
		t.Errorf("stopped=%q resolved=%d calls=%d, want ratelimit/1/2", stopped, n, f.calls)
	}
}

func TestResolveSkipsErrorAndContinues(t *testing.T) {
	p := pl("", "")
	f := &fakeResolver{results: []Result{{}, hit("v2")}, errs: []error{errSome(), nil}}
	n, stopped, err := Resolve(context.Background(), f, p, ResolveOptions{})
	if err != nil || stopped != "" {
		t.Fatalf("n=%d stopped=%q err=%v", n, stopped, err)
	}
	if n != 1 || p.Tracks[0].YouTubeID != "" || p.Tracks[1].YouTubeID != "v2" {
		t.Errorf("resolved=%d ids=%q", n, []string{p.Tracks[0].YouTubeID, p.Tracks[1].YouTubeID})
	}
}

func TestResolveReportsEachOutcome(t *testing.T) {
	p := pl("", "", "")
	f := &fakeResolver{results: []Result{{VideoID: "v1", Source: "odesli"}, {}, {}}, errs: []error{nil, nil, errSome()}}
	var events []Event
	_, _, _ = Resolve(context.Background(), f, p, ResolveOptions{Report: func(e Event) { events = append(events, e) }})

	if len(events) != 3 {
		t.Fatalf("want 3 events, got %d", len(events))
	}
	if events[0].VideoID != "v1" || events[0].Source != "odesli" || events[0].Err != nil { // hit
		t.Errorf("event0 = %+v, want hit v1 via odesli", events[0])
	}
	if events[1].VideoID != "" || events[1].Err != nil { // miss
		t.Errorf("event1 = %+v, want miss", events[1])
	}
	if events[2].Err == nil { // error
		t.Errorf("event2 = %+v, want error", events[2])
	}
}

func TestResolveCallsOnResolvedPerResolution(t *testing.T) {
	p := pl("", "already", "", "")
	f := &fakeResolver{results: []Result{hit("v1"), {}, hit("v4")}} // 3 attempts: hit, miss, hit
	saves := 0
	n, _, err := Resolve(context.Background(), f, p, ResolveOptions{
		OnResolved: func() error {
			saves++
			return nil
		},
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if n != 2 || saves != 2 { // onResolved fires once per resolved id, not per attempt
		t.Errorf("resolved=%d saves=%d, want 2/2", n, saves)
	}
}

func TestResolveStopsWhenOnResolvedErrors(t *testing.T) {
	p := pl("", "", "")
	f := &fakeResolver{results: []Result{hit("v1"), hit("v2"), hit("v3")}}
	boom := context.DeadlineExceeded
	n, _, err := Resolve(context.Background(), f, p, ResolveOptions{OnResolved: func() error { return boom }})
	if !errors.Is(err, boom) {
		t.Fatalf("want persist error surfaced, got %v", err)
	}
	if n != 1 || f.calls != 1 { // stops after the first resolution's failed save
		t.Errorf("resolved=%d calls=%d, want 1/1", n, f.calls)
	}
}

func errSome() error { return context.DeadlineExceeded }
