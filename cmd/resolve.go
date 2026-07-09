package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/lmorchard/byom-sync/internal/youtube"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	resolveInput string
	resolveLimit int
	resolveDelay time.Duration
	resolveFlush int
)

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
  1. yt-dlp — YouTube's own search via the yt-dlp binary ("ytsearch1:..."). Free,
     no quota, no key. Requires yt-dlp on PATH (or set ytdlp_path). Primary.
  2. youtube-search — the YouTube Data API text search, used only as a fallback
     and only when youtube_api_key is set. It spends the ~100 searches/day quota
     and mostly duplicates yt-dlp, so it's rarely needed.

--limit caps tracks attempted per run; --delay paces requests under rate limits.
Resolution stops early (persisting progress) on quota exhaustion or sustained
rate limiting.`,
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
	resolvers := []youtube.Resolver{youtube.YtdlpResolver{Bin: ytdlpBin}}
	names := "yt-dlp"
	if apiKey := viper.GetString("youtube_api_key"); apiKey != "" {
		resolvers = append(resolvers, youtube.SearchResolver{Searcher: youtube.HTTPSearcher{APIKey: apiKey}})
		names = "yt-dlp, youtube-search"
	}
	chain := youtube.NewChain(resolvers...)
	chain.OnDisable = func(name string, err error) {
		log.Warnf("resolver %q exhausted (%v) — continuing without it", name, err)
	}

	var budget *int
	if resolveLimit > 0 {
		budget = &resolveLimit
		log.Infof("resolving YouTube ids across %d file(s) under %s [%s] (limit %d, delay %s)", len(paths), input, names, resolveLimit, resolveDelay)
	} else {
		log.Infof("resolving YouTube ids across %d file(s) under %s [%s] (delay %s)", len(paths), input, names, resolveDelay)
	}

	// report narrates each track's outcome. Errors go to WARN so they surface
	// even without --verbose (e.g. a bad key failing every search); hits/misses
	// are INFO (quiet by default, visible with --verbose).
	report := func(e youtube.Event) {
		switch {
		case e.Err != nil:
			log.Warnf("  error: %s - %s: %v", e.Artist, e.Title, e.Err)
		case e.VideoID != "":
			log.Infof("  resolved: %s - %s -> %s (via %s)", e.Artist, e.Title, e.VideoID, e.Source)
		default:
			log.Infof("  no match: %s - %s", e.Artist, e.Title)
		}
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
		if missing == 0 {
			log.Infof("%s: all %d tracks already resolved, skipping", base, len(p.Tracks))
			continue
		}
		log.Infof("%s: %d of %d tracks need a YouTube id", base, missing, len(p.Tracks))

		// Persist incrementally so a long run is granularly resumable, but batch
		// writes (every resolveFlush resolutions) so we don't rewrite a large
		// playlist file on every single track.
		sinceSave := 0
		save := func() error { return playlist.SaveFile(path, p) }
		onResolved := func() error {
			sinceSave++
			if sinceSave >= resolveFlush {
				sinceSave = 0
				return save()
			}
			return nil
		}

		n, stop, err := youtube.Resolve(ctx, chain, &p, budget, resolveDelay, report, onResolved)
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
		if n > 0 {
			log.Infof("%s: resolved %d id(s), saved", base, n)
		} else {
			log.Infof("%s: resolved 0 (nothing to save)", base)
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
}
