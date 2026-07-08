package cmd

import (
	"fmt"
	"os"

	"github.com/lmorchard/byom-sync/internal/templates"
	"github.com/spf13/cobra"
)

const defaultConfigContent = `# Configuration file for byom-sync
# Copy this to byom-sync.yaml and customize as needed.

# Spotify application client ID (from https://developer.spotify.com/dashboard).
# Register a redirect URI of http://127.0.0.1:8888/callback for the auth flow.
client_id: "your-spotify-client-id"

# Local port for the OAuth callback server (must match the registered redirect URI).
redirect_port: 8888

# Directory where per-playlist YAML files are stored / synced.
dir: "./playlists"

# Playlists to sync when ` + "`byom-sync sync`" + ` is run with no positional arguments.
# Accepts raw IDs, spotify:playlist:<id> URIs, or open.spotify.com URLs.
playlists:
  - "https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M"

# Logging configuration
verbose: false
debug: false
log_json: false
`

// initCmd represents the init command
var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize configuration and template files",
	Long: `Create default configuration file and custom template file for customization.

This command generates:
  - byom-sync.yaml (configuration file)
  - byom-sync.md (customizable template, or use --template-file to specify)

Use --force to overwrite existing files.

Example:
  byom-sync init
  byom-sync init --template-file my-template.md
  byom-sync init --force`,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := GetLogger()
		force, _ := cmd.Flags().GetBool("force")
		templateFile, _ := cmd.Flags().GetString("template-file")

		configFile := "byom-sync.yaml"

		// Check if config file exists
		configExists := fileExists(configFile)
		if configExists && !force {
			return fmt.Errorf("config file %s already exists (use --force to overwrite)", configFile)
		}

		// Check if template file exists
		templateExists := fileExists(templateFile)
		if templateExists && !force {
			return fmt.Errorf("template file %s already exists (use --force to overwrite)", templateFile)
		}

		// Create config file
		if err := os.WriteFile(configFile, []byte(defaultConfigContent), 0o644); err != nil {
			return fmt.Errorf("failed to create config file: %w", err)
		}

		if configExists {
			log.Infof("Overwrote %s", configFile)
		} else {
			log.Infof("Created %s", configFile)
		}

		// Get default template content
		templateContent, err := templates.GetDefaultTemplate()
		if err != nil {
			return fmt.Errorf("failed to get default template: %w", err)
		}

		// Create template file
		if err := os.WriteFile(templateFile, []byte(templateContent), 0o644); err != nil {
			return fmt.Errorf("failed to create template file: %w", err)
		}

		if templateExists {
			log.Infof("Overwrote %s", templateFile)
		} else {
			log.Infof("Created %s", templateFile)
		}

		fmt.Printf("\n✅ Initialization complete!\n\n")
		fmt.Printf("Next steps:\n")
		fmt.Printf("  1. Edit %s and add your configuration\n", configFile)
		fmt.Printf("  2. (Optional) Customize %s for your preferred output format\n", templateFile)
		fmt.Printf("  3. Run: byom-sync <command> --help for usage information\n\n")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().Bool("force", false, "Overwrite existing files")
	initCmd.Flags().String("template-file", "byom-sync.md", "Name of custom template file to create")
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
