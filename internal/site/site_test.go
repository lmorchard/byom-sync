package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildEndToEnd(t *testing.T) {
	out := t.TempDir()
	pagesDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(pagesDir, "about.md"),
		[]byte("---\ntitle: About\norder: 1\n---\nHello.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Build(Options{
		HubDir:   writeFixtureHub(t),
		OutDir:   out,
		PagesDir: pagesDir,
		Site:     testSite(),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, rel := range []string{
		"index.html",
		"site-index.json",
		"feed.xml",
		"CNAME",
		"assets/site.css",
		"assets/site-nav.js",
		"2014-top-songs/index.html",
		"2014-top-songs/playlist.jspf.json",
		"2014-top-songs/embed/index.html",
		"synthpop/index.html",
		"synthpop/bleep-bloop-bop/index.html",
		"synthpop/bleep-bloop-bop/playlist.jspf.json",
		"about/index.html",
	} {
		if _, err := os.Stat(filepath.Join(out, rel)); err != nil {
			t.Errorf("missing output %s: %v", rel, err)
		}
	}

	pl, err := os.ReadFile(filepath.Join(out, "2014-top-songs", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(pl), `href="/about/"`) {
		t.Error("playlist page header should link the content page")
	}
}

func TestCheckSlugCollisions(t *testing.T) {
	root := &Node{IsDir: true, Children: []*Node{{Name: "about", IsDir: true}}}
	if err := checkSlugCollisions(root, []ContentPage{{Slug: "about"}}); err == nil {
		t.Error("expected collision error for matching slug")
	}
	if err := checkSlugCollisions(root, []ContentPage{{Slug: "colophon"}}); err != nil {
		t.Errorf("unexpected error for non-colliding slug: %v", err)
	}
}
