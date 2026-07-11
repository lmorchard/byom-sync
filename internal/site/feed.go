package site

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/gorilla/feeds"
)

// WriteFeed writes an RSS feed of playlists, newest first by DateCreated.
func WriteFeed(outDir string, site SiteMeta, root *Node) error {
	var items []*feeds.Item
	err := walkPlaylists(root, func(n *Node) error {
		items = append(items, &feeds.Item{
			Title:       n.Title,
			Link:        &feeds.Link{Href: canonical(site.BaseURL, n.Path)},
			Description: n.Playlist.Description,
			Created:     n.Playlist.DateCreated,
		})
		return nil
	})
	if err != nil {
		return err
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Created.After(items[j].Created)
	})

	feed := &feeds.Feed{
		Title:       site.Title,
		Link:        &feeds.Link{Href: canonical(site.BaseURL, "")},
		Description: site.Title + " — playlists",
		Items:       items,
	}
	rss, err := feed.ToRss()
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "feed.xml"), []byte(rss), 0o644)
}
