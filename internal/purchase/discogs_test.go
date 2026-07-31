package purchase

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// serveDiscogs routes /database/search to the search fixture and everything
// else to the release fixture, rewriting the resource_url placeholder so the
// second request lands back on the same server. releaseHits, if non-nil,
// counts how many times the release endpoint was actually reached: the
// two-step lookup is easy to get wrong in a way that still passes if the
// second request never fires (e.g. a first-pass gate that rejects for the
// wrong reason), so tests that care about the release step assert on this
// count instead of assuming it happened.
func serveDiscogs(t *testing.T, releaseFixture string, gotAuth *string, releaseHits *int) *httptest.Server {
	t.Helper()
	read := func(n string) string {
		b, err := os.ReadFile(filepath.Join("testdata", n))
		if err != nil {
			t.Fatalf("read fixture %s: %v", n, err)
		}
		return string(b)
	}
	search, release := read("discogs_search.json"), read(releaseFixture)

	mux := http.NewServeMux()
	srv := httptest.NewUnstartedServer(mux)
	mux.HandleFunc("/database/search", func(w http.ResponseWriter, r *http.Request) {
		if gotAuth != nil {
			*gotAuth = r.Header.Get("Authorization")
		}
		_, _ = w.Write([]byte(strings.ReplaceAll(
			search, "RESOURCE_URL_PLACEHOLDER", "http://"+srv.Listener.Addr().String()+"/releases/11662135",
		)))
	})
	mux.HandleFunc("/releases/", func(w http.ResponseWriter, _ *http.Request) {
		if releaseHits != nil {
			*releaseHits++
		}
		_, _ = w.Write([]byte(release))
	})
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

func TestDiscogsHitWithListings(t *testing.T) {
	var hits int
	srv := serveDiscogs(t, "discogs_release.json", nil, &hits)
	dg := NewDiscogs(srv.Client(), srv.URL, "")

	got, err := dg.Lookup(context.Background(), Query{Artist: "Rob Zombie", Album: "Hellbilly Deluxe"})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	want := "https://www.discogs.com/release/11662135-Rob-Zombie-Hellbilly-Deluxe"
	if got.URL != want {
		t.Errorf("URL = %q, want %q", got.URL, want)
	}
	// The rescore uses the release lookup's clean artists[]/title, which measured
	// 1.00 on every survivor — far better than splitting "Artist - Album".
	if got.Score < 1.0 {
		t.Errorf("Score = %v, want 1.0 from the authoritative fields", got.Score)
	}
	if hits != 1 {
		t.Errorf("release endpoint hit %d times, want 1 — the two-step lookup must reach it", hits)
	}
}

// A release that matches the query perfectly is still a dead link if nobody's
// selling it — worse than no link at all. The fixture's artist/title matches
// the query on purpose so the only thing that can reject it is num_for_sale;
// the hit count confirms the rejection actually came from the release lookup
// and not from the first-pass search gate rejecting for an unrelated reason.
func TestDiscogsRejectsNothingForSale(t *testing.T) {
	var hits int
	srv := serveDiscogs(t, "discogs_release_unavailable.json", nil, &hits)
	dg := NewDiscogs(srv.Client(), srv.URL, "")

	got, err := dg.Lookup(context.Background(), Query{Artist: "Rob Zombie", Album: "Hellbilly Deluxe"})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.URL != "" {
		t.Errorf("URL = %q, want empty — num_for_sale is 0", got.URL)
	}
	if hits != 1 {
		t.Errorf("release endpoint hit %d times, want 1 — rejection must come from the release lookup", hits)
	}
}

func TestDiscogsSendsToken(t *testing.T) {
	var auth string
	srv := serveDiscogs(t, "discogs_release.json", &auth, nil)
	dg := NewDiscogs(srv.Client(), srv.URL, "secret123")

	if _, err := dg.Lookup(context.Background(), Query{Artist: "Rob Zombie", Album: "Hellbilly Deluxe"}); err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if auth != "Discogs token=secret123" {
		t.Errorf("Authorization = %q", auth)
	}
}

func TestDiscogsNoTokenSendsNoAuth(t *testing.T) {
	var auth string
	srv := serveDiscogs(t, "discogs_release.json", &auth, nil)
	dg := NewDiscogs(srv.Client(), srv.URL, "")

	if _, err := dg.Lookup(context.Background(), Query{Artist: "Rob Zombie", Album: "Hellbilly Deluxe"}); err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if auth != "" {
		t.Errorf("Authorization = %q, want empty", auth)
	}
}

// A search-endpoint failure must surface as an error, not a silent miss.
func TestDiscogsSearchServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	dg := NewDiscogs(srv.Client(), srv.URL, "")

	if _, err := dg.Lookup(context.Background(), Query{Artist: "A", Album: "B"}); err == nil {
		t.Error("expected an error for HTTP 500 from search")
	}
}

// A release-endpoint failure (after a good search match) must also surface as
// an error rather than being swallowed as a clean miss.
func TestDiscogsReleaseServerError(t *testing.T) {
	read := func(n string) string {
		b, err := os.ReadFile(filepath.Join("testdata", n))
		if err != nil {
			t.Fatalf("read fixture %s: %v", n, err)
		}
		return string(b)
	}
	search := read("discogs_search.json")

	mux := http.NewServeMux()
	srv := httptest.NewUnstartedServer(mux)
	mux.HandleFunc("/database/search", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.ReplaceAll(
			search, "RESOURCE_URL_PLACEHOLDER", "http://"+srv.Listener.Addr().String()+"/releases/11662135",
		)))
	})
	mux.HandleFunc("/releases/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv.Start()
	defer srv.Close()
	dg := NewDiscogs(srv.Client(), srv.URL, "")

	if _, err := dg.Lookup(context.Background(), Query{Artist: "Rob Zombie", Album: "Hellbilly Deluxe"}); err == nil {
		t.Error("expected an error for HTTP 500 from the release lookup")
	}
}

func TestDiscogsName(t *testing.T) {
	if got := NewDiscogs(nil, "", "").Name(); got != "discogs" {
		t.Errorf("Name() = %q, want \"discogs\"", got)
	}
}
