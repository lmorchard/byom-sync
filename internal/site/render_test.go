package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testSite() SiteMeta {
	return SiteMeta{
		Title: "Test Tapes", BaseURL: "https://mix.test",
		PlayerSrc: "https://cdn.example/byom-player.js",
		Provider:  "youtube", Providers: []string{"youtube", "spotify"},
	}
}

func TestRenderSite(t *testing.T) {
	root, err := BuildTree(writeFixtureHub(t))
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewRenderer(testSite())
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := r.RenderSite(out, root); err != nil {
		t.Fatalf("RenderSite: %v", err)
	}
	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(out, rel))
		if err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
		return string(b)
	}
	landing := read("index.html")
	if !strings.Contains(landing, "Welcome") || !strings.Contains(landing, "/synthpop/") {
		t.Error("landing missing intro or tree link")
	}
	pl := read("synthpop/bleep-bloop-bop/index.html")
	if !strings.Contains(pl, `<byom-player`) || !strings.Contains(pl, `src="playlist.jspf.json"`) {
		t.Error("playlist page missing player tag")
	}
	if !strings.Contains(pl, `provider="youtube"`) || !strings.Contains(pl, `providers="youtube,spotify"`) {
		t.Error("player missing provider config")
	}
	if !strings.Contains(pl, `<byom-site-nav>`) {
		t.Error("playlist page missing nav component")
	}
	if !strings.Contains(pl, `>Test Tapes</a>`) {
		t.Error("playlist page breadcrumb missing configured site title")
	}
	if !strings.Contains(pl, `property="og:title"`) {
		t.Error("playlist page missing OG tags")
	}
	embed := read("synthpop/bleep-bloop-bop/embed/index.html")
	if !strings.Contains(embed, "<byom-player") || strings.Contains(embed, "<byom-site-nav>") {
		t.Error("embed should have player but no site nav")
	}
	folder := read("synthpop/index.html")
	if !strings.Contains(folder, "Synthpop picks") {
		t.Error("folder page missing README intro")
	}
}
