package site

import (
	"bytes"
	"html/template"
	"os"
	"path/filepath"
	"strings"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/yuin/goldmark"
)

// SiteMeta carries site-wide settings baked into every page.
type SiteMeta struct {
	Title                 string
	BaseURL               string
	PlayerSrc             string
	Provider              string
	Providers             []string
	YouTubeSearchEndpoint string
	SpotifyClientID       string
	Pages                 []PageLink
}

// Crumb is one breadcrumb link (Href empty → plain text, i.e. current page).
type Crumb struct {
	Label string
	Href  string
}

// pageData is the base data shared by every page template.
type pageData struct {
	Site      SiteMeta
	Title     string
	Desc      string
	Image     string
	Canonical string
	Crumbs    []Crumb
}

type landingData struct {
	pageData
	Intro template.HTML
	Root  *Node
}

type folderData struct {
	pageData
	Intro template.HTML
	Node  *Node
}

type playlistData struct {
	pageData
	Playlist *playlist.Playlist
	JSPFHref string
}

type contentPageData struct {
	pageData
	Body template.HTML
}

// Renderer holds the parsed template set and site settings.
type Renderer struct {
	Site SiteMeta
	tmpl *template.Template
}

// NewRenderer parses the embedded templates.
func NewRenderer(site SiteMeta) (*Renderer, error) {
	funcs := template.FuncMap{
		"markdown":      renderMarkdown,
		"providersCSV":  func(p []string) string { return strings.Join(p, ",") },
		"playlistMeta":  playlistMeta,
		"playlistCover": coverHref,
		"plainText":     plainText,
		"dirsOf":        dirsOf,
		"yearGroupsOf":  yearGroupsOf,
	}
	tmpl, err := template.New("site").Funcs(funcs).ParseFS(embedded, "templates/*.html")
	if err != nil {
		return nil, err
	}
	return &Renderer{Site: site, tmpl: tmpl}, nil
}

func renderMarkdown(md string) template.HTML {
	var buf bytes.Buffer
	if err := goldmark.Convert([]byte(md), &buf); err != nil {
		return ""
	}
	return template.HTML(buf.String()) // #nosec G203 — source is our own hub files
}

// RenderSite renders the landing page plus every folder and playlist page.
func (r *Renderer) RenderSite(outDir string, root *Node) error {
	if err := r.renderLanding(outDir, root); err != nil {
		return err
	}
	return r.renderChildren(outDir, root, nil)
}

// RenderPages writes one HTML page per content page at <outDir>/pages/<slug>/index.html.
func (r *Renderer) RenderPages(outDir string, pages []ContentPage) error {
	for _, p := range pages {
		dir := filepath.Join(outDir, "pages", p.Slug)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		data := contentPageData{
			pageData: pageData{
				Site:      r.Site,
				Title:     p.Title,
				Desc:      p.Desc,
				Canonical: canonical(r.Site.BaseURL, "pages/"+p.Slug),
			},
			Body: p.Body,
		}
		if err := r.write(filepath.Join(dir, "index.html"), "page.html", data); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) renderLanding(outDir string, root *Node) error {
	data := landingData{
		pageData: pageData{
			Site:      r.Site,
			Title:     r.Site.Title,
			Desc:      firstParagraph(root.IntroMD),
			Canonical: canonical(r.Site.BaseURL, ""),
		},
		Intro: renderMarkdown(root.IntroMD),
		Root:  root,
	}
	return r.write(filepath.Join(outDir, "index.html"), "landing.html", data)
}

func (r *Renderer) renderChildren(outDir string, node *Node, crumbs []Crumb) error {
	for _, c := range node.Children {
		trail := append(append([]Crumb{}, crumbs...), Crumb{Label: c.Title, Href: "/" + c.Path + "/"})
		if c.IsDir {
			if err := r.renderFolder(outDir, c, withCurrentLast(trail)); err != nil {
				return err
			}
			if err := r.renderChildren(outDir, c, trail); err != nil {
				return err
			}
			continue
		}
		if err := r.renderPlaylist(outDir, c, withCurrentLast(trail)); err != nil {
			return err
		}
	}
	return nil
}

// withCurrentLast strips the href from the final crumb (the current page). The
// site-root home crumb is intentionally omitted — the site header already links
// home — so the breadcrumb carries only intermediate folder context.
func withCurrentLast(crumbs []Crumb) []Crumb {
	out := append([]Crumb{}, crumbs...)
	out[len(out)-1].Href = ""
	return out
}

func (r *Renderer) renderFolder(outDir string, node *Node, crumbs []Crumb) error {
	dir := pageDir(outDir, node)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data := folderData{
		pageData: pageData{
			Site:      r.Site,
			Title:     node.Title,
			Desc:      firstParagraph(node.IntroMD),
			Canonical: canonical(r.Site.BaseURL, node.Path),
			Crumbs:    crumbs,
		},
		Intro: renderMarkdown(node.IntroMD),
		Node:  node,
	}
	return r.write(filepath.Join(dir, "index.html"), "folder.html", data)
}

func (r *Renderer) renderPlaylist(outDir string, node *Node, crumbs []Crumb) error {
	dir := pageDir(outDir, node)
	if err := os.MkdirAll(filepath.Join(dir, "embed"), 0o755); err != nil {
		return err
	}
	base := pageData{
		Site:      r.Site,
		Title:     node.Title,
		Desc:      plainText(node.Playlist.Description),
		Image:     playlistImage(node.Playlist, r.Site.BaseURL),
		Canonical: canonical(r.Site.BaseURL, node.Path),
		Crumbs:    crumbs,
	}
	data := playlistData{pageData: base, Playlist: node.Playlist, JSPFHref: "/" + node.Path + "/playlist.jspf.json"}
	if err := r.write(filepath.Join(dir, "index.html"), "playlist.html", data); err != nil {
		return err
	}
	return r.write(filepath.Join(dir, "embed", "index.html"), "embed.html", data)
}

func (r *Renderer) write(path, tmplName string, data any) error {
	var buf bytes.Buffer
	if err := r.tmpl.ExecuteTemplate(&buf, tmplName, data); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
