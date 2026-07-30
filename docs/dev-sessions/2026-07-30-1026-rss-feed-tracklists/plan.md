# Rich RSS Item Bodies Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Each RSS item shows the playlist's cover, prose, a meta line, and the opening ~20 tracks as clickable YouTube (or Spotify) links.

**Architecture:** A new `internal/site/feedbody.go` owns per-item HTML and the cover enclosure; `feed.go` keeps its existing job of assembling and sorting the feed. The body builder is a pure function of `(*Node, SiteMeta)` so its tests need no filesystem; only `coverEnclosure` touches disk, because `gorilla/feeds` requires a byte length to emit an enclosure.

**Tech Stack:** Go 1.25 · `github.com/gorilla/feeds` v1.2.0 · stdlib `html`, `mime`, `strconv`, `strings`, `os`, `path/filepath` · Viper for config.

## Global Constraints

- **Escaping:** every interpolated value — track titles, artists, descriptions, URLs — passes through `html.EscapeString` before reaching output. Track titles containing `&` and `<` are common.
- **No network I/O.** The site build is entirely offline. Never add an HTTP request to determine an image size or type.
- **Local art only** for track thumbnails and the cover enclosure. A track or playlist whose art is a remote URL gets no thumbnail and no enclosure. The content-addressed store exists so the feed survives source-URL rot.
- **`spotify_url` is only linked when it begins with `https://`.** Hub data must never be able to put a `javascript:` URL into a published feed.
- **Do not reimplement existing helpers.** Reuse `playlistImage`, `playlistMeta`, `plainText`, `canonical` (all in `internal/site/meta.go`) and `walkPlaylists` (`internal/site/paths.go`).
- **`feeds.Enclosure.Length` is a `string`**, not an integer.
- Run `make test`, `make lint`, and `make format` before each commit. golangci-lint is pinned to v2.12.2; `make setup` installs it.
- Work happens on branch `feat/rss-feed-tracklists` in the worktree at `.claude/worktrees/rss-feed-tracklists`. Never `git checkout` in the primary checkout — other agents share it.

## Background for someone new to this codebase

`byom-sync site` compiles a directory of playlist YAML files (the "hub") into a static site. `internal/site/tree.go` walks the hub into a tree of `Node` values; a `Node` with a non-nil `Playlist` is a playlist leaf, and `Node.Path` is its slash-joined URL path from the hub root.

Two facts the enclosure work depends on, both already true in `internal/site/site.go:Build`:

1. `GenerateMosaics` runs early, writes `<outDir>/art/mosaic/<path>.jpg` for any playlist lacking a cover, and sets that playlist's in-memory `ImageFile`.
2. `CopyArt` mirrors `<hubDir>/art/**` into `<outDir>/art/**`, preserving relative paths.

Both run *before* `WriteFeed`, and `WriteFeed` already receives `outDir`. So when a playlist or track has a non-empty `ImageFile` (a hub-relative slash path like `art/ab/cdef.jpg`), that file exists on disk at `filepath.Join(outDir, ImageFile)` by the time the feed is written.

Relevant `playlist.Track` fields: `Title`, `Artist`, `YouTubeID`, `SpotifyURL`, `Image` (remote URL), `ImageFile` (hub-relative local path).

## File Structure

| File | Responsibility |
|---|---|
| `internal/site/feedbody.go` (create) | Per-item HTML: track links, thumbnails, `<li>` rows, the assembled body, and the cover enclosure |
| `internal/site/feedbody_test.go` (create) | Unit tests for all of the above |
| `internal/site/feed.go` (modify) | Attach body + enclosure to each item; unchanged otherwise |
| `internal/site/feed_test.go` (modify) | Feed-level assertions for both fields and the enclosure |
| `internal/site/render.go` (modify) | Add `FeedTrackLimit` to `SiteMeta` |
| `cmd/root.go` (modify) | `viper.SetDefault("site.feed_track_limit", 20)` |
| `cmd/site.go` (modify) | Read the config value into `SiteMeta` |
| `README.md`, `AGENTS.md` (modify) | Document the config knob and the enriched feed |

---

### Task 1: Track links and thumbnails

Two small pure functions plus the `<li>` renderer that uses them. This is the piece carrying the link-precedence and escaping rules, so it gets the densest tests.

**Files:**
- Create: `internal/site/feedbody.go`
- Test: `internal/site/feedbody_test.go`

**Interfaces:**
- Consumes: `playlist.Track` (from `internal/playlist/types.go`)
- Produces:
  - `func trackLink(t playlist.Track) string`
  - `func trackThumb(t playlist.Track, baseURL string) string`
  - `func trackRow(t playlist.Track, baseURL string) string`

- [ ] **Step 1: Write the failing tests**

Create `internal/site/feedbody_test.go`:

```go
package site

import (
	"strings"
	"testing"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

func TestTrackLink(t *testing.T) {
	tests := []struct {
		name  string
		track playlist.Track
		want  string
	}{
		{
			name:  "youtube id wins over spotify url",
			track: playlist.Track{YouTubeID: "abc123", SpotifyURL: "https://open.spotify.com/track/xyz"},
			want:  "https://www.youtube.com/watch?v=abc123",
		},
		{
			name:  "falls back to spotify url",
			track: playlist.Track{SpotifyURL: "https://open.spotify.com/track/xyz"},
			want:  "https://open.spotify.com/track/xyz",
		},
		{
			name:  "no link when neither is present",
			track: playlist.Track{Title: "Untitled"},
			want:  "",
		},
		{
			name:  "non-https spotify url is refused",
			track: playlist.Track{SpotifyURL: "javascript:alert(1)"},
			want:  "",
		},
		{
			name:  "plain http spotify url is refused",
			track: playlist.Track{SpotifyURL: "http://open.spotify.com/track/xyz"},
			want:  "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := trackLink(tc.track); got != tc.want {
				t.Errorf("trackLink() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTrackThumb(t *testing.T) {
	tests := []struct {
		name  string
		track playlist.Track
		want  string
	}{
		{
			name:  "local image file becomes an absolute url",
			track: playlist.Track{ImageFile: "art/ab/cdef.jpg"},
			want:  "https://mix.test/art/ab/cdef.jpg",
		},
		{
			name:  "remote-only image gets no thumbnail",
			track: playlist.Track{Image: "https://i.scdn.co/image/xyz"},
			want:  "",
		},
		{
			name:  "no art at all gets no thumbnail",
			track: playlist.Track{Title: "Untitled"},
			want:  "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := trackThumb(tc.track, "https://mix.test"); got != tc.want {
				t.Errorf("trackThumb() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTrackRowLinksAndEscapes(t *testing.T) {
	row := trackRow(playlist.Track{
		Artist:    "Simon & Garfunkel",
		Title:     "<Untitled>",
		YouTubeID: "abc123",
		ImageFile: "art/ab/cdef.jpg",
	}, "https://mix.test")

	for _, want := range []string{
		`<li>`,
		`href="https://www.youtube.com/watch?v=abc123"`,
		`src="https://mix.test/art/ab/cdef.jpg"`,
		`width="48"`,
		`Simon &amp; Garfunkel`,
		`&lt;Untitled&gt;`,
	} {
		if !strings.Contains(row, want) {
			t.Errorf("row missing %q\ngot: %s", want, row)
		}
	}
	// Raw angle brackets from the title must not survive into the markup.
	if strings.Contains(row, "<Untitled>") {
		t.Errorf("row contains unescaped title: %s", row)
	}
}

func TestTrackRowWithoutLinkOrThumb(t *testing.T) {
	row := trackRow(playlist.Track{Artist: "Bibio", Title: "Lovers' Carvings"}, "https://mix.test")

	if strings.Contains(row, "<a ") {
		t.Errorf("unlinkable track should not be wrapped in an anchor: %s", row)
	}
	if strings.Contains(row, "<img") {
		t.Errorf("track without local art should have no thumbnail: %s", row)
	}
	if !strings.Contains(row, "Bibio") || !strings.Contains(row, "Carvings") {
		t.Errorf("row should still name the track: %s", row)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/site/ -run 'TestTrack' -v`

Expected: FAIL — compile error, `undefined: trackLink`, `undefined: trackThumb`, `undefined: trackRow`.

- [ ] **Step 3: Write the implementation**

Create `internal/site/feedbody.go`:

```go
package site

import (
	"html"
	"strings"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

// trackLink returns the best playback URL for a track: its YouTube watch URL
// when a youtube_id is present, otherwise its Spotify URL, otherwise "".
//
// A spotify_url is only trusted when it is https. Hub data is generally our own,
// but a published feed is the wrong place to discover otherwise.
func trackLink(t playlist.Track) string {
	if t.YouTubeID != "" {
		return "https://www.youtube.com/watch?v=" + t.YouTubeID
	}
	if strings.HasPrefix(t.SpotifyURL, "https://") {
		return t.SpotifyURL
	}
	return ""
}

// trackThumb returns an absolute URL for a track's locally stored cover art, or
// "" when the track has only a remote image or none at all. Local-only on
// purpose: the content-addressed art store exists so the site and feed survive
// source-URL rot, and a hotlinked CDN thumbnail would defeat that.
func trackThumb(t playlist.Track, baseURL string) string {
	if t.ImageFile == "" {
		return ""
	}
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(t.ImageFile, "/")
}

// trackRow renders one <li> for a track: an optional thumbnail followed by
// "Artist — Title", wrapped in a playback link when one is available.
//
// The thumbnail carries width/height attributes *and* an inline style because
// feed readers sanitize aggressively — inline attributes survive where a <style>
// block does not.
func trackRow(t playlist.Track, baseURL string) string {
	label := html.EscapeString(t.Title)
	if t.Artist != "" {
		label = html.EscapeString(t.Artist) + " — " + label
	}

	var inner strings.Builder
	if thumb := trackThumb(t, baseURL); thumb != "" {
		inner.WriteString(`<img src="` + html.EscapeString(thumb) +
			`" alt="" width="48" height="48" ` +
			`style="vertical-align:middle;margin-right:8px">`)
	}
	inner.WriteString(label)

	if href := trackLink(t); href != "" {
		return `<li><a href="` + html.EscapeString(href) + `">` + inner.String() + `</a></li>`
	}
	return `<li>` + inner.String() + `</li>`
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/site/ -run 'TestTrack' -v`

Expected: PASS — all subtests green.

- [ ] **Step 5: Format, lint, and commit**

```bash
make format && make lint && make test
git add internal/site/feedbody.go internal/site/feedbody_test.go
git commit -m "feat(site): track link and thumbnail helpers for RSS item bodies

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: The item body builder

Assembles cover, prose, meta line, track list, and overflow link. Adds the `FeedTrackLimit` field the body builder reads.

**Files:**
- Modify: `internal/site/feedbody.go`
- Modify: `internal/site/render.go:15-24` (add one field to `SiteMeta`)
- Test: `internal/site/feedbody_test.go`

**Interfaces:**
- Consumes: `trackRow` (Task 1); existing `playlistImage`, `playlistMeta`, `plainText`, `canonical` from `internal/site/meta.go`
- Produces:
  - `SiteMeta.FeedTrackLimit int` — `> 0` caps the list; `<= 0` means all tracks
  - `func itemHTML(n *Node, site SiteMeta) string`

- [ ] **Step 1: Write the failing tests**

Append to `internal/site/feedbody_test.go`:

```go
// feedNode builds a playlist leaf node for body tests.
func feedNode(p *playlist.Playlist) *Node {
	return &Node{Name: "mix", Title: p.Title, Path: "mix", Playlist: p}
}

// manyTracks returns n distinct linkable tracks.
func manyTracks(n int) []playlist.Track {
	out := make([]playlist.Track, 0, n)
	for i := range n {
		out = append(out, playlist.Track{
			Artist:    "Artist" + strconv.Itoa(i),
			Title:     "Title" + strconv.Itoa(i),
			YouTubeID: "yt" + strconv.Itoa(i),
		})
	}
	return out
}

func TestItemHTMLStructure(t *testing.T) {
	site := testSite()
	site.FeedTrackLimit = 20
	body := itemHTML(feedNode(&playlist.Playlist{
		Title:       "Night Drive",
		Description: "Mostly instrumental.",
		ImageFile:   "art/cover.jpg",
		Tracks: []playlist.Track{
			{Artist: "Tycho", Title: "A Walk", YouTubeID: "walk1"},
		},
	}), site)

	for _, want := range []string{
		`<img src="https://mix.test/art/cover.jpg"`,
		`width="300"`,
		`Mostly instrumental.`,
		`1 track`,
		`<ol>`,
		`href="https://www.youtube.com/watch?v=walk1"`,
		`</ol>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\ngot: %s", want, body)
		}
	}
	// A single-page playlist has no overflow line.
	if strings.Contains(body, "more") {
		t.Errorf("unexpected overflow line: %s", body)
	}
}

func TestItemHTMLTruncatesAtLimit(t *testing.T) {
	site := testSite()
	site.FeedTrackLimit = 3
	body := itemHTML(feedNode(&playlist.Playlist{Title: "Long", Tracks: manyTracks(25)}), site)

	if got := strings.Count(body, "<li>"); got != 3 {
		t.Errorf("expected 3 track rows, got %d\n%s", got, body)
	}
	if !strings.Contains(body, "and 22 more") {
		t.Errorf("expected overflow line for 22 remaining tracks\ngot: %s", body)
	}
	// The overflow line links to the playlist's own page.
	if !strings.Contains(body, `href="https://mix.test/mix/"`) {
		t.Errorf("overflow line should link to the playlist page\ngot: %s", body)
	}
	// Track 4 and beyond must not appear.
	if strings.Contains(body, "Title3") {
		t.Errorf("body leaked a track past the limit: %s", body)
	}
}

func TestItemHTMLLimitZeroListsEverything(t *testing.T) {
	site := testSite()
	site.FeedTrackLimit = 0
	body := itemHTML(feedNode(&playlist.Playlist{Title: "All", Tracks: manyTracks(25)}), site)

	if got := strings.Count(body, "<li>"); got != 25 {
		t.Errorf("expected all 25 track rows, got %d", got)
	}
	if strings.Contains(body, "more") {
		t.Errorf("limit 0 should produce no overflow line: %s", body)
	}
}

// Spotify serves descriptions HTML-encoded. They must be decoded once, then
// escaped for output — not passed through doubly encoded.
func TestItemHTMLDecodesEncodedDescription(t *testing.T) {
	body := itemHTML(feedNode(&playlist.Playlist{
		Title:       "Encoded",
		Description: "what&#x27;s next &amp; why",
	}), testSite())

	if strings.Contains(body, "&amp;#x27;") {
		t.Errorf("description is double-encoded: %s", body)
	}
	if !strings.Contains(body, "what&#39;s next &amp; why") {
		t.Errorf("description not decoded-then-escaped as expected: %s", body)
	}
}

func TestItemHTMLOmitsAbsentPieces(t *testing.T) {
	body := itemHTML(feedNode(&playlist.Playlist{Title: "Bare"}), testSite())

	if strings.Contains(body, "<img") {
		t.Errorf("no cover should mean no img: %s", body)
	}
	if strings.Contains(body, "<ol>") {
		t.Errorf("no tracks should mean no list: %s", body)
	}
}
```

Add `"strconv"` to that file's import block.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/site/ -run 'TestItemHTML' -v`

Expected: FAIL — `undefined: itemHTML`, and `site.FeedTrackLimit` is an unknown field.

- [ ] **Step 3: Add the config field to SiteMeta**

In `internal/site/render.go`, add the field to the `SiteMeta` struct (currently lines 15–24):

```go
// SiteMeta carries site-wide settings baked into every page.
type SiteMeta struct {
	Title                 string
	BaseURL               string
	PlayerSrc             string
	Provider              string
	Providers             []string
	YouTubeSearchEndpoint string
	SpotifyClientID       string
	Pages                 []PageLink
	// FeedTrackLimit caps how many tracks each RSS item lists. A value > 0
	// truncates and adds an "…and N more" link; <= 0 lists every track.
	FeedTrackLimit int
}
```

- [ ] **Step 4: Write the body builder**

Append to `internal/site/feedbody.go` and add `"strconv"` to its imports:

```go
// itemHTML builds the RSS item body for one playlist: its cover, its own prose,
// a meta line, and the opening tracks as playback links. Each piece is omitted
// when the underlying data is absent.
//
// The result is used for both <description> and <content:encoded>. Many readers
// render only the former, which is exactly where a missing tracklist would
// defeat the point, so both carry the same HTML.
func itemHTML(n *Node, site SiteMeta) string {
	p := n.Playlist
	var b strings.Builder

	if cover := playlistImage(p, site.BaseURL); cover != "" {
		b.WriteString(`<p><img src="` + html.EscapeString(cover) +
			`" alt="` + html.EscapeString(n.Title) + ` cover" width="300"></p>`)
	}
	// plainText decodes the HTML entities Spotify serves; EscapeString then
	// re-encodes exactly once for output.
	if desc := strings.TrimSpace(plainText(p.Description)); desc != "" {
		b.WriteString(`<p>` + html.EscapeString(desc) + `</p>`)
	}
	if meta := playlistMeta(p); meta != "" {
		b.WriteString(`<p>` + html.EscapeString(meta) + `</p>`)
	}

	shown := p.Tracks
	if site.FeedTrackLimit > 0 && len(shown) > site.FeedTrackLimit {
		shown = shown[:site.FeedTrackLimit]
	}
	if len(shown) > 0 {
		b.WriteString(`<ol>`)
		for _, t := range shown {
			b.WriteString(trackRow(t, site.BaseURL))
		}
		b.WriteString(`</ol>`)
	}
	if rest := len(p.Tracks) - len(shown); rest > 0 {
		b.WriteString(`<p><a href="` + html.EscapeString(canonical(site.BaseURL, n.Path)) +
			`">…and ` + strconv.Itoa(rest) + ` more →</a></p>`)
	}
	return b.String()
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/site/ -run 'TestItemHTML|TestTrack' -v`

Expected: PASS.

- [ ] **Step 6: Format, lint, and commit**

```bash
make format && make lint && make test
git add internal/site/feedbody.go internal/site/feedbody_test.go internal/site/render.go
git commit -m "feat(site): build rich RSS item bodies with cover, meta, tracklist

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: The cover enclosure

The one piece that touches the filesystem, because `gorilla/feeds` needs a byte length.

**Files:**
- Modify: `internal/site/feedbody.go`
- Test: `internal/site/feedbody_test.go`

**Interfaces:**
- Consumes: `playlist.Playlist`, `SiteMeta` (Task 2)
- Produces:
  - `func localCoverPath(p *playlist.Playlist) string`
  - `func coverEnclosure(p *playlist.Playlist, site SiteMeta, outDir string) *feeds.Enclosure`

- [ ] **Step 1: Write the failing tests**

Append to `internal/site/feedbody_test.go`:

```go
// writeArt creates a file of n bytes at outDir/rel and returns rel.
func writeArt(t *testing.T, outDir, rel string, n int) string {
	t.Helper()
	full := filepath.Join(outDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, bytes.Repeat([]byte{0x7f}, n), 0o644); err != nil {
		t.Fatal(err)
	}
	return rel
}

func TestCoverEnclosureLocalFile(t *testing.T) {
	out := t.TempDir()
	rel := writeArt(t, out, "art/ab/cover.jpg", 1234)

	enc := coverEnclosure(&playlist.Playlist{Title: "Mix", ImageFile: rel}, testSite(), out)
	if enc == nil {
		t.Fatal("expected an enclosure for a local cover file")
	}
	if enc.Url != "https://mix.test/art/ab/cover.jpg" {
		t.Errorf("Url = %q", enc.Url)
	}
	if enc.Type != "image/jpeg" {
		t.Errorf("Type = %q, want image/jpeg", enc.Type)
	}
	if enc.Length != "1234" {
		t.Errorf("Length = %q, want \"1234\"", enc.Length)
	}
}

func TestCoverEnclosureFallsBackToTrackArt(t *testing.T) {
	out := t.TempDir()
	rel := writeArt(t, out, "art/cd/track.png", 99)

	enc := coverEnclosure(&playlist.Playlist{
		Title:  "Mix",
		Tracks: []playlist.Track{{Title: "T"}, {Title: "U", ImageFile: rel}},
	}, testSite(), out)
	if enc == nil {
		t.Fatal("expected an enclosure from the first track with local art")
	}
	if enc.Type != "image/png" {
		t.Errorf("Type = %q, want image/png", enc.Type)
	}
}

func TestCoverEnclosureSkipped(t *testing.T) {
	out := t.TempDir()
	missing := "art/never/written.jpg"
	unknownExt := writeArt(t, out, "art/ef/cover.bin", 10)
	empty := writeArt(t, out, "art/gh/empty.jpg", 0)

	tests := []struct {
		name string
		p    *playlist.Playlist
	}{
		{"remote-only cover", &playlist.Playlist{Image: "https://i.scdn.co/image/xyz"}},
		{"no art at all", &playlist.Playlist{Title: "Bare"}},
		{"file not on disk", &playlist.Playlist{ImageFile: missing}},
		{"unknown extension", &playlist.Playlist{ImageFile: unknownExt}},
		{"zero-length file", &playlist.Playlist{ImageFile: empty}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if enc := coverEnclosure(tc.p, testSite(), out); enc != nil {
				t.Errorf("expected no enclosure, got %+v", enc)
			}
		})
	}
}
```

Add `"bytes"`, `"os"`, and `"path/filepath"` to that file's import block.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/site/ -run 'TestCoverEnclosure' -v`

Expected: FAIL — `undefined: coverEnclosure`.

- [ ] **Step 3: Write the implementation**

Append to `internal/site/feedbody.go`, adding `"os"`, `"path/filepath"`, and `"github.com/gorilla/feeds"` to its imports:

```go
// imageTypes maps the cover-art extensions the art store produces to their MIME
// types. This is an explicit table rather than a call to mime.TypeByExtension
// because that function consults system files (e.g. /etc/apache2/mime.types on
// macOS), so its answers vary by machine — and it will happily report
// application/octet-stream for a non-image, which is not something to advertise
// as a cover.
var imageTypes = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".webp": "image/webp",
	".gif":  "image/gif",
	".avif": "image/avif",
}

// localCoverPath returns the site-relative path of the playlist's cover when a
// downloaded local copy exists. It mirrors coverHref's precedence — playlist
// hero first, then the first track with local art — but ignores remote URLs.
func localCoverPath(p *playlist.Playlist) string {
	if p.ImageFile != "" {
		return strings.TrimLeft(p.ImageFile, "/")
	}
	for _, t := range p.Tracks {
		if t.ImageFile != "" {
			return strings.TrimLeft(t.ImageFile, "/")
		}
	}
	return ""
}

// coverEnclosure returns an RSS enclosure for the playlist's cover, or nil when
// one cannot be produced honestly.
//
// gorilla/feeds only emits an enclosure when both Type and Length are set, and
// Length is a byte count. That is knowable for a local file — GenerateMosaics and
// CopyArt both run before WriteFeed, so the file is already in outDir — but not
// for a remote URL without a network request, and the site build stays offline.
// A remote-only cover therefore gets no enclosure; the body's <img> still shows
// it.
func coverEnclosure(p *playlist.Playlist, site SiteMeta, outDir string) *feeds.Enclosure {
	rel := localCoverPath(p)
	if rel == "" {
		return nil
	}
	ctype, ok := imageTypes[strings.ToLower(filepath.Ext(rel))]
	if !ok {
		return nil
	}
	fi, err := os.Stat(filepath.Join(outDir, filepath.FromSlash(rel)))
	if err != nil || fi.Size() == 0 {
		return nil
	}
	return &feeds.Enclosure{
		Url:    strings.TrimRight(site.BaseURL, "/") + "/" + rel,
		Type:   ctype,
		Length: strconv.FormatInt(fi.Size(), 10),
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/site/ -run 'TestCoverEnclosure' -v`

Expected: PASS, including the `unknown extension` subtest — `.bin` is absent from `imageTypes`, so no enclosure is produced regardless of what the host system's MIME database says.

- [ ] **Step 5: Format, lint, and commit**

```bash
make format && make lint && make test
git add internal/site/feedbody.go internal/site/feedbody_test.go
git commit -m "feat(site): per-item cover enclosure for local art

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Wire it into the feed and the CLI

**Files:**
- Modify: `internal/site/feed.go:12-25`
- Modify: `cmd/root.go:90` (add a default after `site.pages_dir`)
- Modify: `cmd/site.go:52-62` (add one field to the `SiteMeta` literal)
- Test: `internal/site/feed_test.go`

**Interfaces:**
- Consumes: `itemHTML` (Task 2), `coverEnclosure` (Task 3), `SiteMeta.FeedTrackLimit` (Task 2)
- Produces: a `feed.xml` whose items carry `<description>`, `<content:encoded>`, and (for local art) `<enclosure>`

- [ ] **Step 1: Write the failing tests**

Replace the body of `TestWriteFeed` in `internal/site/feed_test.go` and add a second test. Keep the existing ordering and absolute-link assertions:

```go
func TestWriteFeed(t *testing.T) {
	older := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	root := &Node{IsDir: true, Children: []*Node{
		{Name: "old", Title: "Old", Path: "old", Playlist: &playlist.Playlist{Title: "Old", DateCreated: older}},
		{Name: "new", Title: "New", Path: "new", Playlist: &playlist.Playlist{
			Title:       "New",
			DateCreated: newer,
			Tracks:      []playlist.Track{{Artist: "Tycho", Title: "A Walk", YouTubeID: "walk1"}},
		}},
	}}
	out := t.TempDir()
	if err := WriteFeed(out, testSite(), root); err != nil {
		t.Fatalf("WriteFeed: %v", err)
	}
	xml, err := os.ReadFile(filepath.Join(out, "feed.xml"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(xml)
	if !strings.Contains(s, "https://mix.test/new/") {
		t.Error("feed missing absolute item link")
	}
	// Newest first: "New" item appears before "Old".
	if strings.Index(s, "<title>New</title>") > strings.Index(s, "<title>Old</title>") {
		t.Error("feed items not newest-first")
	}
	// The tracklist reaches <content:encoded>, which is emitted as CDATA and so
	// carries raw markup.
	if !strings.Contains(s, "<![CDATA[") || !strings.Contains(s, "<ol>") {
		t.Error("feed missing raw tracklist markup in content:encoded")
	}
	// It also reaches <description>, which the XML marshaller escapes.
	if !strings.Contains(s, "&lt;ol&gt;") {
		t.Error("feed missing escaped tracklist markup in description")
	}
	if !strings.Contains(s, "youtube.com/watch?v=walk1") {
		t.Error("feed missing track link")
	}
}

func TestWriteFeedEnclosure(t *testing.T) {
	out := t.TempDir()
	rel := writeArt(t, out, "art/ab/cover.jpg", 4096)

	root := &Node{IsDir: true, Children: []*Node{
		{Name: "local", Title: "Local", Path: "local", Playlist: &playlist.Playlist{
			Title: "Local", ImageFile: rel,
		}},
		{Name: "remote", Title: "Remote", Path: "remote", Playlist: &playlist.Playlist{
			Title: "Remote", Image: "https://i.scdn.co/image/xyz",
		}},
	}}
	if err := WriteFeed(out, testSite(), root); err != nil {
		t.Fatalf("WriteFeed: %v", err)
	}
	xml, err := os.ReadFile(filepath.Join(out, "feed.xml"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(xml)

	if !strings.Contains(s, `url="https://mix.test/art/ab/cover.jpg"`) {
		t.Error("feed missing enclosure for local cover")
	}
	if !strings.Contains(s, `length="4096"`) {
		t.Error("enclosure missing byte length")
	}
	// Exactly one item has local art, so exactly one enclosure.
	if got := strings.Count(s, "<enclosure"); got != 1 {
		t.Errorf("expected 1 enclosure, got %d", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/site/ -run 'TestWriteFeed' -v`

Expected: FAIL — no `<ol>`, no `<enclosure>`, since `WriteFeed` still sets only the plain description.

- [ ] **Step 3: Attach the body and enclosure in `WriteFeed`**

In `internal/site/feed.go`, replace the item construction inside `walkPlaylists`:

```go
	err := walkPlaylists(root, func(n *Node) error {
		body := itemHTML(n, site)
		items = append(items, &feeds.Item{
			Title:       n.Title,
			Link:        &feeds.Link{Href: canonical(site.BaseURL, n.Path)},
			Description: body,
			Content:     body,
			Enclosure:   coverEnclosure(n.Playlist, site, outDir),
			Created:     n.Playlist.DateCreated,
		})
		return nil
	})
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/site/ -v`

Expected: PASS, including the pre-existing `TestBuild` in `site_test.go`.

- [ ] **Step 5: Add the config default and read it**

In `cmd/root.go`, after the `site.pages_dir` default (line 90):

```go
	viper.SetDefault("site.feed_track_limit", 20)
```

In `cmd/site.go`, add one field to the `site.SiteMeta` literal passed to `site.Build`:

```go
			SpotifyClientID:       viper.GetString("site.spotify_client_id"),
			FeedTrackLimit:        viper.GetInt("site.feed_track_limit"),
```

- [ ] **Step 6: Verify the whole build works end to end**

```bash
make build
```

Then generate a feed from the test fixtures and read it:

```bash
go test ./internal/site/ -run 'TestWriteFeed' -v && make test && make lint
```

Expected: all green. If you have a real hub handy, `./byom-sync site --input <hub> --out /tmp/feedcheck --base-url https://example.com` and inspect `/tmp/feedcheck/feed.xml` — items should carry a `<content:encoded>` block with a numbered list.

- [ ] **Step 7: Commit**

```bash
make format && make lint && make test
git add internal/site/feed.go internal/site/feed_test.go cmd/root.go cmd/site.go
git commit -m "feat(site): enrich RSS items with tracklist, cover, and enclosure

Items now carry the playlist cover, prose, a meta line, and the opening
site.feed_track_limit tracks as YouTube (or Spotify) links, in both
description and content:encoded. Local covers also get an enclosure.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Documentation

**Files:**
- Modify: `README.md:213-223` (the `site:` config block)
- Modify: `AGENTS.md` (the `internal/site/` bullet)

- [ ] **Step 1: Document the config knob in README**

In the `site:` YAML block in `README.md`, add a line after `providers`:

```yaml
  feed_track_limit: 20                     # tracks listed per RSS item (<=0 for all)
```

Then extend the sentence at line 200–203 that describes the site output, so the feed's contents are discoverable. Change "and an RSS feed" to:

```
and an RSS feed whose items list the opening tracks as links.
```

- [ ] **Step 2: Update AGENTS.md**

In the `internal/site/` bullet, find the phrase describing the generator's outputs (`OG metadata, RSS`) and extend the description of the feed. Add this sentence to the end of that bullet:

```
The RSS feed (`internal/site/feedbody.go`) gives each item a rich HTML body —
cover art, playlist prose, a meta line, and the first `site.feed_track_limit`
tracks (default 20) as YouTube links, falling back to `spotify_url` — written to
both `<description>` and `<content:encoded>` because many readers render only the
former. Track thumbnails and the per-item `<enclosure>` use locally stored art
only: an enclosure needs a byte length, which would otherwise mean a network
request during an offline build.
```

- [ ] **Step 3: Verify the docs match the code**

Confirm the default in `README.md` matches `cmd/root.go`:

```bash
grep -n "feed_track_limit" README.md AGENTS.md cmd/root.go
```

Expected: the README comment and the `viper.SetDefault` both say 20.

- [ ] **Step 4: Commit**

```bash
git add README.md AGENTS.md
git commit -m "docs: document feed_track_limit and the enriched RSS feed

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Manual verification

After Task 5, check the feed renders as intended in a real reader:

1. Build a site from a hub that has downloaded art: `./byom-sync site --input <hub> --out /tmp/feedcheck --base-url https://example.com`
2. Confirm `<enclosure>`, `<content:encoded>`, thumbnails, and the "…and N more" link are present in `/tmp/feedcheck/feed.xml`.
3. Because links are absolute against `--base-url`, thumbnails only load in a reader if that base URL is actually reachable. To check rendering locally, build with `--base-url` pointed at a local server and serve `/tmp/feedcheck`.

## Definition of done

- `make test`, `make lint`, `make format` all clean
- Every item body carries cover, prose, meta line, and up to `feed_track_limit` linked tracks
- Tracks with no `youtube_id` fall back to an `https` `spotify_url`; tracks with neither still appear, unlinked
- Track thumbnails and enclosures appear only for locally stored art
- `site.feed_track_limit` defaults to 20 and is documented in `README.md`
- No network request was added to the site build
