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
	// The blurb is rendered HTML now, not an escaped string, so the apostrophe
	// stays literal where html/template used to turn it into &#39;. Only
	// & < > " are escaped, matching byom-player.
	if !strings.Contains(s, `class="blurb">It's https://x.test`) {
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

// The card used to be one big <a>, which made a link in the description invalid
// HTML (a nested anchor breaks the outer link). Only the cover and the title are
// links now, so the blurb can carry real ones.
func TestRenderCardLinksOnlyTitleAndCover(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "README.md"), "# hub\n")
	mustWrite(t, filepath.Join(dir, "a.yaml"),
		"title: A\nimage: http://img/1.jpg\ndescription: see [docs](https://e.test/x)\ntracks:\n  - {title: T, artist: X}\n")
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

	// The container is no longer a link.
	if strings.Contains(s, `<a class="playlist-card"`) {
		t.Error("card is still an anchor — a link in the blurb would be a nested anchor")
	}
	if !strings.Contains(s, `<div class="playlist-card">`) {
		t.Error("card should render as a div")
	}
	// Cover and title still navigate to the playlist.
	if !strings.Contains(s, `<a class="cover-link" href="/a/">`) {
		t.Error("cover should link to the playlist")
	}
	if !strings.Contains(s, `<a class="title" href="/a/">A</a>`) {
		t.Error("title should link to the playlist")
	}
	// And the description's own link is live.
	if !strings.Contains(s, `<a href="https://e.test/x" target="_blank" rel="noopener noreferrer">docs</a>`) {
		t.Errorf("blurb should render a markdown link as an anchor:\n%s", s)
	}
	// Within the card itself (not the page, which has header/footer links):
	// exactly three anchors — cover, title, and the one in the blurb. The card
	// has no nested <div>, so the first closing tag ends it.
	start := strings.Index(s, `<div class="playlist-card">`)
	if start < 0 {
		t.Fatal("no playlist card in the rendered page")
	}
	end := strings.Index(s[start:], "</div>")
	if end < 0 {
		t.Fatal("unterminated playlist card")
	}
	card := s[start : start+end]
	if n := strings.Count(card, "<a "); n != 3 {
		t.Errorf("expected 3 anchors in the card, got %d:\n%s", n, card)
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

func TestRenderLanding_FeaturedSection(t *testing.T) {
	hub := writeFixtureHub(t)
	// Feature the nested playlist, to prove the section is not limited to the
	// hub root, and give it a description so the card blurb renders.
	mustWrite(t, filepath.Join(hub, "synthpop", "bleep-bloop-bop.yaml"),
		"title: Bleep Bloop Bop\ncreator: les\nfeatured: true\ndescription: Bleeps and bloops.\ntracks:\n  - {title: T2, artist: A2}\n")
	root, err := BuildTree(hub)
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
	b, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	landing := string(b)
	if !strings.Contains(landing, `class="year featured">Featured`) {
		t.Error("landing missing Featured heading")
	}
	// The featured card links to the nested playlist and renders as a normal
	// card. The link lives on the cover and the title now, not on the card
	// container — the container is a plain div so the blurb can hold its own
	// anchors.
	if !strings.Contains(landing, `<a class="cover-link" href="/synthpop/bleep-bloop-bop/">`) {
		t.Error("landing Featured section missing the featured playlist cover link")
	}
	if !strings.Contains(landing, `<a class="title" href="/synthpop/bleep-bloop-bop/">`) {
		t.Error("landing Featured section missing the featured playlist title link")
	}
	// Featuring is additive: the playlist still appears under its folder, and the
	// unfeatured root playlist is untouched in its year group.
	if !strings.Contains(landing, `href="/synthpop/"`) {
		t.Error("landing lost the folder listing")
	}
	if !strings.Contains(landing, `href="/2014-top-songs/"`) {
		t.Error("landing lost the unfeatured playlist")
	}
	// The Featured heading precedes the year groups.
	if strings.Index(landing, "Featured") > strings.Index(landing, `class="tree"`) {
		t.Error("Featured section should come before the tree section")
	}
}

func TestRenderLanding_NoFeatured(t *testing.T) {
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
	b, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	landing := string(b)
	if strings.Contains(landing, "Featured") {
		t.Error("landing should render no Featured heading when nothing is featured")
	}
	// The regular listing is unaffected.
	if !strings.Contains(landing, `class="playlist-card"`) {
		t.Error("landing lost its playlist cards")
	}
}
