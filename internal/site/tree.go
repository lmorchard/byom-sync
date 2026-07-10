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
	IntroMD  string // raw markdown from index.md (root) or README.md (subdirs)
	Playlist *playlist.Playlist
	Children []*Node
}

// BuildTree walks hubDir recursively and returns the root node.
func BuildTree(hubDir string) (*Node, error) {
	return buildDir(hubDir, "", "")
}

func buildDir(fsDir, urlPath, name string) (*Node, error) {
	node := &Node{Name: name, Title: name, Path: urlPath, IsDir: true}

	introName := "README.md"
	if urlPath == "" {
		introName = "index.md"
	}
	if data, err := os.ReadFile(filepath.Join(fsDir, introName)); err == nil {
		node.IntroMD = string(data)
	}

	entries, err := os.ReadDir(fsDir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
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
		return a.Name < b.Name
	})
	return node, nil
}
