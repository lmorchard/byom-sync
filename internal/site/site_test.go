package site

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildEndToEnd(t *testing.T) {
	out := t.TempDir()
	err := Build(Options{
		HubDir: writeFixtureHub(t),
		OutDir: out,
		Site:   testSite(),
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
	} {
		if _, err := os.Stat(filepath.Join(out, rel)); err != nil {
			t.Errorf("missing output %s: %v", rel, err)
		}
	}
}
