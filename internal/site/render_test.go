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
	// Playlists render as media cards; the top-level leaf carries a remote cover.
	if !strings.Contains(landing, `class="playlist-card"`) {
		t.Error("landing missing playlist cards")
	}
	if !strings.Contains(landing, `<img class="cover" src="http://img/1.jpg"`) {
		t.Error("landing card missing cover image")
	}
	if !strings.Contains(landing, `class="meta">1 track`) {
		t.Error("landing card missing metadata line")
	}
	// The synthpop child (bleep-bloop-bop) has no cover → placeholder box.
	folderPage := read("synthpop/index.html")
	if !strings.Contains(folderPage, `class="cover placeholder"`) {
		t.Error("cover-less playlist should render a placeholder box")
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

func TestRenderYearHeaders(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "README.md"), "# hub\n")
	mustWrite(t, filepath.Join(dir, "a.yaml"), "title: A\ndate_updated: 2020-05-01T00:00:00Z\ntracks:\n  - {title: T, artist: X}\n")
	mustWrite(t, filepath.Join(dir, "b.yaml"), "title: B\ndate_updated: 2018-02-01T00:00:00Z\ntracks:\n  - {title: T, artist: X}\n")
	root, err := BuildTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewRenderer(testSite())
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := r.RenderSite(out, root); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(out, "index.html"))
	s := string(b)
	i20, i18 := strings.Index(s, `<h2 class="year">2020</h2>`), strings.Index(s, `<h2 class="year">2018</h2>`)
	if i20 < 0 || i18 < 0 {
		t.Fatal("missing year headers")
	}
	if i20 > i18 {
		t.Error("year headers not in descending order (2020 should precede 2018)")
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

func TestRenderCardBlurb(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "README.md"), "# hub\n")
	mustWrite(t, filepath.Join(dir, "a.yaml"),
		"title: A\ndescription: It&#x27;s https:&#x2F;&#x2F;x.test\ntracks:\n  - {title: T, artist: X}\n")
	mustWrite(t, filepath.Join(dir, "b.yaml"),
		"title: B\ntracks:\n  - {title: T, artist: X}\n")
	root, err := BuildTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewRenderer(testSite())
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := r.RenderSite(out, root); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(out, "index.html"))
	s := string(b)
	if !strings.Contains(s, `class="blurb">It&#39;s https://x.test`) {
		t.Error("playlist with description should render a decoded blurb")
	}
	if !strings.Contains(s, "https://x.test") {
		t.Error("blurb should contain the decoded URL")
	}
	if strings.Contains(s, "&amp;") {
		t.Error("blurb should not be double-encoded (&amp; found)")
	}
	if strings.Count(s, `class="blurb"`) != 1 {
		t.Errorf("expected exactly one blurb, got %d", strings.Count(s, `class="blurb"`))
	}
}

func TestRenderPlaylistDescriptionDecoded(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "README.md"), "# hub\n")
	mustWrite(t, filepath.Join(dir, "enc.yaml"),
		"title: Enc\nspotify_id: xyz\ndescription: It&#x27;s at https:&#x2F;&#x2F;x.test\ntracks:\n  - {title: T, artist: A}\n")
	root, err := BuildTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewRenderer(testSite())
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := r.RenderSite(out, root); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out, "enc", "index.html"))
	if err != nil {
		t.Fatalf("playlist page: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, `property="og:description" content="It&#39;s at https://x.test"`) {
		t.Error("og:description should render the decoded description")
	}
	if !strings.Contains(s, `name="description" content="It&#39;s at https://x.test"`) {
		t.Error("meta description should render the decoded description")
	}
	if strings.Contains(s, "&amp;") {
		t.Error("description meta tags should not be double-encoded (&amp; found)")
	}
}
