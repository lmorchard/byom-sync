package site

import (
	"html/template"
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
	site := testSite()
	site.Pages = []PageLink{{Title: "About", Href: "/about/"}, {Title: "Colophon", Href: "/colophon/"}}
	r, err := NewRenderer(site)
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
	// Each playlist in the tree carries a light metadata line (fixture leaves
	// have a single track, no duration/date).
	if !strings.Contains(landing, `class="meta">— 1 track`) {
		t.Error("landing tree missing per-playlist metadata line")
	}
	pl := read("synthpop/bleep-bloop-bop/index.html")
	if !strings.Contains(pl, `<byom-player`) || !strings.Contains(pl, `src="/synthpop/bleep-bloop-bop/playlist.jspf.json"`) {
		t.Error("playlist page missing player tag")
	}
	if !strings.Contains(pl, `provider="youtube"`) || !strings.Contains(pl, `providers="youtube,spotify"`) {
		t.Error("player missing provider config")
	}
	if !strings.Contains(pl, `<byom-site-nav>`) {
		t.Error("playlist page missing nav component")
	}
	// A nested page shows only its intermediate folder context, linked upward —
	// and NOT a redundant site-root home crumb (the header already links home).
	if !strings.Contains(pl, `<nav class="crumbs">`) || !strings.Contains(pl, `href="/synthpop/"`) {
		t.Error("nested playlist breadcrumb should show its folder, linked")
	}
	crumbs := pl[strings.Index(pl, `<nav class="crumbs">`):]
	crumbs = crumbs[:strings.Index(crumbs, `</nav>`)]
	if strings.Contains(crumbs, `href="/">`) {
		t.Error("breadcrumb should omit the site-root home crumb")
	}
	// Top-level playlist has nothing above it but home, so no breadcrumb at all.
	top := read("2014-top-songs/index.html")
	if strings.Contains(top, `<nav class="crumbs">`) {
		t.Error("top-level playlist should have no breadcrumb")
	}
	if !strings.Contains(pl, `property="og:title"`) {
		t.Error("playlist page missing OG tags")
	}
	embed := read("synthpop/bleep-bloop-bop/embed/index.html")
	// Header nav: content-page links appear, in order, on interior + landing.
	if i := strings.Index(pl, `href="/about/"`); i < 0 || strings.Index(pl, `href="/colophon/"`) < i {
		t.Error("playlist header missing content-page nav in order")
	}
	if !strings.Contains(landing, `<nav class="page-nav">`) || !strings.Contains(landing, `href="/about/"`) {
		t.Error("landing header missing content-page nav")
	}
	if strings.Contains(embed, `class="page-nav"`) {
		t.Error("embed page must not carry the header nav")
	}
	if !strings.Contains(embed, "<byom-player") || strings.Contains(embed, "<byom-site-nav>") {
		t.Error("embed should have player but no site nav")
	}
	if !strings.Contains(embed, `src="/synthpop/bleep-bloop-bop/playlist.jspf.json"`) {
		t.Error("embed player must point at the root-relative JSPF path, not a relative one")
	}
	folder := read("synthpop/index.html")
	if !strings.Contains(folder, "Synthpop picks") {
		t.Error("folder page missing README intro")
	}
}

func TestRenderPages(t *testing.T) {
	site := testSite()
	site.Pages = []PageLink{{Title: "About", Href: "/about/"}}
	r, err := NewRenderer(site)
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	pages := []ContentPage{{
		Slug: "about", Title: "About", Desc: "Who I am.",
		Body: template.HTML("<p>Hello <strong>world</strong>.</p>"),
	}}
	if err := r.RenderPages(out, pages); err != nil {
		t.Fatalf("RenderPages: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(out, "pages", "about", "index.html"))
	if err != nil {
		t.Fatalf("about page: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, "<strong>world</strong>") {
		t.Error("page body not rendered")
	}
	if !strings.Contains(s, `<nav class="page-nav">`) {
		t.Error("page missing header nav")
	}
	if !strings.Contains(s, `property="og:title" content="About"`) {
		t.Error("page missing OG title")
	}
	if !strings.Contains(s, `href="https://mix.test/pages/about/"`) {
		t.Error("page missing canonical URL")
	}
}
