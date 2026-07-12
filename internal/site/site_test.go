package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
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
		"pages/about/index.html",
	} {
		if _, err := os.Stat(filepath.Join(out, rel)); err != nil {
			t.Errorf("missing output %s: %v", rel, err)
		}
	}

	pl, err := os.ReadFile(filepath.Join(out, "2014-top-songs", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(pl), `href="/pages/about/"`) {
		t.Error("playlist page header should link the content page")
	}
}

func TestBuildNarratesPhases(t *testing.T) {
	logger, hook := logtest.NewNullLogger()
	logger.SetLevel(logrus.InfoLevel)
	err := Build(Options{
		HubDir:   writeFixtureHub(t),
		OutDir:   t.TempDir(),
		PagesDir: t.TempDir(),
		Site:     testSite(),
		Logger:   logger,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var msgs []string
	hasElapsed := false
	for _, e := range hook.AllEntries() {
		msgs = append(msgs, e.Message)
		if _, ok := e.Data["elapsed"]; ok {
			hasElapsed = true
		}
	}
	joined := strings.Join(msgs, "\n")
	for _, want := range []string{"walk hub", "generate mosaics", "copy art", "write feed", "build complete"} {
		if !strings.Contains(joined, want) {
			t.Errorf("narration missing %q; got:\n%s", want, joined)
		}
	}
	if !hasElapsed {
		t.Error("expected phase entries to carry an 'elapsed' field")
	}
}

func TestCheckSlugCollisions(t *testing.T) {
	pagesNode := &Node{IsDir: true, Children: []*Node{{Name: "pages", IsDir: true}}}
	if err := checkSlugCollisions(pagesNode, []ContentPage{{Slug: "about"}}); err == nil {
		t.Error("expected error when a top-level 'pages' folder collides with the prefix")
	}
	if err := checkSlugCollisions(pagesNode, nil); err != nil {
		t.Errorf("no pages → no collision, got %v", err)
	}
	other := &Node{IsDir: true, Children: []*Node{{Name: "synthpop", IsDir: true}}}
	if err := checkSlugCollisions(other, []ContentPage{{Slug: "about"}}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
