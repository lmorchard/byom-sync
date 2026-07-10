package youtube

import (
	"context"
	"errors"
	"strings"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

// Result is a successful resolution: the video id and which resolver produced it.
type Result struct {
	VideoID string
	Source  string
	// Embeddable reports whether the producing resolver confirmed embedded
	// playback. nil = not verified (e.g. the youtube-search fallback).
	Embeddable *bool
}

// Resolver maps a track to a YouTube video id. A zero Result (empty VideoID) with
// a nil error means "no match here" — a Chain then tries the next resolver.
type Resolver interface {
	Name() string
	Resolve(ctx context.Context, t playlist.Track) (Result, error)
}

// isStop reports whether err means a resolver is exhausted for this run: quota
// spent, or sustained rate limiting.
func isStop(err error) bool {
	return errors.Is(err, ErrQuotaExceeded) || errors.Is(err, ErrRateLimited)
}

// Chain tries its resolvers in order per track: the first non-empty result wins.
// When a resolver hits a stop signal (quota/rate-limit) it is disabled for the
// rest of the run and the chain continues with the others — so an exhausted
// fallback (e.g. the quota-limited YouTube search) never halts progress the
// primary resolver could still make. The run only stops once every resolver is
// disabled. Not safe for concurrent use.
type Chain struct {
	resolvers []Resolver
	disabled  []bool
	// OnDisable, if set, is called when a resolver is disabled due to a stop
	// signal (for narration).
	OnDisable func(name string, err error)
}

// NewChain builds a Chain from resolvers in priority order.
func NewChain(resolvers ...Resolver) *Chain {
	return &Chain{resolvers: resolvers, disabled: make([]bool, len(resolvers))}
}

func (c *Chain) Name() string { return "chain" }

func (c *Chain) Resolve(ctx context.Context, t playlist.Track) (Result, error) {
	var lastStop, lastErr error
	for i, r := range c.resolvers {
		if c.disabled[i] {
			continue
		}
		res, err := r.Resolve(ctx, t)
		if err != nil {
			if isStop(err) {
				c.disabled[i] = true
				lastStop = err
				if c.OnDisable != nil {
					c.OnDisable(r.Name(), err)
				}
				continue
			}
			lastErr = err
			continue
		}
		if res.VideoID != "" {
			return res, nil
		}
	}
	// No id this track. Halt the run only if every resolver is now exhausted;
	// otherwise report a miss (or the last transient error) and let it continue.
	if lastStop != nil && c.allDisabled() {
		return Result{}, lastStop
	}
	return Result{}, lastErr
}

func (c *Chain) allDisabled() bool {
	for _, d := range c.disabled {
		if !d {
			return false
		}
	}
	return len(c.disabled) > 0
}

// SearchResolver resolves via a text search of "<artist> <title>" (YouTube Data
// API). It needs an API key and spends quota, so it's best used as a fallback.
type SearchResolver struct {
	Searcher Searcher
}

func (SearchResolver) Name() string { return "youtube-search" }

func (s SearchResolver) Resolve(ctx context.Context, t playlist.Track) (Result, error) {
	id, err := s.Searcher.Search(ctx, strings.TrimSpace(t.Artist+" "+t.Title))
	if err != nil {
		return Result{}, err
	}
	if id == "" {
		return Result{}, nil
	}
	return Result{VideoID: id, Source: "youtube-search"}, nil
}
