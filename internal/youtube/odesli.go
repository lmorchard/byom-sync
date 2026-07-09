package youtube

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

const defaultOdesliBaseURL = "https://api.song.link/v1-alpha.1/links"

// OdesliResolver maps a track to a YouTube video id via the free song.link API,
// using the Spotify URL already stored on the track — so it costs no YouTube
// quota. It returns a clean miss when the track has no Spotify URL or song.link
// has no YouTube link for it.
type OdesliResolver struct {
	Client       *http.Client
	APIKey       string        // optional; raises song.link's rate limit
	baseURL      string        // test override
	retryBackoff time.Duration // 429 backoff base; defaults to 2s
}

func (OdesliResolver) Name() string { return "odesli" }

func (o OdesliResolver) Resolve(ctx context.Context, t playlist.Track) (Result, error) {
	if t.SpotifyURL == "" {
		return Result{}, nil // nothing for Odesli to translate
	}
	client := o.Client
	if client == nil {
		client = http.DefaultClient
	}
	base := o.baseURL
	if base == "" {
		base = defaultOdesliBaseURL
	}
	backoff := o.retryBackoff
	if backoff <= 0 {
		backoff = 2 * time.Second
	}

	q := url.Values{}
	q.Set("url", t.SpotifyURL)
	q.Set("songIfSingle", "true")
	if o.APIKey != "" {
		q.Set("key", o.APIKey)
	}
	reqURL := base + "?" + q.Encode()

	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return Result{}, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return Result{}, err
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests {
			if attempt >= maxAttempts-1 {
				return Result{}, ErrRateLimited
			}
			if err := sleep(ctx, retryDelay(resp, backoff, attempt)); err != nil {
				return Result{}, err
			}
			continue
		}
		if resp.StatusCode == http.StatusNotFound {
			return Result{}, nil // song.link has no entry — clean miss
		}
		if resp.StatusCode != http.StatusOK {
			return Result{}, fmt.Errorf("odesli: HTTP %d", resp.StatusCode)
		}

		var parsed struct {
			LinksByPlatform struct {
				YouTube struct {
					URL string `json:"url"`
				} `json:"youtube"`
				YouTubeMusic struct {
					URL string `json:"url"`
				} `json:"youtubeMusic"`
			} `json:"linksByPlatform"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return Result{}, fmt.Errorf("odesli: decode: %w", err)
		}
		link := parsed.LinksByPlatform.YouTube.URL
		if link == "" {
			link = parsed.LinksByPlatform.YouTubeMusic.URL
		}
		id := parseYouTubeID(link)
		if id == "" {
			return Result{}, nil // no YouTube link — clean miss
		}
		return Result{VideoID: id, Source: "odesli"}, nil
	}
}

// parseYouTubeID extracts the video id from a youtube.com/watch?v=,
// music.youtube.com/watch?v=, or youtu.be/<id> URL.
func parseYouTubeID(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if v := u.Query().Get("v"); v != "" {
		return v
	}
	if strings.Contains(u.Host, "youtu.be") {
		return strings.Trim(u.Path, "/")
	}
	return ""
}
