package cmd

import (
	"fmt"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var datesInput string

var datesCmd = &cobra.Command{
	Use:   "dates",
	Short: "Recompute date_created/date_updated from track added_at across the hub",
	Long: `Backfill and refresh playlist date fields in place.

For each hub file: if it predates this feature (date_created holds the original
"first seen" stamp and date_imported is absent), the old date_created is promoted
to date_imported. Then date_created and date_updated are recomputed from the
tracks' added_at (earliest and latest); when no track has an added_at, both fall
back to date_imported. Idempotent — safe to run repeatedly.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDates()
	},
}

func runDates() error {
	input := datesInput
	if input == "" {
		input = viper.GetString("dir")
	}

	paths, err := hubPaths(input)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		log.Warnf("no playlist YAML files found under %s — nothing to do", input)
		return nil
	}

	for _, path := range paths {
		if err := refreshFileDates(path); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	log.Infof("refreshed dates across %d file(s) under %s", len(paths), input)
	return nil
}

// refreshFileDates loads a single hub file, migrates its import date if needed,
// recomputes created/updated from added_at, and writes it back.
func refreshFileDates(path string) error {
	p, err := playlist.LoadFile(path)
	if err != nil {
		return err
	}
	p.EnsureImportedDate()
	p.RefreshDates()
	if err := playlist.SaveFile(path, p); err != nil {
		return err
	}
	log.Infof("%s: imported=%s created=%s updated=%s",
		path,
		p.DateImported.UTC().Format("2006-01-02"),
		p.DateCreated.UTC().Format("2006-01-02"),
		p.DateUpdated.UTC().Format("2006-01-02"))
	return nil
}

func init() {
	rootCmd.AddCommand(datesCmd)
	datesCmd.Flags().StringVar(&datesInput, "input", "", "hub YAML file or directory (default: config dir)")
}
