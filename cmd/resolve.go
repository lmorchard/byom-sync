package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/lmorchard/byom-sync/internal/youtube"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	resolveInput string
	resolveLimit int
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

	searcher := youtube.HTTPSearcher{APIKey: apiKey}
	var budget *int
	if resolveLimit > 0 {
		budget = &resolveLimit
	}

	total := 0
	stopped := "done"
	for _, path := range paths {
		p, err := playlist.LoadFile(path)
		if err != nil {
			return fmt.Errorf("load %s: %w", path, err)
		}
		n, quota, err := youtube.Resolve(ctx, searcher, &p, budget)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", path, err)
		}
		if n > 0 {
			if err := playlist.SaveFile(path, p); err != nil {
				return fmt.Errorf("save %s: %w", path, err)
			}
		}
		total += n
		log.Infof("resolved %d in %s", n, filepath.Base(path))
		if quota {
			stopped = "quota"
			break
		}
		if budget != nil && *budget <= 0 {
			stopped = "limit"
			break
		}
	}
	log.Warnf("youtube resolve: %d ids resolved; stopped: %s", total, stopped)
	return nil
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
}
