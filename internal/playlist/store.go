package playlist

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
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
// The walk goes through os.DirFS(root) rather than filepath.WalkDir(root, ...)
// so that a root which is itself a symlink to a directory (a NAS mount or a
// cloud-synced folder, say) is walked like a real directory: DirFS's ReadDir
// follows the root, where filepath.WalkDir's initial Lstat would see the
// symlink and report a non-directory, silently returning no paths at all —
// exactly the empty-hub failure mode this function exists to prevent.
// Symlinks *below* the root are left alone, as before. fs.WalkDir yields
// slash-separated paths relative to root, which are rejoined onto the
// caller's own input so results keep its prefix verbatim (callers such as
// export.Run compute filepath.Rel(input, path) against these).
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
	walkErr := fs.WalkDir(os.DirFS(root), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == "." {
			return nil // never skip the root on its own name
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if d.Name() == "art" && path.Dir(p) == "." {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".yaml") {
			paths = append(paths, filepath.Join(root, filepath.FromSlash(p)))
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Strings(paths)
	return paths, nil
}

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
// spotifyID, searching the whole hub recursively via HubPaths — playlists are
// routinely filed in subdirectories, and a shallow scan of the hub root would
// report them as absent, making Save write a duplicate there. ok is false (with a
// nil error) when no file matches.
func FindFileByID(dir, spotifyID string) (path string, ok bool, err error) {
	matches, err := HubPaths(dir)
	if err != nil {
		// A hub directory that doesn't exist yet simply holds no playlists. sync
		// calls this before anything creates the directory, so this must not be
		// an error; Save creates the directory afterwards.
		if errors.Is(err, fs.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
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
