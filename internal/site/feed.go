package site

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/gorilla/feeds"
)

// WriteFeed writes an RSS feed of playlists, newest first by DateCreated and
// capped at site.FeedItemLimit.
//
// Nodes are collected and sorted before any item body is built: each body embeds
// a full tracklist and is stored twice (description and content:encoded), so
// rendering playlists that the cap discards would be the bulk of the work.
func WriteFeed(outDir string, site SiteMeta, root *Node) error {
	var nodes []*Node
	err := walkPlaylists(root, func(n *Node) error {
		nodes = append(nodes, n)
		return nil
	})
	if err != nil {
		return err
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		return nodes[i].Playlist.DateCreated.After(nodes[j].Playlist.DateCreated)
	})
	if site.FeedItemLimit > 0 && len(nodes) > site.FeedItemLimit {
		nodes = nodes[:site.FeedItemLimit]
	}

	items := make([]*feeds.Item, 0, len(nodes))
	for _, n := range nodes {
		body := itemHTML(n, site)
		items = append(items, &feeds.Item{
			Title:       n.Title,
			Link:        &feeds.Link{Href: canonical(site.BaseURL, n.Path)},
			Description: body,
			Content:     body,
			Enclosure:   coverEnclosure(n.Playlist, site, outDir),
			Created:     n.Playlist.DateCreated,
		})
	}

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
