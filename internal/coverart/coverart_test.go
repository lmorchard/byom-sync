package coverart

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

// testServer returns an httptest server standing in for both MusicBrainz and
// the Cover Art Archive, routed by path.
func testServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func TestResolver_AlbumPath(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/ws/2/release-group"):
			if ua := r.Header.Get("User-Agent"); ua != "test-agent" {
				t.Errorf("User-Agent not sent to MusicBrainz: %q", ua)
			}
			q := r.URL.Query().Get("query")
			if !strings.Contains(q, `releasegroup:"Abbey Road"`) || !strings.Contains(q, `artist:"The Beatles"`) {
				t.Errorf("unexpected release-group query: %q", q)
			}
			_, _ = w.Write([]byte(`{"release-groups":[{"id":"rg-mbid-1"}]}`))
		case r.URL.Path == "/release-group/rg-mbid-1":
			_, _ = w.Write([]byte(`{"images":[{"front":true,"image":"https://caa/img.jpg","thumbnails":{"500":"https://caa/500.jpg"}}]}`))
		default:
			http.NotFound(w, r)
		}
	})
	r := Resolver{
		MB:  &MBClient{HTTP: srv.Client(), BaseURL: srv.URL + "/ws/2", UserAgent: "test-agent"},
		CAA: &CAAClient{HTTP: srv.Client(), BaseURL: srv.URL},
	}
	res, err := r.Resolve(context.Background(), playlist.Track{Artist: "The Beatles", Title: "Come Together", Album: "Abbey Road"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.ImageURL != "https://caa/500.jpg" {
		t.Errorf("want the 500px thumbnail, got %q", res.ImageURL)
	}
	if res.Source != "musicbrainz-release-group" {
		t.Errorf("source: %q", res.Source)
	}
}

func TestResolver_RecordingFallbackWhenNoAlbum(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/ws/2/recording"):
			_, _ = w.Write([]byte(`{"recordings":[{"id":"rec1","releases":[{"id":"rel-mbid-1"}]}]}`))
		case r.URL.Path == "/release/rel-mbid-1":
			_, _ = w.Write([]byte(`{"images":[{"front":true,"image":"https://caa/rel.jpg","thumbnails":{}}]}`))
		default:
			http.NotFound(w, r)
		}
	})
	r := Resolver{
		MB:  &MBClient{HTTP: srv.Client(), BaseURL: srv.URL + "/ws/2", UserAgent: "ua"},
		CAA: &CAAClient{HTTP: srv.Client(), BaseURL: srv.URL},
	}
	res, err := r.Resolve(context.Background(), playlist.Track{Artist: "Darkness On Demand", Title: "Tragedy For You"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.ImageURL != "https://caa/rel.jpg" { // no 500 thumb -> full image
		t.Errorf("want full image fallback, got %q", res.ImageURL)
	}
	if res.Source != "musicbrainz-recording" {
		t.Errorf("source: %q", res.Source)
	}
}

func TestResolver_UpgradesHTTPArtToHTTPS(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/ws/2/release-group"):
			_, _ = w.Write([]byte(`{"release-groups":[{"id":"rg"}]}`))
		case r.URL.Path == "/release-group/rg":
			// CAA returns an http:// thumbnail URL
			_, _ = w.Write([]byte(`{"images":[{"front":true,"image":"http://caa/x.jpg","thumbnails":{"500":"http://caa/500.jpg"}}]}`))
		default:
			http.NotFound(w, r)
		}
	})
	r := Resolver{
		MB:  &MBClient{HTTP: srv.Client(), BaseURL: srv.URL + "/ws/2", UserAgent: "ua"},
		CAA: &CAAClient{HTTP: srv.Client(), BaseURL: srv.URL},
	}
	res, err := r.Resolve(context.Background(), playlist.Track{Artist: "A", Title: "B", Album: "C"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.ImageURL != "https://caa/500.jpg" {
		t.Errorf("http art URL should be upgraded to https: %q", res.ImageURL)
	}
}

func TestResolver_MissWhenCAA404(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/ws/2/release-group") {
			_, _ = w.Write([]byte(`{"release-groups":[{"id":"rg"}]}`))
			return
		}
		http.NotFound(w, r) // CAA has no art for this MBID
	})
	r := Resolver{
		MB:  &MBClient{HTTP: srv.Client(), BaseURL: srv.URL + "/ws/2", UserAgent: "ua"},
		CAA: &CAAClient{HTTP: srv.Client(), BaseURL: srv.URL},
	}
	res, err := r.Resolve(context.Background(), playlist.Track{Artist: "A", Title: "B", Album: "C"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.ImageURL != "" {
		t.Errorf("expected a miss (no art), got %q", res.ImageURL)
	}
}
