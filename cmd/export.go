package cmd

import (
	"github.com/lmorchard/byom-sync/internal/export"
	"github.com/spf13/cobra"
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Compile playlist YAML into destination formats (m3u8, jspf, hugo)",
	Long: `Compile the local playlist "hub" (YAML) into destination "spoke" formats.

--input may be a single YAML file or a directory of them. When it is a directory,
--out is treated as an output directory and each playlist is written as
"<input-basename>.<ext>".`,
}

var (
	exportInput  string
	exportOut    string
	exportPrefix string
	exportExt    string
	exportTmpl   string
)

var exportM3U8Cmd = &cobra.Command{
	Use:   "m3u8",
	Short: "Export to extended M3U8 for local media servers",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := map[string]string{"lib_prefix": exportPrefix, "ext": exportExt}
		return export.Run(export.M3U8Exporter{}, "m3u8", exportInput, exportOut, cfg)
	},
}

var exportJSPFCmd = &cobra.Command{
	Use:   "jspf",
	Short: "Export to JSPF JSON",
	RunE: func(cmd *cobra.Command, args []string) error {
		return export.Run(export.JSPFExporter{}, "jspf", exportInput, exportOut, nil)
	},
}

var exportHugoCmd = &cobra.Command{
	Use:   "hugo",
	Short: "Export to Hugo Markdown (frontmatter + tracklist table)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := map[string]string{"template": exportTmpl}
		return export.Run(export.HugoExporter{}, "md", exportInput, exportOut, cfg)
	},
}

func init() {
	rootCmd.AddCommand(exportCmd)
	exportCmd.AddCommand(exportM3U8Cmd, exportJSPFCmd, exportHugoCmd)

	for _, c := range []*cobra.Command{exportM3U8Cmd, exportJSPFCmd, exportHugoCmd} {
		c.Flags().StringVar(&exportInput, "input", "", "input playlist YAML file or directory (required)")
		c.Flags().StringVar(&exportOut, "out", "", "output file (file input) or directory (dir input) (required)")
		_ = c.MarkFlagRequired("input")
		_ = c.MarkFlagRequired("out")
	}

	exportM3U8Cmd.Flags().StringVar(&exportPrefix, "lib-prefix", "", "library path prefix, e.g. /mnt/nas/music (required)")
	exportM3U8Cmd.Flags().StringVar(&exportExt, "ext", "flac", "media file extension for constructed paths")
	_ = exportM3U8Cmd.MarkFlagRequired("lib-prefix")

	exportHugoCmd.Flags().StringVar(&exportTmpl, "template", "", "custom Hugo template file (default: embedded)")
}
