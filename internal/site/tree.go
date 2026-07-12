// Package site compiles the playlist hub into a static, navigable web site:
// one page per playlist embedding <byom-player>, a tree mirroring the hub's
// subdirectories, a shared nav index, Open Graph metadata, and an RSS feed.
package site

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

// Node is one entry in the site tree: either a directory (IsDir) with Children,
// or a playlist leaf carrying its loaded Playlist. Path is the slash-joined slug
// path from the hub root ("" for the root itself).
type Node struct {
	Name     string
	Title    string
	Path     string
	IsDir    bool
	IntroMD  string // raw markdown from the folder's README.md (root and subdirs)
	Playlist *playlist.Playlist
	Children []*Node
}

// BuildTree walks hubDir recursively and returns the root node.
func BuildTree(hubDir string) (*Node, error) {
	return buildDir(hubDir, "", "")
}

func buildDir(fsDir, urlPath, name string) (*Node, error) {
	node := &Node{Name: name, Title: name, Path: urlPath, IsDir: true}

	// Every folder, root included, sources its intro prose from README.md.
	if data, err := os.ReadFile(filepath.Join(fsDir, "README.md")); err == nil {
		node.IntroMD = string(data)
	}

	entries, err := os.ReadDir(fsDir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		// Skip hidden entries (dotfiles/dirs): editor/VCS cruft and macOS
		// AppleDouble sidecars (`._*.yaml`) that would otherwise be parsed as
		// playlists and fail on their binary contents.
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if e.IsDir() {
			// Skip the content-addressed cover-art store at the hub root
			// (`resolve art --download` writes <hub>/art); it holds images, not
			// playlist content, and would otherwise render as empty folders.
			if urlPath == "" && e.Name() == "art" {
				continue
			}
			child, err := buildDir(
				filepath.Join(fsDir, e.Name()),
				path.Join(urlPath, e.Name()),
				e.Name(),
			)
			if err != nil {
				return nil, err
			}
			node.Children = append(node.Children, child)
			continue
		}
		if !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".yaml")
		p, err := playlist.LoadFile(filepath.Join(fsDir, e.Name()))
		if err != nil {
			return nil, err
		}
		node.Children = append(node.Children, &Node{
			Name:     stem,
			Title:    p.Title,
			Path:     path.Join(urlPath, stem),
			Playlist: &p,
		})
	}

	sort.SliceStable(node.Children, func(i, j int) bool {
		a, b := node.Children[i], node.Children[j]
		if a.IsDir != b.IsDir {
			return a.IsDir // directories first
		}
		if a.IsDir {
			return a.Name < b.Name
		}
		// Playlists: newest DateUpdated first; undated (zero) last; ties by Title.
		au, bu := a.Playlist.DateUpdated, b.Playlist.DateUpdated
		if au.IsZero() != bu.IsZero() {
			return !au.IsZero()
		}
		if !au.Equal(bu) {
			return au.After(bu)
		}
		return a.Title < b.Title
	})
	return node, nil
}
