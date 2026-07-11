# Notes: playlist date fields

## What was built

Split the single, misleading `date_created` (stamped at first-sync time) into
three fields on `playlist.Playlist`:

- `date_imported` — when byom-sync first saw the playlist (the old `date_created`
  meaning).
- `date_created` — earliest track `added_at`.
- `date_updated` — latest track `added_at`.

Derived pair falls back to `date_imported` when no track has a parseable
`added_at`. All tracks contribute (orphaned included).

## Tasks (subagent-driven, all reviewed clean)

1. Fields on `Playlist` (`internal/playlist/types.go`).
2. Pure helpers `RefreshDates()` + `EnsureImportedDate()` (`internal/playlist/dates.go`).
3. Sync wiring: `importedDate` helper + `RefreshDates` after merge (`cmd/sync.go`).
4. Native `import` stamps `date_imported` (`cmd/import.go`).
5. `byom-sync dates` backfill command (`cmd/dates.go`).
6. JSPF playlist-level extension for `date_updated`/`date_imported` (`internal/export/jspf.go`).
7. Markdown `updated` frontmatter (`internal/export/markdown.go` + `default.md`).
8. Verification + docs + real backfill.

Commit range: `5813c81` (plan) → head. Ledger: `.superpowers/sdd/progress.md`.

## Decisions

- Store-and-recompute on sync, plus a standalone backfill tool (`dates`).
- Ignore missing `added_at`; fall back to `date_imported`.
- JSPF: `date` stays = `date_created`; updated/imported ride a playlist-level
  byom extension. Markdown: add `updated`.
- Migration heuristic: promote old `date_created` → `date_imported` when imported
  is absent (`EnsureImportedDate`). Idempotent.

## Branch note (important)

This branch (`feat/playlist-dates`) was cut from `origin/main`, which does **not**
include the cover-art work (`resolve art`, JSPF/playlist `Image`,
`playlistImage()`) — that lives on the separate `feat/spotify-art` / PR #20 stack.
The plan was drafted while `feat/spotify-art` was checked out, so it referenced an
`Image` line in `jspf.go` that isn't here. Task 6 adapted by placing the new
playlist `Extension` after the `date` block. Expect a trivial merge reconciliation
in `jspf.go`'s `jspfPlaylist` struct when the two branches converge.

## Smoke test (real hub)

`./byom-sync dates --input ./playlists` refreshed 60 files. Spot checks:
`drones` imported=2026-07-08 created=2024-11-07 updated=2024-11-07;
`before-the-western-odyssey` 2011-09-23 → 2012-06-29;
`bass-guitar-appreciation` 2013-09-22 → 2022-09-26. Idempotent across repeated
runs. (`playlists/` is gitignored local hub data — nothing to commit.)

## Verification

`make lint` (0 issues), `make test` (all pass), `make build` — all green.

## Follow-ups

- byom-player: read the JSPF playlist-level date extension (separate repo).
- Custom (init-overridden) markdown templates won't gain `updated:` automatically.
