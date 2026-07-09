package youtube

import (
	"context"
	"errors"
	"strings"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

// Resolve searches for a YouTube video ID for every track in p that lacks one,
// mutating the tracks in place. budget (if non-nil) caps the number of searches
// this call performs. It stops early — returning quotaHit=true — when the API
// reports quota exhaustion, leaving already-resolved IDs in place for the caller
// to persist. A per-track search error is skipped (that track keeps its empty
// ID) and does not abort the run.
func Resolve(ctx context.Context, s Searcher, p *playlist.Playlist, budget *int) (resolved int, quotaHit bool, err error) {
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
