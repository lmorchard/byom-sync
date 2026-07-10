package spotifyenrich

import (
	"context"
	"sort"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/lmorchard/byom-sync/internal/rcache"
)

// EventKind classifies what happened to a track, for narration.
type EventKind string

const (
	KindEnriched  EventKind = "enriched"  // confident match filled the track
	KindPicked    EventKind = "picked"    // pick-by-editing applied a chosen id
	KindAmbiguous EventKind = "ambiguous" // no confident match; candidates recorded
	KindMiss      EventKind = "miss"      // no search results at all
	KindError     EventKind = "error"     // lookup error (track left unchanged)
	KindSkipped   EventKind = "skipped"   // spotify:false — opted out of enrichment
)

// Event reports the outcome of one track so the caller can narrate progress.
type Event struct {
	Kind      EventKind
	Artist    string
	Title     string
	SpotifyID string  // the resulting id (enriched/picked)
	Score     float64 // best candidate score (enriched/ambiguous)
	Err       error
}

// Cache short-circuits enrichment: a positive hit fills the track without a
// network call; a fresh miss skips re-searching. Results are written back.
type Cache interface {
	GetEnrich(key string) (rcache.EnrichEntry, bool)
	PutEnrich(key string, e rcache.EnrichEntry) error
}

// Options configures an Enrich run.
type Options struct {
	Budget        *int          // if non-nil, caps tracks attempted this call
	Pace          time.Duration // if > 0, waits between network attempts
	Report        func(Event)   // if set, called once per track attempted
	OnEnriched    func() error  // if set, called after each fill (incremental persist)
	Canonicalize  bool          // overwrite authored title/artist/album with Spotify's
	Threshold     float64       // auto-accept score (0 → DefaultThreshold)
	MaxCandidates int           // ambiguous candidates recorded (0 → 5)
	Cache         Cache
	Now           func() time.Time // clock for TTL/timestamps (default time.Now)
	MissTTL       time.Duration    // negative-result freshness window
}

// Enrich fills Spotify metadata for every track in p that lacks it, mutating
// tracks in place. Confident matches fill technical fields; ambiguous tracks get
// an enrich_candidates list; misses are left untouched. Per-track lookup errors
// are reported and skipped. Only a returned error (from OnEnriched) aborts.
func Enrich(ctx context.Context, s Searcher, p *playlist.Playlist, opts Options) (enriched int, err error) {
	threshold := opts.Threshold
	if threshold == 0 {
		threshold = DefaultThreshold
	}
	maxCand := opts.MaxCandidates
	if maxCand == 0 {
		maxCand = 5
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	report := func(e Event) {
		if opts.Report != nil {
			opts.Report(e)
		}
	}
	persist := func() error {
		if opts.OnEnriched != nil {
			return opts.OnEnriched()
		}
		return nil
	}
	fresh := func(ts time.Time, ttl time.Duration) bool {
		return ttl > 0 && now().Sub(ts) < ttl
	}
	cachePut := func(key string, e rcache.EnrichEntry) {
		if opts.Cache != nil {
			_ = opts.Cache.PutEnrich(key, e)
		}
	}

	attempted := 0
	for i := range p.Tracks {
		t := &p.Tracks[i]

		// spotify:false opts the track out of enrichment entirely. Clear any stale
		// candidates so marking + re-running tidies up the junk left from before.
		if t.Spotify != nil && !*t.Spotify {
			t.EnrichCandidates = nil
			report(Event{Kind: KindSkipped, Artist: t.Artist, Title: t.Title})
			continue
		}

		picked := t.SpotifyID != "" && len(t.EnrichCandidates) > 0
		if t.SpotifyID != "" && !picked {
			continue // already enriched
		}
		key := t.Key()

		// Cache short-circuit (search path only; a pick always re-fetches by id).
		if !picked && opts.Cache != nil {
			if e, ok := opts.Cache.GetEnrich(key); ok {
				switch {
				case e.SpotifyID != "":
					applyCandidate(t, entryToCandidate(e), opts.Canonicalize)
					t.EnrichCandidates = nil
					enriched++
					report(Event{Kind: KindEnriched, Artist: t.Artist, Title: t.Title, SpotifyID: e.SpotifyID})
					if perr := persist(); perr != nil {
						return enriched, perr
					}
					continue
				case fresh(e.CheckedAt, opts.MissTTL):
					report(Event{Kind: KindMiss, Artist: t.Artist, Title: t.Title})
					continue
				}
				// expired miss → fall through to a live lookup
			}
		}

		if opts.Budget != nil && *opts.Budget <= 0 {
			return enriched, nil
		}
		if attempted > 0 && opts.Pace > 0 {
			if serr := sleep(ctx, opts.Pace); serr != nil {
				return enriched, nil
			}
		}
		attempted++
		if opts.Budget != nil {
			*opts.Budget--
		}

		if picked {
			c, gerr := s.GetByID(ctx, t.SpotifyID)
			if gerr != nil {
				report(Event{Kind: KindError, Artist: t.Artist, Title: t.Title, Err: gerr})
				continue
			}
			applyCandidate(t, c, opts.Canonicalize)
			t.EnrichCandidates = nil
			enriched++
			cachePut(key, candidateToEntry(c, now()))
			report(Event{Kind: KindPicked, Artist: t.Artist, Title: t.Title, SpotifyID: t.SpotifyID})
			if perr := persist(); perr != nil {
				return enriched, perr
			}
			continue
		}

		cands, serr := s.Search(ctx, *t)
		if serr != nil {
			report(Event{Kind: KindError, Artist: t.Artist, Title: t.Title, Err: serr})
			continue
		}
		if len(cands) == 0 {
			cachePut(key, rcache.EnrichEntry{CheckedAt: now()})
			report(Event{Kind: KindMiss, Artist: t.Artist, Title: t.Title})
			continue
		}

		// Score and rank.
		scored := make([]scoredCandidate, len(cands))
		for j, c := range cands {
			scored[j] = scoredCandidate{c: c, score: Score(*t, c)}
		}
		sort.SliceStable(scored, func(a, b int) bool { return scored[a].score > scored[b].score })
		best := scored[0]

		if best.score >= threshold {
			applyCandidate(t, best.c, opts.Canonicalize)
			t.EnrichCandidates = nil
			enriched++
			cachePut(key, candidateToEntry(best.c, now()))
			report(Event{Kind: KindEnriched, Artist: t.Artist, Title: t.Title, SpotifyID: best.c.SpotifyID, Score: best.score})
			if perr := persist(); perr != nil {
				return enriched, perr
			}
			continue
		}

		// Ambiguous: record the top candidates for the user to pick from.
		t.EnrichCandidates = topCandidates(scored, maxCand)
		report(Event{Kind: KindAmbiguous, Artist: t.Artist, Title: t.Title, Score: best.score})
	}
	return enriched, nil
}

type scoredCandidate struct {
	c     Candidate
	score float64
}

func topCandidates(scored []scoredCandidate, n int) []playlist.EnrichCandidate {
	if len(scored) < n {
		n = len(scored)
	}
	out := make([]playlist.EnrichCandidate, 0, n)
	for _, sc := range scored[:n] {
		out = append(out, playlist.EnrichCandidate{
			SpotifyID:  sc.c.SpotifyID,
			Title:      sc.c.Title,
			Artist:     sc.c.Artist,
			Album:      sc.c.Album,
			ISRC:       sc.c.ISRC,
			DurationMS: sc.c.DurationMS,
			Score:      round2(sc.score),
		})
	}
	return out
}

func round2(f float64) float64 { return float64(int(f*100+0.5)) / 100 }

// applyCandidate fills only-empty technical fields from c (never overwriting a
// value already set — so a user-picked spotify_id survives). Authored
// title/artist/album text is overwritten only when canonicalize is true.
func applyCandidate(t *playlist.Track, c Candidate, canonicalize bool) {
	if t.ISRC == "" {
		t.ISRC = c.ISRC
	}
	if t.SpotifyID == "" {
		t.SpotifyID = c.SpotifyID
	}
	if t.SpotifyURL == "" {
		t.SpotifyURL = c.SpotifyURL
	}
	if t.DurationMS == 0 {
		t.DurationMS = c.DurationMS
	}
	if t.Album == "" {
		t.Album = c.Album
	}
	if t.Image == "" {
		t.Image = c.Image
	}
	if canonicalize {
		if c.Title != "" {
			t.Title = c.Title
		}
		if c.Artist != "" {
			t.Artist = c.Artist
		}
		if c.Album != "" {
			t.Album = c.Album
		}
	}
}

// sleep waits d or until ctx is cancelled.
func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
