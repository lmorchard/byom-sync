package site

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
)

// The HTML cases mirror byom-player's src/markdown.test.ts one-for-one. The two
// renderers must agree on the same description string, so a case added there
// belongs here too.
func TestInlineMarkdownHTML(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"escapes html", `<b>x</b> & "q"`, `&lt;b&gt;x&lt;/b&gt; &amp; &quot;q&quot;`},
		{"bold stars", "a **bold** b", "a <strong>bold</strong> b"},
		{"bold underscores", "a __bold__ b", "a <strong>bold</strong> b"},
		{"italic star", "a *it* b", "a <em>it</em> b"},
		{"italic underscore", "a _it_ b", "a <em>it</em> b"},
		{
			"allowed link",
			"see [notes](https://e.com/x)",
			`see <a href="https://e.com/x" target="_blank" rel="noopener noreferrer">notes</a>`,
		},
		{
			"mailto link",
			"[mail](mailto:a@b.com)",
			`<a href="mailto:a@b.com" target="_blank" rel="noopener noreferrer">mail</a>`,
		},
		{"disallowed protocol keeps text", "[x](javascript:evil)", "x"},
		{"newline to br", "a\nb", "a<br>b"},

		// byom-sync-specific: Spotify serves descriptions HTML-encoded, so the
		// entities are decoded before formatting and re-encoded exactly once.
		// Only & < > " are escaped, matching byom-player's escapeHtml. A bare
		// apostrophe is safe in text and in a double-quoted attribute.
		{"decodes spotify entities", "what&#x27;s up", "what's up"},
		{
			"decoded markup stays inert",
			`&lt;a href=&quot;https://evil.example&quot;&gt;click&lt;/a&gt;`,
			`&lt;a href=&quot;https://evil.example&quot;&gt;click&lt;/a&gt;`,
		},
		{
			"ampersand in url survives as an entity",
			"[q](https://e.com/s?a=1&b=2)",
			`<a href="https://e.com/s?a=1&amp;b=2" target="_blank" rel="noopener noreferrer">q</a>`,
		},
		// Deliberate divergence from byom-player, which runs its emphasis
		// passes over already-rendered anchors and so splices an <em> into any
		// href containing underscores. Ours parks the anchor first.
		{
			"underscores in a url are not emphasis",
			"[q](https://e.com/a_b_c)",
			`<a href="https://e.com/a_b_c" target="_blank" rel="noopener noreferrer">q</a>`,
		},
		{
			"emphasis still applies inside link text",
			"[**q**](https://e.com/x)",
			`<a href="https://e.com/x" target="_blank" rel="noopener noreferrer"><strong>q</strong></a>`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(inlineMarkdownHTML(tt.in)); got != tt.want {
				t.Errorf("inlineMarkdownHTML(%q)\n got %q\nwant %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestInlineMarkdownText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"link becomes its text", "see [notes](https://e.com/x)", "see notes"},
		{"disallowed protocol keeps text", "[x](javascript:evil)", "x"},
		{"bold markers stripped", "a **bold** b", "a bold b"},
		{"italic markers stripped", "a _it_ b", "a it b"},
		{"newline becomes a space", "a\nb", "a b"},
		{"decodes spotify entities", "what&#x27;s up", "what's up"},
		{"markup is not interpreted", `<b>x</b>`, `<b>x</b>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := inlineMarkdownText(tt.in); got != tt.want {
				t.Errorf("inlineMarkdownText(%q)\n got %q\nwant %q", tt.in, got, tt.want)
			}
		})
	}
}

// The landing-page blurb sits inside <a class="playlist-card">, and a nested
// anchor is invalid HTML that breaks the outer link. inlineMarkdownText returns
// plain text that html/template escapes at the point of use, so the invariant
// that matters is about the *rendered* markup, not the intermediate string —
// Spotify-supplied "<a href=...>" legitimately survives decoding as visible
// text and is neutralised by the template.
func TestInlineMarkdownTextRendersNoAnchor(t *testing.T) {
	tmpl := template.Must(template.New("card").Funcs(template.FuncMap{
		"inlineText": inlineMarkdownText,
	}).Parse(`<a class="playlist-card" href="/x/"><span class="blurb">{{inlineText .}}</span></a>`))

	for _, in := range []string{
		"see [notes](https://e.com/x)",
		"bare https://e.com/x",
		`&lt;a href=&quot;https://e.com&quot;&gt;x&lt;/a&gt;`,
		"[a](https://e.com) and [b](mailto:x@y.z)",
		"[x](javascript:evil)",
	} {
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, in); err != nil {
			t.Fatalf("execute(%q): %v", in, err)
		}
		// Exactly one anchor: the card link the template itself opens.
		if n := strings.Count(buf.String(), "<a "); n != 1 {
			t.Errorf("input %q rendered %d anchors, want 1:\n%s", in, n, buf.String())
		}
		if strings.Contains(buf.String(), "javascript:") {
			t.Errorf("input %q leaked a javascript: URL:\n%s", in, buf.String())
		}
	}
}
