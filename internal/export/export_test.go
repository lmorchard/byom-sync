package export

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"gopkg.in/yaml.v3"
)

func mustYAML(t *testing.T, p playlist.Playlist) []byte {
	t.Helper()
	data, err := yaml.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

func samplePlaylist() playlist.Playlist {
	return playlist.Playlist{
		SpotifyID:   "PID",
		Title:       "Road Trip",
		Creator:     "Les",
		DateCreated: time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC),
		Tracks: []playlist.Track{
			{Title: "Song One", Artist: "Artist A", Album: "Album X", ISRC: "GBA098000010", SpotifyID: "track123", SpotifyURL: "https://open.spotify.com/track/track123", DurationMS: 354000, AddedAt: "2026-05-29T04:02:20Z", SyncState: playlist.SyncState{SpotifyPresent: true}},
			{Title: "Song Two", Artist: "Artist B", Album: "Album Y", DurationMS: 200000, SyncState: playlist.SyncState{SpotifyPresent: false, DateOrphaned: "2026-01-01T00:00:00Z"}},
		},
	}
}

func TestM3U8Export(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.m3u8")
	err := M3U8Exporter{}.Export(samplePlaylist(), out, map[string]string{"lib_prefix": "/mnt/nas/music"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(out)
	s := string(got)

	if !strings.HasPrefix(s, "#EXTM3U\n") {
		t.Errorf("missing #EXTM3U header:\n%s", s)
	}
	if !strings.Contains(s, "#EXTINF:354,Artist A - Song One\n") {
		t.Errorf("missing/incorrect EXTINF line:\n%s", s)
	}
	if !strings.Contains(s, "/mnt/nas/music/Artist A/Album X/Song One.flac\n") {
		t.Errorf("missing/incorrect default-ext path:\n%s", s)
	}
	// Orphaned tracks are still exported (m3u8 is a compile, not a filter).
	if !strings.Contains(s, "Song Two.flac") {
		t.Errorf("orphaned track missing from m3u8:\n%s", s)
	}
}

func TestM3U8Export_ExtOverride(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.m3u8")
	err := M3U8Exporter{}.Export(samplePlaylist(), out, map[string]string{"lib_prefix": "/music", "ext": "mp3"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(out)
	if !strings.Contains(string(got), "/music/Artist A/Album X/Song One.mp3\n") {
		t.Errorf("ext override not applied:\n%s", got)
	}
}

func TestJSPFExport(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.jspf")
	if err := (JSPFExporter{}).Export(samplePlaylist(), out, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(out)

	var doc map[string]any
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, got)
	}
	pl, ok := doc["playlist"].(map[string]any)
	if !ok {
		t.Fatalf("no playlist object: %s", got)
	}
	if pl["title"] != "Road Trip" || pl["creator"] != "Les" {
		t.Errorf("metadata wrong: %v", pl)
	}
	tracks, ok := pl["track"].([]any)
	if !ok || len(tracks) != 2 {
		t.Fatalf("expected 2 tracks: %v", pl["track"])
	}
	t0 := tracks[0].(map[string]any)
	// duration in seconds
	if t0["duration"].(float64) != 354 {
		t.Errorf("duration not in seconds: %v", t0["duration"])
	}
	// identifier urn:isrc
	ids := t0["identifier"].([]any)
	if len(ids) != 1 || ids[0] != "urn:isrc:GBA098000010" {
		t.Errorf("identifier wrong: %v", ids)
	}
	// spotify_url exposed as JSPF location
	loc, ok := t0["location"].([]any)
	if !ok || len(loc) != 1 || loc[0] != "https://open.spotify.com/track/track123" {
		t.Errorf("location should carry spotify_url: %v", t0["location"])
	}

	// track with no ISRC gets a synthesized urn:byom identifier so every track
	// is addressable downstream (e.g. by byom-player), even off-Spotify ones.
	t1 := tracks[1].(map[string]any)
	ids1, ok := t1["identifier"].([]any)
	if !ok || len(ids1) != 1 {
		t.Fatalf("no-ISRC track should carry one identifier: %v", t1["identifier"])
	}
	if s, _ := ids1[0].(string); !strings.HasPrefix(s, "urn:byom:") {
		t.Errorf("no-ISRC identifier should be urn:byom:<hash>, got %v", ids1[0])
	}
	// track with no spotify_url omits location
	if _, present := t1["location"]; present {
		t.Errorf("location should be omitted when spotify_url empty: %v", t1)
	}
}

func TestMarkdownExport(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.md")
	if err := (MarkdownExporter{}).Export(samplePlaylist(), out, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(out)
	s := string(got)

	if !strings.HasPrefix(s, "---\n") {
		t.Errorf("missing YAML frontmatter:\n%s", s)
	}
	if !strings.Contains(s, `title: "Road Trip"`) {
		t.Errorf("frontmatter title missing:\n%s", s)
	}
	// a table row per track
	if !strings.Contains(s, "Song One") || !strings.Contains(s, "Artist A") || !strings.Contains(s, "Album X") {
		t.Errorf("track row missing:\n%s", s)
	}
	if !strings.Contains(s, "| Title | Artist | Album | Added |") {
		t.Errorf("table header missing/incorrect:\n%s", s)
	}
	if !strings.Contains(s, "2026-05-29T04:02:20Z") {
		t.Errorf("added_at value missing from table:\n%s", s)
	}
	// title linked to spotify_url when present
	if !strings.Contains(s, "[Song One](https://open.spotify.com/track/track123)") {
		t.Errorf("title should link to spotify_url:\n%s", s)
	}
	// track without a spotify_url renders a plain title
	if !strings.Contains(s, "| Song Two |") {
		t.Errorf("title without url should be plain text:\n%s", s)
	}
}

func TestRun_DirModeWritesFilePerInput(t *testing.T) {
	inDir := t.TempDir()
	outDir := t.TempDir()

	// Two input playlists.
	for _, name := range []string{"alpha", "beta"} {
		p := samplePlaylist()
		p.Title = name
		data := mustYAML(t, p)
		if err := os.WriteFile(filepath.Join(inDir, name+".yaml"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := Run(M3U8Exporter{}, "m3u8", inDir, outDir, map[string]string{"lib_prefix": "/m"}); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"alpha", "beta"} {
		if _, err := os.Stat(filepath.Join(outDir, name+".m3u8")); err != nil {
			t.Errorf("expected output %s.m3u8: %v", name, err)
		}
	}
}

func TestRun_FileModeSingleOutput(t *testing.T) {
	inDir := t.TempDir()
	inFile := filepath.Join(inDir, "one.yaml")
	if err := os.WriteFile(inFile, mustYAML(t, samplePlaylist()), 0o644); err != nil {
		t.Fatal(err)
	}
	outFile := filepath.Join(t.TempDir(), "custom-name.jspf")
	if err := Run(JSPFExporter{}, "jspf", inFile, outFile, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(outFile); err != nil {
		t.Errorf("expected single output file at %s: %v", outFile, err)
	}
}

func TestJSPFExportEmitsYouTubeExtension(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.jspf.json")
	p := samplePlaylist()
	p.Tracks[0].YouTubeID = "vid42"
	if err := (JSPFExporter{}).Export(p, out, nil); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(out)

	var doc struct {
		Playlist struct {
			Track []struct {
				Extension map[string][]struct {
					Resolved struct {
						YouTube string `json:"youtube"`
					} `json:"resolved"`
				} `json:"extension"`
			} `json:"track"`
		} `json:"playlist"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	ext := doc.Playlist.Track[0].Extension["https://github.com/lmorchard/byom-sync"]
	if len(ext) == 0 || ext[0].Resolved.YouTube != "vid42" {
		t.Errorf("missing/incorrect youtube extension:\n%s", raw)
	}
	// Track 2 is orphaned, so it carries a sync_state extension but no resolved id.
	ext2 := doc.Playlist.Track[1].Extension["https://github.com/lmorchard/byom-sync"]
	if len(ext2) == 0 || ext2[0].Resolved.YouTube != "" {
		t.Errorf("track 2 should have a sync_state extension and no resolved id, got %v", ext2)
	}
}

func TestJSPFExportEmitsSyncStateForOrphaned(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.jspf.json")
	if err := (JSPFExporter{}).Export(samplePlaylist(), out, nil); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(out)

	var doc struct {
		Playlist struct {
			Track []struct {
				Extension map[string][]struct {
					SpotifyPresent *bool  `json:"spotify_present"`
					DateOrphaned   string `json:"date_orphaned"`
				} `json:"extension"`
			} `json:"track"`
		} `json:"playlist"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Track 1 is present → no byom-sync extension at all.
	if len(doc.Playlist.Track[0].Extension) != 0 {
		t.Errorf("present track should emit no extension, got %v", doc.Playlist.Track[0].Extension)
	}
	// Track 2 is orphaned → spotify_present:false + date_orphaned.
	ext := doc.Playlist.Track[1].Extension["https://github.com/lmorchard/byom-sync"]
	if len(ext) == 0 {
		t.Fatalf("orphaned track missing sync_state extension:\n%s", raw)
	}
	if ext[0].SpotifyPresent == nil || *ext[0].SpotifyPresent {
		t.Errorf("want spotify_present:false, got %v", ext[0].SpotifyPresent)
	}
	if ext[0].DateOrphaned != "2026-01-01T00:00:00Z" {
		t.Errorf("want date_orphaned 2026-01-01T00:00:00Z, got %q", ext[0].DateOrphaned)
	}
}

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
