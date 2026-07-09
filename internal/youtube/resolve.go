package youtube

import (
	"context"
	"errors"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

// EventKind classifies what happened to a track, for narration.
type EventKind string

const (
	KindResolved EventKind = "resolved" // a missing track got an id
	KindMiss     EventKind = "miss"     // no match found
	KindError    EventKind = "error"    // resolution/verify error (track skipped)
	KindKept     EventKind = "kept"     // reresolve: existing id still embeddable
	KindReplaced EventKind = "replaced" // reresolve: non-embeddable id replaced
	KindRemoved  EventKind = "removed"  // reresolve: non-embeddable id, no alternative — removed
)

// Event reports the outcome of one track's resolution so the caller can narrate
// progress.
type Event struct {
	Kind    EventKind
	Artist  string
	Title   string
	VideoID string // the resulting/kept id (empty for miss/removed/error)
	Source  string // resolver that produced VideoID
	Err     error
}

// Stop reasons returned by Resolve. "" means it finished (or hit the budget).
const (
	StopQuota     = "quota"
	StopRateLimit = "ratelimit"
)

// ResolveOptions configures a Resolve run.
type ResolveOptions struct {
	Budget     *int          // if non-nil, caps tracks attempted this call
	Pace       time.Duration // if > 0, waits between attempts (rate limiting)
	Report     func(Event)   // if set, called once per track attempted (narration)
	OnResolved func() error  // if set, called after each id is filled (incremental persist); a returned error stops the run
	// Reresolve, with Verify set, re-checks tracks that already have an id: an id
	// that fails Verify is cleared and resolved fresh; a passing one is kept.
	Reresolve bool
	Verify    func(ctx context.Context, videoID string) (bool, error)
}

// Resolve resolves a YouTube video ID for every track in p that lacks one,
// mutating the tracks in place. It stops early — returning a non-empty stopped
// reason — on quota exhaustion or sustained rate limiting. A per-track
// resolution error is reported and skipped (that track keeps its empty ID) and
// does not abort the run. An OnResolved error stops the run.
func Resolve(ctx context.Context, r Resolver, p *playlist.Playlist, opts ResolveOptions) (resolved int, stopped string, err error) {
	report := func(e Event) {
		if opts.Report != nil {
			opts.Report(e)
		}
	}
	persist := func() error {
		if opts.OnResolved != nil {
			return opts.OnResolved()
		}
		return nil
	}

	attempted := 0
	for i := range p.Tracks {
		t := &p.Tracks[i]
		// Already-resolved tracks are skipped unless we're re-verifying them.
		reverify := t.YouTubeID != "" && opts.Reresolve && opts.Verify != nil
		if t.YouTubeID != "" && !reverify {
			continue
		}
		if opts.Budget != nil && *opts.Budget <= 0 {
			return resolved, "", nil
		}
		if attempted > 0 && opts.Pace > 0 {
			if err := sleep(ctx, opts.Pace); err != nil {
				return resolved, "", err
			}
		}
		attempted++
		if opts.Budget != nil {
			*opts.Budget--
		}

		// Re-resolve: keep an id that's still embeddable, else clear + re-resolve.
		replacing := false
		if reverify {
			ok, verr := opts.Verify(ctx, t.YouTubeID)
			if verr != nil {
				report(Event{Kind: KindError, Artist: t.Artist, Title: t.Title, Err: verr})
				continue // keep the id; skip this track
			}
			if ok {
				report(Event{Kind: KindKept, Artist: t.Artist, Title: t.Title, VideoID: t.YouTubeID})
				continue
			}
			replacing = true
			t.YouTubeID = "" // not embeddable → resolve fresh below
		}

		res, rerr := r.Resolve(ctx, *t)
		if errors.Is(rerr, ErrQuotaExceeded) {
			return resolved, StopQuota, nil
		}
		if errors.Is(rerr, ErrRateLimited) {
			return resolved, StopRateLimit, nil
		}
		if rerr != nil {
			report(Event{Kind: KindError, Artist: t.Artist, Title: t.Title, Err: rerr})
			continue // transient/other error — skip this track, keep going
		}

		if res.VideoID != "" {
			t.YouTubeID = res.VideoID
			resolved++
			kind := KindResolved
			if replacing {
				kind = KindReplaced
			}
			report(Event{Kind: kind, Artist: t.Artist, Title: t.Title, VideoID: res.VideoID, Source: res.Source})
			if err := persist(); err != nil {
				return resolved, "", err
			}
		} else if replacing {
			// Non-embeddable id with no embeddable alternative: it's already cleared;
			// persist the removal so the broken id doesn't linger on disk.
			report(Event{Kind: KindRemoved, Artist: t.Artist, Title: t.Title})
			if err := persist(); err != nil {
				return resolved, "", err
			}
		} else {
			report(Event{Kind: KindMiss, Artist: t.Artist, Title: t.Title})
		}
	}
	return resolved, "", nil
}
