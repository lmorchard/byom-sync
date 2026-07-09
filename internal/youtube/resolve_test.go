package youtube

import (
	"context"
	"testing"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

// fakeSearcher returns queued results/errors in order.
type fakeSearcher struct {
	ids   []string
	errs  []error
	calls int
}

func (f *fakeSearcher) Search(_ context.Context, _ string) (string, error) {
	i := f.calls
	f.calls++
	var id string
	if i < len(f.ids) {
		id = f.ids[i]
	}
	var err error
	if i < len(f.errs) {
		err = f.errs[i]
	}
	return id, err
}

func pl(ids ...string) *playlist.Playlist {
	p := &playlist.Playlist{}
	for i := range ids {
		p.Tracks = append(p.Tracks, playlist.Track{Title: "t", Artist: "a", YouTubeID: ids[i]})
	}
	return p
}

func TestResolveOnlyFillsEmptyIDs(t *testing.T) {
	p := pl("", "already", "")
	f := &fakeSearcher{ids: []string{"v1", "v2"}}
	n, quota, err := Resolve(context.Background(), f, p, nil, nil)
	if err != nil || quota {
		t.Fatalf("n=%d quota=%v err=%v", n, quota, err)
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
	f := &fakeSearcher{ids: []string{"v1", "v2", "v3"}}
	budget := 1
	n, _, _ := Resolve(context.Background(), f, p, &budget, nil)
	if n != 1 || f.calls != 1 {
		t.Errorf("resolved=%d calls=%d, want 1/1", n, f.calls)
	}
	if budget != 0 {
		t.Errorf("budget=%d, want 0", budget)
	}
}

func TestResolveStopsOnQuota(t *testing.T) {
	p := pl("", "", "")
	f := &fakeSearcher{ids: []string{"v1", "", ""}, errs: []error{nil, ErrQuotaExceeded, nil}}
	n, quota, err := Resolve(context.Background(), f, p, nil, nil)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if !quota || n != 1 || f.calls != 2 {
		t.Errorf("quota=%v resolved=%d calls=%d, want true/1/2", quota, n, f.calls)
	}
	if p.Tracks[2].YouTubeID != "" {
		t.Errorf("track after quota should stay empty, got %q", p.Tracks[2].YouTubeID)
	}
}

func TestResolveSkipsSearchErrorAndContinues(t *testing.T) {
	p := pl("", "")
	f := &fakeSearcher{ids: []string{"", "v2"}, errs: []error{errSome(), nil}}
	n, quota, err := Resolve(context.Background(), f, p, nil, nil)
	if err != nil || quota {
		t.Fatalf("n=%d quota=%v err=%v", n, quota, err)
	}
	if n != 1 || p.Tracks[0].YouTubeID != "" || p.Tracks[1].YouTubeID != "v2" {
		t.Errorf("resolved=%d ids=%q", n, []string{p.Tracks[0].YouTubeID, p.Tracks[1].YouTubeID})
	}
}

func TestResolveReportsEachOutcome(t *testing.T) {
	p := pl("", "", "")
	f := &fakeSearcher{ids: []string{"v1", "", ""}, errs: []error{nil, nil, errSome()}}
	var events []Event
	_, _, _ = Resolve(context.Background(), f, p, nil, func(e Event) { events = append(events, e) })

	if len(events) != 3 {
		t.Fatalf("want 3 events, got %d", len(events))
	}
	if events[0].VideoID != "v1" || events[0].Err != nil { // hit
		t.Errorf("event0 = %+v, want hit v1", events[0])
	}
	if events[1].VideoID != "" || events[1].Err != nil { // miss
		t.Errorf("event1 = %+v, want miss", events[1])
	}
	if events[2].Err == nil { // error
		t.Errorf("event2 = %+v, want error", events[2])
	}
}

func errSome() error { return context.DeadlineExceeded }
