package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFrontmatter(t *testing.T) {
	title, order, body := parseFrontmatter("---\ntitle: About\norder: 2\n---\nHello *world*.\n")
	if title != "About" || order != 2 || strings.TrimSpace(body) != "Hello *world*." {
		t.Fatalf("got (%q, %d, %q)", title, order, body)
	}
	// No frontmatter: whole input is the body.
	title, order, body = parseFrontmatter("# Just body\n")
	if title != "" || order != 0 || body != "# Just body\n" {
		t.Fatalf("no-fm got (%q, %d, %q)", title, order, body)
	}
}

func TestLoadPages(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "about.md"),
		[]byte("---\ntitle: About\norder: 2\n---\nAbout **me**.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "colophon.md"),
		[]byte("---\ntitle: Colophon\norder: 1\n---\nBuilt with care.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "elsewhere.md"),
		[]byte("Find me around.\n"), 0o644); err != nil { // no frontmatter
		t.Fatal(err)
	}

	pages, err := LoadPages(dir)
	if err != nil {
		t.Fatalf("LoadPages: %v", err)
	}
	if len(pages) != 3 {
		t.Fatalf("got %d pages", len(pages))
	}
	// Sorted by (order, title): Colophon(1), About(2), elsewhere(order 0)… wait order 0 sorts first.
	// elsewhere has order 0 → first; then Colophon(1); then About(2).
	if pages[0].Slug != "elsewhere" || pages[1].Title != "Colophon" || pages[2].Title != "About" {
		t.Fatalf("order = [%s, %s, %s]", pages[0].Slug, pages[1].Title, pages[2].Title)
	}
	if pages[0].Title != "elsewhere" {
		t.Errorf("title fallback = %q, want filename stem", pages[0].Title)
	}
	if !strings.Contains(string(pages[2].Body), "<strong>me</strong>") {
		t.Errorf("body not rendered to HTML: %q", pages[2].Body)
	}
	if pages[2].Desc != "About me." && pages[2].Desc != "About **me**." {
		t.Errorf("desc = %q", pages[2].Desc)
	}

	// Missing dir → no pages, no error.
	empty, err := LoadPages(filepath.Join(dir, "nope"))
	if err != nil || len(empty) != 0 {
		t.Errorf("missing dir: got %d pages, err %v", len(empty), err)
	}
}
