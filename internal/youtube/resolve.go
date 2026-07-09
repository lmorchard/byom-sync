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

// Resolve resolves a YouTube video ID for every track in p that lacks one,
// mutating the tracks in place. budget (if non-nil) caps the number of tracks
// attempted this call; pace (if > 0) waits between attempts to stay under API
// rate limits. report (if non-nil) is called once per track attempted, for
// narration. It stops early — returning a non-empty stopped reason — on quota
// exhaustion or sustained rate limiting, leaving already-resolved IDs in place
// for the caller to persist. A per-track error is reported and skipped (that
// track keeps its empty ID) and does not abort the run.
func Resolve(ctx context.Context, r Resolver, p *playlist.Playlist, budget *int, pace time.Duration, report func(Event)) (resolved int, stopped string, err error) {
	attempted := 0
	for i := range p.Tracks {
		t := &p.Tracks[i]
		if t.YouTubeID != "" {
			continue
		}
		if budget != nil && *budget <= 0 {
			return resolved, "", nil
		}
		if attempted > 0 && pace > 0 {
			if err := sleep(ctx, pace); err != nil {
				return resolved, "", err
			}
		}

		res, rerr := r.Resolve(ctx, *t)
		attempted++
		if budget != nil {
			*budget--
		}

		if errors.Is(rerr, ErrQuotaExceeded) {
			return resolved, StopQuota, nil
		}
		if errors.Is(rerr, ErrRateLimited) {
			return resolved, StopRateLimit, nil
		}
		if report != nil {
			report(Event{Artist: t.Artist, Title: t.Title, VideoID: res.VideoID, Source: res.Source, Err: rerr})
		}
		if rerr != nil {
			continue // transient/other error — skip this track, keep going
		}
		if res.VideoID != "" {
			t.YouTubeID = res.VideoID
			resolved++
		}
	}
	return resolved, "", nil
}
