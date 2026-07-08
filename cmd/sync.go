package cmd

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lmorchard/byom-sync/internal/auth"
	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/lmorchard/byom-sync/internal/spotifyfetch"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/sync/errgroup"
)

var (
	syncDir      string
	syncStrategy string
)

var syncCmd = &cobra.Command{
	Use:   "sync [playlist-url-or-id ...]",
	Short: "Sync Spotify playlists into local YAML files",
	Long: `Fetch playlists from Spotify and store them as per-playlist YAML files.

With no arguments, syncs the playlists listed under ` + "`playlists`" + ` in the config.
With one or more arguments (IDs, spotify:playlist: URIs, or open.spotify.com URLs),
syncs exactly those, ignoring the config list.

Strategies:
  archive (default)  append-only; tracks removed upstream are kept and marked
                     orphaned (spotify_present=false + date_orphaned)
  mirror             overwrite local to match remote exactly`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSync(context.Background(), args)
	},
}

// selectTargets returns the playlists to sync: positional args take precedence
// over the configured list, replacing it entirely when present.
func selectTargets(args, configPlaylists []string) []string {
	if len(args) > 0 {
		return args
	}
	return configPlaylists
}

func runSync(ctx context.Context, args []string) error {
	clientID := viper.GetString("client_id")
	port := viper.GetInt("redirect_port")

	dir := syncDir
	if dir == "" {
		dir = viper.GetString("dir")
	}

	strat := playlist.Strategy(syncStrategy)
	if strat != playlist.Archive && strat != playlist.Mirror {
		return fmt.Errorf("invalid strategy %q (want archive or mirror)", syncStrategy)
	}

	targets := selectTargets(args, viper.GetStringSlice("playlists"))
	if len(targets) == 0 {
		return fmt.Errorf("no playlists to sync (pass IDs/URLs as arguments or set `playlists` in config)")
	}

	client, tok, err := auth.Client(ctx, clientID, port)
	if err != nil {
		return err
	}
	defer auth.PersistRefreshed(client, tok)

	log := GetLogger()
	now := time.Now()

	var mu sync.Mutex // serializes per-file read/merge/write (files share a dir)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(4)

	for _, target := range targets {
		g.Go(func() error {
			id, err := spotifyfetch.ParseID(target)
			if err != nil {
				return err
			}
			remote, err := spotifyfetch.Fetch(gctx, client, id)
			if err != nil {
				return err
			}

			mu.Lock()
			defer mu.Unlock()

			var local playlist.Playlist
			path, ok, err := playlist.FindFileByID(dir, remote.SpotifyID)
			if err != nil {
				return err
			}
			if ok {
				local, err = playlist.LoadFile(path)
				if err != nil {
					return err
				}
				remote.DateCreated = local.DateCreated // preserve original creation date
			} else {
				remote.DateCreated = now.UTC()
			}

			merged := playlist.Merge(local, remote, strat, now)
			savedPath, err := playlist.Save(dir, merged)
			if err != nil {
				return err
			}
			log.Infof("synced %q (%d tracks) -> %s", merged.Title, len(merged.Tracks), savedPath)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}
	fmt.Printf("✅ Synced %d playlist(s) into %s\n", len(targets), dir)
	return nil
}

func init() {
	rootCmd.AddCommand(syncCmd)
	syncCmd.Flags().StringVar(&syncDir, "dir", "", "directory for playlist YAML files (default: config `dir`, else ./playlists)")
	syncCmd.Flags().StringVar(&syncStrategy, "strategy", "archive", "sync strategy: archive or mirror")
}
