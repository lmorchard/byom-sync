package site

import (
	"fmt"
	"os"
)

// Options configures a site build.
type Options struct {
	HubDir   string
	OutDir   string
	PagesDir string
	Site     SiteMeta
}

// checkSlugCollisions returns an error if any content-page slug matches a
// top-level playlist/folder slug — both render to <out>/<slug>/index.html, so a
// collision would silently overwrite the playlist/folder page.
func checkSlugCollisions(root *Node, pages []ContentPage) error {
	top := make(map[string]bool, len(root.Children))
	for _, c := range root.Children {
		top[c.Name] = true
	}
	for _, p := range pages {
		if top[p.Slug] {
			return fmt.Errorf("content page %q collides with a top-level playlist/folder of the same slug; rename one", p.Slug)
		}
	}
	return nil
}

// Build compiles the hub at opts.HubDir into a static site at opts.OutDir.
func Build(opts Options) error {
	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return err
	}
	root, err := BuildTree(opts.HubDir)
	if err != nil {
		return err
	}
	pages, err := LoadPages(opts.PagesDir)
	if err != nil {
		return err
	}
	opts.Site.Pages = pageLinks(pages)
	if err := checkSlugCollisions(root, pages); err != nil {
		return err
	}
	r, err := NewRenderer(opts.Site)
	if err != nil {
		return err
	}
	if err := WriteJSPF(opts.OutDir, root); err != nil {
		return err
	}
	if err := WriteIndexJSON(opts.OutDir, root); err != nil {
		return err
	}
	if err := r.RenderSite(opts.OutDir, root); err != nil {
		return err
	}
	if err := r.RenderPages(opts.OutDir, pages); err != nil {
		return err
	}
	if err := WriteAssets(opts.OutDir); err != nil {
		return err
	}
	if err := WriteCNAME(opts.OutDir, opts.Site.BaseURL); err != nil {
		return err
	}
	return WriteFeed(opts.OutDir, opts.Site, root)
}
