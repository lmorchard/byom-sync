package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	importTitle   string
	importCreator string
	importDir     string
	importForce   bool
)

var importCmd = &cobra.Command{
	Use:   "import <file>",
	Short: "Create a native playlist YAML from a plain-text track list",
	Long: `Read a text file of "{artist} - {title}" lines and write a new native
playlist (no spotify_id) into the hub, ready for 'resolve spotify' and then
'resolve youtube'.

Lines beginning with '#' are metadata/comments: "# title: X" and "# creator: X"
set the playlist fields; other '#' lines are ignored. Blank lines are skipped,
and a line without a " - " separator is skipped with a warning.

Title precedence: --title, then a "# title:" header, then the input filename.
The playlist is written to <dir>/<slug>.yaml and will not overwrite an existing
file unless --force is set.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runImport(args[0])
	},
}

func runImport(inputPath string) error {
	f, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", inputPath, err)
	}
	defer func() { _ = f.Close() }()

	p, warnings, err := playlist.ParseText(f)
	if err != nil {
		return fmt.Errorf("parse %s: %w", inputPath, err)
	}
	if len(p.Tracks) == 0 {
		return fmt.Errorf("no tracks parsed from %s", inputPath)
	}

	// Title/creator precedence: flag > header > (filename stem / empty).
	if importTitle != "" {
		p.Title = importTitle
	}
	if p.Title == "" {
		p.Title = filenameStem(inputPath)
	}
	if importCreator != "" {
		p.Creator = importCreator
	}
	p.DateCreated = time.Now().UTC()

	dir := importDir
	if dir == "" {
		dir = viper.GetString("dir")
	}
	if dir == "" {
		return fmt.Errorf("no output directory (set --dir or `dir` in config)")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create dir %s: %w", dir, err)
	}

	path := filepath.Join(dir, playlist.Slug(p.Title)+".yaml")
	if _, statErr := os.Stat(path); statErr == nil {
		if !importForce {
			return fmt.Errorf("%s already exists (use --force to overwrite)", path)
		}
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("stat %s: %w", path, statErr)
	}

	if err := playlist.SaveFile(path, p); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	for _, w := range warnings {
		log.Warnf("skipped unparseable line: %q", w)
	}
	skipped := ""
	if len(warnings) > 0 {
		skipped = fmt.Sprintf(" (%d line(s) skipped)", len(warnings))
	}
	log.Infof("imported %d track(s)%s into %s", len(p.Tracks), skipped, path)
	return nil
}

// filenameStem returns the base filename without its extension.
func filenameStem(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func init() {
	rootCmd.AddCommand(importCmd)
	importCmd.Flags().StringVar(&importTitle, "title", "", "playlist title (overrides a `# title:` header; default: input filename)")
	importCmd.Flags().StringVar(&importCreator, "creator", "", "playlist creator (overrides a `# creator:` header)")
	importCmd.Flags().StringVar(&importDir, "dir", "", "output directory (default: config `dir`)")
	importCmd.Flags().BoolVar(&importForce, "force", false, "overwrite an existing playlist file")
}
