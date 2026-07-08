package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/lmorchard/byom-sync/internal/config"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
	log     = logrus.New()
	cfg     *config.Config
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "byom-sync",
	Short: "A brief description of your application",
	Long: `A longer description of what your application does and how it works.

This can be multiple lines and should provide helpful context about the
purpose and usage of your CLI tool.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		initConfig()
		setupLogging()
	},
}

// Execute adds all child commands to the root command and sets appropriate flags.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	// Configuration file flag
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is ./byom-sync.yaml)")

	// Logging flags
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().Bool("debug", false, "debug output")
	rootCmd.PersistentFlags().Bool("log-json", false, "output logs in JSON format")

	// Bind flags to viper
	_ = viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))
	_ = viper.BindPFlag("debug", rootCmd.PersistentFlags().Lookup("debug"))
	_ = viper.BindPFlag("log_json", rootCmd.PersistentFlags().Lookup("log-json"))
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag
		viper.SetConfigFile(cfgFile)
	} else {
		// Search for config in the current directory and XDG config dir
		viper.AddConfigPath(".")
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			viper.AddConfigPath(filepath.Join(xdg, "byom-sync"))
		} else if home, err := os.UserHomeDir(); err == nil {
			viper.AddConfigPath(filepath.Join(home, ".config", "byom-sync"))
		}
		viper.SetConfigType("yaml")
		viper.SetConfigName("byom-sync")
	}

	// Set defaults
	viper.SetDefault("verbose", false)
	viper.SetDefault("debug", false)
	viper.SetDefault("log_json", false)
	viper.SetDefault("client_id", "")
	viper.SetDefault("redirect_port", 8888)
	viper.SetDefault("dir", "./playlists")

	// Read in environment variables that match
	viper.AutomaticEnv()

	// If a config file is found, read it in
	if err := viper.ReadInConfig(); err != nil {
		if cfgFile != "" {
			// Only error if config was explicitly specified
			fmt.Fprintf(os.Stderr, "Error reading config file: %v\n", err)
			os.Exit(1)
		}
	}
}

// setupLogging configures the logger based on configuration
func setupLogging() {
	if viper.GetBool("log_json") {
		log.SetFormatter(&logrus.JSONFormatter{})
	} else {
		log.SetFormatter(&logrus.TextFormatter{
			FullTimestamp: true,
		})
	}

	if viper.GetBool("debug") {
		log.SetLevel(logrus.DebugLevel)
	} else if viper.GetBool("verbose") {
		log.SetLevel(logrus.InfoLevel)
	} else {
		log.SetLevel(logrus.WarnLevel)
	}
}

// GetConfig returns the application configuration, loading it if necessary
func GetConfig() *config.Config {
	if cfg == nil {
		cfg = &config.Config{
			Verbose: viper.GetBool("verbose"),
			Debug:   viper.GetBool("debug"),
			LogJSON: viper.GetBool("log_json"),
		}
	}
	return cfg
}

// GetLogger returns the configured logger
func GetLogger() *logrus.Logger {
	return log
}
