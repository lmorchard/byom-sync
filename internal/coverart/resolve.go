package coverart

import (
	"context"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/lmorchard/byom-sync/internal/rcache"
)

// EventKind classifies a track's art outcome, for narration.
type EventKind string

const (
	KindFilled EventKind = "filled" // art found and written to the track
	KindMiss   EventKind = "miss"   // no art found
	KindError  EventKind = "error"  // lookup error (track left unchanged)
)

// Event reports one track's outcome.
type Event struct {
	Kind     EventKind
	Artist   string
	Title    string
	ImageURL string
	Source   string
	Err      error
}

// Cache short-circuits art resolution: a positive hit fills without a network
// call; a fresh miss skips re-looking-up. Results are written back.
type Cache interface {
	GetArt(key string) (rcache.ArtEntry, bool)
	PutArt(key string, e rcache.ArtEntry) error
}

// Options configures a Resolve run.
type Options struct {
	Budget   *int
	Pace     time.Duration
	Report   func(Event)
	OnFilled func() error
	Cache    Cache
	Now      func() time.Time
	MissTTL  time.Duration
}

// Resolve fills Track.Image for every track in p that lacks one, mutating tracks
// in place. Per-track lookup errors are reported and skipped; only an OnFilled
// error aborts the run.
func Resolve(ctx context.Context, a Arter, p *playlist.Playlist, opts Options) (filled int, err error) {
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
		if opts.OnFilled != nil {
			return opts.OnFilled()
		}
		return nil
	}
	fresh := func(ts time.Time, ttl time.Duration) bool {
		return ttl > 0 && now().Sub(ts) < ttl
	}
	cachePut := func(key string, e rcache.ArtEntry) {
		if opts.Cache != nil {
			_ = opts.Cache.PutArt(key, e)
		}
	}

	attempted := 0
	for i := range p.Tracks {
		t := &p.Tracks[i]
		if t.Image != "" {
			continue // already has art
		}
		key := t.Key()

		if opts.Cache != nil {
			if e, ok := opts.Cache.GetArt(key); ok {
				switch {
				case e.ImageURL != "":
					t.Image = e.ImageURL
					filled++
					report(Event{Kind: KindFilled, Artist: t.Artist, Title: t.Title, ImageURL: e.ImageURL, Source: "cache"})
					if perr := persist(); perr != nil {
						return filled, perr
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
			return filled, nil
		}
		if attempted > 0 && opts.Pace > 0 {
			if serr := sleep(ctx, opts.Pace); serr != nil {
				return filled, nil
			}
		}
		attempted++
		if opts.Budget != nil {
			*opts.Budget--
		}

		res, rerr := a.Resolve(ctx, *t)
		if rerr != nil {
			report(Event{Kind: KindError, Artist: t.Artist, Title: t.Title, Err: rerr})
			continue
		}
		if res.ImageURL == "" {
			cachePut(key, rcache.ArtEntry{CheckedAt: now()})
			report(Event{Kind: KindMiss, Artist: t.Artist, Title: t.Title})
			continue
		}
		t.Image = res.ImageURL
		filled++
		cachePut(key, rcache.ArtEntry{ImageURL: res.ImageURL, Source: res.Source, CheckedAt: now()})
		report(Event{Kind: KindFilled, Artist: t.Artist, Title: t.Title, ImageURL: res.ImageURL, Source: res.Source})
		if perr := persist(); perr != nil {
			return filled, perr
		}
	}
	return filled, nil
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
