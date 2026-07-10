package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/lmorchard/byom-sync/internal/rcache"
	"github.com/lmorchard/byom-sync/internal/youtube"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	resolveInput     string
	resolveLimit     int
	resolveDelay     time.Duration
	resolveFlush     int
	resolveReresolve bool
	resolveNoCache   bool
)

// defaultCachePath mirrors the auth config-dir logic: $XDG_CONFIG_HOME/byom-sync
// (or ~/.config/byom-sync), file cache.db.
func defaultCachePath() string {
	var base string
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		base = filepath.Join(v, "byom-sync")
	} else if home, err := os.UserHomeDir(); err == nil {
		base = filepath.Join(home, ".config", "byom-sync")
	} else {
		base = "byom-sync"
	}
	return filepath.Join(base, "cache.db")
}

// openCache opens the resolution cache unless --no-cache is set (then nil, nil).
func openCache() (*rcache.DB, error) {
	if resolveNoCache {
		return nil, nil
	}
	path := viper.GetString("cache_path")
	if path == "" {
		path = defaultCachePath()
	}
	return rcache.Open(path)
}

var resolveCmd = &cobra.Command{
	Use:   "resolve",
	Short: "Resolve external IDs for hub tracks (e.g. YouTube video IDs)",
}

var resolveYouTubeCmd = &cobra.Command{
	Use:   "youtube",
	Short: "Resolve a YouTube video id for tracks missing one and store it in the hub",
	Long: `Resolve a YouTube video ID for each hub track that has no youtube_id yet and
write it back into the YAML. Only missing tracks are attempted, so runs are
incremental.

Resolvers, tried in order per track:
  1. yt-dlp — YouTube's own search via the yt-dlp binary. Searches the top few
     results and picks the first that allows embedded playback. Free, no quota,
     no key. Requires yt-dlp on PATH (or set ytdlp_path). Primary.
  2. youtube-search — the YouTube Data API text search, used only as a fallback
     and only when youtube_api_key is set. It spends the ~100 searches/day quota
     and mostly duplicates yt-dlp, so it's rarely needed.

--limit caps tracks attempted per run; --delay paces requests under rate limits.
--reresolve re-checks tracks that already have an id and replaces any that are no
longer embeddable. Resolution stops early (persisting progress) on quota
exhaustion or sustained rate limiting.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runResolveYouTube(context.Background())
	},
}

func runResolveYouTube(ctx context.Context) error {
	input := resolveInput
	if input == "" {
		input = viper.GetString("dir")
	}

	paths, err := hubPaths(input)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		log.Warnf("no playlist YAML files found under %s — nothing to resolve", input)
		return nil
	}

	// yt-dlp (free, no quota, no key) is the primary resolver; the YouTube Data
	// API search is an optional fallback, added only when a key is configured
	// (it spends the ~100/day search quota, and mostly duplicates yt-dlp).
	ytdlpBin := viper.GetString("ytdlp_path")
	if ytdlpBin == "" {
		ytdlpBin = "yt-dlp"
	}
	if _, err := exec.LookPath(ytdlpBin); err != nil {
		return fmt.Errorf("%q not found in PATH — install yt-dlp (https://github.com/yt-dlp/yt-dlp) or set ytdlp_path", ytdlpBin)
	}
	ytdlp := youtube.YtdlpResolver{Bin: ytdlpBin}
	resolvers := []youtube.Resolver{ytdlp}
	names := "yt-dlp"
	if apiKey := viper.GetString("youtube_api_key"); apiKey != "" {
		resolvers = append(resolvers, youtube.SearchResolver{Searcher: youtube.HTTPSearcher{APIKey: apiKey}})
		names = "yt-dlp, youtube-search"
	}
	chain := youtube.NewChain(resolvers...)
	chain.OnDisable = func(name string, err error) {
		log.Warnf("resolver %q exhausted (%v) — continuing without it", name, err)
	}

	cache, err := openCache()
	if err != nil {
		return fmt.Errorf("open cache: %w", err)
	}
	if cache != nil {
		defer func() { _ = cache.Close() }()
	}
	missTTL := viper.GetDuration("cache_miss_ttl")
	embedTTL := viper.GetDuration("cache_embed_ttl")

	var budget *int
	if resolveLimit > 0 {
		budget = &resolveLimit
		log.Infof("resolving YouTube ids across %d file(s) under %s [%s] (limit %d, delay %s)", len(paths), input, names, resolveLimit, resolveDelay)
	} else {
		log.Infof("resolving YouTube ids across %d file(s) under %s [%s] (delay %s)", len(paths), input, names, resolveDelay)
	}

	total := 0
	stopped := "done"
	for _, path := range paths {
		p, err := playlist.LoadFile(path)
		if err != nil {
			return fmt.Errorf("load %s: %w", path, err)
		}
		missing := countMissingYouTube(p)
		base := filepath.Base(path)
		if missing == 0 && !resolveReresolve {
			log.Infof("%s: all %d tracks already resolved, skipping", base, len(p.Tracks))
			continue
		}
		if resolveReresolve {
			log.Infof("%s: re-resolving (%d of %d missing; existing ids re-checked)", base, missing, len(p.Tracks))
		} else {
			log.Infof("%s: %d of %d tracks need a YouTube id", base, missing, len(p.Tracks))
		}

		// Per-track narration + per-file tallies. Errors/removals go to WARN so
		// they surface without --verbose; the rest are DEBUG (--verbose).
		var got, kept, replaced, removed int
		report := func(e youtube.Event) {
			switch e.Kind {
			case youtube.KindResolved:
				got++
				log.Debugf("  resolved: %s - %s -> %s (via %s)", e.Artist, e.Title, e.VideoID, e.Source)
			case youtube.KindReplaced:
				replaced++
				log.Debugf("  replaced: %s - %s -> %s (was non-embeddable)", e.Artist, e.Title, e.VideoID)
			case youtube.KindKept:
				kept++
				log.Debugf("  kept: %s - %s (still embeddable)", e.Artist, e.Title)
			case youtube.KindRemoved:
				removed++
				log.Warnf("  removed: %s - %s (non-embeddable, no alternative found)", e.Artist, e.Title)
			case youtube.KindMiss:
				log.Debugf("  no match: %s - %s", e.Artist, e.Title)
			case youtube.KindError:
				log.Warnf("  error: %s - %s: %v", e.Artist, e.Title, e.Err)
			}
		}

		// Persist incrementally so a long run is granularly resumable, but batch
		// writes (every resolveFlush resolutions) so we don't rewrite a large
		// playlist file on every single track.
		sinceSave := 0
		savedTotal := 0
		save := func() error { return playlist.SaveFile(path, p) }
		onResolved := func() error {
			sinceSave++
			savedTotal++
			if sinceSave >= resolveFlush {
				sinceSave = 0
				if err := save(); err != nil {
					return err
				}
				log.Infof("  %s: checkpoint — %d ids saved to disk", base, savedTotal)
			}
			return nil
		}

		opts := youtube.ResolveOptions{
			Budget:     budget,
			Pace:       resolveDelay,
			Report:     report,
			OnResolved: onResolved,
			Reresolve:  resolveReresolve,
			Verify:     ytdlp.IsEmbeddable,
			MissTTL:    missTTL,
			EmbedTTL:   embedTTL,
		}
		// Assign only when non-nil: a typed-nil *rcache.DB in the interface field
		// would read as non-nil and Resolve would call methods on a nil DB.
		if cache != nil {
			opts.Cache = cache
		}
		n, stop, err := youtube.Resolve(ctx, chain, &p, opts)
		// Flush any resolutions since the last batched save (also covers an early
		// stop). Do this before surfacing a resolve error so partial progress sticks.
		if sinceSave > 0 {
			if serr := save(); serr != nil {
				return fmt.Errorf("save %s: %w", path, serr)
			}
		}
		if err != nil {
			return fmt.Errorf("resolve %s: %w", path, err)
		}
		if resolveReresolve {
			log.Infof("%s: re-checked — %d kept, %d replaced, %d removed, %d newly resolved", base, kept, replaced, removed, got)
		} else if got > 0 {
			log.Infof("%s: resolved %d id(s), saved", base, got)
		} else {
			log.Infof("%s: nothing resolved", base)
		}
		total += n
		if stop == youtube.StopQuota {
			log.Warnf("YouTube daily quota exceeded — stopping (progress saved). Re-run tomorrow to continue.")
			stopped = "quota"
			break
		}
		if stop == youtube.StopRateLimit {
			log.Warnf("YouTube rate limit hit repeatedly — stopping (progress saved). Retry later or raise --delay.")
			stopped = "ratelimit"
			break
		}
		if budget != nil && *budget <= 0 {
			stopped = "limit"
			break
		}
	}
	log.Warnf("YouTube resolve done: %d ids resolved; stopped: %s", total, stopped)
	return nil
}

// countMissingYouTube counts tracks in p that still lack a YouTube id.
func countMissingYouTube(p playlist.Playlist) int {
	n := 0
	for _, t := range p.Tracks {
		if t.YouTubeID == "" {
			n++
		}
	}
	return n
}

// hubPaths returns the YAML files to process: a single file, or every *.yaml in
// a directory.
func hubPaths(input string) ([]string, error) {
	info, err := os.Stat(input)
	if err != nil {
		return nil, fmt.Errorf("input %s: %w", input, err)
	}
	if !info.IsDir() {
		return []string{input}, nil
	}
	matches, err := filepath.Glob(filepath.Join(input, "*.yaml"))
	if err != nil {
		return nil, err
	}
	return matches, nil
}

func init() {
	rootCmd.AddCommand(resolveCmd)
	resolveCmd.AddCommand(resolveYouTubeCmd)
	resolveYouTubeCmd.Flags().StringVar(&resolveInput, "input", "", "hub YAML file or directory (default: config dir)")
	resolveYouTubeCmd.Flags().IntVar(&resolveLimit, "limit", 0, "max searches this run (0 = unlimited; quota is the backstop)")
	resolveYouTubeCmd.Flags().DurationVar(&resolveDelay, "delay", 500*time.Millisecond, "pause between searches to stay under the API rate limit")
	resolveYouTubeCmd.Flags().IntVar(&resolveFlush, "flush", 20, "write resolved ids to disk every N resolutions (granular resume)")
	resolveYouTubeCmd.Flags().BoolVar(&resolveReresolve, "reresolve", false, "re-check tracks that already have a youtube_id and replace ones no longer embeddable")
	resolveYouTubeCmd.Flags().BoolVar(&resolveNoCache, "no-cache", false, "bypass the resolution cache (pure network resolution)")
}
