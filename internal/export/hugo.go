package export

import (
	"bytes"
	"os"
	"text/template"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/lmorchard/byom-sync/internal/templates"
)

// HugoExporter renders a playlist as a Hugo-friendly Markdown page: YAML
// frontmatter plus a tracklist table. The template is the embedded default
// unless cfg["template"] points to a custom template file (see `byom-sync init`).
type HugoExporter struct{}

// hugoView is the data handed to the template.
type hugoView struct {
	Title   string
	Creator string
	Date    string
	Tracks  []playlist.Track
}

func (HugoExporter) Export(p playlist.Playlist, outputPath string, cfg map[string]string) error {
	tmplStr, err := loadHugoTemplate(cfg["template"])
	if err != nil {
		return err
	}
	tmpl, err := template.New("hugo").Parse(tmplStr)
	if err != nil {
		return err
	}

	view := hugoView{Title: p.Title, Creator: p.Creator, Tracks: p.Tracks}
	if !p.DateCreated.IsZero() {
		view.Date = p.DateCreated.UTC().Format("2006-01-02")
	}

	var b bytes.Buffer
	if err := tmpl.Execute(&b, view); err != nil {
		return err
	}
	return os.WriteFile(outputPath, b.Bytes(), 0o644)
}

func loadHugoTemplate(path string) (string, error) {
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	return templates.GetDefaultTemplate()
}
