package coverart

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// CAAClient fetches cover art metadata from the Cover Art Archive.
type CAAClient struct {
	HTTP    *http.Client
	BaseURL string // e.g. CAABaseURL; overridable for tests
}

// FrontImage returns the URL of the front cover for a MusicBrainz entity
// ("release" or "release-group") MBID: the 500px thumbnail when available, else
// the full image. Returns "" when the entity has no art (HTTP 404) or no front
// image.
func (c *CAAClient) FrontImage(ctx context.Context, entity, mbid string) (string, error) {
	u := fmt.Sprintf("%s/%s/%s", c.BaseURL, entity, mbid)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil // no art for this MBID
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("cover art archive %s/%s: status %d", entity, mbid, resp.StatusCode)
	}
	var out struct {
		Images []struct {
			Front      bool              `json:"front"`
			Image      string            `json:"image"`
			Thumbnails map[string]string `json:"thumbnails"`
		} `json:"images"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	for _, img := range out.Images {
		if !img.Front {
			continue
		}
		if u500 := img.Thumbnails["500"]; u500 != "" {
			return httpsURL(u500), nil
		}
		return httpsURL(img.Image), nil
	}
	return "", nil
}

// httpsURL upgrades an http:// URL to https:// (Cover Art Archive serves https;
// http URLs would be mixed-content-blocked in an https page).
func httpsURL(u string) string {
	if rest, ok := strings.CutPrefix(u, "http://"); ok {
		return "https://" + rest
	}
	return u
}
