package cmd

import (
	"context"
	"fmt"

	"github.com/lmorchard/byom-sync/internal/auth"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authenticate with Spotify (PKCE OAuth) and cache a token",
	Long: `Run the Spotify authorization-code + PKCE flow.

Opens your browser to Spotify's consent page, captures the redirect on a local
callback server, and caches the resulting token so later commands can refresh
it silently.

Requires client_id in the config and a matching redirect URI registered on your
Spotify application (default: http://127.0.0.1:8888/callback).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		clientID := viper.GetString("client_id")
		port := viper.GetInt("redirect_port")

		fmt.Printf("Using redirect URI: %s\n", auth.RedirectURL(port))
		if err := auth.RunInteractiveFlow(context.Background(), clientID, port); err != nil {
			return err
		}
		fmt.Println("✅ Authentication successful. Token cached.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(authCmd)
}
