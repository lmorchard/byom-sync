package spotifyenrich

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/lmorchard/byom-sync/internal/rcache"
	"github.com/zmb3/spotify/v2"
)

// defaultImageMaxWidth is the preferred upper bound for album art width.
const defaultImageMaxWidth = 640

// Searcher looks up Spotify candidates for a track and fetches a specific track
// by id (for the pick-by-editing flow). Abstracted for testability.
type Searcher interface {
	Search(ctx context.Context, t playlist.Track) ([]Candidate, error)
	GetByID(ctx context.Context, id string) (Candidate, error)
}

// ClientSearcher is the live Spotify-backed Searcher. Max caps how many search
// results are considered (0 → a sensible default of 5).
type ClientSearcher struct {
	Client *spotify.Client
	Max    int
}

func (s ClientSearcher) limit() int {
	if s.Max > 0 {
		return s.Max
	}
	return 5
}

func (s ClientSearcher) Search(ctx context.Context, t playlist.Track) ([]Candidate, error) {
	res, err := s.Client.Search(ctx, buildQuery(t), spotify.SearchTypeTrack, spotify.Limit(s.limit()))
	if err != nil {
		return nil, err
	}
	cands := fromResult(res)
	if len(cands) == 0 {
		// Fall back to an unfielded query — some tracks don't match the strict
		// field filters but do match a plain "artist title" search.
		res, err = s.Client.Search(ctx, strings.TrimSpace(t.Artist+" "+t.Title), spotify.SearchTypeTrack, spotify.Limit(s.limit()))
		if err != nil {
			return nil, err
		}
		cands = fromResult(res)
	}
	return cands, nil
}

func (s ClientSearcher) GetByID(ctx context.Context, id string) (Candidate, error) {
	ft, err := s.Client.GetTrack(ctx, spotify.ID(id))
	if err != nil {
		return Candidate{}, err
	}
	return toCandidate(*ft), nil
}

func fromResult(res *spotify.SearchResult) []Candidate {
	if res == nil || res.Tracks == nil {
		return nil
	}
	out := make([]Candidate, 0, len(res.Tracks.Tracks))
	for _, ft := range res.Tracks.Tracks {
		out = append(out, toCandidate(ft))
	}
	return out
}

// buildQuery constructs a fielded Spotify search query from a track.
func buildQuery(t playlist.Track) string {
	q := fmt.Sprintf(`track:%q artist:%q`, t.Title, t.Artist)
	if t.Album != "" {
		q += fmt.Sprintf(` album:%q`, t.Album)
	}
	return q
}

// toCandidate maps a Spotify FullTrack to a Candidate, mirroring
// spotifyfetch.convert()'s field handling for the v2.4.3 API shape.
func toCandidate(ft spotify.FullTrack) Candidate {
	names := make([]string, 0, len(ft.Artists))
	for _, a := range ft.Artists {
		names = append(names, a.Name)
	}
	return Candidate{
		SpotifyID:  string(ft.ID),
		ISRC:       ft.ExternalIDs["isrc"],
		Title:      ft.Name,
		Artist:     strings.Join(names, ", "),
		Album:      ft.Album.Name,
		SpotifyURL: ft.ExternalURLs["spotify"],
		Image:      pickImage(ft.Album.Images, defaultImageMaxWidth),
		DurationMS: int(ft.Duration),
	}
}

// pickImage returns the URL of the largest image no wider than maxWidth; if none
// qualify it returns the smallest available; empty when there are no images.
func pickImage(images []spotify.Image, maxWidth int) string {
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

// candidateToEntry converts a Candidate to a positive enrichment-cache entry.
func candidateToEntry(c Candidate, now time.Time) rcache.EnrichEntry {
	return rcache.EnrichEntry{
		SpotifyID: c.SpotifyID, ISRC: c.ISRC, SpotifyURL: c.SpotifyURL,
		Album: c.Album, Title: c.Title, Artist: c.Artist, Image: c.Image,
		DurationMS: c.DurationMS, CheckedAt: now,
	}
}

// entryToCandidate converts a cached enrichment entry back to a Candidate.
func entryToCandidate(e rcache.EnrichEntry) Candidate {
	return Candidate{
		SpotifyID: e.SpotifyID, ISRC: e.ISRC, Title: e.Title, Artist: e.Artist,
		Album: e.Album, SpotifyURL: e.SpotifyURL, Image: e.Image, DurationMS: e.DurationMS,
	}
}
