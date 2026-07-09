package youtube

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

func newTestOdesli(h http.HandlerFunc) (OdesliResolver, *httptest.Server) {
	srv := httptest.NewServer(h)
	return OdesliResolver{Client: srv.Client(), baseURL: srv.URL, retryBackoff: time.Millisecond}, srv
}

var trackWithURL = playlist.Track{
	Artist:     "Kavinsky",
	Title:      "Nightcall",
	SpotifyURL: "https://open.spotify.com/track/ABC",
}

func TestOdesliResolvesYouTubeLink(t *testing.T) {
	var gotURL string
	o, srv := newTestOdesli(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.Query().Get("url")
		_, _ = w.Write([]byte(`{"linksByPlatform":{"youtube":{"url":"https://www.youtube.com/watch?v=vidX"}}}`))
	})
	defer srv.Close()

	res, err := o.Resolve(context.Background(), trackWithURL)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.VideoID != "vidX" || res.Source != "odesli" {
		t.Errorf("res=%+v, want vidX via odesli", res)
	}
	if gotURL != trackWithURL.SpotifyURL {
		t.Errorf("sent url=%q, want the track's spotify url", gotURL)
	}
}

func TestOdesliFallsBackToYouTubeMusic(t *testing.T) {
	o, srv := newTestOdesli(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"linksByPlatform":{"youtubeMusic":{"url":"https://music.youtube.com/watch?v=musY"}}}`))
	})
	defer srv.Close()
	res, err := o.Resolve(context.Background(), trackWithURL)
	if err != nil || res.VideoID != "musY" {
		t.Errorf("res=%+v err=%v, want musY", res, err)
	}
}

func TestOdesliNoYouTubeLinkIsMiss(t *testing.T) {
	o, srv := newTestOdesli(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"linksByPlatform":{"spotify":{"url":"https://open.spotify.com/track/ABC"}}}`))
	})
	defer srv.Close()
	res, err := o.Resolve(context.Background(), trackWithURL)
	if err != nil || res.VideoID != "" {
		t.Errorf("want clean miss, got res=%+v err=%v", res, err)
	}
}

func TestOdesli404IsMiss(t *testing.T) {
	o, srv := newTestOdesli(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	defer srv.Close()
	res, err := o.Resolve(context.Background(), trackWithURL)
	if err != nil || res.VideoID != "" {
		t.Errorf("404 should be a clean miss, got res=%+v err=%v", res, err)
	}
}

func TestOdesliNoSpotifyURLSkipsRequest(t *testing.T) {
	var called bool
	o, srv := newTestOdesli(func(w http.ResponseWriter, _ *http.Request) {
		called = true
	})
	defer srv.Close()
	res, err := o.Resolve(context.Background(), playlist.Track{Artist: "A", Title: "T"})
	if err != nil || res.VideoID != "" {
		t.Errorf("want miss, got res=%+v err=%v", res, err)
	}
	if called {
		t.Error("should not call song.link when the track has no Spotify URL")
	}
}

func TestOdesliRetriesOn429ThenSucceeds(t *testing.T) {
	var calls int
	o, srv := newTestOdesli(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"linksByPlatform":{"youtube":{"url":"https://youtu.be/beID"}}}`))
	})
	defer srv.Close()
	res, err := o.Resolve(context.Background(), trackWithURL)
	if err != nil || res.VideoID != "beID" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if calls != 2 {
		t.Errorf("calls=%d, want 2", calls)
	}
}

func TestOdesliRateLimitedAfterRetries(t *testing.T) {
	o, srv := newTestOdesli(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	defer srv.Close()
	_, err := o.Resolve(context.Background(), trackWithURL)
	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("want ErrRateLimited, got %v", err)
	}
}

func TestParseYouTubeID(t *testing.T) {
	cases := map[string]string{
		"https://www.youtube.com/watch?v=abc123":       "abc123",
		"https://music.youtube.com/watch?v=xyz789&foo": "xyz789",
		"https://youtu.be/short99":                     "short99",
		"https://open.spotify.com/track/ABC":           "",
		"":                                             "",
	}
	for in, want := range cases {
		if got := parseYouTubeID(in); got != want {
			t.Errorf("parseYouTubeID(%q) = %q, want %q", in, got, want)
		}
	}
}
