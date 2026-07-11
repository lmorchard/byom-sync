// Package coverart resolves cover-art URLs for tracks via MusicBrainz search
// and the Cover Art Archive. Public APIs, no key; MusicBrainz needs a
// descriptive User-Agent and ~1 req/sec (paced by the caller).
package coverart

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	MBBaseURL        = "https://musicbrainz.org/ws/2"
	CAABaseURL       = "https://coverartarchive.org"
	DefaultUserAgent = "byom-sync ( https://github.com/lmorchard/byom-sync )"
)

// MBClient queries the MusicBrainz search API.
type MBClient struct {
	HTTP      *http.Client
	BaseURL   string // e.g. MBBaseURL; overridable for tests
	UserAgent string
}

// SearchReleaseGroup returns the top release-group MBID matching artist+album,
// or "" when there is no match.
func (c *MBClient) SearchReleaseGroup(ctx context.Context, artist, album string) (string, error) {
	q := fmt.Sprintf(`artist:%s AND releasegroup:%s`, luceneQuote(artist), luceneQuote(album))
	var out struct {
		ReleaseGroups []struct {
			ID string `json:"id"`
		} `json:"release-groups"`
	}
	if err := c.search(ctx, "release-group", q, &out); err != nil {
		return "", err
	}
	if len(out.ReleaseGroups) == 0 {
		return "", nil
	}
	return out.ReleaseGroups[0].ID, nil
}

// SearchRecordingRelease returns the first release MBID of the top recording
// matching artist+title, or "" when there is no match.
func (c *MBClient) SearchRecordingRelease(ctx context.Context, artist, title string) (string, error) {
	q := fmt.Sprintf(`artist:%s AND recording:%s`, luceneQuote(artist), luceneQuote(title))
	var out struct {
		Recordings []struct {
			ID       string `json:"id"`
			Releases []struct {
				ID string `json:"id"`
			} `json:"releases"`
		} `json:"recordings"`
	}
	if err := c.search(ctx, "recording", q, &out); err != nil {
		return "", err
	}
	for _, rec := range out.Recordings {
		if len(rec.Releases) > 0 {
			return rec.Releases[0].ID, nil
		}
	}
	return "", nil
}

func (c *MBClient) search(ctx context.Context, entity, query string, out any) error {
	u := fmt.Sprintf("%s/%s?%s", c.BaseURL, entity, url.Values{
		"query": {query},
		"fmt":   {"json"},
		"limit": {"5"},
	}.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("musicbrainz %s: status %d", entity, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// luceneQuote wraps a value in double quotes for a Lucene field query, dropping
// any embedded double quotes so they can't break the query syntax.
func luceneQuote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, "") + `"`
}
