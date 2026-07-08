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
			{Title: "Song One", Artist: "Artist A", Album: "Album X", ISRC: "GBA098000010", DurationMS: 354000, SyncState: playlist.SyncState{SpotifyPresent: true}},
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
	// track with no ISRC omits identifier
	t1 := tracks[1].(map[string]any)
	if _, present := t1["identifier"]; present {
		t.Errorf("identifier should be omitted when ISRC empty: %v", t1)
	}
}

func TestHugoExport(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.md")
	if err := (HugoExporter{}).Export(samplePlaylist(), out, nil); err != nil {
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
	if !strings.Contains(s, "| Title | Artist | Album |") {
		t.Errorf("table header missing:\n%s", s)
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
