// Package purchase resolves a best-effort "where to buy this" URL for hub
// albums and tracks. Sources are tried tier-at-a-time by the caller, each as a
// full pass with its own rate limit; every fuzzy result is admitted only by a
// confidence gate, because stores will happily return a real but wrong album.
package purchase

import (
	"context"
	"strings"

	"github.com/lmorchard/byom-sync/internal/match"
	"github.com/lmorchard/byom-sync/internal/spotifyenrich"
)

// Kind distinguishes an album purchase from a single-track one. Bandcamp and
// iTunes both sell individual tracks, which is often what a shopping list wants.
type Kind string

const (
	KindAlbum Kind = "album"
	KindTrack Kind = "track"
)

// Threshold is the minimum Score for an accepted match. Shared with the Spotify
// enricher; empirically supported here — in a 31-album measured run accepted
// matches scored 1.00 and most rejections scored 0.62 or below. The closest
// measured rejection, "Piano Is Evil" for a "Theatre Is Evil" query, scores
// 0.79 (see the weights above) — still correctly below the 0.8 gate, but by a
// narrower margin than the rest of the corpus.
const Threshold = spotifyenrich.DefaultThreshold

// Scoring weights (must sum to 1.0). The album is the actual thing being
// purchased, so it dominates the way title dominates in track enrichment;
// artist is a secondary check, not an equal partner. Equal weighting lets a
// same-artist, wrong-album response slip through: the real iTunes response
// "Amanda Palmer / Piano Is Evil" for a "Theatre Is Evil" query scores 0.81
// at 0.5/0.5 — above threshold — because the shared artist alone contributes
// 0.5. At 0.45/0.55 it scores 0.79, correctly rejected.
const (
	artistWeight = 0.45
	albumWeight  = 0.55
)

// Query is one purchase lookup. Album-scoped when Album is set, else track-scoped
// by Title. Callers pass already-normalized values (see FirstArtist/CleanAlbum).
type Query struct {
	Artist string
	Album  string
	Title  string
}

// Kind reports whether this is an album or a single-track lookup.
func (q Query) Kind() Kind {
	if q.Album != "" {
		return KindAlbum
	}
	return KindTrack
}

// subject returns whichever of Album/Title this query is about.
func (q Query) subject() string {
	if q.Album != "" {
		return q.Album
	}
	return q.Title
}

// Text is the free-text search string handed to a store's search endpoint.
func (q Query) Text() string {
	return strings.TrimSpace(q.Artist + " " + q.subject())
}

// CacheKey is the purchase_cache identity for this query under one source.
// Source-scoped so each tier owns its key space: a tier-1 miss must not stop
// tier 2 from trying. Scope-tagged so an album and a same-named track don't
// collide. Normalized so cosmetic differences share a row.
func (q Query) CacheKey(source string) string {
	return source + "\t" + string(q.Kind()) + "\t" +
		match.Norm(q.Artist) + "\t" + match.Norm(q.subject())
}

// Result is one store's answer. URL is empty for a clean miss.
type Result struct {
	URL   string
	Kind  Kind
	Score float64
}

// Source is one store. Implementations own their own HTTP calls and response
// shape; the caller owns pacing, caching, and tier order.
type Source interface {
	// Name is the stable identifier used in cache keys and CLI flags.
	Name() string
	// Lookup returns a Result whose URL is empty when the store has nothing
	// matching. A transport or decode failure is an error; "no match" is not.
	Lookup(ctx context.Context, q Query) (Result, error)
}

// Score rates a store's returned artist+album against the query, 0..1. This is
// the gate that stops a real-but-wrong album from becoming a purchase link:
// iTunes answers "amanda palmer theatre is evil" with "Piano Is Evil".
func Score(q Query, artist, album string) float64 {
	return artistWeight*match.Sim(match.Norm(q.Artist), match.Norm(artist)) +
		albumWeight*match.Sim(match.Norm(q.subject()), match.Norm(album))
}
