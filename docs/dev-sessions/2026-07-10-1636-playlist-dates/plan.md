# Playlist Date Fields Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split the single ambiguous `date_created` into `date_imported` (first
seen by byom-sync), `date_created` (earliest track `added_at`), and `date_updated`
(latest track `added_at`), recomputed on sync and backfillable via a new command.

**Architecture:** A pure `Playlist.RefreshDates()` derives created/updated from
track `added_at` (min/max, falling back to `date_imported` when none parse); a
pure `Playlist.EnsureImportedDate()` migrates pre-existing files by promoting an
old `date_created` to `date_imported`. Sync and native import wire these in; a new
`byom-sync dates` command backfills the hub in place. Exporters surface the new
dates (JSPF playlist-level extension + Markdown frontmatter).

**Tech Stack:** Go 1.25 · Cobra · Viper · logrus · `gopkg.in/yaml.v3` · stdlib `time`.

## Global Constraints

- Go 1.25; format with `gofumpt` (run `make format`), lint with golangci-lint v2 (`make lint`).
- **errcheck is strict:** assign intentionally-ignored returns to `_ =` (e.g. `_ = cmd.MarkFlagRequired(...)`).
- Times are `time.Time`, stored UTC; parse/emit `added_at` and orphan dates as RFC3339 (`time.RFC3339`).
- YAML tags are snake_case; JSON tags for JSPF are snake_case with `omitempty` where a zero value should be omitted.
- Verify before done: `make lint && make test && make build` all green.
- Commit trailer on every commit: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.

---

### Task 1: Add the three date fields to the Playlist model

**Files:**
- Modify: `internal/playlist/types.go:13-26` (the `Playlist` struct + doc comment)
- Test: `internal/playlist/types_test.go` (extend `TestPlaylist_YAMLRoundTrip`)

**Interfaces:**
- Produces: `Playlist.DateImported time.Time`, `Playlist.DateCreated time.Time`,
  `Playlist.DateUpdated time.Time` (YAML keys `date_imported`, `date_created`,
  `date_updated`, in that order).

- [ ] **Step 1: Extend the round-trip test to cover all three date fields**

In `internal/playlist/types_test.go`, inside `TestPlaylist_YAMLRoundTrip`, add
the two new fields to the `orig` literal (just below `DateCreated`):

```go
		DateImported: time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC),
		DateCreated:  time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC),
		DateUpdated:  time.Date(2026, 7, 9, 0, 0, 0, 0, time.UTC),
```

And after the existing `DateCreated` assertion, add:

```go
	if !got.DateImported.Equal(orig.DateImported) {
		t.Errorf("date_imported mismatch: got %v want %v", got.DateImported, orig.DateImported)
	}
	if !got.DateUpdated.Equal(orig.DateUpdated) {
		t.Errorf("date_updated mismatch: got %v want %v", got.DateUpdated, orig.DateUpdated)
	}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/playlist/ -run TestPlaylist_YAMLRoundTrip -v`
Expected: FAIL — `orig` has unknown fields `DateImported`/`DateUpdated` (compile error).

- [ ] **Step 3: Add the fields to the struct**

In `internal/playlist/types.go`, replace the current `DateCreated` field and its
comment (lines 21-25) with:

```go
	// DateImported is when byom-sync first saw this playlist (its original
	// "first seen" time). Spotify's API exposes no true playlist creation date.
	DateImported time.Time `yaml:"date_imported"`
	// DateCreated is the earliest track added_at (start of curation); it falls
	// back to DateImported when no track has a parseable added_at.
	DateCreated time.Time `yaml:"date_created"`
	// DateUpdated is the latest track added_at (most recent curation); it falls
	// back to DateImported when no track has a parseable added_at.
	DateUpdated time.Time `yaml:"date_updated"`
	Tracks      []Track   `yaml:"tracks"`
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/playlist/ -run TestPlaylist_YAMLRoundTrip -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/playlist/types.go internal/playlist/types_test.go
git commit -m "feat(playlist): add date_imported/date_updated fields

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: RefreshDates + EnsureImportedDate helpers

**Files:**
- Create: `internal/playlist/dates.go`
- Test: `internal/playlist/dates_test.go`

**Interfaces:**
- Consumes: `Playlist.Tracks[].AddedAt string` (RFC3339), `Playlist.DateImported`,
  `Playlist.DateCreated` from Task 1.
- Produces:
  - `func (p *Playlist) RefreshDates()` — sets `DateCreated`/`DateUpdated` to the
    min/max parseable `added_at` (UTC); when none parse, both become `DateImported`.
  - `func (p *Playlist) EnsureImportedDate()` — if `DateImported` is zero and
    `DateCreated` is non-zero, sets `DateImported = DateCreated` (migration guard).

- [ ] **Step 1: Write the failing tests**

Create `internal/playlist/dates_test.go`:

```go
package playlist

import (
	"testing"
	"time"
)

func trackAt(added string) Track { return Track{Title: "t", Artist: "a", AddedAt: added} }

func TestRefreshDates_MinMaxAcrossTracks(t *testing.T) {
	p := Playlist{
		DateImported: time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC),
		Tracks: []Track{
			trackAt("2024-11-07T22:04:35Z"),
			trackAt("2020-01-01T00:00:00Z"), // earliest
			trackAt("2025-06-15T12:00:00Z"), // latest
		},
	}
	p.RefreshDates()
	if want := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC); !p.DateCreated.Equal(want) {
		t.Errorf("DateCreated: got %v want %v", p.DateCreated, want)
	}
	if want := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC); !p.DateUpdated.Equal(want) {
		t.Errorf("DateUpdated: got %v want %v", p.DateUpdated, want)
	}
}

func TestRefreshDates_IgnoresMissingAndUnparseable(t *testing.T) {
	p := Playlist{
		DateImported: time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC),
		Tracks: []Track{
			trackAt(""),             // no added_at
			trackAt("not-a-date"),   // unparseable
			trackAt("2023-03-03T03:03:03Z"),
		},
	}
	p.RefreshDates()
	want := time.Date(2023, 3, 3, 3, 3, 3, 0, time.UTC)
	if !p.DateCreated.Equal(want) || !p.DateUpdated.Equal(want) {
		t.Errorf("single valid track: created=%v updated=%v want %v", p.DateCreated, p.DateUpdated, want)
	}
}

func TestRefreshDates_FallsBackToImportedWhenNoDates(t *testing.T) {
	imported := time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)
	p := Playlist{
		DateImported: imported,
		Tracks:       []Track{trackAt(""), {Title: "x", Artist: "y"}},
	}
	p.RefreshDates()
	if !p.DateCreated.Equal(imported) || !p.DateUpdated.Equal(imported) {
		t.Errorf("fallback: created=%v updated=%v want %v", p.DateCreated, p.DateUpdated, imported)
	}
}

func TestEnsureImportedDate_PromotesOldCreated(t *testing.T) {
	created := time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)
	p := Playlist{DateCreated: created} // pre-migration file: imported absent
	p.EnsureImportedDate()
	if !p.DateImported.Equal(created) {
		t.Errorf("DateImported: got %v want %v", p.DateImported, created)
	}
}

func TestEnsureImportedDate_Idempotent(t *testing.T) {
	imported := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	created := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	p := Playlist{DateImported: imported, DateCreated: created}
	p.EnsureImportedDate()
	if !p.DateImported.Equal(imported) {
		t.Errorf("should not overwrite existing imported: got %v want %v", p.DateImported, imported)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/playlist/ -run 'TestRefreshDates|TestEnsureImportedDate' -v`
Expected: FAIL — `RefreshDates`/`EnsureImportedDate` undefined.

- [ ] **Step 3: Implement the helpers**

Create `internal/playlist/dates.go`:

```go
package playlist

import "time"

// RefreshDates recomputes DateCreated and DateUpdated from the tracks' added_at
// values: DateCreated is the earliest parseable added_at, DateUpdated the latest
// (both normalized to UTC). All tracks contribute, including orphaned ones.
// When no track has a parseable added_at (e.g. a native playlist), both fall
// back to DateImported so the fields are never left zero in normal operation.
func (p *Playlist) RefreshDates() {
	var earliest, latest time.Time
	found := false
	for _, t := range p.Tracks {
		if t.AddedAt == "" {
			continue
		}
		ts, err := time.Parse(time.RFC3339, t.AddedAt)
		if err != nil {
			continue
		}
		ts = ts.UTC()
		if !found {
			earliest, latest, found = ts, ts, true
			continue
		}
		if ts.Before(earliest) {
			earliest = ts
		}
		if ts.After(latest) {
			latest = ts
		}
	}
	if !found {
		p.DateCreated = p.DateImported
		p.DateUpdated = p.DateImported
		return
	}
	p.DateCreated = earliest
	p.DateUpdated = latest
}

// EnsureImportedDate migrates a pre-existing file whose DateCreated held the
// original "first seen" stamp: when DateImported is zero but DateCreated is set,
// it promotes DateCreated to DateImported. Idempotent — a file that already has
// DateImported is left unchanged. Call before RefreshDates so the fallback and
// the recomputation both see the correct import date.
func (p *Playlist) EnsureImportedDate() {
	if p.DateImported.IsZero() && !p.DateCreated.IsZero() {
		p.DateImported = p.DateCreated
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/playlist/ -run 'TestRefreshDates|TestEnsureImportedDate' -v`
Expected: PASS (all five)

- [ ] **Step 5: Commit**

```bash
git add internal/playlist/dates.go internal/playlist/dates_test.go
git commit -m "feat(playlist): RefreshDates + EnsureImportedDate helpers

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Wire date handling into sync

**Files:**
- Modify: `cmd/sync.go:122-137` (the date preserve/stamp block + post-merge call)
- Test: `cmd/sync_test.go` (add `TestImportedDate`)

**Interfaces:**
- Consumes: `Playlist.EnsureImportedDate()`, `Playlist.RefreshDates()` (Task 2),
  `playlist.Merge` (existing).
- Produces: `func importedDate(local playlist.Playlist, existed bool, now time.Time) time.Time`
  in `cmd/sync.go` — returns the import stamp to carry onto the merged playlist:
  `now.UTC()` for a first sync, else the local file's (migrated) `DateImported`.

- [ ] **Step 1: Write the failing test**

In `cmd/sync_test.go`, add imports `"time"` and
`"github.com/lmorchard/byom-sync/internal/playlist"` to the import block, then add:

```go
func TestImportedDate(t *testing.T) {
	now := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)

	// First sync (no existing file): stamp now.
	if got := importedDate(playlist.Playlist{}, false, now); !got.Equal(now) {
		t.Errorf("first sync: got %v want %v", got, now)
	}

	// Re-sync of a file that already has date_imported: preserve it.
	imported := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	local := playlist.Playlist{DateImported: imported, DateCreated: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}
	if got := importedDate(local, true, now); !got.Equal(imported) {
		t.Errorf("resync preserve: got %v want %v", got, imported)
	}

	// Re-sync of a pre-migration file (only date_created): migrate it up.
	oldCreated := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	preMigration := playlist.Playlist{DateCreated: oldCreated}
	if got := importedDate(preMigration, true, now); !got.Equal(oldCreated) {
		t.Errorf("resync migrate: got %v want %v", got, oldCreated)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/ -run TestImportedDate -v`
Expected: FAIL — `importedDate` undefined.

- [ ] **Step 3: Add the helper and rewire the sync block**

In `cmd/sync.go`, replace the block at lines 127-137 (the `if ok { ... } else { ... }`
through `merged := playlist.Merge(...)`) with:

```go
			if ok {
				local, err = playlist.LoadFile(path)
				if err != nil {
					return err
				}
			}
			remote.DateImported = importedDate(local, ok, now)

			merged := playlist.Merge(local, remote, strat, now)
			merged.RefreshDates()
```

Then add this helper function at the end of `cmd/sync.go` (before `func init()`):

```go
// importedDate returns the "first seen" stamp to carry onto a synced playlist:
// now for a brand-new playlist, otherwise the local file's DateImported —
// migrating a pre-change file whose original stamp lived in DateCreated.
func importedDate(local playlist.Playlist, existed bool, now time.Time) time.Time {
	if !existed {
		return now.UTC()
	}
	local.EnsureImportedDate()
	return local.DateImported
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./cmd/ -run TestImportedDate -v`
Expected: PASS

- [ ] **Step 5: Verify the package still builds and full cmd tests pass**

Run: `go build ./... && go test ./cmd/`
Expected: PASS (no other cmd test regressed)

- [ ] **Step 6: Commit**

```bash
git add cmd/sync.go cmd/sync_test.go
git commit -m "feat(sync): stamp date_imported and RefreshDates on sync

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Native import uses date_imported

**Files:**
- Modify: `cmd/import.go:67` (the `p.DateCreated = time.Now().UTC()` line)
- Test: `cmd/import_test.go:53-59` (the `DateCreated` assertion)

**Interfaces:**
- Consumes: `Playlist.RefreshDates()` (Task 2).

- [ ] **Step 1: Update the import test to assert the new date semantics**

In `cmd/import_test.go`, replace the existing `DateCreated` zero-check (around
lines 57-59) with:

```go
	if p.DateImported.IsZero() {
		t.Errorf("date_imported should be stamped")
	}
	if !p.DateCreated.Equal(p.DateImported) || !p.DateUpdated.Equal(p.DateImported) {
		t.Errorf("native created/updated should fall back to imported: imported=%v created=%v updated=%v",
			p.DateImported, p.DateCreated, p.DateUpdated)
	}
```

(If the test loads the saved file rather than holding `p` directly, apply the
same assertions to the loaded playlist variable — check the surrounding code and
match the existing variable name.)

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/ -run TestImport -v`
Expected: FAIL — `date_imported` is zero (import still sets `DateCreated`).

- [ ] **Step 3: Update the import command**

In `cmd/import.go`, replace line 67:

```go
	p.DateCreated = time.Now().UTC()
```

with:

```go
	p.DateImported = time.Now().UTC()
	p.RefreshDates()
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./cmd/ -run TestImport -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/import.go cmd/import_test.go
git commit -m "feat(import): stamp date_imported for native playlists

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: `byom-sync dates` backfill command

**Files:**
- Create: `cmd/dates.go`
- Test: `cmd/dates_test.go`

**Interfaces:**
- Consumes: `hubPaths(input string) ([]string, error)` (existing, `cmd/resolve.go:560`),
  `playlist.LoadFile`, `playlist.SaveFile`, `Playlist.EnsureImportedDate`,
  `Playlist.RefreshDates`, `viper.GetString("dir")`, package `log`.
- Produces: `func refreshFileDates(path string) error` — loads one hub file,
  applies `EnsureImportedDate` + `RefreshDates`, saves it back. Also registers
  the `dates` command with a `--input` flag (default: config `dir`).

- [ ] **Step 1: Write the failing test**

Create `cmd/dates_test.go`:

```go
package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

func TestRefreshFileDates_MigratesAndDerives(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.yaml")

	// A pre-migration file: date_created holds the import stamp, no date_imported.
	raw := `spotify_id: PID
title: Test
creator: Les
date_created: 2026-07-08T07:05:24Z
tracks:
    - title: Old
      artist: A
      added_at: "2020-01-01T00:00:00Z"
    - title: New
      artist: B
      added_at: "2025-06-15T12:00:00Z"
`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := refreshFileDates(path); err != nil {
		t.Fatalf("refreshFileDates: %v", err)
	}

	p, err := playlist.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// imported = old date_created; created/updated = min/max added_at.
	if got := p.DateImported.UTC().Format("2006-01-02T15:04:05Z"); got != "2026-07-08T07:05:24Z" {
		t.Errorf("DateImported: got %s", got)
	}
	if got := p.DateCreated.UTC().Format("2006-01-02"); got != "2020-01-01" {
		t.Errorf("DateCreated: got %s", got)
	}
	if got := p.DateUpdated.UTC().Format("2006-01-02"); got != "2025-06-15" {
		t.Errorf("DateUpdated: got %s", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/ -run TestRefreshFileDates -v`
Expected: FAIL — `refreshFileDates` undefined.

- [ ] **Step 3: Implement the command**

Create `cmd/dates.go`:

```go
package cmd

import (
	"fmt"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var datesInput string

var datesCmd = &cobra.Command{
	Use:   "dates",
	Short: "Recompute date_created/date_updated from track added_at across the hub",
	Long: `Backfill and refresh playlist date fields in place.

For each hub file: if it predates this feature (date_created holds the original
"first seen" stamp and date_imported is absent), the old date_created is promoted
to date_imported. Then date_created and date_updated are recomputed from the
tracks' added_at (earliest and latest); when no track has an added_at, both fall
back to date_imported. Idempotent — safe to run repeatedly.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDates()
	},
}

func runDates() error {
	input := datesInput
	if input == "" {
		input = viper.GetString("dir")
	}

	paths, err := hubPaths(input)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		log.Warnf("no playlist YAML files found under %s — nothing to do", input)
		return nil
	}

	for _, path := range paths {
		if err := refreshFileDates(path); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	log.Infof("refreshed dates across %d file(s) under %s", len(paths), input)
	return nil
}

// refreshFileDates loads a single hub file, migrates its import date if needed,
// recomputes created/updated from added_at, and writes it back.
func refreshFileDates(path string) error {
	p, err := playlist.LoadFile(path)
	if err != nil {
		return err
	}
	p.EnsureImportedDate()
	p.RefreshDates()
	if err := playlist.SaveFile(path, p); err != nil {
		return err
	}
	log.Infof("%s: imported=%s created=%s updated=%s",
		path,
		p.DateImported.UTC().Format("2006-01-02"),
		p.DateCreated.UTC().Format("2006-01-02"),
		p.DateUpdated.UTC().Format("2006-01-02"))
	return nil
}

func init() {
	rootCmd.AddCommand(datesCmd)
	datesCmd.Flags().StringVar(&datesInput, "input", "", "hub YAML file or directory (default: config dir)")
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./cmd/ -run TestRefreshFileDates -v`
Expected: PASS

- [ ] **Step 5: Verify build + full cmd tests**

Run: `go build ./... && go test ./cmd/`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/dates.go cmd/dates_test.go
git commit -m "feat(dates): add 'dates' command to backfill hub date fields

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: JSPF playlist-level extension for updated/imported

**Files:**
- Modify: `internal/export/jspf.go:22-28` (add `Extension` to `jspfPlaylist`),
  add a `jspfPlaylistExt` type, and populate it in `Export` after the `date` block.
- Test: `internal/export/export_test.go` (add `TestJSPFExportPlaylistDatesExtension`)

**Interfaces:**
- Consumes: `Playlist.DateUpdated`, `Playlist.DateImported` (Task 1), existing
  `byomExtNS` constant.
- Produces: JSPF playlist object carries
  `extension["https://github.com/lmorchard/byom-sync"][0]` with
  `date_updated` and `date_imported` (RFC3339 UTC), emitted only when at least
  one is non-zero. `date` (playlist-level) stays mapped to `DateCreated`.

- [ ] **Step 1: Write the failing test**

In `internal/export/export_test.go`, add:

```go
func TestJSPFExportPlaylistDatesExtension(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.jspf.json")
	p := samplePlaylist()
	p.DateCreated = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	p.DateUpdated = time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	p.DateImported = time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)
	if err := (JSPFExporter{}).Export(p, out, nil); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(out)

	var doc struct {
		Playlist struct {
			Date      string `json:"date"`
			Extension map[string][]struct {
				DateUpdated  string `json:"date_updated"`
				DateImported string `json:"date_imported"`
			} `json:"extension"`
		} `json:"playlist"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, raw)
	}
	if doc.Playlist.Date != "2020-01-01T00:00:00Z" {
		t.Errorf("playlist date should be date_created: got %q", doc.Playlist.Date)
	}
	ext := doc.Playlist.Extension["https://github.com/lmorchard/byom-sync"]
	if len(ext) == 0 {
		t.Fatalf("missing playlist-level byom extension:\n%s", raw)
	}
	if ext[0].DateUpdated != "2025-06-15T12:00:00Z" {
		t.Errorf("date_updated: got %q", ext[0].DateUpdated)
	}
	if ext[0].DateImported != "2026-07-08T00:00:00Z" {
		t.Errorf("date_imported: got %q", ext[0].DateImported)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/export/ -run TestJSPFExportPlaylistDatesExtension -v`
Expected: FAIL — no playlist-level extension emitted.

- [ ] **Step 3: Add the extension type and field**

In `internal/export/jspf.go`, add `Extension` to the `jspfPlaylist` struct (after `Image`):

```go
type jspfPlaylist struct {
	Title     string                       `json:"title,omitempty"`
	Creator   string                       `json:"creator,omitempty"`
	Date      string                       `json:"date,omitempty"`
	Image     string                       `json:"image,omitempty"`
	Extension map[string][]jspfPlaylistExt `json:"extension,omitempty"`
	Track     []jspfTrack                  `json:"track"`
}
```

And add this type next to `jspfExt` (below the `byomExtNS` const block):

```go
// jspfPlaylistExt carries byom-sync's playlist-level dates that JSPF has no
// native slot for. date_created maps to the standard playlist "date"; these two
// are emitted under the byom namespace only when non-zero. byom-player may read
// them for display and degrades gracefully when absent.
type jspfPlaylistExt struct {
	DateUpdated  string `json:"date_updated,omitempty"`
	DateImported string `json:"date_imported,omitempty"`
}
```

- [ ] **Step 4: Populate the extension in Export**

In `internal/export/jspf.go`, immediately after the existing
`doc.Playlist.Image = playlistImage(p)` line (~line 70), add:

```go
	var pext jspfPlaylistExt
	if !p.DateUpdated.IsZero() {
		pext.DateUpdated = p.DateUpdated.UTC().Format("2006-01-02T15:04:05Z")
	}
	if !p.DateImported.IsZero() {
		pext.DateImported = p.DateImported.UTC().Format("2006-01-02T15:04:05Z")
	}
	if pext.DateUpdated != "" || pext.DateImported != "" {
		doc.Playlist.Extension = map[string][]jspfPlaylistExt{byomExtNS: {pext}}
	}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/export/ -run TestJSPFExportPlaylistDatesExtension -v`
Expected: PASS

- [ ] **Step 6: Verify no existing export test regressed**

Run: `go test ./internal/export/`
Expected: PASS (samplePlaylist has zero updated/imported → no playlist extension,
so existing JSPF tests are unaffected).

- [ ] **Step 7: Commit**

```bash
git add internal/export/jspf.go internal/export/export_test.go
git commit -m "feat(export): JSPF playlist-level date_updated/date_imported extension

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Markdown frontmatter `updated`

**Files:**
- Modify: `internal/export/markdown.go:19-24` (`markdownView`) and `:36-39` (populate)
- Modify: `internal/templates/default.md` (frontmatter)
- Test: `internal/export/export_test.go` (extend `TestMarkdownExport`)

**Interfaces:**
- Consumes: `Playlist.DateUpdated` (Task 1).
- Produces: `markdownView.Updated string`; default template emits
  `updated: "{{ .Updated }}"` in the frontmatter (created stays as `date`).

- [ ] **Step 1: Extend the markdown test**

In `internal/export/export_test.go`, inside `TestMarkdownExport`, set an updated
date on the playlist before export. Replace the line
`if err := (MarkdownExporter{}).Export(samplePlaylist(), out, nil); err != nil {`
with:

```go
	p := samplePlaylist()
	p.DateCreated = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	p.DateUpdated = time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
	if err := (MarkdownExporter{}).Export(p, out, nil); err != nil {
```

Then add, after the existing `title:` frontmatter assertion:

```go
	if !strings.Contains(s, `date: "2020-01-01"`) {
		t.Errorf("frontmatter date should be date_created:\n%s", s)
	}
	if !strings.Contains(s, `updated: "2025-06-15"`) {
		t.Errorf("frontmatter updated missing:\n%s", s)
	}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/export/ -run TestMarkdownExport -v`
Expected: FAIL — `updated:` not in output.

- [ ] **Step 3: Add Updated to the view and populate it**

In `internal/export/markdown.go`, add `Updated string` to `markdownView` (after `Date`):

```go
type markdownView struct {
	Title   string
	Creator string
	Date    string
	Updated string
	Tracks  []playlist.Track
}
```

Then, after the existing `if !p.DateCreated.IsZero() { ... }` block (~line 39), add:

```go
	if !p.DateUpdated.IsZero() {
		view.Updated = p.DateUpdated.UTC().Format("2006-01-02")
	}
```

- [ ] **Step 4: Add `updated` to the default template**

In `internal/templates/default.md`, change the frontmatter block from:

```
---
title: "{{ .Title }}"
creator: "{{ .Creator }}"
date: "{{ .Date }}"
---
```

to:

```
---
title: "{{ .Title }}"
creator: "{{ .Creator }}"
date: "{{ .Date }}"
updated: "{{ .Updated }}"
---
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/export/ -run TestMarkdownExport -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/export/markdown.go internal/templates/default.md internal/export/export_test.go
git commit -m "feat(export): add 'updated' to Markdown frontmatter

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Full verification + docs

**Files:**
- Modify: `AGENTS.md` (dates command + field semantics note)
- Modify: `docs/dev-sessions/2026-07-10-1636-playlist-dates/notes.md` (session summary)

- [ ] **Step 1: Run the full gate**

Run: `make lint && make test && make build`
Expected: all green. Fix any gofumpt/errcheck findings before proceeding.

- [ ] **Step 2: Backfill the real playlists and eyeball the result**

Run: `./byom-sync dates --input ./playlists`
Then inspect a couple of files, e.g. `git diff playlists/drones.yaml`, and confirm:
`date_imported` ≈ the old `date_created`; `date_created` is the earliest track
`added_at`; `date_updated` is the latest. Do NOT commit the playlist YAML changes
as part of this feature branch unless Les asks — they are data, not code.

- [ ] **Step 3: Document in AGENTS.md**

Under the `## Commands` / `## Conventions & gotchas` sections, add a short note
describing the three date fields and the `dates` backfill command, mirroring the
style of the existing bullets. (Keep it to a few lines.)

- [ ] **Step 4: Write the session summary**

Fill in `docs/dev-sessions/2026-07-10-1636-playlist-dates/notes.md` with what was
built, decisions made, and any follow-ups (e.g. byom-player reading the new JSPF
playlist extension).

- [ ] **Step 5: Commit docs**

```bash
git add AGENTS.md docs/dev-sessions/2026-07-10-1636-playlist-dates/notes.md
git commit -m "docs(dates): document date fields + 'dates' command

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Self-Review

**Spec coverage:**
- Data model (3 fields, YAML order) → Task 1. ✓
- `RefreshDates` derivation + fallback + migration guard → Task 2. ✓
- Orphaned tracks count toward min/max → covered by `RefreshDates` iterating all `p.Tracks` (Task 2). ✓
- Sync recompute-on-sync + imported preservation/migration → Task 3. ✓
- Native import stamps imported → Task 4. ✓
- Standalone backfill tool (`dates`) → Task 5. ✓
- JSPF: `date` = created, extension for updated/imported → Task 6. ✓
- Markdown frontmatter created + updated → Task 7. ✓
- Tests for all above → each task is TDD. ✓
- Existing tests updated (types_test, import_test, export_test) → Tasks 1, 4, 6, 7. ✓

**Placeholder scan:** No TBD/TODO; every code step shows full code. ✓

**Type consistency:** `RefreshDates()` / `EnsureImportedDate()` (Task 2) used verbatim
in Tasks 3, 4, 5. `importedDate` (Task 3) matches its test. `jspfPlaylistExt`
fields (`DateUpdated`/`DateImported`) match the Task 6 test JSON tags. `markdownView.Updated`
(Task 7) matches the template variable `.Updated`. `refreshFileDates` (Task 5) matches its test. ✓

## Notes / follow-ups (out of scope)

- byom-player needs a reader update to consume the new JSPF playlist-level extension.
- Custom (init-overridden) Markdown templates won't gain `updated:` automatically.
