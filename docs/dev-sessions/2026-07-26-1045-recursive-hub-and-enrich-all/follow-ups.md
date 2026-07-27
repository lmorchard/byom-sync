# Follow-ups from the recursive-hub branch

Known issues found while implementing
[`spec.md`](spec.md) that were deliberately left out of scope. Recorded here so
they survive the branch.

## 1. `sync` writes duplicate playlists on a nested hub (real bug)

`playlist.FindFileByID` (`internal/playlist/store.go`) still globs
`<dir>/*.yaml` one level deep — the last surviving instance of the shallow-glob
family this branch otherwise eliminated.

It is the `spotify_id` → file matcher behind `playlist.Save`, so on a hub whose
playlists live in subdirectories, `byom-sync sync`:

1. fails to find the existing `playlists/00-conceptual/drones.yaml`,
2. concludes the playlist is new,
3. writes a **duplicate** at `playlists/drones.yaml`.

The site generator then renders both, and the two diverge on every subsequent
sync.

**Why it wasn't fixed here:** unlike the read-only walkers, making this
recursive changes *where `sync` writes files*, which needs a decision the spec
never took — when a playlist genuinely is new, which directory should it land
in? The hub root, or somewhere inferred? That deserves its own design pass.

Pre-existing and not worsened by this branch (nothing was recursive before), but
now the only inconsistent path. `README.md` carries a caveat noting `sync`
matches and writes at the hub root only.

**When fixing:** `playlist.HubPaths` already provides the recursive walk;
`FindFileByID` can iterate its results instead of a glob. The open question is
only the create-new-file path in `Save`.

## 2. `export --embed-art` on a subdirectory resolves art against the wrong root

`artRootOf` (`cmd/export.go`) resolves hub-relative `image_file` values against
`--input`'s own directory. Exporting the hub root is fine; exporting a *single
section* misses the store at the hub root and embeds nothing.

This is the same mismatch that `artStoreRoot` (`cmd/resolve.go`) now fixes on
the *write* side. Listed as a Non-goal in the spec because it was pre-existing
and independent, but the helper needed to fix it now exists — `artRootOf` can
delegate to `artStoreRoot`. A pointer comment sits at the call site.

## 3. `artStoreRoot` containment check and `..`-prefixed directory names

`artStoreRoot` treats a `filepath.Rel` result beginning with `..` as "outside
the hub". A directory literally named `..something` *inside* the hub would
therefore be misread as outside, falling back to the pre-fix root.

Unreachable in practice: `HubPaths` skips every dot-prefixed entry, so such a
directory can never hold a discoverable playlist. Noted only so the next reader
doesn't have to re-derive it. The strict form is
`rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))`.
