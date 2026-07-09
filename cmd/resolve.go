package cmd

import (
	"context"
	"fmt"
	"os"
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
)

var resolveCmd = &cobra.Command{
	Use:   "resolve",
	Short: "Resolve external IDs for hub tracks (e.g. YouTube video IDs)",
}

var resolveYouTubeCmd = &cobra.Command{
	Use:   "youtube",
	Short: "Search YouTube for tracks missing a youtube_id and store it in the hub",
	Long: `Search the YouTube Data API for each hub track that has no youtube_id yet
and write the resolved video ID back into the YAML. Only missing tracks are
searched, so runs are incremental; --limit caps searches per run, and resolution
stops early (persisting progress) if the daily API quota is exhausted.

Requires youtube_api_key in config (or the YOUTUBE_API_KEY environment variable).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runResolveYouTube(context.Background())
	},
}

func runResolveYouTube(ctx context.Context) error {
	apiKey := viper.GetString("youtube_api_key")
	if apiKey == "" {
		return fmt.Errorf("youtube_api_key is not set (config or YOUTUBE_API_KEY env)")
	}

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

	searcher := youtube.HTTPSearcher{APIKey: apiKey}
	var budget *int
	if resolveLimit > 0 {
		budget = &resolveLimit
		log.Infof("resolving YouTube ids across %d file(s) under %s (limit %d searches)", len(paths), input, resolveLimit)
	} else {
		log.Infof("resolving YouTube ids across %d file(s) under %s", len(paths), input)
	}

	// report narrates each search outcome. Errors go to WARN so they surface
	// even without --verbose (e.g. a bad key failing every search); hits/misses
	// are INFO (quiet by default, visible with --verbose).
	report := func(e youtube.Event) {
		switch {
		case e.Err != nil:
			log.Warnf("  search error: %s - %s: %v", e.Artist, e.Title, e.Err)
		case e.VideoID != "":
			log.Infof("  resolved: %s - %s -> %s", e.Artist, e.Title, e.VideoID)
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

		n, stop, err := youtube.Resolve(ctx, searcher, &p, budget, resolveDelay, report)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", path, err)
		}
		if n > 0 {
			if err := playlist.SaveFile(path, p); err != nil {
				return fmt.Errorf("save %s: %w", path, err)
			}
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
}
