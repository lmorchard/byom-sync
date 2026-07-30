# Notes: rich RSS item bodies

## What shipped

`feed.xml` items went from "title + link + raw description" to a real body: cover
art, the playlist's prose, a meta line, and the opening `site.feed_track_limit`
tracks (default 20) as links — YouTube preferred, `https://` Spotify URL as
fallback, unlinked text when neither exists. Plus a per-item `<enclosure>` for
locally stored cover art and 48px per-track thumbnails.

Commits (branch `feat/rss-feed-tracklists`, off `b6ea462`):

| Commit | What |
|---|---|
| `0cea644` | spec |
| `0e29035` | plan |
| `b6d64a2` | `trackLink`, `trackThumb`, `trackRow` |
| `7405aae` | `itemHTML` + `SiteMeta.FeedTrackLimit` |
| `2a31361` | `localCoverPath`, `coverEnclosure`, `imageTypes` |
| `9ec0e6f` | wiring: `WriteFeed`, viper default, `cmd/site.go` |
| `57b26dc` | docs |
| `ba4d311` | final-review fixes |

New file `internal/site/feedbody.go`; `feed.go` kept its single job of assembling
and sorting.

## The bug worth remembering

The final whole-branch review found a defect four per-task reviews had missed,
and it is the useful lesson from this session.

The same HTML string goes into both `Item.Description` and `Item.Content`.
`encoding/xml` escapes the former and *sanitizes C0 control characters to
U+FFFD*. But `Content` is marshalled `xml:",cdata"`, and Go's `emitCDATA` passes
bytes through verbatim with **no character-range check**. So a single control
character in any description, title, or artist made the entire `feed.xml`
unparseable — not one item, the whole document — while the build reported
success.

Reachability is low but real: go-yaml rejects raw control bytes in scalars, but a
double-quoted escape (`"a\x0cb"`, `\v`, `\e`) decodes fine, and an importer
round-tripping `` out of provider JSON carries it end to end.

Fixed with `stripInvalidXML` applied once at `itemHTML`'s return, which covers
every interpolated field at a stroke.

**Why it hid so long:** nothing in the repo had ever parsed the XML it generates.
Every feed assertion was `strings.Contains`, which confirms the right strings are
present but never that the document is well-formed. `TestWriteFeed` now decodes
`feed.xml` token by token, with a control character in the fixture so the
assertion would fail without the guard.

Generalizable: when a test suite asserts on the *content* of generated structured
output, at least one test should assert the output *parses*.

## Decisions and their reasons

- **Both `<description>` and `<content:encoded>`** get the same HTML. `content:encoded`
  is the correct home for full HTML, but many readers render only `description` —
  exactly the readers where a missing tracklist defeats the feature. ~2KB/item
  duplication is the price of compatibility.
- **Local art only** for thumbnails and the enclosure. `gorilla/feeds` needs a byte
  length (`rss.go:122`), which is knowable for a file in `outDir` but not for a
  remote URL without a network request — and the site build is otherwise entirely
  offline. Remote covers still show in the body `<img>`.
- **Explicit `imageTypes` table, not `mime.TypeByExtension`.** That function consults
  system files (`/etc/apache2/mime.types` on macOS), so answers vary per machine,
  and it returns `application/octet-stream` for non-images. Caught while writing
  the plan: the "unknown extension" test would have passed in CI and failed
  locally, and a non-image could have been advertised as a cover.
- **`spotify_url` linked only when `https://`-prefixed.** One condition, deliberately
  not a general sanitizer. Verified in real output: a `javascript:` URL renders as
  unlinked text.
- **Fixed in passing:** `feed.go` fed the raw `Playlist.Description` into the XML, so
  Spotify's entity-encoded text showed literally in readers. Now routed through
  `plainText` like `render.go:197` already did for HTML pages.

## Known, accepted

- **Body `<img>` and `<enclosure>` can disagree.** A playlist with a remote `Image`,
  no `ImageFile`, and a track that *does* have local art shows the remote hero in
  the body while the enclosure advertises the track's file. Reachable —
  `GenerateMosaics` skips any playlist where `p.Image != ""`, so such a playlist
  never gets an `ImageFile`. Documented in `localCoverPath`'s comment; behavior
  left alone since the alternative strictly loses enclosures.
- **No cap on feed item count.** Pre-existing, but this branch multiplied per-item
  cost ~30×, so a few hundred playlists puts `feed.xml` into the low megabytes.
  Worth revisiting as a newest-N cap.
- **Node `Path` isn't slugified**, so `Night Drive.yaml` yields an enclosure URL with
  a raw space. Pre-existing and consistent with `og:image`/`canonical`/JSPF
  `art_base`, but the enclosure is the first place a feed validator will see it.
- **No `<guid>`** on items; validators flag this as a recommendation. Readers fall
  back to `<link>`, which is stable and unique here.

## Verification

`go test -count=1 ./...` green, `make lint` 0 issues, `make format` no changes.

Also built two fixture hubs and read the generated XML, which confirmed what unit
tests can't: `&#x27;` decoded exactly once to `&#39;`, `&` escaped once,
`<Untitled>` escaped and unlinked, `enclosure length="209"` matching the real
bytes on disk (so `CopyArt` → `WriteFeed` path assumptions hold in a real build),
and the document parsing clean.
