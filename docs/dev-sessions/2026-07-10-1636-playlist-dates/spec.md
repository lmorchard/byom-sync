# Spec: playlist date fields (imported / created / updated)

## Problem

Playlists imported from Spotify all carry a very recent `date_created`, because
today that field is stamped at first-sync time (`cmd/sync.go`), not derived from
the tracks. Spotify's API exposes no true playlist creation date; the real
curation history lives in each track's `added_at`. We want the hub to surface
that history.

## Goal

Replace the single ambiguous `date_created` with three clearly-defined fields:

- `date_imported` — when byom-sync first saw the playlist (today's meaning of
  `date_created`).
- `date_created` — the earliest track `added_at` (start of curation).
- `date_updated` — the latest track `added_at` (most recent curation).

Recompute the derived pair automatically on sync, and provide a standalone tool
to backfill/repair the playlists already on disk.

## Decisions (from brainstorming)

- **Compute model:** store all three fields in the YAML and recompute the
  derived pair on sync; also expose a standalone tool that recomputes against
  playlists as they currently sit on disk.
- **Missing `added_at`:** compute min/max only over tracks with a parseable
  `added_at`. If none have one, `date_created`/`date_updated` fall back to
  `date_imported`. (Native playlists therefore get created = updated = imported.)
- **Orphaned tracks count:** all tracks in the file contribute to min/max,
  including archived/orphaned ones (`spotify_present:false`) — they are still
  part of the curation history. (In mirror mode they are gone anyway.)
- **Export surface:** JSPF extension + Markdown frontmatter (see below).
- **Migration heuristic:** if `date_imported` is absent but `date_created` is
  set (a pre-migration file), treat the existing `date_created` as the import
  stamp — copy it to `date_imported`, then recompute. Idempotent.

## Design

### 1. Data model — `internal/playlist/types.go`

Add two fields and reinterpret one, in this YAML order on `Playlist`:

```go
DateImported time.Time `yaml:"date_imported"` // first seen by byom-sync
DateCreated  time.Time `yaml:"date_created"`  // earliest track added_at
DateUpdated  time.Time `yaml:"date_updated"`  // latest track added_at
```

The derivation guarantees non-zero values in normal operation (so no
`0001-01-01…` appears in YAML), because created/updated fall back to
`date_imported`, which is always stamped.

### 2. Derivation + migration helpers

- `func (p *Playlist) RefreshDates()` — parses each track's `added_at`
  (RFC3339), sets `DateCreated`/`DateUpdated` to the min/max, falling back to
  `DateImported` when no track has a parseable `added_at`. Pure, no network,
  unit-testable.
- Migration guard (small helper or inline): if `DateImported.IsZero() &&
  !DateCreated.IsZero()`, set `DateImported = DateCreated` before refreshing.
  Idempotent — re-running only recomputes.

Location: `types.go`, or a small `dates.go` in the same package.

### 3. Sync path — `cmd/sync.go`

Replace the current `DateCreated` preserve/stamp block:

- On re-sync: preserve `DateImported` from the local file (applying the
  migration guard when the local file predates this change).
- On first sync: stamp `DateImported = now.UTC()`.
- After `Merge`, call `merged.RefreshDates()`.

`Merge` is unchanged: `out := remote` already carries `DateImported` through, and
the derived pair is recomputed after the merge produces the final track set.

### 4. Native import — `cmd/import.go`

Stamp `date_imported = now.UTC()` (instead of `date_created`), then call
`RefreshDates()`. Native tracks have no `added_at`, so created/updated resolve to
imported.

### 5. Backfill tool

New top-level command `byom-sync dates` (no network, so not under `resolve`):

- Loads every hub file in the playlists dir.
- Applies the migration guard, then `RefreshDates()`, then saves.
- Idempotent — safe to re-run.

This is what repairs the existing on-disk playlists. (Name open to
reconsideration — `restamp` was floated — but `dates` reads clearly.)

### 6. Exports

- **JSPF** — `internal/export/jspf.go`: `date` stays mapped to `date_created`
  (now the real earliest). Add a **playlist-level** byom extension reusing
  `byomExtNS`, carrying `date_updated` and `date_imported`, mirroring the
  existing track-level extension pattern. JSPF stays valid regardless; consuming
  the new extension is a follow-up in byom-player (separate repo).
- **Markdown** — `internal/export/markdown.go` + default template in
  `internal/templates/`: keep `date` = created, add `updated` to the frontmatter
  view and template. (Custom init-overridden templates won't pick this up
  automatically — expected.)

### 7. Tests

- `RefreshDates`: all tracks have `added_at`; some missing; none (fall back to
  imported); single track; unparseable `added_at` ignored.
- Migration guard: idempotency; imported-absent/created-present promotes
  correctly; both-present is left alone.
- Exports: JSPF playlist-level extension carries updated + imported; Markdown
  frontmatter shows created + updated.
- Update existing tests that construct playlists or assert on `DateCreated`
  (store_test, types_test, export_test, import_test, sync).

## Out of scope

- byom-player changes to read the new JSPF playlist-level extension.
- Any change to per-track `added_at` semantics or sourcing.
