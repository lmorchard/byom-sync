package purchase

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func serveFixture(t *testing.T, name string, capture *map[string]any) *httptest.Server {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			raw, _ := io.ReadAll(r.Body)
			m := map[string]any{}
			_ = json.Unmarshal(raw, &m)
			*capture = m
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestBandcampAlbumHit(t *testing.T) {
	var sent map[string]any
	srv := serveFixture(t, "bandcamp_hit.json", &sent)
	bc := NewBandcamp(srv.Client(), srv.URL)

	got, err := bc.Lookup(context.Background(), Query{Artist: "Amanda Palmer", Album: "Theatre Is Evil"})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	want := "https://amandapalmer.bandcamp.com/album/theatre-is-evil-2"
	if got.URL != want {
		t.Errorf("URL = %q, want %q", got.URL, want)
	}
	if got.Kind != KindAlbum {
		t.Errorf("Kind = %q, want %q", got.Kind, KindAlbum)
	}
	if got.Score < Threshold {
		t.Errorf("Score = %v, want >= %v", got.Score, Threshold)
	}
	if sent["search_filter"] != "a" {
		t.Errorf("search_filter = %v, want \"a\"", sent["search_filter"])
	}
}

// A clean zero-result miss must be a miss, never an error — measured behaviour
// for major-label catalogue that genuinely isn't on Bandcamp.
func TestBandcampCleanMiss(t *testing.T) {
	srv := serveFixture(t, "bandcamp_empty.json", nil)
	bc := NewBandcamp(srv.Client(), srv.URL)

	got, err := bc.Lookup(context.Background(), Query{Artist: "The Smiths", Album: "The Queen Is Dead"})
	if err != nil {
		t.Fatalf("clean miss must not error: %v", err)
	}
	if got.URL != "" {
		t.Errorf("URL = %q, want empty", got.URL)
	}
}

func TestBandcampTrackFilter(t *testing.T) {
	var sent map[string]any
	srv := serveFixture(t, "bandcamp_hit.json", &sent)
	bc := NewBandcamp(srv.Client(), srv.URL)

	if _, err := bc.Lookup(context.Background(), Query{Artist: "Beach House", Title: "Superstar"}); err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if sent["search_filter"] != "t" {
		t.Errorf("search_filter = %v, want \"t\" for a track query", sent["search_filter"])
	}
}

// A result that doesn't match the query must be rejected, not returned.
func TestBandcampBelowThresholdRejected(t *testing.T) {
	srv := serveFixture(t, "bandcamp_hit.json", nil)
	bc := NewBandcamp(srv.Client(), srv.URL)

	got, err := bc.Lookup(context.Background(), Query{Artist: "Nine Inch Nails", Album: "The Downward Spiral"})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.URL != "" {
		t.Errorf("URL = %q, want empty (fixture is a different album)", got.URL)
	}
}

func TestBandcampServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	bc := NewBandcamp(srv.Client(), srv.URL)

	if _, err := bc.Lookup(context.Background(), Query{Artist: "A", Album: "B"}); err == nil {
		t.Error("expected an error for HTTP 500")
	}
}

func TestBandcampName(t *testing.T) {
	if got := NewBandcamp(nil, "").Name(); got != "bandcamp" {
		t.Errorf("Name() = %q, want \"bandcamp\"", got)
	}
}
