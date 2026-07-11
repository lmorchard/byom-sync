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
	syncDir         string
	syncStrategy    string
	syncAll         bool
	syncIncludeFoll bool
)

var syncCmd = &cobra.Command{
	Use:   "sync [playlist-url-or-id ...]",
	Short: "Sync Spotify playlists into local YAML files",
	Long: `Fetch playlists from Spotify and store them as per-playlist YAML files.

With no arguments, syncs the playlists listed under ` + "`playlists`" + ` in the config.
With one or more arguments (IDs, spotify:playlist: URIs, or open.spotify.com URLs),
syncs exactly those, ignoring the config list.
With --all, syncs every playlist you own (add --include-followed for followed and
algorithmic playlists too); --all cannot be combined with arguments.

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

	if syncAll && len(args) > 0 {
		return fmt.Errorf("cannot combine --all with explicit playlist arguments")
	}

	log := GetLogger()

	var targets []string
	if !syncAll {
		targets = selectTargets(args, viper.GetStringSlice("playlists"))
		if len(targets) == 0 {
			return fmt.Errorf("no playlists to sync (pass IDs/URLs as arguments, set `playlists` in config, or use --all)")
		}
	}

	client, tok, err := auth.Client(ctx, clientID, port)
	if err != nil {
		return err
	}
	defer auth.PersistRefreshed(client, tok)

	if syncAll {
		targets, err = spotifyfetch.ListMyPlaylists(ctx, client, syncIncludeFoll)
		if err != nil {
			return err
		}
		if len(targets) == 0 {
			log.Warn("no playlists found for the current user")
			return nil
		}
		scope := "owned"
		if syncIncludeFoll {
			scope = "owned + followed"
		}
		log.Infof("--all: syncing %d playlists (%s)", len(targets), scope)
	}
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
			}
			remote.DateImported = importedDate(local, ok, now)

			merged := playlist.Merge(local, remote, strat, now)
			merged.RefreshDates()
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

// importedDate returns the "first seen" stamp to carry onto a synced playlist:
// now for a brand-new playlist, otherwise the local file's DateImported —
// migrating a pre-change file whose original stamp lived in DateCreated.
func importedDate(local playlist.Playlist, existed bool, now time.Time) time.Time {
	if !existed {
		return now.UTC()
	}
	local.EnsureImportedDate()
	return local.DateImported
}

func init() {
	rootCmd.AddCommand(syncCmd)
	syncCmd.Flags().StringVar(&syncDir, "dir", "", "directory for playlist YAML files (default: config `dir`, else ./playlists)")
	syncCmd.Flags().StringVar(&syncStrategy, "strategy", "archive", "sync strategy: archive or mirror")
	syncCmd.Flags().BoolVar(&syncAll, "all", false, "sync all playlists you own (ignores args/config; excludes followed)")
	syncCmd.Flags().BoolVar(&syncIncludeFoll, "include-followed", false, "with --all, also sync followed/algorithmic playlists")
}
