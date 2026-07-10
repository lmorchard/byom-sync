# Phase 1 — Native Playlists Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the byom-sync hub safely hold hand-authored ("native") playlists — playlists with no Spotify source — without any Spotify-specific behavior misfiring on them.

**Architecture:** A playlist's provenance is *derived*, never stored: a new `Playlist.Source()` helper returns `spotify` when `SpotifyID` is set and `native` otherwise. The one place this currently matters — the JSPF exporter's Spotify-orphan logic — is gated on that helper so native tracks never export as "orphaned." `sync` already cannot overwrite a native file (slug-collision handling in `store.go`), so that safety is locked in with a characterization test rather than new code.

**Tech Stack:** Go 1.25 · `gopkg.in/yaml.v3` · standard `testing` package. No new dependencies.

## Global Constraints

- Go 1.25; no cgo.
- Formatting via `gofumpt` (`make format`); lint via golangci-lint v2 (`make lint`).
- **errcheck is strict** — assign intentionally-ignored returns to `_ =`.
- Run `make lint && make test && make build` before claiming done; read the output.
- Commit trailer on every commit: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
- Work happens on branch `feat/enrich-playlists`; PR into `main` when the phase is complete. No direct pushes to `main`.
- Make the smallest reasonable change. No unrelated refactoring.

## File Structure

- `internal/playlist/types.go` — **modify.** Add `Source` type, `Playlist.Source()`, `Playlist.IsNative()`.
- `internal/playlist/types_test.go` — **modify.** Add provenance truth-table test.
- `internal/export/jspf.go` — **modify.** Gate the orphan/`spotify_present` block on `p.Source() == playlist.SourceSpotify`.
- `internal/export/export_test.go` — **modify.** Add a native-playlist JSPF test.
- `internal/playlist/store_test.go` — **modify.** Add a characterization test: saving a Spotify playlist whose slug collides with an existing native file never overwrites the native file.
- `AGENTS.md` — **modify.** Document native playlists + the derived-provenance model.

---

### Task 1: Provenance helper (`Source` / `IsNative`)

**Files:**
- Modify: `internal/playlist/types.go`
- Test: `internal/playlist/types_test.go`

**Interfaces:**
- Consumes: nothing (foundational).
- Produces:
  - `type Source string`
  - `const ( SourceSpotify Source = "spotify"; SourceNative Source = "native" )`
  - `func (p Playlist) Source() Source`
  - `func (p Playlist) IsNative() bool`

- [ ] **Step 1: Write the failing test**

Add to `internal/playlist/types_test.go`:

```go
func TestPlaylist_Source(t *testing.T) {
	spotify := Playlist{SpotifyID: "37i9dQZF1DXcBWIGoYBM5M", Title: "Synced"}
	if got := spotify.Source(); got != SourceSpotify {
		t.Errorf("Source() with spotify_id: got %q want %q", got, SourceSpotify)
	}
	if spotify.IsNative() {
		t.Error("IsNative() with spotify_id: got true want false")
	}

	native := Playlist{Title: "Hand Authored"}
	if got := native.Source(); got != SourceNative {
		t.Errorf("Source() without spotify_id: got %q want %q", got, SourceNative)
	}
	if !native.IsNative() {
		t.Error("IsNative() without spotify_id: got false want true")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/playlist/ -run TestPlaylist_Source -v`
Expected: FAIL — `undefined: SourceSpotify` (build error).

- [ ] **Step 3: Write minimal implementation**

Add to `internal/playlist/types.go`, after the `Playlist` struct (before `Track`):

```go
// Source identifies where a playlist came from. It is derived from which
// source-ID field is populated, never stored as an explicit label — so adding a
// new ingestion source later (e.g. YouTube playlists) means adding one field and
// one case here, not migrating data.
type Source string

const (
	SourceSpotify Source = "spotify"
	SourceNative  Source = "native"
)

// Source returns the playlist's provenance: SourceSpotify when it carries a
// Spotify playlist ID, otherwise SourceNative (hand-authored).
func (p Playlist) Source() Source {
	if p.SpotifyID != "" {
		return SourceSpotify
	}
	return SourceNative
}

// IsNative reports whether the playlist has no upstream source (hand-authored).
func (p Playlist) IsNative() bool {
	return p.Source() == SourceNative
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/playlist/ -run TestPlaylist_Source -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/playlist/types.go internal/playlist/types_test.go
git commit -m "feat(playlist): derive playlist provenance via Source()/IsNative()

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Gate JSPF orphan emission on provenance

**Files:**
- Modify: `internal/export/jspf.go` (the orphan block around line 85)
- Test: `internal/export/export_test.go`

**Interfaces:**
- Consumes: `playlist.SourceSpotify`, `playlist.Playlist.Source()` (Task 1).
- Produces: nothing new; changes exporter behavior for native playlists only.

**Context:** The exporter's per-track extension block emits two things — the resolved YouTube id (`if t.YouTubeID != ""`) and the Spotify orphan state (`if !t.SyncState.SpotifyPresent`). A native track's `SpotifyPresent` defaults to `false`, so today every native track would emit `spotify_present:false` and trip byom-player's orphan indicator. Only the **orphan** half must be gated; the YouTube-id half must keep working for native tracks.

- [ ] **Step 1: Write the failing test**

Add to `internal/export/export_test.go`:

```go
// nativePlaylist is a hand-authored playlist (no spotify_id). Its tracks have
// SpotifyPresent=false by default, which must NOT be treated as "orphaned".
func nativePlaylist() playlist.Playlist {
	return playlist.Playlist{
		Title:   "Late Night Drives",
		Creator: "Les",
		Tracks: []playlist.Track{
			{Title: "Come Together", Artist: "The Beatles", Album: "Abbey Road"},
			{Title: "Nightcall", Artist: "Kavinsky", YouTubeID: "MV_3Dpw-BRY"},
		},
	}
}

func TestJSPFExport_NativeOmitsOrphanState(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "native.jspf")
	if err := (JSPFExporter{}).Export(nativePlaylist(), out, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(out)

	var doc map[string]any
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, got)
	}
	tracks := doc["playlist"].(map[string]any)["track"].([]any)
	if len(tracks) != 2 {
		t.Fatalf("expected 2 tracks: %v", tracks)
	}

	// Track 0: no youtube id, native -> no extension at all.
	if _, present := tracks[0].(map[string]any)["extension"]; present {
		t.Errorf("native track without a resolved id should have no extension: %v", tracks[0])
	}

	// Track 1: has a youtube id -> extension present, carries the resolved id,
	// but must NOT carry spotify_present.
	ext1, present := tracks[1].(map[string]any)["extension"]
	if !present {
		t.Fatalf("track with youtube id should keep its extension: %v", tracks[1])
	}
	elems := ext1.(map[string]any)["https://github.com/lmorchard/byom-sync"].([]any)
	entry := elems[0].(map[string]any)
	if _, hasOrphan := entry["spotify_present"]; hasOrphan {
		t.Errorf("native track must not emit spotify_present: %v", entry)
	}
	if entry["resolved"].(map[string]any)["youtube"] != "MV_3Dpw-BRY" {
		t.Errorf("resolved youtube id missing/incorrect: %v", entry)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/export/ -run TestJSPFExport_NativeOmitsOrphanState -v`
Expected: FAIL — track 0 emits an extension with `spotify_present:false`, and track 1's entry carries `spotify_present`. (The current code emits orphan state for both.)

- [ ] **Step 3: Write minimal implementation**

In `internal/export/jspf.go`, change the orphan block (currently `if !t.SyncState.SpotifyPresent {`) to gate on provenance:

```go
		if p.Source() == playlist.SourceSpotify && !t.SyncState.SpotifyPresent {
			absent := false
			ext.SpotifyPresent = &absent
			ext.DateOrphaned = t.SyncState.DateOrphaned
			hasExt = true
		}
```

Leave the YouTube-id block above it unchanged.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/export/ -run 'TestJSPFExport' -v`
Expected: PASS for both `TestJSPFExport` (existing Spotify playlist still emits orphan state) and `TestJSPFExport_NativeOmitsOrphanState`.

- [ ] **Step 5: Commit**

```bash
git add internal/export/jspf.go internal/export/export_test.go
git commit -m "fix(export): don't emit Spotify orphan state for native playlists

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Characterization test — sync never clobbers a native file

**Files:**
- Test: `internal/playlist/store_test.go`

**Interfaces:**
- Consumes: `playlist.Save` (existing), `playlist.LoadFile` (existing).
- Produces: nothing (test-only; locks an existing invariant).

**Context:** `sync` writes via `Save`, which matches an existing file by `SpotifyID` (native files have none) and otherwise calls `newFilePath`, which appends a `-<id>` suffix on a slug collision (`store.go:154`). So a first-time sync of a Spotify playlist whose title slugifies to the same name as a hand-authored file creates a *new* suffixed file and leaves the native one intact. This test documents and locks that behavior — it should **pass against the current code**. If it fails, that is a real regression to surface, not a test to weaken.

- [ ] **Step 1: Write the test**

Add to `internal/playlist/store_test.go`:

```go
func TestSave_DoesNotClobberNativeFileOnSlugCollision(t *testing.T) {
	dir := t.TempDir()

	// A hand-authored native playlist (no SpotifyID).
	nativePath, err := Save(dir, Playlist{
		Title:  "Late Night Drives",
		Tracks: []Track{{Title: "Nightcall", Artist: "Kavinsky"}},
	})
	if err != nil {
		t.Fatalf("save native: %v", err)
	}

	// A Spotify playlist that slugifies to the same base name, synced for the
	// first time.
	spotifyPath, err := Save(dir, Playlist{
		SpotifyID: "PID123",
		Title:     "Late Night Drives",
		Tracks:    []Track{{Title: "Something Else", Artist: "Someone", SyncState: SyncState{SpotifyPresent: true}}},
	})
	if err != nil {
		t.Fatalf("save spotify: %v", err)
	}

	if spotifyPath == nativePath {
		t.Fatalf("spotify playlist overwrote the native file at %q", nativePath)
	}

	// The native file must still be native (no SpotifyID) and unchanged.
	got, err := LoadFile(nativePath)
	if err != nil {
		t.Fatalf("reload native: %v", err)
	}
	if got.SpotifyID != "" {
		t.Errorf("native file gained a SpotifyID: %+v", got)
	}
	if !got.IsNative() {
		t.Errorf("native file is no longer native: %+v", got)
	}
	if len(got.Tracks) != 1 || got.Tracks[0].Title != "Nightcall" {
		t.Errorf("native file tracks changed: %+v", got.Tracks)
	}
}
```

- [ ] **Step 2: Run the test — expect PASS against current code**

Run: `go test ./internal/playlist/ -run TestSave_DoesNotClobberNativeFileOnSlugCollision -v`
Expected: PASS. (This is a characterization test; the invariant already holds.) If it FAILS, stop and surface it — do not modify the test to pass.

- [ ] **Step 3: Commit**

```bash
git add internal/playlist/store_test.go
git commit -m "test(playlist): lock that sync never overwrites a native file

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Document native playlists in AGENTS.md

**Files:**
- Modify: `AGENTS.md`

**Interfaces:**
- Consumes: nothing.
- Produces: documentation only.

**Context:** Phase 1 introduces the concept of a hub playlist that isn't Spotify-sourced. Document what one is and the derived-provenance rule, so future contributors (human or agent) route provenance checks through `Source()` instead of scattering `SpotifyID == ""`. Do **not** document enrichment or `resolve` subcommands here — those land in Phases 2–3.

- [ ] **Step 1: Add a "Native (hand-authored) playlists" subsection**

In `AGENTS.md`, under the "Conventions & gotchas" section (after the **Sync** bullet), add:

```markdown
- **Native playlists:** a hub file with no `spotify_id` is a hand-authored
  ("native") playlist — just `title`/`creator`/`tracks`, where each track needs
  only `title` and `artist` (`album` optional). Provenance is *derived*, never
  stored: use `playlist.Playlist.Source()` / `IsNative()` (source `native` when
  no source ID is set), not ad-hoc `spotify_id == ""` checks — this is the single
  extension point for future ingestion sources. `sync` never touches native files
  (it matches by `spotify_id`; slug collisions get a `-<id>` suffix). Spotify-only
  behavior (orphan/`sync_state` emission) is gated on `Source()`.
```

- [ ] **Step 2: Verify the doc reads correctly**

Run: `git diff AGENTS.md`
Expected: the new bullet appears under "Conventions & gotchas", well-formed Markdown, no other changes.

- [ ] **Step 3: Commit**

```bash
git add AGENTS.md
git commit -m "docs(agents): document native (hand-authored) playlists

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Final verification (after all tasks)

- [ ] **Run the full suite and build:**

Run: `make lint && make test && make build`
Expected: lint clean, all tests pass, build succeeds. Read the output — do not claim done without it.

---

## Self-Review

**Spec coverage (Phase 1 section of spec.md):**
- Authoring format → documented in Task 4; exercised in Task 2/3 tests (minimal native playlists load & export).
- Provenance helper (`Source`/`IsNative`) → Task 1.
- JSPF orphan-gating fix → Task 2.
- `sync` never touches native files → Task 3 (characterization test; no production guard needed because `Save`/`newFilePath` already protect native files — this is the honest smallest change, deviating from the spec's "add a guard" wording with a documented reason).
- Tests enumerated in the spec (round-trip, truth table, native JSPF omits orphan state, Spotify JSPF unchanged, sync leaves native untouched) → all covered across Tasks 1–3 plus the existing `TestPlaylist_YAMLRoundTrip`.

**Placeholder scan:** none — every step has concrete code/commands.

**Type consistency:** `Source`, `SourceSpotify`, `SourceNative`, `Source()`, `IsNative()` used identically in Tasks 1–3 and the AGENTS.md doc. JSPF extension namespace string matches `jspf.go`'s `byomExtNS`. Test helper `nativePlaylist()` (export pkg) does not collide with `store_test`'s inline literals.
