package cmd

import (
	"fmt"

	"github.com/lmorchard/byom-sync/internal/site"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	siteInput   string
	siteOut     string
	siteBaseURL string
)

var siteCmd = &cobra.Command{
	Use:   "site",
	Short: "Compile the playlist hub into a static web site",
	Long: `Compile the local playlist "hub" (YAML) into a navigable static site:
one page per playlist embedding <byom-player>, a tree mirroring hub
subdirectories, a shared nav, Open Graph metadata, and an RSS feed.

Configure defaults under a "site:" block in byom-sync.yaml. --base-url (or
site.base_url) is required.`,
	RunE: runSite,
}

func runSite(_ *cobra.Command, _ []string) error {
	hub := siteInput
	if hub == "" {
		hub = viper.GetString("dir")
	}
	out := siteOut
	if out == "" {
		out = viper.GetString("site.out_dir")
	}
	baseURL := siteBaseURL
	if baseURL == "" {
		baseURL = viper.GetString("site.base_url")
	}
	if baseURL == "" {
		return fmt.Errorf("site: base_url is required (set site.base_url or pass --base-url)")
	}

	return site.Build(site.Options{
		HubDir: hub,
		OutDir: out,
		Site: site.SiteMeta{
			Title:                 viper.GetString("site.title"),
			BaseURL:               baseURL,
			PlayerSrc:             viper.GetString("site.player_src"),
			Provider:              viper.GetString("site.provider"),
			Providers:             viper.GetStringSlice("site.providers"),
			YouTubeSearchEndpoint: viper.GetString("site.youtube_search_endpoint"),
			SpotifyClientID:       viper.GetString("site.spotify_client_id"),
		},
	})
}

func init() {
	rootCmd.AddCommand(siteCmd)
	siteCmd.Flags().StringVar(&siteInput, "input", "", "hub directory (default: config `dir`)")
	siteCmd.Flags().StringVar(&siteOut, "out", "", "output directory (default: config `site.out_dir`)")
	siteCmd.Flags().StringVar(&siteBaseURL, "base-url", "", "site base URL (default: config `site.base_url`)")
}
