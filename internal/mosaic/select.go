package mosaic

import (
	"sort"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

// Cover is a distinct album cover and how many tracks reference it.
type Cover struct {
	ImageFile string
	Count     int
}

// Select returns the playlist's distinct downloaded covers ranked by track
// count (desc), tie-broken by first appearance in the tracklist (asc). Tracks
// without a downloaded cover (empty ImageFile) are ignored. Deterministic.
func Select(p playlist.Playlist) []Cover {
	type agg struct {
		count int
		first int
	}
	seen := map[string]*agg{}
	order := []string{}
	for i, t := range p.Tracks {
		if t.ImageFile == "" {
			continue
		}
		a := seen[t.ImageFile]
		if a == nil {
			a = &agg{first: i}
			seen[t.ImageFile] = a
			order = append(order, t.ImageFile)
		}
		a.count++
	}
	out := make([]Cover, 0, len(order))
	for _, f := range order {
		out = append(out, Cover{ImageFile: f, Count: seen[f].count})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return seen[out[i].ImageFile].first < seen[out[j].ImageFile].first
	})
	return out
}
