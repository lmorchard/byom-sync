Here is a comprehensive technical specification formatted specifically for an LLM coding agent like Claude Code. You can copy and paste this directly into your project directory as a `spec.md` or feed it directly into the prompt.

---

# Technical Specification: `byom-sync` CLI

## 1. Project Overview

`byom-sync` (Bring Your Own Music) is a Go-based CLI tool designed to decouple playlist curation from streaming platforms (specifically Spotify). It extracts playlist data via API, stores it locally in an agnostic, Git-friendly YAML format, and utilizes a "Hub and Spoke" architecture to compile that YAML into various destination formats (M3U8 for homelab servers, JSPF for web components, and Markdown for static sites).

## 2. Tech Stack & Dependencies

* **Language:** Go (1.21+)
* **CLI Framework:** `github.com/spf13/cobra`
* **Spotify API:** `github.com/zmb3/spotify/v2` (Must support v2 `/items` endpoints)
* **OAuth2:** `golang.org/x/oauth2`
* **Serialization:** `gopkg.in/yaml.v3` (Crucial for preserving order/comments) & standard `encoding/json`
* **Concurrency:** `golang.org/x/sync/errgroup`

## 3. Core Data Schema (The "Hub")

The local source of truth is a flattened, ergonomic YAML schema inspired by JSPF. The CLI must parse this into strongly typed Go structs.

```yaml
# Example: playlist.yaml
title: "Playlist Name"
creator: "User Name"
date_created: "2026-07-07T00:00:00Z"
tracks:
  - title: "Track Title"
    artist: "Artist Name"
    album: "Album Name"
    isrc: "GBA098000010"
    duration_ms: 354000
    sync_state:
      spotify_present: true
      date_orphaned: ""

```

```go
// Internal Go Representation
type Playlist struct {
	Title       string    `yaml:"title"`
	Creator     string    `yaml:"creator"`
	DateCreated time.Time `yaml:"date_created"`
	Tracks      []Track   `yaml:"tracks"`
}

type Track struct {
	Title      string    `yaml:"title"`
	Artist     string    `yaml:"artist"`
	Album      string    `yaml:"album,omitempty"`
	ISRC       string    `yaml:"isrc"`
	DurationMS int       `yaml:"duration_ms,omitempty"`
	SyncState  SyncState `yaml:"sync_state"`
}

type SyncState struct {
	SpotifyPresent bool   `yaml:"spotify_present"`
	DateOrphaned   string `yaml:"date_orphaned,omitempty"`
}

```

## 4. Command Topology

Implement the following `cobra` command structure:

* `byom-sync auth`
* `byom-sync sync --dir <path> --strategy <archive|mirror>`
* `byom-sync export m3u8 --input <path> --out <path> --lib-prefix <string>`
* `byom-sync export jspf --input <path> --out <path>`
* `byom-sync export hugo --input <path> --out <path>`

## 5. Implementation Details

### A. The OAuth Flow (`auth`)

The Spotify API (Developer Mode) requires an active Premium account. Implement the Authorization Code flow:

1. Generate OAuth URL and open the system's default web browser.
2. Spin up a temporary local HTTP server (e.g., `localhost:8080/callback`).
3. Intercept the redirect, extract the `code`, and exchange for Access/Refresh tokens.
4. Save tokens to `~/.config/byom-sync/config.json`.
5. Shut down the local server cleanly. Future commands should silently refresh via token cache.

### B. The State Engine (`sync`)

Fetches Spotify playlists and merges them with local YAML files using ISRC (fallback to Artist+Title) as the primary key.

**Strategies:**

* **`archive` (Default):** Append-only. New tracks are added. If a local track is missing from the remote Spotify payload, do NOT delete it. Set `sync_state.spotify_present = false` and populate `date_orphaned` with `time.Now()`.
* **`mirror`:** Strict overwrite. The local YAML is destructively overwritten to exactly match the current remote Spotify state.

**Pagination & Rate Limiting:**

* Use the `zmb3/spotify/v2` built-in pagination to handle playlists > 100 tracks.
* Initialize the Spotify client with `spotify.WithRetry(true)` to automatically respect HTTP 429 `Retry-After` headers.
* Use a bounded worker pool (`errgroup` with a limit of 3-5 concurrent workers) if processing multiple playlists simultaneously.

### C. The Exporter Architecture (`export`)

Implement a standard interface for generating output formats from the internal `Playlist` struct.

```go
type Exporter interface {
	Export(p Playlist, outputPath string, config map[string]string) error
}

```

**Required Spokes:**

1. **M3U8 (`export m3u8`):** Generates `.m3u8` files for systems like Navidrome/Mopidy. Requires a `--lib-prefix` flag (e.g., `/mnt/nas/music/`) to construct hardcoded file paths in the format: `{prefix}/{Artist}/{Album}/{Title}.flac`.
2. **JSPF (`export jspf`):** Marshals the internal struct into a strict JSPF JSON payload (`urn:isrc:{isrc}`) suitable for web components.
3. **Hugo Markdown (`export hugo`):** Uses Go's `text/template` to generate a `.md` file containing YAML frontmatter (for Hugo page metadata) and an HTML/Markdown table of the tracklist.