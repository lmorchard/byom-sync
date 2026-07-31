package purchase

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	discogsAPI  = "https://api.discogs.com"
	discogsSite = "https://www.discogs.com"
)

// Discogs resolves marketplace listings for secondhand physical media.
//
// This is a two-step lookup because the search response is insufficient on its
// own: it carries no num_for_sale, and its `title` is a single "Artist - Album"
// string that breaks whenever either side contains " - ". The release lookup
// supplies authoritative artists[]/title for rescoring plus the availability
// signal, so the second request pays for itself twice. The lookup only fires
// for a candidate that already cleared the first gate, so most misses cost one
// request, not two.
//
// A Discogs link is a listing for a used record: it does not fill a gap in a
// digital collection unless the record gets ripped, and a secondhand sale pays
// the artist nothing. That is why this is the last tier.
type Discogs struct {
	client  *http.Client
	baseURL string
	token   string
}

// NewDiscogs returns a Discogs source. A zero baseURL means the real API. token
// is optional: without one the rate limit is 25/min, with one 60/min.
func NewDiscogs(client *http.Client, baseURL, token string) *Discogs {
	if client == nil {
		client = http.DefaultClient
	}
	if baseURL == "" {
		baseURL = discogsAPI
	}
	return &Discogs{client: client, baseURL: strings.TrimRight(baseURL, "/"), token: token}
}

func (*Discogs) Name() string { return "discogs" }

type discogsSearch struct {
	Results []struct {
		Title       string `json:"title"`
		ResourceURL string `json:"resource_url"`
	} `json:"results"`
}

type discogsRelease struct {
	Title      string `json:"title"`
	URI        string `json:"uri"`
	NumForSale int    `json:"num_for_sale"`
	Artists    []struct {
		Name string `json:"name"`
	} `json:"artists"`
}

// get issues an authenticated GET and decodes JSON into out.
func (d *Discogs) get(ctx context.Context, rawURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	// Discogs rejects default user agents outright.
	req.Header.Set("User-Agent", userAgent)
	if d.token != "" {
		req.Header.Set("Authorization", "Discogs token="+d.token)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("discogs %s: status %d", rawURL, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Lookup searches Discogs, then confirms the best candidate against the release
// endpoint for an authoritative rescore and a marketplace-availability check.
func (d *Discogs) Lookup(ctx context.Context, q Query) (Result, error) {
	params := url.Values{
		"q":        {q.Text()},
		"type":     {"release"},
		"per_page": {"5"},
	}
	var search discogsSearch
	if err := d.get(ctx, d.baseURL+"/database/search?"+params.Encode(), &search); err != nil {
		return Result{}, err
	}

	// First pass: rank on the combined "Artist - Album" string. Fragile by
	// nature, so it only picks a candidate to spend a second request on — never
	// the final answer. Using raw Score (not Accept) here is deliberate: this
	// pass is ranking, not deciding, and the authoritative decision happens
	// below on the release lookup's clean fields.
	bestScore, bestResource := 0.0, ""
	for _, r := range search.Results {
		if r.ResourceURL == "" {
			continue
		}
		artist, album, found := strings.Cut(r.Title, " - ")
		if !found {
			album = r.Title
		}
		if s := Score(q, artist, album); s > bestScore {
			bestScore, bestResource = s, r.ResourceURL
		}
	}
	if bestScore < Threshold || bestResource == "" {
		return Result{Kind: q.Kind()}, nil
	}

	// Second pass: authoritative fields + availability.
	var rel discogsRelease
	if err := d.get(ctx, bestResource, &rel); err != nil {
		return Result{}, err
	}
	if rel.NumForSale <= 0 {
		return Result{Kind: q.Kind()}, nil // nothing to buy — a dead link
	}
	names := make([]string, 0, len(rel.Artists))
	for _, a := range rel.Artists {
		names = append(names, a.Name)
	}
	score, ok := Accept(q, strings.Join(names, ", "), rel.Title)
	if !ok {
		return Result{Kind: q.Kind()}, nil
	}
	if rel.URI == "" {
		return Result{Kind: q.Kind()}, nil
	}
	// The path comes from the API's own uri field — never hand-constructed.
	// Discogs 403s scripted traffic, so an invented store-URL pattern can't
	// even be verified against a live account.
	return Result{URL: discogsSite + rel.URI, Kind: q.Kind(), Score: score}, nil
}
