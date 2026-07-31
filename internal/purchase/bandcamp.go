package purchase

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// bandcampEndpoint is the search endpoint Bandcamp's own site calls. It is
// undocumented and unauthenticated: there is no public Bandcamp search API, and
// this is the only way to reach the catalogue. Measured as the best single
// source — one request, the exact album URL, and a clean zero-result miss rather
// than a wrong guess. Because it is unsanctioned it is deliberately one tier of
// a cascade: if it breaks or starts refusing traffic, the pass degrades to the
// remaining tiers instead of failing. Keep the request rate polite (~1/sec).
const bandcampEndpoint = "https://bandcamp.com/api/bcsearch_public_api/1/autocomplete_elastic"

// Bandcamp resolves purchase URLs from Bandcamp's search endpoint.
type Bandcamp struct {
	client   *http.Client
	endpoint string
}

// NewBandcamp returns a Bandcamp source. A zero endpoint means the real one;
// tests inject an httptest URL.
func NewBandcamp(client *http.Client, endpoint string) *Bandcamp {
	if client == nil {
		client = defaultHTTPClient()
	}
	if endpoint == "" {
		endpoint = bandcampEndpoint
	}
	return &Bandcamp{client: client, endpoint: endpoint}
}

func (*Bandcamp) Name() string { return "bandcamp" }

type bandcampResponse struct {
	Auto struct {
		Results []struct {
			Type        string `json:"type"`
			Name        string `json:"name"`
			BandName    string `json:"band_name"`
			ItemURLPath string `json:"item_url_path"`
		} `json:"results"`
	} `json:"auto"`
}

// Lookup searches Bandcamp for the query and returns the best-scoring result
// that clears the threshold. An empty result set is a miss, not an error.
func (b *Bandcamp) Lookup(ctx context.Context, q Query) (Result, error) {
	filter := "a"
	if q.Kind() == KindTrack {
		filter = "t"
	}
	payload, err := json.Marshal(map[string]any{
		"search_text":   q.Text(),
		"search_filter": filter,
		"full_page":     false,
		"fan_id":        nil,
	})
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.endpoint, bytes.NewReader(payload))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := b.client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("bandcamp search: status %d", resp.StatusCode)
	}

	var body bandcampResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Result{}, err
	}

	best := Result{Kind: q.Kind()}
	for _, r := range body.Auto.Results {
		if r.ItemURLPath == "" {
			continue
		}
		s, ok := Accept(q, r.BandName, r.Name)
		if !ok {
			continue // wrong record, or too weak a match to trust
		}
		if s > best.Score {
			best.Score, best.URL = s, r.ItemURLPath
		}
	}
	if best.URL == "" {
		return Result{Kind: q.Kind()}, nil // clean miss
	}
	return best, nil
}
