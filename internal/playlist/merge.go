package playlist

import "time"

// Strategy selects how a fetched remote playlist is combined with the local one.
type Strategy string

const (
	// Archive is append-only: new remote tracks are added, and local tracks that
	// have disappeared from the remote are kept but marked orphaned.
	Archive Strategy = "archive"
	// Mirror overwrites the local playlist to match the remote exactly.
	Mirror Strategy = "mirror"
)

// Merge combines a locally-stored playlist with a freshly-fetched remote one.
//
// Metadata (Title, Creator, DateCreated, SpotifyID) always comes from remote.
//
//	Archive: union by Track.Key(). Remote tracks are marked SpotifyPresent=true
//	         with any orphan date cleared. Local tracks absent from the remote are
//	         kept (never deleted): marked SpotifyPresent=false and, if not already
//	         orphaned, stamped with DateOrphaned=now (RFC3339 UTC). Ordering: all
//	         remote tracks first (in remote order), then orphaned local tracks in
//	         their prior order.
//	Mirror:  the remote tracks exactly, all marked SpotifyPresent=true; local-only
//	         tracks are discarded.
func Merge(local, remote Playlist, strat Strategy, now time.Time) Playlist {
	out := remote
	out.Tracks = make([]Track, 0, len(remote.Tracks))

	remoteKeys := make(map[string]bool, len(remote.Tracks))
	for _, rt := range remote.Tracks {
		rt.SyncState = SyncState{SpotifyPresent: true}
		out.Tracks = append(out.Tracks, rt)
		remoteKeys[rt.Key()] = true
	}

	if strat == Mirror {
		return out
	}

	for _, lt := range local.Tracks {
		if remoteKeys[lt.Key()] {
			continue // still present upstream; the remote copy already covers it
		}
		lt.SyncState.SpotifyPresent = false
		if lt.SyncState.DateOrphaned == "" {
			lt.SyncState.DateOrphaned = now.UTC().Format(time.RFC3339)
		}
		out.Tracks = append(out.Tracks, lt)
	}

	return out
}
