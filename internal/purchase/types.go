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

// Threshold is the minimum combined Score for an accepted match. Shared with
// the Spotify enricher. On its own it is not a safe gate: a store's search has
// already filtered by artist, so a perfect artist match is nearly free, and a
// wrong album can ride that free half of the score over 0.8 on a merely
// coincidental title overlap. The real iTunes response "Amanda Palmer / Piano
// Is Evil" for a "Theatre Is Evil" query scores 0.808 combined — above
// Threshold — on an artist similarity of 1.0 and an album similarity of only
// 0.615. See SubjectFloor for the second condition that actually rejects it;
// callers should use Accept, not this constant alone.
const Threshold = spotifyenrich.DefaultThreshold

// SubjectFloor is the minimum independent similarity the subject (album or
// track title) must clear, regardless of how well the artist matches. It
// exists because Score blends artist and album into one number, and the
// artist half is nearly free — stores already search by artist, so a wrong
// album by the right artist can still combine with a merely-plausible title
// to clear Threshold. Measured gap: Theatre Is Evil vs. Piano Is Evil scores
// 0.615 on album similarity alone, while Theatre Is Evil vs. itself scores
// 1.000 and Hellbilly Deluxe vs. The Sinister Urge scores 0.250. 0.70 sits in
// that gap rather than close to either edge.
const SubjectFloor = 0.70

// Scoring weights (must sum to 1.0). Artist and album matter equally for a
// purchase lookup, unlike track enrichment where the title dominates — but
// equal weighting alone is not enough to reject a same-artist, wrong-album
// response; that is what SubjectFloor is for.
const (
	artistWeight = 0.5
	albumWeight  = 0.5
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
// half of the gate that stops a real-but-wrong album from becoming a purchase
// link: iTunes answers "amanda palmer theatre is evil" with "Piano Is Evil".
// Score alone is not sufficient to reject it — see SubjectFloor and Accept.
func Score(q Query, artist, album string) float64 {
	return artistWeight*match.Sim(match.Norm(q.Artist), match.Norm(artist)) +
		albumWeight*match.Sim(match.Norm(q.subject()), match.Norm(album))
}

// Accept reports whether a store's result matches the query well enough to
// use as a purchase link, and returns the combined score for the caller to
// rank by. Both conditions must hold: the subject (album or track title)
// must independently clear SubjectFloor, and the combined score must clear
// Threshold. See SubjectFloor for why the combined score alone is not enough.
func Accept(q Query, artist, album string) (score float64, ok bool) {
	score = Score(q, artist, album)
	subjectSim := match.Sim(match.Norm(q.subject()), match.Norm(album))
	return score, subjectSim >= SubjectFloor && score >= Threshold
}

// userAgent identifies byom-sync to the stores. Discogs rejects default agents
// outright; the others are simply better behaved with a real one.
const userAgent = "byom-sync (+https://github.com/lmorchard/byom-sync)"
