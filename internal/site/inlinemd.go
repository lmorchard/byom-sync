package site

import (
	"html"
	"html/template"
	"regexp"
	"strconv"
	"strings"
)

// A deliberately tiny inline-markdown renderer for playlist descriptions,
// mirroring byom-player's src/markdown.ts: **bold**/__bold__, *italic*/_italic_,
// [text](url) links, and line breaks — nothing else. The player renders the same
// description string from the JSPF annotation, so one blurb has to look the same
// on the playlist page, on the landing page, and in the feed. A grammar change
// on either side belongs on both.
//
// Two renderers over one grammar:
//
//   - inlineMarkdownHTML for the RSS body, where links should be live.
//   - inlineMarkdownText for the landing card and the <meta>/og:description
//     tags, which must stay plain. The card blurb sits inside
//     <a class="playlist-card">, and a nested anchor is invalid HTML that
//     breaks the outer link.
var (
	inlineLinkRe  = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
	allowedHrefRe = regexp.MustCompile(`(?i)^(https?:|mailto:)`)
	boldStarRe    = regexp.MustCompile(`\*\*(.+?)\*\*`)
	boldScoreRe   = regexp.MustCompile(`__(.+?)__`)
	italStarRe    = regexp.MustCompile(`\*(.+?)\*`)
	italScoreRe   = regexp.MustCompile(`_(.+?)_`)

	// Brackets a rendered anchor while the emphasis passes run. NUL cannot
	// occur in the escaped source text and carries no markdown meaning.
	linkSlotRe = regexp.MustCompile("\x00L(\\d+)\x00")
)

// inlineEscaper covers the same four characters as byom-player's escapeHtml. A
// bare apostrophe is safe both in text and inside our double-quoted hrefs, and
// escaping it would show a literal &#39; where the player shows "'".
var inlineEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	`"`, "&quot;",
)

// inlineMarkdownHTML renders the description to HTML with live links.
//
// Order matters: entities are decoded first because Spotify serves descriptions
// HTML-encoded, then everything is escaped exactly once, and only then is the
// markdown grammar applied. Markup that arrives encoded therefore stays inert —
// an <a> Spotify put in a description renders as visible text, not a link.
func inlineMarkdownHTML(s string) template.HTML {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	out := inlineEscaper.Replace(html.UnescapeString(s))

	// Anchors are parked in slots before emphasis runs, so an emphasis pass
	// can't reach inside an href. Without this a perfectly ordinary URL
	// containing underscores (…/a_b_c) comes back with an <em> spliced into
	// the middle of the link target.
	var links []string
	out = inlineLinkRe.ReplaceAllStringFunc(out, func(m string) string {
		g := inlineLinkRe.FindStringSubmatch(m)
		text, href := g[1], g[2]
		if !allowedHrefRe.MatchString(href) {
			// Unknown protocol: drop the link, keep what the reader was meant
			// to see.
			return text
		}
		links = append(links, `<a href="`+href+`" target="_blank" rel="noopener noreferrer">`+
			applyEmphasis(text, true)+`</a>`)
		return "\x00L" + strconv.Itoa(len(links)-1) + "\x00"
	})

	out = applyEmphasis(out, true)
	out = strings.ReplaceAll(out, "\n", "<br>")

	out = linkSlotRe.ReplaceAllStringFunc(out, func(m string) string {
		i, err := strconv.Atoi(linkSlotRe.FindStringSubmatch(m)[1])
		if err != nil || i < 0 || i >= len(links) {
			return ""
		}
		return links[i]
	})

	// #nosec G203 — every tag in the result was emitted by this function; the
	// source was escaped before any of it ran.
	return template.HTML(out)
}

// inlineMarkdownText renders the description to plain text over the same
// grammar: a link collapses to its visible text and emphasis markers are
// dropped. It never escapes — callers hand the result to html/template, which
// escapes once at the point of use.
func inlineMarkdownText(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	out := html.UnescapeString(s)
	out = inlineLinkRe.ReplaceAllString(out, "$1")
	out = applyEmphasis(out, false)
	// A blurb is rendered on one line in both places that use this, so a
	// newline reads as a space.
	out = strings.ReplaceAll(out, "\n", " ")
	return out
}

// applyEmphasis runs the bold passes before the italic ones so that ** is
// consumed before the single-* pass can see it. With tags=false the markers are
// simply dropped, which is what the plain-text renderer wants.
func applyEmphasis(s string, tags bool) string {
	if !strings.ContainsAny(s, "*_") {
		return s
	}
	strong, em := "<strong>$1</strong>", "<em>$1</em>"
	if !tags {
		strong, em = "$1", "$1"
	}
	s = boldStarRe.ReplaceAllString(s, strong)
	s = boldScoreRe.ReplaceAllString(s, strong)
	s = italStarRe.ReplaceAllString(s, em)
	s = italScoreRe.ReplaceAllString(s, em)
	return s
}
