package export

import (
	"bytes"
	"os"
	"text/template"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/lmorchard/byom-sync/internal/templates"
)

// MarkdownExporter renders a playlist as a Markdown page with YAML frontmatter
// plus a tracklist table (compatible with Hugo and other frontmatter-based site
// generators). The template is the embedded default unless cfg["template"]
// points to a custom template file (see `byom-sync init`).
type MarkdownExporter struct{}

// markdownView is the data handed to the template.
type markdownView struct {
	Title   string
	Creator string
	Date    string
	Tracks  []playlist.Track
}

func (MarkdownExporter) Export(p playlist.Playlist, outputPath string, cfg map[string]string) error {
	tmplStr, err := loadMarkdownTemplate(cfg["template"])
	if err != nil {
		return err
	}
	tmpl, err := template.New("markdown").Parse(tmplStr)
	if err != nil {
		return err
	}

	view := markdownView{Title: p.Title, Creator: p.Creator, Tracks: p.Tracks}
	if !p.DateCreated.IsZero() {
		view.Date = p.DateCreated.UTC().Format("2006-01-02")
	}

	var b bytes.Buffer
	if err := tmpl.Execute(&b, view); err != nil {
		return err
	}
	return os.WriteFile(outputPath, b.Bytes(), 0o644)
}

func loadMarkdownTemplate(path string) (string, error) {
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	return templates.GetDefaultTemplate()
}
