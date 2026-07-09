package youtube

import (
	"context"
	"errors"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

// Event reports the outcome of one track's resolution so the caller can narrate
// progress. Exactly one state holds: Err set (failed), VideoID set (a hit, with
// Source naming the resolver), or both empty (no match from any resolver).
type Event struct {
	Artist  string
	Title   string
	VideoID string
	Source  string
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
		if reverify {
			ok, verr := opts.Verify(ctx, t.YouTubeID)
			if verr != nil {
				if opts.Report != nil {
					opts.Report(Event{Artist: t.Artist, Title: t.Title, Err: verr})
				}
				continue // keep the id; skip this track
			}
			if ok {
				continue // still embeddable
			}
			t.YouTubeID = "" // not embeddable → resolve fresh below
		}

		res, rerr := r.Resolve(ctx, *t)

		if errors.Is(rerr, ErrQuotaExceeded) {
			return resolved, StopQuota, nil
		}
		if errors.Is(rerr, ErrRateLimited) {
			return resolved, StopRateLimit, nil
		}
		if opts.Report != nil {
			opts.Report(Event{Artist: t.Artist, Title: t.Title, VideoID: res.VideoID, Source: res.Source, Err: rerr})
		}
		if rerr != nil {
			continue // transient/other error — skip this track, keep going
		}
		if res.VideoID != "" {
			t.YouTubeID = res.VideoID
			resolved++
			if opts.OnResolved != nil {
				if err := opts.OnResolved(); err != nil {
					return resolved, "", err
				}
			}
		}
	}
	return resolved, "", nil
}
