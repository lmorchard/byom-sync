// Package spotifyfetch pulls playlists from the Spotify API and converts them
// into byom-sync's local playlist representation.
package spotifyfetch

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/zmb3/spotify/v2"
)

// ParseID extracts a Spotify playlist ID from a raw ID, a spotify:playlist:<id>
// URI, or an open.spotify.com/playlist/<id> URL (query strings ignored).
func ParseID(raw string) (spotify.ID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty playlist reference")
	}

	if rest, ok := strings.CutPrefix(raw, "spotify:playlist:"); ok {
		return spotify.ID(rest), nil
	}

	if strings.Contains(raw, "open.spotify.com") {
		u, err := url.Parse(raw)
		if err != nil {
			return "", fmt.Errorf("parse url %q: %w", raw, err)
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		for i, p := range parts {
			if p == "playlist" && i+1 < len(parts) {
				return spotify.ID(parts[i+1]), nil
			}
		}
		return "", fmt.Errorf("no playlist id in URL %q", raw)
	}

	return spotify.ID(raw), nil
}

// Fetch retrieves a playlist and all of its tracks (following pagination),
// converting each into a playlist.Track. Episodes and market-unavailable items
// are skipped. DateCreated is left zero — the caller sets it (preserving the
// local value on re-sync, stamping now for a new playlist).
func Fetch(ctx context.Context, c *spotify.Client, id spotify.ID) (playlist.Playlist, error) {
	fp, err := c.GetPlaylist(ctx, id)
	if err != nil {
		return playlist.Playlist{}, fmt.Errorf("get playlist %s: %w", id, err)
	}

	out := playlist.Playlist{
		SpotifyID:   string(id),
		Title:       fp.Name,
		Creator:     fp.Owner.DisplayName,
		Description: fp.Description,
	}

	page, err := c.GetPlaylistItems(ctx, id)
	for err == nil {
		for i := range page.Items {
			ft := page.Items[i].Track.Track
			if ft == nil {
				continue // episode or unavailable in this market
			}
			if isCatalogStub(ft) {
				continue // catalog-removed placeholder with no usable metadata
			}
			out.Tracks = append(out.Tracks, convert(page.Items[i]))
		}
		err = c.NextPage(ctx, page)
	}
	if err != nil && !errors.Is(err, spotify.ErrNoMorePages) {
		return out, fmt.Errorf("paginate playlist %s: %w", id, err)
	}
	return out, nil
}

// ListMyPlaylists returns the IDs of the current user's playlists (following
// pagination). When includeFollowed is false, only playlists owned by the
// current user are returned, excluding followed/algorithmic playlists.
func ListMyPlaylists(ctx context.Context, c *spotify.Client, includeFollowed bool) ([]string, error) {
	me, err := c.CurrentUser(ctx)
	if err != nil {
		return nil, fmt.Errorf("get current user: %w", err)
	}

	var all []spotify.SimplePlaylist
	page, err := c.CurrentUsersPlaylists(ctx)
	for err == nil {
		all = append(all, page.Playlists...)
		err = c.NextPage(ctx, page)
	}
	if err != nil && !errors.Is(err, spotify.ErrNoMorePages) {
		return nil, fmt.Errorf("list playlists: %w", err)
	}

	return selectOwnedIDs(all, me.ID, includeFollowed), nil
}

// selectOwnedIDs returns the IDs of playlists owned by userID (or all of them
// when includeFollowed is true).
func selectOwnedIDs(playlists []spotify.SimplePlaylist, userID string, includeFollowed bool) []string {
	ids := make([]string, 0, len(playlists))
	for _, pl := range playlists {
		if includeFollowed || pl.Owner.ID == userID {
			ids = append(ids, string(pl.ID))
		}
	}
	return ids
}

// TrackGetter fetches full tracks by id (satisfied by *spotify.Client). Abstracted
// for testability.
type TrackGetter interface {
	GetTracks(ctx context.Context, ids []spotify.ID, opts ...spotify.RequestOption) ([]*spotify.FullTrack, error)
}

// artBatchSize is the Spotify GetTracks per-call id limit.
const artBatchSize = 50

// FetchTrackArt returns a map of spotify track id -> best album-image URL for the
// given ids, fetched in batches of 50. Ids that don't resolve, or resolve without
// album art, are simply absent from the map.
func FetchTrackArt(ctx context.Context, g TrackGetter, ids []string, maxWidth int) (map[string]string, error) {
	out := make(map[string]string, len(ids))
	for start := 0; start < len(ids); start += artBatchSize {
		end := start + artBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		chunk := make([]spotify.ID, 0, end-start)
		for _, id := range ids[start:end] {
			chunk = append(chunk, spotify.ID(id))
		}
		tracks, err := g.GetTracks(ctx, chunk)
		if err != nil {
			return nil, fmt.Errorf("get tracks: %w", err)
		}
		for _, ft := range tracks {
			if ft == nil {
				continue
			}
			if url := PickImage(ft.Album.Images, maxWidth); url != "" {
				out[string(ft.ID)] = url
			}
		}
	}
	return out, nil
}

// isCatalogStub reports whether a track is a metadata-less placeholder — Spotify
// sometimes returns a non-nil track for a playlist slot whose underlying item has
// been removed from the catalog, with an empty name and no artists. These carry
// no useful curation data, so they're skipped rather than stored as noise.
func isCatalogStub(ft *spotify.FullTrack) bool {
	return ft.Name == "" && len(ft.Artists) == 0
}

func convert(item spotify.PlaylistItem) playlist.Track {
	ft := item.Track.Track
	return playlist.Track{
		Title:  ft.Name,
		Artist: joinArtists(ft.Artists),
		Album:  ft.Album.Name,
		// FullTrack.ExternalIDs is a map[string]string in zmb3/spotify/v2 v2.4.3
		// (it shadows the embedded SimpleTrack's typed field).
		ISRC:       ft.ExternalIDs["isrc"],
		SpotifyID:  string(ft.ID),
		SpotifyURL: ft.ExternalURLs["spotify"],
		DurationMS: int(ft.Duration),
		Image:      PickImage(ft.Album.Images, DefaultImageMaxWidth),
		AddedAt:    item.AddedAt,
		SyncState:  playlist.SyncState{SpotifyPresent: true},
	}
}

func joinArtists(artists []spotify.SimpleArtist) string {
	names := make([]string, 0, len(artists))
	for _, a := range artists {
		names = append(names, a.Name)
	}
	return strings.Join(names, ", ")
}

// DefaultImageMaxWidth is the preferred upper bound for album art width.
const DefaultImageMaxWidth = 640

// PickImage returns the URL of the largest album image no wider than maxWidth;
// if none qualify it returns the smallest available; "" when there are none.
func PickImage(images []spotify.Image, maxWidth int) string {
	best := ""
	bestW := -1
	fallback := ""
	fallbackW := 1 << 30
	for _, img := range images {
		w := int(img.Width)
		if w <= maxWidth {
			if w > bestW {
				bestW = w
				best = img.URL
			}
		} else if w < fallbackW {
			fallbackW = w
			fallback = img.URL
		}
	}
	if best != "" {
		return best
	}
	return fallback
}
