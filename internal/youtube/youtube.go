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
	"strconv"
	"strings"
	"time"
)

// ErrQuotaExceeded is returned when the Data API reports the daily quota is
// spent, so the caller can stop and persist progress.
var ErrQuotaExceeded = errors.New("youtube: quota exceeded")

// ErrRateLimited is returned when the API keeps answering 429 (too many
// requests) after retries — a signal to slow down or stop for now.
var ErrRateLimited = errors.New("youtube: rate limited")

// Searcher resolves a query to the top matching video ID, or "" when the API
// answered with no result (a clean miss).
type Searcher interface {
	Search(ctx context.Context, query string) (videoID string, err error)
}

const (
	defaultBaseURL     = "https://www.googleapis.com/youtube/v3/search"
	maxAttempts        = 4               // 1 try + 3 retries on 429
	defaultRetryBackon = 1 * time.Second // base backoff (doubled per attempt)
	maxBackoff         = 30 * time.Second
)

// HTTPSearcher calls the YouTube Data API v3 search.list endpoint.
type HTTPSearcher struct {
	APIKey       string
	Client       *http.Client
	baseURL      string        // overridable in tests; defaults to the Data API endpoint
	retryBackoff time.Duration // base 429 backoff; defaults to 1s (tests set it tiny)
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
	backoff := h.retryBackoff
	if backoff <= 0 {
		backoff = defaultRetryBackon
	}

	q := url.Values{}
	q.Set("part", "snippet")
	q.Set("type", "video")
	q.Set("maxResults", "1")
	q.Set("q", query)
	q.Set("key", h.APIKey)
	reqURL := base + "?" + q.Encode()

	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return "", err
		}
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		// 429: back off (honoring Retry-After) and retry a few times before
		// giving up with ErrRateLimited so the caller can stop.
		if resp.StatusCode == http.StatusTooManyRequests {
			if attempt >= maxAttempts-1 {
				return "", ErrRateLimited
			}
			if err := sleep(ctx, retryDelay(resp, backoff, attempt)); err != nil {
				return "", err
			}
			continue
		}

		if resp.StatusCode != http.StatusOK {
			reason, message := parseAPIError(body)
			if resp.StatusCode == http.StatusForbidden && (reason == "quotaExceeded" || reason == "dailyLimitExceeded") {
				return "", ErrQuotaExceeded
			}
			// Surface Google's own reason/message so misconfig (bad key, referrer
			// restriction, disabled API) is diagnosable, not a bare status code.
			detail := strings.Trim(strings.TrimSpace(reason+": "+message), ": ")
			if detail == "" {
				return "", fmt.Errorf("youtube search: HTTP %d", resp.StatusCode)
			}
			return "", fmt.Errorf("youtube search: HTTP %d: %s", resp.StatusCode, detail)
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
}

// retryDelay picks a 429 backoff: the Retry-After header when present, else an
// exponential backoff from base, capped.
func retryDelay(resp *http.Response, base time.Duration, attempt int) time.Duration {
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		if secs, err := strconv.Atoi(strings.TrimSpace(ra)); err == nil && secs >= 0 {
			return capDur(time.Duration(secs) * time.Second)
		}
	}
	d := base << attempt // base * 2^attempt
	return capDur(d)
}

func capDur(d time.Duration) time.Duration {
	if d > maxBackoff {
		return maxBackoff
	}
	return d
}

// sleep waits for d, or returns early if the context is cancelled.
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// parseAPIError extracts the reason + human message from a Data API error body.
func parseAPIError(body []byte) (reason, message string) {
	var e struct {
		Error struct {
			Message string `json:"message"`
			Errors  []struct {
				Reason  string `json:"reason"`
				Message string `json:"message"`
			} `json:"errors"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) != nil {
		return "", ""
	}
	message = e.Error.Message
	if len(e.Error.Errors) > 0 {
		reason = e.Error.Errors[0].Reason
		if message == "" {
			message = e.Error.Errors[0].Message
		}
	}
	return reason, message
}
