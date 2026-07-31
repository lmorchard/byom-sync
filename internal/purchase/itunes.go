package purchase

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

const itunesEndpoint = "https://itunes.apple.com/search"

// ITunes resolves purchase URLs from the iTunes Search API. No key required.
//
// iTunes Store music purchases are DRM-free (iTunes Plus, 256kbps AAC, since
// 2009) — it is Apple Music *streaming* that is protected. The distinction
// matters operationally: collectionViewUrl points at music.apple.com, which
// hosts both, so a result is only accepted when it carries a positive price.
// That is what separates "Apple has this to stream" from "Apple will sell you
// this," and it is most of the gap between this tier's 65% and 100%.
type ITunes struct {
	client   *http.Client
	endpoint string
}

// NewITunes returns an iTunes source. A zero endpoint means the real one.
func NewITunes(client *http.Client, endpoint string) *ITunes {
	if client == nil {
		client = defaultHTTPClient()
	}
	if endpoint == "" {
		endpoint = itunesEndpoint
	}
	return &ITunes{client: client, endpoint: endpoint}
}

func (*ITunes) Name() string { return "itunes" }

type itunesResponse struct {
	Results []struct {
		ArtistName        string  `json:"artistName"`
		CollectionName    string  `json:"collectionName"`
		CollectionViewURL string  `json:"collectionViewUrl"`
		CollectionPrice   float64 `json:"collectionPrice"`
		TrackName         string  `json:"trackName"`
		TrackViewURL      string  `json:"trackViewUrl"`
		TrackPrice        float64 `json:"trackPrice"`
	} `json:"results"`
}

// Lookup searches iTunes and returns the best priced result clearing the
// threshold. Query construction is deliberately the plain blended term: four
// constructions were measured (blended at limit 5 and 25, mixTerm, albumTerm)
// and none beat this one. albumTerm was actively worse — dropping the artist
// lets same-titled albums by other artists win. Do not "improve" this without
// re-measuring.
func (i *ITunes) Lookup(ctx context.Context, q Query) (Result, error) {
	entity := "album"
	if q.Kind() == KindTrack {
		entity = "song"
	}
	params := url.Values{
		"term":   {q.Text()},
		"entity": {entity},
		"limit":  {"5"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, i.endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := i.client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("itunes search: status %d", resp.StatusCode)
	}

	var body itunesResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Result{}, err
	}

	best := Result{Kind: q.Kind()}
	for _, r := range body.Results {
		name, link, price := r.CollectionName, r.CollectionViewURL, r.CollectionPrice
		if q.Kind() == KindTrack {
			name, link, price = r.TrackName, r.TrackViewURL, r.TrackPrice
		}
		if link == "" || price <= 0 {
			continue // stream-only or unpriced: not a purchase
		}
		s, ok := Accept(q, r.ArtistName, name)
		if !ok {
			continue
		}
		if s > best.Score {
			best.Score, best.URL = s, link
		}
	}
	if best.URL == "" {
		return Result{Kind: q.Kind()}, nil
	}
	return best, nil
}
