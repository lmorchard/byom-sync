# Recursive Hub, Recursive Export, `resolve all`, Headless Auth — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make hub discovery recursive everywhere, collapse the three-command enrichment pipeline into `byom-sync resolve all`, and let `byom-sync auth` complete on a headless host.

**Architecture:** One exported walker (`playlist.HubPaths`) becomes the single definition of "what files are in the hub," and the three places that currently glob one level deep (`cmd.hubPaths`, `export.Run`, `playlist.Load`) all delegate to it. `resolve all` is a thin sequencer over the existing `runResolve*` functions, fanning its flags out to the per-stage globals those functions already read and gating on an up-front prerequisite check. `auth --manual` reuses the existing PKCE machinery but swaps the local callback server for a pasted redirect URL.

**Tech Stack:** Go 1.25 · Cobra · Viper · logrus · `github.com/zmb3/spotify/v2` (+ `/v2/auth`) · `golang.org/x/oauth2` · `gopkg.in/yaml.v3`

**Spec:** `docs/dev-sessions/2026-07-26-1045-recursive-hub-and-enrich-all/spec.md`

## Global Constraints

- **Go toolchain is not on the default PATH.** Every `go` / `make` command in this plan requires `export PATH="$HOME/.local/go-toolchain/bin:$PATH"` first (Go 1.25.12 installed there). Verify with `go version`.
- **Branch:** `feat/recursive-hub-and-enrich-all` (already created, spec already committed).
- **Formatting:** `gofumpt` (not plain `gofmt`). Run `make format` before committing.
- **Linting:** golangci-lint pinned to **v2.12.2** in both `Makefile` and `.github/workflows/ci.yml`. **errcheck is strict** — every ignored return value needs an explicit `_ =` (e.g. `_ = fmt.Fprintln(...)`, `_ = viper.BindPFlag(...)`). This is the single most common CI failure in this repo.
- **Commands are Makefile-first:** `make test`, `make lint`, `make format`, `make build`. There is no `make check`.
- **Provenance is derived, never stored.** Use `playlist.Playlist.Source()` / `IsNative()`, never ad-hoc `spotify_id == ""` comparisons.
- **Comments explain why, not what.** Match the existing density in `cmd/resolve.go` and `internal/playlist/store.go` — non-obvious decisions get a sentence; mechanical code gets none.

## File Structure

| File | Responsibility | Change |
|---|---|---|
| `internal/playlist/store.go` | Canonical hub walk (`HubPaths`) + existing load/save | Modify |
| `internal/playlist/store_test.go` | Walker test matrix | Modify |
| `internal/export/export.go` | Recursive export with mirrored output tree | Modify |
| `internal/export/export_test.go` | Mirrored-tree tests | Modify |
| `internal/auth/manual.go` | Headless PKCE flow + pasted-redirect parsing | **Create** |
| `internal/auth/manual_test.go` | Paste-parsing tests | **Create** |
| `cmd/auth.go` | `--manual` flag wiring | Modify |
| `cmd/resolve.go` | `hubPaths` delegation; `resolve all`; preflight | Modify |
| `cmd/resolve_all_test.go` | Stage sequencing + preflight tests | **Create** |
| `cmd/hubpaths_test.go` | Delegation smoke test | **Create** |
| `README.md`, `AGENTS.md` | Document new behavior | Modify |

`resolve all` and the preflight helper live in `cmd/resolve.go` alongside the stages they sequence (~870 lines today, growing by ~120). Splitting the file is out of scope; the additions go at the end, before `init()`.

---

### Task 1: `playlist.HubPaths` — the canonical hub walk

**Files:**
- Modify: `internal/playlist/store.go:31-49` (the `Load` function and the area above it)
- Test: `internal/playlist/store_test.go`

**Interfaces:**
- Consumes: nothing (this is the base layer)
- Produces: `func HubPaths(input string) ([]string, error)` — returns sorted absolute-or-relative paths (whatever form `input` was given in) of every `*.yaml` under `input`, recursively. A non-directory `input` returns `[]string{input}`. Errors when `input` does not exist.

- [ ] **Step 1: Write the failing tests**

Add to `internal/playlist/store_test.go`:

```go
// mkfile creates path (with parents) holding minimal valid playlist YAML.
func mkfile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("title: x\ntracks: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHubPaths(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, "root.yaml"))
	mkfile(t, filepath.Join(dir, "01-covers", "beta.yaml"))
	mkfile(t, filepath.Join(dir, "01-covers", "alpha.yaml"))
	mkfile(t, filepath.Join(dir, "00-conceptual", "deep", "nested.yaml"))
	// The hub-root cover-art store holds images, not playlists.
	mkfile(t, filepath.Join(dir, "art", "stray.yaml"))
	// A nested dir that merely happens to be named "art" is NOT the store.
	mkfile(t, filepath.Join(dir, "01-covers", "art", "kept.yaml"))
	// Editor/VCS cruft and macOS AppleDouble sidecars.
	mkfile(t, filepath.Join(dir, ".hidden.yaml"))
	mkfile(t, filepath.Join(dir, "01-covers", "._beta.yaml"))
	mkfile(t, filepath.Join(dir, ".git", "config.yaml"))
	// Non-YAML is ignored.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := HubPaths(dir)
	if err != nil {
		t.Fatalf("HubPaths: %v", err)
	}

	want := []string{
		filepath.Join(dir, "00-conceptual", "deep", "nested.yaml"),
		filepath.Join(dir, "01-covers", "alpha.yaml"),
		filepath.Join(dir, "01-covers", "art", "kept.yaml"),
		filepath.Join(dir, "01-covers", "beta.yaml"),
		filepath.Join(dir, "root.yaml"),
	}
	if len(got) != len(want) {
		t.Fatalf("got %d paths, want %d:\ngot:  %v\nwant: %v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("path %d: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestHubPaths_SingleFileReturnedAsIs(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "one.yaml")
	mkfile(t, f)

	got, err := HubPaths(f)
	if err != nil {
		t.Fatalf("HubPaths: %v", err)
	}
	if len(got) != 1 || got[0] != f {
		t.Errorf("got %v, want [%s]", got, f)
	}
}

func TestHubPaths_EmptyDirIsNotAnError(t *testing.T) {
	got, err := HubPaths(t.TempDir())
	if err != nil {
		t.Fatalf("HubPaths: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestHubPaths_MissingInputErrors(t *testing.T) {
	if _, err := HubPaths(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("expected an error for a missing input path")
	}
}

// Load keeps its documented contract: a missing hub dir is empty, not an error
// (the first sync creates it).
func TestLoad_MissingDirYieldsEmpty(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "not-created-yet"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestLoad_IsRecursive(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, "sub", "one.yaml"))
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d playlists, want 1", len(got))
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
export PATH="$HOME/.local/go-toolchain/bin:$PATH"
go test ./internal/playlist/ -run 'TestHubPaths|TestLoad_' -v
```

Expected: FAIL — `undefined: HubPaths`.

- [ ] **Step 3: Implement `HubPaths` and make `Load` delegate**

In `internal/playlist/store.go`, add `HubPaths` immediately above `Load`, and replace the body of `Load`. Add `"io/fs"` to the imports; `errors`, `os`, `path/filepath`, `sort`, `strings`, and `fmt` are already there or needed.

```go
// HubPaths returns the path of every playlist YAML under input, recursively.
// A non-directory input is returned unchanged as a single-element slice.
//
// The walk deliberately mirrors the site generator's view of the hub
// (internal/site/tree.go), so the resolvers, the exporters, and the site build
// never disagree about which files are playlists:
//
//   - Entries whose name begins with "." are skipped — editor/VCS cruft and
//     macOS AppleDouble sidecars ("._foo.yaml"), which would otherwise be
//     parsed as playlists and fail on their binary contents.
//   - A top-level "art" directory is skipped: it is the content-addressed
//     cover-art store written by `resolve art --download`, not playlist
//     content. Only the store at the hub root is special; a nested directory
//     that happens to be named "art" is walked normally.
//
// Results are sorted so runs are deterministic regardless of directory order.
func HubPaths(input string) ([]string, error) {
	info, err := os.Stat(input)
	if err != nil {
		return nil, fmt.Errorf("input %s: %w", input, err)
	}
	if !info.IsDir() {
		return []string{input}, nil
	}

	// Clean once so the "is this the hub root?" comparisons below hold even
	// when the caller passed a trailing slash.
	root := filepath.Clean(input)

	var paths []string
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil // never skip the root on its own name
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if d.Name() == "art" && filepath.Dir(p) == root {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".yaml") {
			paths = append(paths, p)
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Strings(paths)
	return paths, nil
}
```

Then replace `Load`'s body (keeping its existing doc comment, which already promises the missing-dir behavior):

```go
// Load reads every playlist YAML under dir, recursively, into a slice. A
// missing directory yields an empty slice (not an error) — the first sync
// creates it.
func Load(dir string) ([]Playlist, error) {
	paths, err := HubPaths(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var playlists []Playlist
	for _, path := range paths {
		p, err := loadFile(path)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", path, err)
		}
		playlists = append(playlists, p)
	}
	return playlists, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
export PATH="$HOME/.local/go-toolchain/bin:$PATH"
go test ./internal/playlist/ -v
```

Expected: PASS, including the pre-existing `TestSaveLoad_RoundTrip` and `TestSave_*` tests.

- [ ] **Step 5: Format, lint, full test sweep**

```bash
export PATH="$HOME/.local/go-toolchain/bin:$PATH"
make format && make lint && make test
```

Expected: clean. If `golangci-lint` isn't installed yet, run `make setup` first.

- [ ] **Step 6: Commit**

```bash
git add internal/playlist/store.go internal/playlist/store_test.go
git commit -m "feat(playlist): HubPaths — one recursive definition of the hub

hubPaths(), export.Run(), and Load() each globbed <dir>/*.yaml one level
deep, so a hub with playlists in subdirectories looked empty. Add the
canonical recursive walk (skipping dotfiles and the root art store, as
internal/site/tree.go already does) and make Load delegate to it.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: `cmd.hubPaths` delegates to the canonical walker

**Files:**
- Modify: `cmd/resolve.go:808-821` (the `hubPaths` function)
- Test: `cmd/hubpaths_test.go` (create)

**Interfaces:**
- Consumes: `playlist.HubPaths(input string) ([]string, error)` from Task 1
- Produces: `cmd.hubPaths` keeps its existing signature, so `runResolveYouTube`, `runResolveArt`, `runResolveSpotify`, `resolve prime`'s `RunE`, and `runDates` need no changes.

- [ ] **Step 1: Write the failing test**

Create `cmd/hubpaths_test.go`:

```go
package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// hubPaths must see playlists filed in subdirectories — the mixtapes hub keeps
// every playlist under playlists/<section>/, and a shallow glob silently found
// none of them.
func TestHubPaths_FindsNestedPlaylists(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "00-conceptual", "drones.yaml")
	if err := os.MkdirAll(filepath.Dir(nested), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nested, []byte("title: Drones\ntracks: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := hubPaths(dir)
	if err != nil {
		t.Fatalf("hubPaths: %v", err)
	}
	if len(got) != 1 || got[0] != nested {
		t.Errorf("got %v, want [%s]", got, nested)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
export PATH="$HOME/.local/go-toolchain/bin:$PATH"
go test ./cmd/ -run TestHubPaths_FindsNestedPlaylists -v
```

Expected: FAIL — `got [], want [.../drones.yaml]`.

- [ ] **Step 3: Replace the glob with a delegation**

In `cmd/resolve.go`, replace the whole `hubPaths` function (currently lines 808-821):

```go
// hubPaths returns every playlist YAML under input, recursively. Thin
// delegation to playlist.HubPaths so the resolvers, the exporters, and the site
// generator share one definition of the hub.
func hubPaths(input string) ([]string, error) {
	return playlist.HubPaths(input)
}
```

`playlist` is already imported in `cmd/resolve.go`. Check whether `path/filepath` is now unused in that file — it is still used by `defaultCachePath` and `filepath.Base` calls in the resolve loops, so leave the import.

- [ ] **Step 4: Run the tests to verify they pass**

```bash
export PATH="$HOME/.local/go-toolchain/bin:$PATH"
go test ./cmd/ -v
```

Expected: PASS.

- [ ] **Step 5: Verify against the real nested hub**

This is the actual regression, so confirm it with real data rather than only a temp dir:

```bash
export PATH="$HOME/.local/go-toolchain/bin:$PATH"
make build
./byom-sync dates --input /home/lmorchard/devel/mixtapes/mixtapes.lmorchard.com/playlists
```

Expected: `refreshed dates across 58 file(s)`. Before this change it printed `no playlist YAML files found`. (`dates` is idempotent and the only writes are date normalizations, so this is safe to run against the real hub. If it reports 58 files, `git -C ../mixtapes.lmorchard.com diff --stat` to see whether anything actually changed; revert with `git -C ../mixtapes.lmorchard.com checkout playlists/` if you'd rather leave it untouched for now.)

- [ ] **Step 6: Commit**

```bash
export PATH="$HOME/.local/go-toolchain/bin:$PATH"
make format && make lint && make test
git add cmd/resolve.go cmd/hubpaths_test.go
git commit -m "fix(resolve): find playlists in hub subdirectories

hubPaths() globbed <input>/*.yaml one level deep, so resolve youtube,
resolve spotify, resolve art, resolve prime, and dates all exited
successfully having done nothing on a hub whose playlists live in
subdirectories. Delegate to playlist.HubPaths.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Recursive `export` with a mirrored output tree

**Files:**
- Modify: `internal/export/export.go:19-53` (the `Run` function)
- Test: `internal/export/export_test.go`

**Interfaces:**
- Consumes: `playlist.HubPaths` from Task 1
- Produces: `export.Run` keeps its signature `Run(e Exporter, ext, input, out string, cfg map[string]string) error`. Directory mode now writes `out/<rel-path-of-input>.<ext>`, creating intermediate directories.

- [ ] **Step 1: Write the failing tests**

Add to `internal/export/export_test.go`:

```go
func TestRun_DirModeMirrorsHubTree(t *testing.T) {
	inDir := t.TempDir()
	outDir := t.TempDir()

	// Same basename in two different folders: under the old flat behavior
	// these would have collided and one would have silently won.
	for _, rel := range []string{
		filepath.Join("00-conceptual", "drones.yaml"),
		filepath.Join("zz-not-mine", "drones.yaml"),
		filepath.Join("01-covers", "numan-s-shadow.yaml"),
	} {
		path := filepath.Join(inDir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		p := samplePlaylist()
		p.Title = rel
		if err := os.WriteFile(path, mustYAML(t, p), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := Run(M3U8Exporter{}, "m3u8", inDir, outDir, map[string]string{"lib_prefix": "/m"}); err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{
		filepath.Join("00-conceptual", "drones.m3u8"),
		filepath.Join("zz-not-mine", "drones.m3u8"),
		filepath.Join("01-covers", "numan-s-shadow.m3u8"),
	} {
		if _, err := os.Stat(filepath.Join(outDir, rel)); err != nil {
			t.Errorf("expected mirrored output %s: %v", rel, err)
		}
	}
}

// A hub with no subdirectories must export exactly as it always has.
func TestRun_DirModeFlatHubUnchanged(t *testing.T) {
	inDir := t.TempDir()
	outDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(inDir, "solo.yaml"), mustYAML(t, samplePlaylist()), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Run(M3U8Exporter{}, "m3u8", inDir, outDir, map[string]string{"lib_prefix": "/m"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "solo.m3u8")); err != nil {
		t.Errorf("flat hub should produce out/solo.m3u8 with no subdirectory: %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
export PATH="$HOME/.local/go-toolchain/bin:$PATH"
go test ./internal/export/ -run TestRun_Dir -v
```

Expected: `TestRun_DirModeMirrorsHubTree` FAILS (no output files at all — the shallow glob matched nothing); `TestRun_DirModeFlatHubUnchanged` passes already.

- [ ] **Step 3: Make `Run` recursive and mirror the tree**

Replace the directory branch of `Run` in `internal/export/export.go`. The full function becomes:

```go
// Run dispatches input to e. When input is a directory, every playlist YAML
// under it is exported recursively and the hub's structure is mirrored beneath
// out — "00-conceptual/drones.yaml" becomes "<out>/00-conceptual/drones.<ext>".
// Mirroring (rather than flattening) means two playlists with the same
// basename in different folders can't overwrite each other. When input is a
// single file, out is the exact output path.
func Run(e Exporter, ext, input, out string, cfg map[string]string) error {
	info, err := os.Stat(input)
	if err != nil {
		return fmt.Errorf("input %s: %w", input, err)
	}

	if !info.IsDir() {
		p, err := playlist.LoadFile(input)
		if err != nil {
			return fmt.Errorf("load %s: %w", input, err)
		}
		return e.Export(p, out, cfg)
	}

	paths, err := playlist.HubPaths(input)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	for _, path := range paths {
		p, err := playlist.LoadFile(path)
		if err != nil {
			return fmt.Errorf("load %s: %w", path, err)
		}
		rel, err := filepath.Rel(input, path)
		if err != nil {
			return fmt.Errorf("relativize %s: %w", path, err)
		}
		outPath := filepath.Join(out, strings.TrimSuffix(rel, filepath.Ext(rel))+"."+ext)
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return err
		}
		if err := e.Export(p, outPath, cfg); err != nil {
			return fmt.Errorf("export %s: %w", path, err)
		}
	}
	return nil
}
```

`strings` and `filepath` are already imported in this file.

- [ ] **Step 4: Run the tests to verify they pass**

```bash
export PATH="$HOME/.local/go-toolchain/bin:$PATH"
go test ./internal/export/ -v
```

Expected: PASS, including the pre-existing `TestRun_DirModeWritesFilePerInput` and `TestRun_FileModeSingleOutput`.

- [ ] **Step 5: Commit**

```bash
export PATH="$HOME/.local/go-toolchain/bin:$PATH"
make format && make lint && make test
git add internal/export/export.go internal/export/export_test.go
git commit -m "fix(export): recurse into hub subdirectories, mirroring the tree

export.Run globbed one level deep, so exporting a nested hub wrote
nothing. Walk via playlist.HubPaths and reproduce the hub's structure
under --out, so same-named playlists in different folders can't collide.
A flat hub exports byte-identically to before.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Headless PKCE flow (`internal/auth`)

**Files:**
- Create: `internal/auth/manual.go`
- Test: `internal/auth/manual_test.go` (create)

**Interfaces:**
- Consumes: existing unexported `newAuthenticator(clientID string, port int) *spotifyauth.Authenticator` and `randomState() (string, error)` from `internal/auth/auth.go`; existing `SaveToken(*oauth2.Token) error` from `internal/auth/store.go`; `RedirectURL(port int) string`.
- Produces:
  - `func ParseManualRedirect(pasted, wantState string) (string, error)` — extracts the authorization code from a pasted redirect URL or a bare code.
  - `func RunManualFlow(ctx context.Context, clientID string, port int, in io.Reader, out io.Writer) error`

Note: `spotifyauth.Authenticator` has `Exchange(ctx context.Context, code string, opts ...oauth2.AuthCodeOption) (*oauth2.Token, error)` (value receiver), so no synthetic `*http.Request` is needed.

- [ ] **Step 1: Write the failing tests**

Create `internal/auth/manual_test.go`:

```go
package auth

import "testing"

func TestParseManualRedirect(t *testing.T) {
	const state = "abc123"

	tests := []struct {
		name    string
		pasted  string
		want    string
		wantErr bool
	}{
		{
			name:   "full redirect URL",
			pasted: "http://127.0.0.1:8888/callback?code=THECODE&state=abc123",
			want:   "THECODE",
		},
		{
			name:   "surrounding whitespace and newline",
			pasted: "  http://127.0.0.1:8888/callback?code=THECODE&state=abc123  \n",
			want:   "THECODE",
		},
		{
			name:   "bare code",
			pasted: "THECODE\n",
			want:   "THECODE",
		},
		{
			name:    "state mismatch is rejected",
			pasted:  "http://127.0.0.1:8888/callback?code=THECODE&state=wrong",
			wantErr: true,
		},
		{
			name:    "authorization denied is surfaced",
			pasted:  "http://127.0.0.1:8888/callback?error=access_denied&state=abc123",
			wantErr: true,
		},
		{
			name:    "URL with no code",
			pasted:  "http://127.0.0.1:8888/callback?state=abc123",
			wantErr: true,
		},
		{
			name:    "empty input",
			pasted:  "   \n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseManualRedirect(tt.pasted, state)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got code %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q want %q", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
export PATH="$HOME/.local/go-toolchain/bin:$PATH"
go test ./internal/auth/ -run TestParseManualRedirect -v
```

Expected: FAIL — `undefined: ParseManualRedirect`.

- [ ] **Step 3: Implement the manual flow**

Create `internal/auth/manual.go`:

```go
package auth

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"golang.org/x/oauth2"
)

// ParseManualRedirect extracts the authorization code from whatever the user
// pasted after authorizing in a browser. It accepts either the full redirect
// URL (whose query carries both code and state) or a bare code value, because
// copying a whole address bar is fiddly and users often grab just the code.
//
// When a URL is pasted, its state must equal wantState — that check is the CSRF
// defense the local callback server performs in the interactive flow. A bare
// code carries no state to verify, which is an accepted trade-off for a value
// the user has just copied out of their own browser.
func ParseManualRedirect(pasted, wantState string) (string, error) {
	s := strings.TrimSpace(pasted)
	if s == "" {
		return "", errors.New("nothing pasted — expected the redirect URL or the code from it")
	}

	// A bare code has no scheme and no query string.
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") && !strings.Contains(s, "?") {
		return s, nil
	}

	u, err := url.Parse(s)
	if err != nil {
		return "", fmt.Errorf("parse pasted URL: %w", err)
	}
	q := u.Query()

	if e := q.Get("error"); e != "" {
		if desc := q.Get("error_description"); desc != "" {
			return "", fmt.Errorf("authorization denied: %s: %s", e, desc)
		}
		return "", fmt.Errorf("authorization denied: %s", e)
	}
	if got := q.Get("state"); got != wantState {
		return "", fmt.Errorf("state mismatch (possible CSRF): pasted URL carried state %q", got)
	}
	code := q.Get("code")
	if code == "" {
		return "", errors.New("pasted URL has no code parameter")
	}
	return code, nil
}

// RunManualFlow performs the authorization-code + PKCE flow without a local
// callback server, for headless or remote hosts.
//
// Spotify requires the redirect URI to be exactly http://127.0.0.1:<port>/callback
// and matches it as a literal string, so it cannot be pointed at a remote host.
// Over SSH that means the consent page redirects the *local* browser to the
// *local* machine's port, and the code never reaches the machine running this
// command. Here the user relays the code by hand instead.
func RunManualFlow(ctx context.Context, clientID string, port int, in io.Reader, out io.Writer) error {
	if clientID == "" {
		return errors.New("client_id is not set (see byom-sync.yaml.example)")
	}

	verifier := oauth2.GenerateVerifier()
	state, err := randomState()
	if err != nil {
		return err
	}
	authr := newAuthenticator(clientID, port)
	authURL := authr.AuthURL(state, oauth2.S256ChallengeOption(verifier))

	_, _ = fmt.Fprintf(out, `Open this URL in a browser on any machine and authorize:

%s

Your browser will then fail to load %s — that is expected, and the
address bar still holds the code.

Paste that full URL here (or just the code): `, authURL, RedirectURL(port))

	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read pasted input: %w", err)
	}
	_, _ = fmt.Fprintln(out)

	code, err := ParseManualRedirect(line, state)
	if err != nil {
		return err
	}

	tok, err := authr.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return fmt.Errorf("token exchange: %w", err)
	}
	return SaveToken(tok)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
export PATH="$HOME/.local/go-toolchain/bin:$PATH"
go test ./internal/auth/ -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
export PATH="$HOME/.local/go-toolchain/bin:$PATH"
make format && make lint && make test
git add internal/auth/manual.go internal/auth/manual_test.go
git commit -m "feat(auth): headless PKCE flow via pasted redirect

Spotify pins the redirect URI to 127.0.0.1, so over SSH the consent page
redirects the local browser to the local machine and the code never
reaches the remote host running the command. Add a flow that prints the
consent URL and takes the code back by hand.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Wire `auth --manual`

**Files:**
- Modify: `cmd/auth.go`

**Interfaces:**
- Consumes: `auth.RunManualFlow(ctx, clientID, port, in, out)` from Task 4
- Produces: `byom-sync auth --manual`

- [ ] **Step 1: Add the flag and branch**

Replace the body of `cmd/auth.go` (keeping the package clause). Add `"os"` to the imports:

```go
var authManual bool

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authenticate with Spotify (PKCE OAuth) and cache a token",
	Long: `Run the Spotify authorization-code + PKCE flow.

Opens your browser to Spotify's consent page, captures the redirect on a local
callback server, and caches the resulting token so later commands can refresh
it silently.

On a headless or remote host, pass --manual: byom-sync prints the consent URL
and asks you to paste the redirect back, with no local callback server. This is
needed over SSH because Spotify pins the redirect URI to 127.0.0.1, so the
browser would otherwise deliver the code to the wrong machine. Alternatively,
forward the port before connecting: ssh -L 8888:127.0.0.1:8888 <host>.

Requires client_id in the config and a matching redirect URI registered on your
Spotify application (default: http://127.0.0.1:8888/callback).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		clientID := viper.GetString("client_id")
		port := viper.GetInt("redirect_port")

		fmt.Printf("Using redirect URI: %s\n", auth.RedirectURL(port))

		var err error
		if authManual {
			err = auth.RunManualFlow(context.Background(), clientID, port, os.Stdin, os.Stdout)
		} else {
			err = auth.RunInteractiveFlow(context.Background(), clientID, port)
		}
		if err != nil {
			return err
		}
		fmt.Println("✅ Authentication successful. Token cached.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(authCmd)
	authCmd.Flags().BoolVar(&authManual, "manual", false, "headless flow: print the consent URL and paste the redirect back (no local callback server)")
}
```

- [ ] **Step 2: Verify it builds and the flag is registered**

```bash
export PATH="$HOME/.local/go-toolchain/bin:$PATH"
make build
./byom-sync auth --help
```

Expected: help text shows `--manual` and the SSH port-forward hint.

- [ ] **Step 3: Run the full test suite**

```bash
export PATH="$HOME/.local/go-toolchain/bin:$PATH"
make format && make lint && make test
```

Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add cmd/auth.go
git commit -m "feat(auth): --manual flag for headless hosts

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Prerequisite preflight helper

**Files:**
- Modify: `cmd/resolve.go` (append before `init()`)
- Test: `cmd/resolve_all_test.go` (create)

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `type prereq struct { name string; check func() error; remedy string }`
  - `func checkPrereqs(reqs []prereq) error` — runs every check and returns one error naming all failures, or nil.

- [ ] **Step 1: Write the failing test**

Create `cmd/resolve_all_test.go`:

```go
package cmd

import (
	"errors"
	"strings"
	"testing"
)

func TestCheckPrereqs_AllPresent(t *testing.T) {
	err := checkPrereqs([]prereq{
		{name: "one", check: func() error { return nil }, remedy: "n/a"},
		{name: "two", check: func() error { return nil }, remedy: "n/a"},
	})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

// A user missing two prerequisites should learn about both from one run,
// rather than fixing one and immediately hitting the other.
func TestCheckPrereqs_ReportsEveryFailure(t *testing.T) {
	err := checkPrereqs([]prereq{
		{name: "Spotify token", check: func() error { return errors.New("no token stored") }, remedy: "run `byom-sync auth`"},
		{name: "ok thing", check: func() error { return nil }, remedy: "n/a"},
		{name: "yt-dlp", check: func() error { return errors.New("not found in PATH") }, remedy: "install yt-dlp"},
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	for _, want := range []string{"Spotify token", "no token stored", "run `byom-sync auth`", "yt-dlp", "not found in PATH", "install yt-dlp"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error should mention %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "ok thing") {
		t.Errorf("error should not mention satisfied prerequisites:\n%s", msg)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
export PATH="$HOME/.local/go-toolchain/bin:$PATH"
go test ./cmd/ -run TestCheckPrereqs -v
```

Expected: FAIL — `undefined: checkPrereqs`, `undefined: prereq`.

- [ ] **Step 3: Implement the helper**

Append to `cmd/resolve.go`, immediately before `func init()`:

```go
// prereq is one external requirement of an enrichment stage: a name for the
// message, a cheap local check, and what the user should do about it.
type prereq struct {
	name   string
	check  func() error
	remedy string
}

// checkPrereqs runs every check and reports all failures in a single error.
// Deliberately not fail-fast: someone missing both a Spotify token and yt-dlp
// should learn that once, not discover the second after fixing the first.
func checkPrereqs(reqs []prereq) error {
	var missing []string
	for _, r := range reqs {
		if err := r.check(); err != nil {
			missing = append(missing, fmt.Sprintf("  - %s: %v\n    fix: %s", r.name, err, r.remedy))
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("missing prerequisites:\n%s", strings.Join(missing, "\n"))
}
```

Add `"strings"` to `cmd/resolve.go`'s imports if it isn't already present.

- [ ] **Step 4: Run the tests to verify they pass**

```bash
export PATH="$HOME/.local/go-toolchain/bin:$PATH"
go test ./cmd/ -run TestCheckPrereqs -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
export PATH="$HOME/.local/go-toolchain/bin:$PATH"
make format && make lint && make test
git add cmd/resolve.go cmd/resolve_all_test.go
git commit -m "feat(resolve): prerequisite preflight helper

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: `byom-sync resolve all`

**Files:**
- Modify: `cmd/resolve.go` (flag vars near the top; command + helpers before `init()`; registration inside `init()`)
- Test: `cmd/resolve_all_test.go` (extend)

**Interfaces:**
- Consumes: `prereq`, `checkPrereqs` (Task 6); existing `runResolveSpotify`, `runResolveArt`, `runResolveYouTube`, all `func(context.Context) error`; existing globals `resolveInput/artInput/enrichInput`, `resolveLimit/artLimit/enrichLimit`, `resolveNoCache/artNoCache/enrichNoCache`, `resolveDelay/artDelay/enrichDelay`, `artDownload`.
- Produces:
  - `type stage struct { name string; run func(context.Context) error }`
  - `func resolveAllStages(skipSpotify, skipArt, skipYouTube bool) []stage`
  - `func resolveAllPrereqs(stages []stage) []prereq`
  - `func runStages(ctx context.Context, stages []stage) error`
  - `func runResolveAll(ctx context.Context, cmd *cobra.Command) error`

- [ ] **Step 1: Write the failing tests**

Add `"context"` to `cmd/resolve_all_test.go`'s import block (it already has
`errors`, `strings`, `testing` from Task 6), then add:

```go
func stageNames(stages []stage) []string {
	names := make([]string, 0, len(stages))
	for _, s := range stages {
		names = append(names, s.name)
	}
	return names
}

// Order is a data dependency, not a preference: resolve spotify writes the
// ISRCs that the art and youtube stages use as their cache identity.
func TestResolveAllStages_DependencyOrder(t *testing.T) {
	got := stageNames(resolveAllStages(false, false, false))
	want := []string{"spotify", "art", "youtube"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestResolveAllStages_SkipFlags(t *testing.T) {
	tests := []struct {
		name                          string
		skipSpotify, skipArt, skipYT  bool
		want                          []string
	}{
		{"skip spotify", true, false, false, []string{"art", "youtube"}},
		{"skip art", false, true, false, []string{"spotify", "youtube"}},
		{"skip youtube", false, false, true, []string{"spotify", "art"}},
		{"skip all", true, true, true, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stageNames(resolveAllStages(tt.skipSpotify, tt.skipArt, tt.skipYT))
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("got %v want %v", got, tt.want)
			}
		})
	}
}

func prereqNames(reqs []prereq) []string {
	names := make([]string, 0, len(reqs))
	for _, r := range reqs {
		names = append(names, r.name)
	}
	return names
}

// A failing stage must abort the pipeline rather than pressing on: the later
// stages depend on data the failed one was supposed to write.
func TestRunStages_AbortsOnFirstError(t *testing.T) {
	var ran []string
	boom := errors.New("stage exploded")
	stages := []stage{
		{name: "first", run: func(context.Context) error { ran = append(ran, "first"); return nil }},
		{name: "second", run: func(context.Context) error { ran = append(ran, "second"); return boom }},
		{name: "third", run: func(context.Context) error { ran = append(ran, "third"); return nil }},
	}

	err := runStages(context.Background(), stages)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error should wrap the stage's own error, got %v", err)
	}
	if !strings.Contains(err.Error(), "second") {
		t.Errorf("error should name the failing stage:\n%s", err)
	}
	if strings.Join(ran, ",") != "first,second" {
		t.Errorf("third stage should not have run, got %v", ran)
	}
}

func TestRunStages_RunsAllInOrder(t *testing.T) {
	var ran []string
	stages := []stage{
		{name: "a", run: func(context.Context) error { ran = append(ran, "a"); return nil }},
		{name: "b", run: func(context.Context) error { ran = append(ran, "b"); return nil }},
	}
	if err := runStages(context.Background(), stages); err != nil {
		t.Fatalf("runStages: %v", err)
	}
	if strings.Join(ran, ",") != "a,b" {
		t.Errorf("got %v want [a b]", ran)
	}
}

// Skipped stages must not have their prerequisites checked — --skip-youtube is
// exactly how you run this on a box with no yt-dlp.
func TestResolveAllPrereqs_OnlyForEnabledStages(t *testing.T) {
	tests := []struct {
		name   string
		stages []stage
		want   []string
	}{
		{"all stages", resolveAllStages(false, false, false), []string{"Spotify token", "yt-dlp"}},
		{"no youtube", resolveAllStages(false, false, true), []string{"Spotify token"}},
		{"youtube only", resolveAllStages(true, true, false), []string{"yt-dlp"}},
		{"art only still needs a token", resolveAllStages(true, false, true), []string{"Spotify token"}},
		{"nothing", resolveAllStages(true, true, true), []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := prereqNames(resolveAllPrereqs(tt.stages))
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("got %v want %v", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
export PATH="$HOME/.local/go-toolchain/bin:$PATH"
go test ./cmd/ -run TestResolveAll -v
```

Expected: FAIL — `undefined: resolveAllStages`, `undefined: stage`.

- [ ] **Step 3: Add the flag vars**

In `cmd/resolve.go`, after the existing `enrich*` var block (around line 48), add:

```go
var (
	allInput       string
	allLimit       int
	allDelay       time.Duration
	allNoCache     bool
	allDownload    bool
	allSkipSpotify bool
	allSkipArt     bool
	allSkipYouTube bool
)
```

- [ ] **Step 4: Implement the pipeline**

Append to `cmd/resolve.go`, before `func init()` (after the `checkPrereqs` helper from Task 6):

```go
// stage is one step of the resolve-all pipeline, named for logging and for the
// prerequisite mapping.
type stage struct {
	name string
	run  func(context.Context) error
}

// resolveAllStages returns the enabled stages in dependency order. The order is
// a data dependency, not a preference: `resolve spotify` writes the ISRCs that
// the art and youtube stages use as their cache identity (Track.Key()).
func resolveAllStages(skipSpotify, skipArt, skipYouTube bool) []stage {
	stages := make([]stage, 0, 3)
	if !skipSpotify {
		stages = append(stages, stage{name: "spotify", run: runResolveSpotify})
	}
	if !skipArt {
		stages = append(stages, stage{name: "art", run: runResolveArt})
	}
	if !skipYouTube {
		stages = append(stages, stage{name: "youtube", run: runResolveYouTube})
	}
	return stages
}

// resolveAllPrereqs returns the external requirements of the enabled stages,
// deduplicated (spotify and art both need the same token).
func resolveAllPrereqs(stages []stage) []prereq {
	var needToken, needYtdlp bool
	for _, s := range stages {
		switch s.name {
		case "spotify", "art":
			needToken = true
		case "youtube":
			needYtdlp = true
		}
	}

	reqs := make([]prereq, 0, 2)
	if needToken {
		reqs = append(reqs, prereq{
			name:   "Spotify token",
			check:  func() error { _, err := auth.LoadToken(); return err },
			remedy: "run `byom-sync auth` (or `byom-sync auth --manual` on a headless host)",
		})
	}
	if needYtdlp {
		bin := viper.GetString("ytdlp_path")
		if bin == "" {
			bin = "yt-dlp"
		}
		reqs = append(reqs, prereq{
			name:   "yt-dlp",
			check:  func() error { _, err := exec.LookPath(bin); return err },
			remedy: "install yt-dlp (https://github.com/yt-dlp/yt-dlp) or set ytdlp_path",
		})
	}
	return reqs
}

var resolveAllCmd = &cobra.Command{
	Use:   "all",
	Short: "Run the full enrichment pipeline: spotify, then art, then youtube",
	Long: `Run every enrichment stage over the hub in dependency order:

  1. resolve spotify  — isrc, spotify_id, spotify_url, duration_ms, album, image
  2. resolve art      — cover art, downloaded into <hub>/art by default
  3. resolve youtube  — a playable youtube_id per track

The order matters: the spotify stage writes the ISRCs that the art and youtube
stages use as their cache identity, so running them in this sequence reuses work
instead of repeating it.

Prerequisites for every enabled stage are checked before any work starts, so a
missing yt-dlp is reported immediately rather than after a long art crawl. Use
--skip-youtube on a host without yt-dlp (its prerequisite is then not checked).

Unlike the individual commands, a missing Spotify token is fatal here rather
than degrading to MusicBrainz-only art: running the full pipeline implies you
want the Spotify data. Run 'resolve art' on its own for the degrading behavior.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runResolveAll(context.Background(), cmd)
	},
}

func runResolveAll(ctx context.Context, cmd *cobra.Command) error {
	input := allInput
	if input == "" {
		input = viper.GetString("dir")
	}

	// Fan this command's flags out to the per-stage globals the run functions
	// read. resolveNoCache is shared state that both runResolveArt and
	// runResolveSpotify assign to (see their openCache calls), so driving all
	// three cache flags from one value keeps the stages consistent instead of
	// letting whichever ran last decide for the youtube stage.
	resolveInput, artInput, enrichInput = input, input, input
	resolveLimit, artLimit, enrichLimit = allLimit, allLimit, allLimit
	resolveNoCache, artNoCache, enrichNoCache = allNoCache, allNoCache, allNoCache
	artDownload = allDownload

	// Each stage has a different sensible pace (youtube 500ms, spotify 200ms,
	// art 1100ms for MusicBrainz's ~1 req/sec policy), so only override them
	// when the user actually passed --delay.
	if cmd.Flags().Changed("delay") {
		resolveDelay, artDelay, enrichDelay = allDelay, allDelay, allDelay
	}

	stages := resolveAllStages(allSkipSpotify, allSkipArt, allSkipYouTube)
	if len(stages) == 0 {
		return fmt.Errorf("every stage skipped — nothing to do")
	}
	if err := checkPrereqs(resolveAllPrereqs(stages)); err != nil {
		return err
	}
	if err := runStages(ctx, stages); err != nil {
		return err
	}
	log.Infof("resolve all: %d stage(s) complete over %s", len(stages), input)
	return nil
}

// runStages executes stages in order, aborting on the first failure — the later
// stages consume data the failed one was supposed to write, so continuing would
// just produce confusing downstream errors. Split out from runResolveAll so the
// sequencing is testable with fake stages, without network or credentials.
func runStages(ctx context.Context, stages []stage) error {
	for i, s := range stages {
		log.Infof("resolve all [%d/%d] %s: starting", i+1, len(stages), s.name)
		if err := s.run(ctx); err != nil {
			return fmt.Errorf("%s stage: %w", s.name, err)
		}
	}
	return nil
}
```

`auth`, `exec`, `context`, `viper`, and `cobra` are already imported in `cmd/resolve.go`.

- [ ] **Step 5: Register the command**

Inside `func init()` in `cmd/resolve.go`, after the `resolvePrimeCmd` block:

```go
	resolveCmd.AddCommand(resolveAllCmd)
	resolveAllCmd.Flags().StringVar(&allInput, "input", "", "hub YAML file or directory (default: config dir)")
	resolveAllCmd.Flags().IntVar(&allLimit, "limit", 0, "max tracks attempted per stage (0 = unlimited)")
	resolveAllCmd.Flags().DurationVar(&allDelay, "delay", 0, "override every stage's request pacing (default: each stage's own)")
	resolveAllCmd.Flags().BoolVar(&allNoCache, "no-cache", false, "bypass the resolution caches for every stage")
	resolveAllCmd.Flags().BoolVar(&allDownload, "download", true, "download cover art into <hub>/art and record image_file")
	resolveAllCmd.Flags().BoolVar(&allSkipSpotify, "skip-spotify", false, "skip the Spotify enrichment stage")
	resolveAllCmd.Flags().BoolVar(&allSkipArt, "skip-art", false, "skip the cover-art stage")
	resolveAllCmd.Flags().BoolVar(&allSkipYouTube, "skip-youtube", false, "skip the YouTube resolution stage")
```

- [ ] **Step 6: Run the tests to verify they pass**

```bash
export PATH="$HOME/.local/go-toolchain/bin:$PATH"
go test ./cmd/ -v
```

Expected: PASS.

- [ ] **Step 7: Verify the preflight behaves on this machine**

This machine has neither a Spotify token nor yt-dlp, which makes it a perfect preflight test:

```bash
export PATH="$HOME/.local/go-toolchain/bin:$PATH"
make build
./byom-sync resolve all --input /home/lmorchard/devel/mixtapes/mixtapes.lmorchard.com/playlists
```

Expected: exits immediately, before touching any playlist, with a message naming **both** the missing Spotify token and the missing yt-dlp, each with its remedy. Then confirm skipping works:

```bash
./byom-sync resolve all --help
```

Expected: help text lists all eight flags with `--download` defaulting to `true`.

- [ ] **Step 8: Commit**

```bash
export PATH="$HOME/.local/go-toolchain/bin:$PATH"
make format && make lint && make test
git add cmd/resolve.go cmd/resolve_all_test.go
git commit -m "feat(resolve): 'resolve all' runs the full enrichment pipeline

Sequences spotify -> art -> youtube in dependency order (spotify writes
the ISRCs the later stages key on) behind a single command, and checks
every enabled stage's prerequisites up front so a missing yt-dlp is
reported before a long art crawl rather than after it.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Document the new behavior in byom-sync

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`

**Interfaces:**
- Consumes: everything from Tasks 1-7
- Produces: no code

- [ ] **Step 1: Update `README.md`**

Four edits:

1. In **Usage → Authenticate**, after the existing `byom-sync auth` block, add:

````markdown
On a headless or remote host, the normal flow can't complete: Spotify pins the
redirect URI to `http://127.0.0.1:<port>/callback` and matches it literally, so
over SSH the consent page redirects *your laptop's* browser to *your laptop's*
port and the code never reaches the machine running the command. Two ways
around it:

```sh
# Relay the code by hand — no callback server, works from any shell
byom-sync auth --manual

# Or forward the port before connecting, then use the normal flow
ssh -L 8888:127.0.0.1:8888 myhost
```
````

2. In **Usage**, add a new subsection immediately before `### Resolve YouTube IDs`:

````markdown
### Enrich everything at once

```sh
# Full pipeline over one playlist: spotify -> art -> youtube
byom-sync resolve all --input playlists/00-conceptual/my-mixtape.yaml

# Skip a stage (also skips its prerequisite check)
byom-sync resolve all --skip-youtube
```

`resolve all` runs the three enrichment stages in dependency order — the Spotify
stage writes the ISRCs that the art and YouTube stages use as their cache
identity. Prerequisites for every enabled stage (a cached Spotify token, `yt-dlp`
on `PATH`) are checked *before* any stage runs, so a missing tool is reported
immediately rather than after a long art crawl.

`--download` defaults to true here, unlike `resolve art`. A missing Spotify token
is fatal here rather than degrading to MusicBrainz-only art; run `resolve art`
on its own if you want the degrading behavior.
````

3. In **The hub schema**, after the "Tracks are matched across syncs..." paragraph, add:

```markdown
Playlists may be filed in subdirectories to any depth — `resolve`, `dates`,
`export`, and `site` all walk the hub recursively. Dotfiles and the hub-root
`art/` store are skipped.
```

4. In **Export**, replace the paragraph beginning "`--input` may be a single YAML file or a directory." with:

```markdown
`--input` may be a single YAML file or a directory. When it's a directory, the
hub is walked recursively and `--out` mirrors its structure — so
`playlists/01-covers/numan-s-shadow.yaml` exports to
`<out>/01-covers/numan-s-shadow.<ext>`. Mirroring rather than flattening means
two playlists sharing a basename in different folders can't overwrite each other.
```

- [ ] **Step 2: Update `AGENTS.md`**

1. In the **Layout** section, amend the `internal/playlist/` bullet so `store.go` reads:

```markdown
  `store.go` (`HubPaths` — the canonical recursive hub walk, plus
  `Load`/`LoadFile`/`FindFileByID`/`Save`/`Slug`),
```

2. In the **Layout** section's `cmd/` bullet, add `all` to the `resolve` subcommand list:

```markdown
  `export`, `resolve` (subcommands `all`, `youtube`, `spotify`, `art`, `prime`,
  `cache stats`, `cache clear`), `site`, `dates`.
```

3. Add two bullets to **Conventions & gotchas**:

```markdown
- **Hub discovery is recursive and centralized.** `playlist.HubPaths(input)` is
  the single definition of "which files are in the hub": it walks subdirectories
  to any depth, skips dotfiles (including macOS `._*.yaml` sidecars), and skips
  the hub-root `art/` store. `cmd.hubPaths`, `export.Run`, and `playlist.Load`
  all delegate to it, and it matches `internal/site/tree.go`'s rules. Do not
  reintroduce a `filepath.Glob(dir + "/*.yaml")` — that shallow glob silently
  found zero playlists in a subdirectory-organized hub, which broke `resolve`
  and `dates` for months without an error.
- **`resolve all` drives the per-stage globals.** The stage functions
  (`runResolveSpotify`/`runResolveArt`/`runResolveYouTube`) read package-level
  flag vars, and `resolveNoCache` in particular is assigned by two of them. When
  adding a stage flag, fan it out in `runResolveAll` too, or the pipeline and the
  standalone command will disagree.
```

- [ ] **Step 3: Verify the docs match reality**

```bash
export PATH="$HOME/.local/go-toolchain/bin:$PATH"
make build
./byom-sync resolve all --help
./byom-sync auth --help
```

Expected: every flag and default mentioned in the README appears in the help output. Fix any drift in the README, not the code.

- [ ] **Step 4: Commit**

```bash
git add README.md AGENTS.md
git commit -m "docs: recursive hub discovery, resolve all, auth --manual

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: Consume the new commands in `mixtapes.lmorchard.com`

**Files:**
- Modify: `/home/lmorchard/devel/mixtapes/mixtapes.lmorchard.com/Makefile`
- Modify: `/home/lmorchard/devel/mixtapes/mixtapes.lmorchard.com/README.md`

**Interfaces:**
- Consumes: `byom-sync resolve all` (Task 7), recursive `resolve art` (Task 2)
- Produces: `make enrich`

**Note:** this is a *different repository* with its own git history. Do not commit it together with the byom-sync changes. It also consumes byom-sync from published releases via `scripts/fetch-byom-sync.sh`, so these targets only work once the byom-sync change is released and `make update-tools` has been run — call that out in the commit message.

- [ ] **Step 1: Add the `enrich` target and fix `resolve-art`**

In `mixtapes.lmorchard.com/Makefile`, add `enrich` to the `.PHONY` line and replace the `resolve-art` target:

```makefile
.PHONY: build serve watch enrich resolve-art update-tools clean

# Full enrichment pipeline over one playlist (or the whole hub if PL is unset):
# Spotify metadata, then cover art, then YouTube ids. Needs a Spotify token
# (./bin/byom-sync auth) and yt-dlp on PATH; both are checked before any work
# starts. Example:
#   make enrich PL=playlists/00-conceptual/my-mixtape.yaml
PL ?= playlists
enrich:
	./scripts/fetch-byom-sync.sh
	./bin/byom-sync resolve all --input $(PL)

# Opt-in: (re)download cover art into playlists/art for the whole hub. Needs a
# Spotify token (run `./bin/byom-sync auth` once) and takes a while (~1 req/sec
# to MusicBrainz).
resolve-art:
	./scripts/fetch-byom-sync.sh
	./bin/byom-sync resolve art --input playlists --download
```

The `resolve-art` recipe text is unchanged — it starts working again once the recursive walk from Task 2 ships.

- [ ] **Step 2: Update the mixtapes README**

Three edits to `mixtapes.lmorchard.com/README.md`:

1. In **Composing a playlist by hand → 4. Enrich it**, replace the whole "**These commands are not recursive.**" paragraph and the four-command block with:

````markdown
```sh
PL=playlists/00-conceptual/music-for-staring-at-ceilings.yaml

./bin/byom-sync auth              # one-time Spotify PKCE login
make enrich PL="$PL"              # spotify -> art -> youtube, in order
```

`make enrich` runs the three stages in dependency order and checks their
prerequisites first, so a missing `yt-dlp` is reported before anything else
happens rather than after the cover-art crawl. Omit `PL` to enrich the whole hub.

To run a single stage, the underlying commands still work individually:

```sh
./bin/byom-sync resolve spotify --input "$PL"
./bin/byom-sync resolve art     --input "$PL" --download
./bin/byom-sync resolve youtube --input "$PL"
```
````

Keep the three "Notes on each pass" bullets that follow, unchanged.

2. In **Reconstructing the art store**, delete the entire `> **Caveat:**` blockquote (the `make resolve-art` no-op warning and its `for d in playlists/*/` workaround). It is fixed by Task 2.

3. In **Deployment**, after the "To publish" code block, add:

````markdown
Enrichment on the server needs a Spotify token, and the normal browser login
can't complete over SSH — Spotify pins the redirect to `127.0.0.1`, so the
consent page would send the code to your laptop instead of the server:

```sh
ssh myriad-docker
cd docker/mixtapes.lmorchard.com
./bin/byom-sync auth --manual     # prints a URL; paste the redirect back
```
````

- [ ] **Step 3: Verify the Makefile parses**

```bash
cd /home/lmorchard/devel/mixtapes/mixtapes.lmorchard.com
make -n enrich
```

Expected: prints the two recipe lines without executing them. (Running `make enrich` for real will fail the preflight until byom-sync is released, `make update-tools` is run, and a token plus yt-dlp exist.)

- [ ] **Step 4: Commit (in the mixtapes repo)**

```bash
cd /home/lmorchard/devel/mixtapes/mixtapes.lmorchard.com
git add Makefile README.md
git commit -m "feat: make enrich — one-command playlist enrichment

Wraps 'byom-sync resolve all', which sequences spotify -> art -> youtube
and preflights their prerequisites. Also drops the resolve-art caveat:
byom-sync now walks the hub recursively, so --input playlists finds the
playlists filed under subdirectories again.

Requires a byom-sync release containing recursive hub discovery; run
'make update-tools' to pick it up.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Final verification

- [ ] **Full sweep in byom-sync**

```bash
export PATH="$HOME/.local/go-toolchain/bin:$PATH"
cd /home/lmorchard/devel/mixtapes/byom-sync
make format && make lint && make test && make build
```

Expected: all clean, all 14 packages pass.

- [ ] **End-to-end against the real hub**

```bash
export PATH="$HOME/.local/go-toolchain/bin:$PATH"
HUB=/home/lmorchard/devel/mixtapes/mixtapes.lmorchard.com/playlists

# Recursion: was 0 files before, should be 58 now
./byom-sync dates --input "$HUB"

# Export mirroring: should produce subdirectories, not a flat dir
./byom-sync export jspf --input "$HUB" --out /tmp/jspf-check
find /tmp/jspf-check -name '*.jspf' | head
ls /tmp/jspf-check

# Preflight: should name both missing prerequisites and do no work
./byom-sync resolve all --input "$HUB"
```

Expected: `dates` reports 58 files; the export tree contains `00-conceptual/`, `01-covers/`, `02-top-lists/`, `zz-not-mine/` subdirectories; `resolve all` exits immediately naming the missing token and yt-dlp.

- [ ] **Clean up scratch output**

```bash
rm -rf /tmp/jspf-check
```

- [ ] **Confirm the mixtapes hub wasn't left modified unintentionally**

```bash
git -C /home/lmorchard/devel/mixtapes/mixtapes.lmorchard.com status --short
```

Expected: only `Makefile` and `README.md` from Task 9. If `playlists/` shows changes from the `dates` run, review the diff and decide whether to keep or revert it.
