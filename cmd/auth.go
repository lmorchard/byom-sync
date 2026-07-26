package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/lmorchard/byom-sync/internal/auth"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var authManual bool

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authenticate with Spotify (PKCE OAuth) and cache a token",
	Long: `Run the Spotify authorization-code + PKCE flow.

Opens your browser to Spotify's consent page, captures the redirect on a local
callback server, and caches the resulting token so later commands can refresh
it silently.

On a headless or remote host, pass --manual: byom-sync prints the consent URL
and asks you to paste the redirect back, with no local callback server. This is
needed over SSH because Spotify pins the redirect URI to 127.0.0.1, so the
browser would otherwise deliver the code to the wrong machine. Alternatively,
forward the port before connecting: ssh -L 8888:127.0.0.1:8888 <host>.

Requires client_id in the config and a matching redirect URI registered on your
Spotify application (default: http://127.0.0.1:8888/callback).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		clientID := viper.GetString("client_id")
		port := viper.GetInt("redirect_port")

		fmt.Printf("Using redirect URI: %s\n", auth.RedirectURL(port))

		var err error
		if authManual {
			err = auth.RunManualFlow(context.Background(), clientID, port, os.Stdin, os.Stdout)
		} else {
			err = auth.RunInteractiveFlow(context.Background(), clientID, port)
		}
		if err != nil {
			return err
		}
		fmt.Println("✅ Authentication successful. Token cached.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(authCmd)
	authCmd.Flags().BoolVar(&authManual, "manual", false, "headless flow: print the consent URL and paste the redirect back (no local callback server)")
}
