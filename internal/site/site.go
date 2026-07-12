package site

import (
	"fmt"
	"os"
	"time"

	"github.com/sirupsen/logrus"
)

// Options configures a site build.
type Options struct {
	HubDir   string
	OutDir   string
	PagesDir string
	Site     SiteMeta

	// Logger narrates build phases; nil → a default logrus logger.
	Logger logrus.FieldLogger
}

// checkSlugCollisions returns an error if a top-level playlist/folder is named
// "pages", which would collide with the content-pages path prefix (/pages/).
func checkSlugCollisions(root *Node, pages []ContentPage) error {
	if len(pages) == 0 {
		return nil
	}
	for _, c := range root.Children {
		if c.Name == "pages" {
			return fmt.Errorf("a top-level playlist/folder named %q collides with the content-pages path prefix (/pages/); rename it", "pages")
		}
	}
	return nil
}

// Build compiles the hub at opts.HubDir into a static site at opts.OutDir.
func Build(opts Options) error {
	lg := opts.Logger
	if lg == nil {
		lg = logrus.New()
	}
	overall := time.Now()

	timed := func(name string, fn func() error) error {
		t := time.Now()
		if err := fn(); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		lg.WithField("elapsed", time.Since(t).Round(time.Millisecond).String()).Infof("site: %s", name)
		return nil
	}

	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return err
	}

	var root *Node
	if err := timed("walk hub", func() error {
		var e error
		root, e = BuildTree(opts.HubDir)
		return e
	}); err != nil {
		return err
	}
	lg.Infof("site: hub has %d playlists in %d folders", countPlaylists(root), countFolders(root))

	var pages []ContentPage
	if err := timed("load pages", func() error {
		var e error
		pages, e = LoadPages(opts.PagesDir)
		return e
	}); err != nil {
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

	if err := timed("generate mosaics", func() error { return GenerateMosaics(opts.HubDir, opts.OutDir, root) }); err != nil {
		return err
	}
	if err := timed("write jspf", func() error { return WriteJSPF(opts.OutDir, root, opts.Site.BaseURL) }); err != nil {
		return err
	}
	if err := timed("write index json", func() error { return WriteIndexJSON(opts.OutDir, root) }); err != nil {
		return err
	}
	if err := timed("render pages", func() error { return r.RenderSite(opts.OutDir, root) }); err != nil {
		return err
	}
	if err := timed("render content pages", func() error { return r.RenderPages(opts.OutDir, pages) }); err != nil {
		return err
	}
	if err := timed("write assets", func() error { return WriteAssets(opts.OutDir) }); err != nil {
		return err
	}
	if err := timed("copy art", func() error { return CopyArt(opts.HubDir, opts.OutDir) }); err != nil {
		return err
	}
	if err := timed("write cname", func() error { return WriteCNAME(opts.OutDir, opts.Site.BaseURL) }); err != nil {
		return err
	}
	if err := timed("write feed", func() error { return WriteFeed(opts.OutDir, opts.Site, root) }); err != nil {
		return err
	}

	lg.WithField("elapsed", time.Since(overall).Round(time.Millisecond).String()).Infof("site: build complete → %s", opts.OutDir)
	return nil
}

// countPlaylists returns the number of playlist leaves in the tree.
func countPlaylists(root *Node) int {
	n := 0
	_ = walkPlaylists(root, func(*Node) error { n++; return nil })
	return n
}

// countFolders returns the number of directory nodes below root.
func countFolders(root *Node) int {
	n := 0
	var walk func(*Node)
	walk = func(node *Node) {
		for _, c := range node.Children {
			if c.IsDir {
				n++
				walk(c)
			}
		}
	}
	walk(root)
	return n
}
