package youtube

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

// Event reports the outcome of one track's search so the caller can narrate
// progress. Exactly one of the states holds: Err set (search failed), VideoID
// set (a hit), or both empty (the API answered with no match).
type Event struct {
	Artist  string
	Title   string
	VideoID string
	Err     error
}

// Stop reasons returned by Resolve. "" means it finished (or hit the budget).
const (
	StopQuota     = "quota"
	StopRateLimit = "ratelimit"
)

// Resolve searches for a YouTube video ID for every track in p that lacks one,
// mutating the tracks in place. budget (if non-nil) caps the number of searches
// this call performs; pace (if > 0) waits between searches to stay under the
// API rate limit. report (if non-nil) is called once per search performed, for
// narration. It stops early — returning a non-empty stopped reason — on quota
// exhaustion or sustained rate limiting, leaving already-resolved IDs in place
// for the caller to persist. A per-track search error is reported and skipped
// (that track keeps its empty ID) and does not abort the run.
func Resolve(ctx context.Context, s Searcher, p *playlist.Playlist, budget *int, pace time.Duration, report func(Event)) (resolved int, stopped string, err error) {
	searched := 0
	for i := range p.Tracks {
		t := &p.Tracks[i]
		if t.YouTubeID != "" {
			continue
		}
		if budget != nil && *budget <= 0 {
			return resolved, "", nil
		}
		if searched > 0 && pace > 0 {
			if err := sleep(ctx, pace); err != nil {
				return resolved, "", err
			}
		}

		query := strings.TrimSpace(t.Artist + " " + t.Title)
		id, searchErr := s.Search(ctx, query)
		searched++
		if budget != nil {
			*budget--
		}

		if errors.Is(searchErr, ErrQuotaExceeded) {
			return resolved, StopQuota, nil
		}
		if errors.Is(searchErr, ErrRateLimited) {
			return resolved, StopRateLimit, nil
		}
		if report != nil {
			report(Event{Artist: t.Artist, Title: t.Title, VideoID: id, Err: searchErr})
		}
		if searchErr != nil {
			continue // transient/other error — skip this track, keep going
		}
		if id != "" {
			t.YouTubeID = id
			resolved++
		}
	}
	return resolved, "", nil
}
