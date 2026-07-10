package site

import "os"

// Options configures a site build.
type Options struct {
	HubDir   string
	OutDir   string
	PagesDir string
	Site     SiteMeta
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
