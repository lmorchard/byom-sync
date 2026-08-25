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
// Metadata (Title, Creator, DateImported, SpotifyID) comes from remote;
// DateCreated/DateUpdated are recomputed post-merge via RefreshDates.
//
// Locally-derived fields survive the merge under both strategies, because Spotify
// has no equivalent to send back and a fetched copy would otherwise blank them:
// `featured` and the playlist hero art at the playlist level, and each surviving
// track's `youtube_id`, `image_file`, `purchase_url`, `spotify` opt-out, and
// `enrich_candidates`. See adoptLocalFields.
//
// Two playlist-level fields are shared rather than purely local — `image` and
// `description`. Spotify owns both when it sends them, and the local value only
// fills the gap when it doesn't. Anything Spotify can leave empty needs that
// treatment, or a sync silently replaces authored content with nothing.
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
	// Playlist-level presentation is authored locally (or filled by `resolve art
	// --download`) and never comes back from Spotify.
	out.Featured = local.Featured
	out.ImageFile = local.ImageFile
	if out.Image == "" {
		out.Image = local.Image
	}
	// Description follows the same rule as Image: Spotify owns it when it sends
	// one, but most of these playlists have no blurb upstream, so a fetched copy
	// carries "" and would overwrite hand-authored prose with nothing. That is
	// not hypothetical — it wiped 30 descriptions on the live hub, they were
	// restored by hand, and the next nightly run wiped them again.
	if out.Description == "" {
		out.Description = local.Description
	}
	out.Tracks = make([]Track, 0, len(remote.Tracks))

	localByKey := make(map[string]Track, len(local.Tracks))
	for _, lt := range local.Tracks {
		localByKey[lt.Key()] = lt
	}

	remoteKeys := make(map[string]bool, len(remote.Tracks))
	for _, rt := range remote.Tracks {
		if lt, ok := localByKey[rt.Key()]; ok {
			rt = adoptLocalFields(rt, lt)
		}
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

// adoptLocalFields carries a local track's locally-derived fields onto its
// freshly-fetched remote counterpart. Spotify owns the descriptive metadata
// (title, artist, album, ISRC, ids, duration, added_at); everything copied here
// is produced by the `resolve` commands or authored by hand, has no remote
// equivalent, and would be silently destroyed on every sync if the remote track
// simply replaced the local one.
//
// Image is the exception: Spotify's album art wins when it sent any, and the
// local URL is only a fallback for tracks Spotify had no art for (filled by
// `resolve art` from MusicBrainz/Cover Art Archive). ImageFile is kept either
// way — it may point at art downloaded for a superseded URL, which is still
// better than dropping the reference; the next `resolve art --download` refreshes
// it.
func adoptLocalFields(remote, local Track) Track {
	remote.YouTubeID = local.YouTubeID
	remote.ImageFile = local.ImageFile
	remote.PurchaseURL = local.PurchaseURL
	remote.Spotify = local.Spotify
	remote.EnrichCandidates = local.EnrichCandidates
	if remote.Image == "" {
		remote.Image = local.Image
	}
	return remote
}
