package coverart

import (
	"context"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

// Result is a resolved cover-art URL and which path produced it. An empty
// ImageURL means no art was found.
type Result struct {
	ImageURL string
	Source   string
}

// Arter resolves cover art for a track. Abstracted for testability.
type Arter interface {
	Resolve(ctx context.Context, t playlist.Track) (Result, error)
}

// Resolver finds cover art via MusicBrainz + the Cover Art Archive.
type Resolver struct {
	MB  *MBClient
	CAA *CAAClient
}

// Resolve tries the album path first (release-group art when the track has an
// album), then falls back to the recording path (art from the recording's first
// release). Returns an empty Result (nil error) when no art is found.
func (r Resolver) Resolve(ctx context.Context, t playlist.Track) (Result, error) {
	if t.Album != "" {
		mbid, err := r.MB.SearchReleaseGroup(ctx, t.Artist, t.Album)
		if err != nil {
			return Result{}, err
		}
		if mbid != "" {
			url, err := r.CAA.FrontImage(ctx, "release-group", mbid)
			if err != nil {
				return Result{}, err
			}
			if url != "" {
				return Result{ImageURL: url, Source: "musicbrainz-release-group"}, nil
			}
			// A specific release-group was matched but the archive has no art
			// for it. Don't fall back to the weaker recording-based match --
			// that could return art for a different release than the one just
			// confirmed. Treat this as a terminal miss.
			return Result{}, nil
		}
	}

	relMBID, err := r.MB.SearchRecordingRelease(ctx, t.Artist, t.Title)
	if err != nil {
		return Result{}, err
	}
	if relMBID != "" {
		url, err := r.CAA.FrontImage(ctx, "release", relMBID)
		if err != nil {
			return Result{}, err
		}
		if url != "" {
			return Result{ImageURL: url, Source: "musicbrainz-recording"}, nil
		}
	}
	return Result{}, nil
}
