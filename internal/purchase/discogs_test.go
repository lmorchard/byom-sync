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

// discogsHeaders records the headers each of the two requests carried. They are
// captured separately because the two-step lookup builds its second request
// from a URL out of the search response — an easy place to lose the headers —
// and Discogs rejects default user agents outright, so a regression there would
// fail in production while a search-only assertion still passed.
type discogsHeaders struct {
	searchAuth, searchUA   string
	releaseAuth, releaseUA string
}

// serveDiscogs routes /database/search to the search fixture and everything
// else to the release fixture, rewriting the resource_url placeholder so the
// second request lands back on the same server. releaseHits, if non-nil,
// counts how many times the release endpoint was actually reached: the
// two-step lookup is easy to get wrong in a way that still passes if the
// second request never fires (e.g. a first-pass gate that rejects for the
// wrong reason), so tests that care about the release step assert on this
// count instead of assuming it happened.
func serveDiscogs(t *testing.T, releaseFixture string, hdrs *discogsHeaders, releaseHits *int) *httptest.Server {
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
		if hdrs != nil {
			hdrs.searchAuth = r.Header.Get("Authorization")
			hdrs.searchUA = r.Header.Get("User-Agent")
		}
		_, _ = w.Write([]byte(strings.ReplaceAll(
			search, "RESOURCE_URL_PLACEHOLDER", "http://"+srv.Listener.Addr().String()+"/releases/11662135",
		)))
	})
	mux.HandleFunc("/releases/", func(w http.ResponseWriter, r *http.Request) {
		if releaseHits != nil {
			*releaseHits++
		}
		if hdrs != nil {
			hdrs.releaseAuth = r.Header.Get("Authorization")
			hdrs.releaseUA = r.Header.Get("User-Agent")
		}
		_, _ = w.Write([]byte(release))
	})
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

// The release fixture carries the shape the *release resource* actually
// returns: an absolute uri. (The search response's uri is relative — see
// TestDiscogsRelativeReleaseURI.) Prefixing the site onto this one is how every
// Discogs link came out as
// "https://www.discogs.comhttps://www.discogs.com/release/...".
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

// The search endpoint's uri is site-relative, unlike the release resource's.
// The code shouldn't care which shape it is handed, so prove the relative form
// still produces one scheme and one host.
func TestDiscogsRelativeReleaseURI(t *testing.T) {
	srv := serveDiscogs(t, "discogs_release_relative_uri.json", nil, nil)
	dg := NewDiscogs(srv.Client(), srv.URL, "")

	got, err := dg.Lookup(context.Background(), Query{Artist: "Rob Zombie", Album: "Hellbilly Deluxe"})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	want := "https://www.discogs.com/release/11662135-Rob-Zombie-Hellbilly-Deluxe"
	if got.URL != want {
		t.Errorf("URL = %q, want %q", got.URL, want)
	}
}

func TestDiscogsPermalink(t *testing.T) {
	cases := map[string]string{
		// The release resource's shape: already absolute, pass through.
		"https://www.discogs.com/release/1-X": "https://www.discogs.com/release/1-X",
		"http://www.discogs.com/release/1-X":  "http://www.discogs.com/release/1-X",
		// The search response's shape: site-relative, needs the host.
		"/release/1-X": "https://www.discogs.com/release/1-X",
		"release/1-X":  "https://www.discogs.com/release/1-X",
	}
	for in, want := range cases {
		if got := discogsPermalink(in); got != want {
			t.Errorf("discogsPermalink(%q) = %q, want %q", in, got, want)
		}
	}
}

// Both requests must carry the headers: Discogs rejects default user agents
// outright, and dropping the token on the second request silently halves the
// rate limit.
func TestDiscogsSendsToken(t *testing.T) {
	var h discogsHeaders
	srv := serveDiscogs(t, "discogs_release.json", &h, nil)
	dg := NewDiscogs(srv.Client(), srv.URL, "secret123")

	if _, err := dg.Lookup(context.Background(), Query{Artist: "Rob Zombie", Album: "Hellbilly Deluxe"}); err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if h.searchAuth != "Discogs token=secret123" {
		t.Errorf("search Authorization = %q", h.searchAuth)
	}
	if h.releaseAuth != "Discogs token=secret123" {
		t.Errorf("release Authorization = %q — the second request must be authenticated too", h.releaseAuth)
	}
	if h.searchUA != userAgent {
		t.Errorf("search User-Agent = %q, want %q", h.searchUA, userAgent)
	}
	if h.releaseUA != userAgent {
		t.Errorf("release User-Agent = %q, want %q", h.releaseUA, userAgent)
	}
}

func TestDiscogsNoTokenSendsNoAuth(t *testing.T) {
	var h discogsHeaders
	srv := serveDiscogs(t, "discogs_release.json", &h, nil)
	dg := NewDiscogs(srv.Client(), srv.URL, "")

	if _, err := dg.Lookup(context.Background(), Query{Artist: "Rob Zombie", Album: "Hellbilly Deluxe"}); err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if h.searchAuth != "" || h.releaseAuth != "" {
		t.Errorf("Authorization = %q/%q, want empty on both requests", h.searchAuth, h.releaseAuth)
	}
	// The user agent is not optional even without a token.
	if h.searchUA != userAgent || h.releaseUA != userAgent {
		t.Errorf("User-Agent = %q/%q, want %q on both requests", h.searchUA, h.releaseUA, userAgent)
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
