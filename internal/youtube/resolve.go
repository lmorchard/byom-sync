package youtube

import (
	"context"
	"errors"
	"strings"

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

// Resolve searches for a YouTube video ID for every track in p that lacks one,
// mutating the tracks in place. budget (if non-nil) caps the number of searches
// this call performs. report (if non-nil) is called once per search performed,
// for narration. It stops early — returning quotaHit=true — when the API reports
// quota exhaustion, leaving already-resolved IDs in place for the caller to
// persist. A per-track search error is reported and skipped (that track keeps
// its empty ID) and does not abort the run.
func Resolve(ctx context.Context, s Searcher, p *playlist.Playlist, budget *int, report func(Event)) (resolved int, quotaHit bool, err error) {
	for i := range p.Tracks {
		t := &p.Tracks[i]
		if t.YouTubeID != "" {
			continue
		}
		if budget != nil && *budget <= 0 {
			return resolved, false, nil
		}
		query := strings.TrimSpace(t.Artist + " " + t.Title)
		id, searchErr := s.Search(ctx, query)
		if budget != nil {
			*budget--
		}
		if errors.Is(searchErr, ErrQuotaExceeded) {
			return resolved, true, nil
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
	return resolved, false, nil
}
