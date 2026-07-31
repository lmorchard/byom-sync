package purchase

import (
	"context"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/lmorchard/byom-sync/internal/rcache"
)

// EventKind classifies one album's outcome, for narration.
type EventKind string

const (
	KindFilled EventKind = "filled"
	KindMissed EventKind = "missed"
	KindError  EventKind = "error"
)

// Event reports one album's outcome.
type Event struct {
	Kind   EventKind
	Artist string
	Album  string
	URL    string
	Source string
	Err    error
}

// Cache short-circuits resolution: a positive hit fills without a network call,
// a fresh miss skips the lookup. Satisfied by *rcache.DB.
type Cache interface {
	GetPurchase(key string) (rcache.PurchaseEntry, bool)
	PutPurchase(key string, e rcache.PurchaseEntry) error
}

// Stop reasons returned by Resolve, mirroring internal/youtube. "" means the
// pass finished normally (or ran out of budget).
const StopErrors = "errors"

// maxConsecutiveErrors ends a tier once this many lookups in a row have failed.
// A single failure is routine — a 500, a timeout, a malformed response — and
// must not abort a six-hour pass, so the threshold has to be well clear of
// noise. But with the per-source rate floor between attempts (≥1.1s for the
// cheapest tier) ten in a row means the source has been failing continuously
// for at least ten seconds, which in practice means it is refusing us rather
// than flaking. Continuing from there just fires thousands more requests at an
// endpoint that is already saying no — the specific risk with Bandcamp, whose
// search endpoint is undocumented and unsanctioned.
const maxConsecutiveErrors = 10

// Tier carries the state that belongs to a source rather than to one playlist
// file. The caller runs Resolve once per hub file, so anything scoped to a
// single call silently resets at every file boundary: the rate floor would
// leave the first lookup in every file unpaced (most lookups, on a hub where
// earlier tiers already filled most albums), and an error streak would never
// reach its threshold on files holding one or two leftovers each.
//
// Create one Tier per tier and pass it to every Resolve call in that tier. The
// zero value is ready to use. Not safe for concurrent use; tiers run in series.
type Tier struct {
	lastRequest time.Time // when this tier's last network lookup fired
	errors      int       // consecutive lookup failures; any success resets it
}

// Options configures one tier's pass.
type Options struct {
	Budget   *int          // max network lookups this run; nil = unlimited
	Pace     time.Duration // minimum interval between network lookups
	MissTTL  time.Duration // how long a cached miss suppresses a retry
	Report   func(Event)
	OnFilled func() error // persist hook, called after each filled album
	Cache    Cache
	Now      func() time.Time
	// Tier holds pacing and error-streak state across the caller's per-file
	// Resolve calls. Optional: nil gets call-local state, which is only correct
	// when the whole tier is one call.
	Tier *Tier
}

// Resolve fills Track.PurchaseURL for every track in p that lacks one, using a
// single source. Callers run this once per tier, in order.
//
// One lookup fans out to every track sharing an album — the hub averages 1.93
// tracks per album, so this is most of the request saving. A track that already
// has a PurchaseURL is skipped, which is how a later tier knows an earlier one
// already answered.
//
// Per-album lookup errors are reported and skipped; only an OnFilled error
// aborts the run. A sustained run of consecutive lookup errors ends the pass
// early with stopped == StopErrors, so a source that has started refusing us
// doesn't get thousands more requests (see maxConsecutiveErrors).
func Resolve(ctx context.Context, src Source, p *playlist.Playlist, opts Options) (filled int, stopped string, err error) {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	tier := opts.Tier
	if tier == nil {
		tier = &Tier{}
	}
	report := func(e Event) {
		if opts.Report != nil {
			opts.Report(e)
		}
	}
	persist := func() error {
		if opts.OnFilled != nil {
			return opts.OnFilled()
		}
		return nil
	}
	fresh := func(ts time.Time, ttl time.Duration) bool {
		return ttl > 0 && now().Sub(ts) < ttl
	}

	// Group track indices by lookup identity so one answer fills them all.
	order := make([]string, 0, len(p.Tracks))
	groups := make(map[string][]int, len(p.Tracks))
	queries := make(map[string]Query, len(p.Tracks))
	for i := range p.Tracks {
		t := &p.Tracks[i]
		if t.PurchaseURL != "" {
			continue
		}
		q := Query{
			Artist: FirstArtist(t.Artist),
			Album:  CleanAlbum(t.Album),
			Title:  t.Title,
		}
		if q.Artist == "" || q.subject() == "" {
			continue // nothing to search on
		}
		key := q.CacheKey(src.Name())
		if _, seen := groups[key]; !seen {
			order = append(order, key)
			queries[key] = q
		}
		groups[key] = append(groups[key], i)
	}

	apply := func(key, url string) {
		for _, i := range groups[key] {
			p.Tracks[i].PurchaseURL = url
			filled++
		}
	}

	for _, key := range order {
		q := queries[key]

		if opts.Cache != nil {
			if e, ok := opts.Cache.GetPurchase(key); ok {
				switch {
				case e.URL != "":
					apply(key, e.URL)
					report(Event{Kind: KindFilled, Artist: q.Artist, Album: q.subject(), URL: e.URL, Source: "cache"})
					if perr := persist(); perr != nil {
						return filled, "", perr
					}
					continue
				case fresh(e.CheckedAt, opts.MissTTL):
					report(Event{Kind: KindMissed, Artist: q.Artist, Album: q.subject()})
					continue
				}
				// expired miss → fall through to a live lookup
			}
		}

		if opts.Budget != nil && *opts.Budget <= 0 {
			return filled, "", nil
		}
		// Pace against this tier's last request, not this call's — see Tier.
		// A zero lastRequest (nothing sent yet) yields a huge elapsed time, so
		// the very first lookup of a tier is not delayed.
		if opts.Pace > 0 {
			if wait := opts.Pace - now().Sub(tier.lastRequest); wait > 0 {
				if serr := sleep(ctx, wait); serr != nil {
					return filled, "", nil
				}
			}
		}
		tier.lastRequest = now()
		if opts.Budget != nil {
			*opts.Budget--
		}

		res, lerr := src.Lookup(ctx, q)
		if lerr != nil {
			tier.errors++
			report(Event{Kind: KindError, Artist: q.Artist, Album: q.subject(), Err: lerr})
			if tier.errors >= maxConsecutiveErrors {
				return filled, StopErrors, nil
			}
			continue
		}
		tier.errors = 0
		if res.URL == "" {
			if opts.Cache != nil {
				_ = opts.Cache.PutPurchase(key, rcache.PurchaseEntry{Source: src.Name(), CheckedAt: now()})
			}
			report(Event{Kind: KindMissed, Artist: q.Artist, Album: q.subject()})
			continue
		}
		apply(key, res.URL)
		if opts.Cache != nil {
			_ = opts.Cache.PutPurchase(key, rcache.PurchaseEntry{
				URL: res.URL, Source: src.Name(), Score: res.Score, CheckedAt: now(),
			})
		}
		report(Event{Kind: KindFilled, Artist: q.Artist, Album: q.subject(), URL: res.URL, Source: src.Name()})
		if perr := persist(); perr != nil {
			return filled, "", perr
		}
	}
	return filled, "", nil
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
