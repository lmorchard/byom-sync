package site

import (
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// PageLink is a content page's entry in the site header nav.
type PageLink struct {
	Title string
	Href  string
}

// ContentPage is a render-ready standalone markdown page.
type ContentPage struct {
	Slug  string
	Title string
	Order int
	Desc  string
	Body  template.HTML
}

// parseFrontmatter strips a leading "---\n…\n---\n" YAML block, returning its
// title/order and the remaining body. Absent or malformed frontmatter yields
// ("", 0, raw).
func parseFrontmatter(raw string) (title string, order int, body string) {
	if !strings.HasPrefix(raw, "---\n") {
		return "", 0, raw
	}
	rest := raw[4:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return "", 0, raw
	}
	var meta struct {
		Title string `yaml:"title"`
		Order int    `yaml:"order"`
	}
	_ = yaml.Unmarshal([]byte(rest[:idx]), &meta) // best-effort
	body = strings.TrimPrefix(rest[idx+4:], "\n")
	return meta.Title, meta.Order, body
}

// LoadPages reads every *.md in dir into a sorted slice of ContentPages. A
// missing directory yields (nil, nil) — content pages are opt-in.
func LoadPages(dir string) ([]ContentPage, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return nil, err
	}
	pages := make([]ContentPage, 0, len(matches))
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		title, order, body := parseFrontmatter(string(data))
		slug := strings.TrimSuffix(filepath.Base(path), ".md")
		if title == "" {
			title = slug
		}
		pages = append(pages, ContentPage{
			Slug:  slug,
			Title: title,
			Order: order,
			Desc:  firstParagraph(body),
			Body:  renderMarkdown(body),
		})
	}
	sort.SliceStable(pages, func(i, j int) bool {
		if pages[i].Order != pages[j].Order {
			return pages[i].Order < pages[j].Order
		}
		return pages[i].Title < pages[j].Title
	})
	return pages, nil
}

// pageLinks projects loaded pages into header nav links, preserving order.
func pageLinks(pages []ContentPage) []PageLink {
	links := make([]PageLink, 0, len(pages))
	for _, p := range pages {
		links = append(links, PageLink{Title: p.Title, Href: "/" + p.Slug + "/"})
	}
	return links
}
