// Package youtube resolves tracks to YouTube video IDs via the Data API and
// fills them into the hub. Search costs ~100 quota units each (~100/day on the
// default budget), so resolution is incremental and IDs are stored in the hub.
package youtube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// ErrQuotaExceeded is returned when the Data API reports the daily quota is
// spent, so the caller can stop and persist progress.
var ErrQuotaExceeded = errors.New("youtube: quota exceeded")

// Searcher resolves a query to the top matching video ID, or "" when the API
// answered with no result (a clean miss).
type Searcher interface {
	Search(ctx context.Context, query string) (videoID string, err error)
}

const defaultBaseURL = "https://www.googleapis.com/youtube/v3/search"

// HTTPSearcher calls the YouTube Data API v3 search.list endpoint.
type HTTPSearcher struct {
	APIKey  string
	Client  *http.Client
	baseURL string // overridable in tests; defaults to the Data API endpoint
}

func (h HTTPSearcher) Search(ctx context.Context, query string) (string, error) {
	client := h.Client
	if client == nil {
		client = http.DefaultClient
	}
	base := h.baseURL
	if base == "" {
		base = defaultBaseURL
	}

	q := url.Values{}
	q.Set("part", "snippet")
	q.Set("type", "video")
	q.Set("maxResults", "1")
	q.Set("q", query)
	q.Set("key", h.APIKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusForbidden && isQuota(body) {
		return "", ErrQuotaExceeded
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("youtube search: HTTP %d", resp.StatusCode)
	}

	var parsed struct {
		Items []struct {
			ID struct {
				VideoID string `json:"videoId"`
			} `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("youtube search: decode: %w", err)
	}
	if len(parsed.Items) == 0 {
		return "", nil
	}
	return parsed.Items[0].ID.VideoID, nil
}

func isQuota(body []byte) bool {
	var e struct {
		Error struct {
			Errors []struct {
				Reason string `json:"reason"`
			} `json:"errors"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &e); err != nil {
		return false
	}
	for _, x := range e.Error.Errors {
		if x.Reason == "quotaExceeded" || x.Reason == "dailyLimitExceeded" {
			return true
		}
	}
	return false
}
