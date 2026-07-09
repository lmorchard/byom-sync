package playlist

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var nonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)

// Slug converts a playlist title into a filesystem-friendly base name:
// lowercased, with runs of non-alphanumeric characters collapsed to a single
// hyphen and leading/trailing hyphens trimmed. Empty results fall back to
// "playlist".
func Slug(title string) string {
	s := nonSlugChars.ReplaceAllString(strings.ToLower(title), "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "playlist"
	}
	return s
}

// Load reads every *.yaml file in dir into a slice of playlists. A missing
// directory yields an empty slice (not an error) — the first sync creates it.
func Load(dir string) ([]Playlist, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)

	var playlists []Playlist
	for _, path := range matches {
		p, err := loadFile(path)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", path, err)
		}
		playlists = append(playlists, p)
	}
	return playlists, nil
}

// LoadFile reads a single playlist YAML file.
func LoadFile(path string) (Playlist, error) {
	return loadFile(path)
}

func loadFile(path string) (Playlist, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Playlist{}, err
	}
	var p Playlist
	if err := yaml.Unmarshal(data, &p); err != nil {
		return Playlist{}, err
	}
	return p, nil
}

// SaveFile writes a single playlist to an exact path (used to update a hub file
// in place, preserving its filename). The write is atomic: it goes to a temp
// file in the same directory, is flushed, then renamed over the target — so a
// crash mid-write can never leave the original truncated or corrupt (important
// for large hub files written repeatedly during a long resolve run).
func SaveFile(path string, p Playlist) error {
	data, err := yaml.Marshal(p)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Clean up the temp file on any failure before the rename (no-op after it).
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil { // flush to disk before the rename
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// FindFileByID returns the path of the YAML file in dir whose spotify_id matches
// spotifyID. ok is false (with a nil error) when no file matches.
func FindFileByID(dir, spotifyID string) (path string, ok bool, err error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return "", false, err
	}
	sort.Strings(matches)
	for _, m := range matches {
		p, err := loadFile(m)
		if err != nil {
			return "", false, fmt.Errorf("scan %s: %w", m, err)
		}
		if p.SpotifyID != "" && p.SpotifyID == spotifyID {
			return m, true, nil
		}
	}
	return "", false, nil
}

// Save writes p into dir as YAML. If an existing file already carries p.SpotifyID,
// that file is overwritten in place (its filename is preserved even if the title
// changed). Otherwise a new file "<Slug(Title)>.yaml" is created; on a filename
// collision with a different playlist, "-<first 6 of SpotifyID>" is appended.
func Save(dir string, p Playlist) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	path, ok, err := FindFileByID(dir, p.SpotifyID)
	if err != nil {
		return "", err
	}
	if !ok {
		path, err = newFilePath(dir, p)
		if err != nil {
			return "", err
		}
	}

	data, err := yaml.Marshal(p)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// newFilePath picks a filename for a playlist not yet stored in dir, resolving
// slug collisions by appending a short SpotifyID suffix.
func newFilePath(dir string, p Playlist) (string, error) {
	base := Slug(p.Title)
	candidate := filepath.Join(dir, base+".yaml")
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate, nil
	} else if err != nil {
		return "", err
	}

	suffix := p.SpotifyID
	if len(suffix) > 6 {
		suffix = suffix[:6]
	}
	suffix = strings.ToLower(suffix)
	return filepath.Join(dir, fmt.Sprintf("%s-%s.yaml", base, suffix)), nil
}
