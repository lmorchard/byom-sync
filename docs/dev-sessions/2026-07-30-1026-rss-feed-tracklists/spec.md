# Spec: rich RSS item bodies with track links

## Problem

`feed.xml` lists one item per playlist carrying only a title, a link, and the
playlist's description. A reader sees a name and a URL with nothing to judge it
by. The feed should show what is actually *in* each playlist.

## Goal

Each RSS item body shows the playlist's cover art, its prose, a meta line, and
the opening tracks as clickable links — so a subscriber can scan a playlist and
jump straight to a song without visiting the site.

## Scope

`internal/site/feed.go` and one new file beside it. No changes to the hub schema,
the exporters, or the rendered HTML pages.

## Design

### New file: `internal/site/feedbody.go`

`feed.go` keeps its current job — assembling and sorting the feed — and the new
file owns the two per-item pieces:

```go
// The item body HTML, shared by Description and Content.
func itemHTML(n *Node, site SiteMeta) string

// The cover enclosure, or nil when the cover is not a local file.
func coverEnclosure(p *playlist.Playlist, site SiteMeta, outDir string) *feeds.Enclosure
```

Splitting them keeps the body builder free of filesystem access — it is a pure
function of the node and site meta, which is what makes the table-driven tests
below cheap to write. Only `coverEnclosure` touches disk.

`itemHTML` reuses existing helpers rather than reimplementing them: `playlistImage`
(absolute cover URL), `playlistMeta` (`"16 tracks · 1 hr 8 min · Jul 2026"`),
`plainText` (entity decoding), and `canonical` (absolute page URL).

### Body structure

In order, with each piece omitted when its data is absent:

1. `<p><img src="{absolute cover}" alt="{title} cover" width="300"></p>`
2. `<p>{description}</p>`
3. `<p>{meta line}</p>`
4. `<ol>` of up to `FeedTrackLimit` tracks, one `<li>` per track
5. `<p><a href="{playlist page}">…and N more →</a></p>` — only when truncated

### Track rows

Each `<li>` reads `Artist — Title`, preceded by a thumbnail when one is
available, with both wrapped in the link:

```
 1. [img] Boards of Canada — Dayvan Cowboy
 2. [img] Tycho — A Walk
 3.       Bibio — Lovers' Carvings          ← no local art, text only
```

**Link precedence:** `youtube_id` → `https://www.youtube.com/watch?v={id}`;
otherwise `spotify_url`; otherwise unlinked text. A track missing both still gets
its `<li>`, so the numbering reflects real playlist positions.

**URL guard:** `spotify_url` is only linked when it begins with `https://`. Hub
data should never be able to put a `javascript:` URL into a published feed. This
is one condition, not a general sanitizer.

**Thumbnails:** a new helper

```go
func trackThumb(t playlist.Track, baseURL string) string
```

returns an absolute URL derived from the track's `ImageFile`, or `""` when the
track has only a remote `Image` or no art at all. Local art only — the
content-addressed store exists so the feed survives source-URL rot, and a
hotlinked Spotify CDN thumbnail defeats that.

Thumbnails render at 48px with `width`/`height` attributes *and* an inline
`style`. Feed readers sanitize aggressively; inline attributes survive where
`<style>` blocks do not.

No new file copying is required: `CopyArt` already mirrors the whole
`<hub>/art/**` store into the output, and track `ImageFile` paths point into it.

### Per-item image (enclosure)

Each item gets an `<enclosure>` for the playlist cover when — and only when —
the cover is a **local** file:

- `gorilla/feeds` emits an enclosure only if both `Type` and `Length` are set
  (`rss.go:122`), and `Length` is a byte count.
- `WriteFeed` already receives `outDir`, and both `GenerateMosaics` and
  `CopyArt` run before it in `Build`, so a local cover is on disk at
  `filepath.Join(outDir, ImageFile)` and can be `os.Stat`-ed for its size.
- MIME type comes from `mime.TypeByExtension`. If that returns empty, the
  enclosure is skipped rather than guessed.
- For a remote-only cover the enclosure is skipped. Determining its length would
  mean an HTTP HEAD, and the site build is otherwise entirely offline. The body
  `<img>` still displays the cover in that case.

The enclosure is one image per *item* — the playlist cover. Per-track art appears
only as the inline thumbnails above.

### Field placement

The same HTML goes into **both** `Item.Description` and `Item.Content`
(→ `<content:encoded>`, emitted in CDATA).

`content:encoded` is the correct home for full HTML, but many readers render only
`<description>` — and those are precisely the readers where an empty tracklist
would defeat the feature. The duplication costs roughly 2 KB per item, which is
worth paying for compatibility.

### Configuration

`site.feed_track_limit`, default `20`:

- `viper.SetDefault("site.feed_track_limit", 20)` in `cmd/root.go`
- read via `viper.GetInt` in `cmd/site.go`
- carried as `SiteMeta.FeedTrackLimit`

Semantics: a value `> 0` caps the list and enables the overflow line; `<= 0`
means list every track with no overflow line.

### Bug fixed in passing

`feed.go:18` puts the raw `Playlist.Description` into the feed. Spotify
descriptions arrive HTML-encoded (`what&#x27;s`), so readers currently show
literal entities. Routing it through the existing `plainText` helper before
escaping for HTML fixes this. `render.go:197` already does exactly this for the
HTML pages; the feed was the one place that missed it.

## Escaping

Every interpolated value — track titles, artists, descriptions, URLs — is escaped
with `html.EscapeString` before reaching the output. Track titles containing `&`
or `<` are real and common.

## Testing

Tests are written before the implementation.

`feedbody_test.go`:

- a track with `youtube_id` links to YouTube even when `spotify_url` is also set
- a track with only `spotify_url` falls back to it
- a track with neither renders text with no `<a>`
- a non-`https` `spotify_url` is not linked
- `FeedTrackLimit` truncates the list and emits "…and N more"
- `FeedTrackLimit` of 0 lists every track with no overflow line
- `&` and `<` in a track title are escaped
- an entity-encoded description is decoded once, not double-encoded
- a track with `ImageFile` gets an absolute-URL thumbnail
- a track with only a remote `Image` gets no thumbnail, and keeps its `<li>`
- the cover `<img>` resolves to an absolute URL

`feed_test.go` (extending the existing test):

- the tracklist reaches both `<description>` and `<content:encoded>`
- a local cover produces an `<enclosure>` with a non-zero length and a MIME type
- a remote-only cover produces no `<enclosure>`
- the existing newest-first ordering and absolute-link assertions still hold

## Out of scope

- An Atom feed alongside RSS
- `media:thumbnail` — needs no byte length and would cover remote art, but
  `gorilla/feeds` cannot emit it, so it would mean post-processing the generated
  XML
- Markdown rendering of descriptions — they are plain text from Spotify, not
  markdown
- Network requests during the site build

## Docs to update

- the `site:` config block in `README.md` (add `feed_track_limit`)
- the `internal/site/` bullet in `AGENTS.md` (note the enriched feed)
