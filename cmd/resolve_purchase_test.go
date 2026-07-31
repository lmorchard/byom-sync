package cmd

import (
	"testing"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/lmorchard/byom-sync/internal/purchase"
)

func TestPurchaseSourcesForOrder(t *testing.T) {
	got, err := purchaseSourcesFor("all", "")
	if err != nil {
		t.Fatalf("purchaseSourcesFor: %v", err)
	}
	want := []string{"bandcamp", "itunes", "discogs"}
	if len(got) != len(want) {
		t.Fatalf("got %d sources, want %d", len(got), len(want))
	}
	for i, s := range got {
		if s.Name() != want[i] {
			t.Errorf("tier %d = %q, want %q", i, s.Name(), want[i])
		}
	}
}

func TestPurchaseSourcesForSingle(t *testing.T) {
	got, err := purchaseSourcesFor("itunes", "")
	if err != nil {
		t.Fatalf("purchaseSourcesFor: %v", err)
	}
	if len(got) != 1 || got[0].Name() != "itunes" {
		t.Errorf("got %v, want just itunes", got)
	}
}

func TestPurchaseSourcesForUnknown(t *testing.T) {
	if _, err := purchaseSourcesFor("napster", ""); err == nil {
		t.Error("expected an error for an unknown source")
	}
}

// Each tier has its own limit; a single --delay would be wrong for all of them.
func TestPurchasePaceForSource(t *testing.T) {
	for _, tc := range []struct {
		name string
		min  time.Duration
	}{
		{"bandcamp", time.Second},
		{"itunes", 3 * time.Second},
		{"discogs", 4900 * time.Millisecond}, // two requests per lookup
	} {
		if got := purchasePaceFor(tc.name, 0, false); got < tc.min {
			t.Errorf("pace for %s = %v, want >= %v", tc.name, got, tc.min)
		}
	}
}

// An explicit --delay raises the floor but must never go below the source's own.
func TestPurchasePaceForRespectsFloor(t *testing.T) {
	if got := purchasePaceFor("itunes", 10*time.Millisecond, false); got < 3*time.Second {
		t.Errorf("pace = %v, want the source floor to win over a smaller --delay", got)
	}
	if got := purchasePaceFor("bandcamp", 5*time.Second, false); got != 5*time.Second {
		t.Errorf("pace = %v, want the larger explicit delay to win", got)
	}
}

var _ purchase.Source = (*purchase.Bandcamp)(nil)

// --reresolve has to un-write one tier's links without touching the others'.
func TestClearPurchaseURLsIsSourceScoped(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{
		{Title: "bc", PurchaseURL: "https://artist.bandcamp.com/album/x"},
		{Title: "it", PurchaseURL: "https://music.apple.com/us/album/x/123"},
		{Title: "dg", PurchaseURL: "https://www.discogs.com/release/1-X"},
		{Title: "none"},
	}}

	if n := clearPurchaseURLs(p, "discogs"); n != 1 {
		t.Errorf("cleared %d, want 1", n)
	}
	if p.Tracks[2].PurchaseURL != "" {
		t.Error("the discogs link should have been cleared")
	}
	if p.Tracks[0].PurchaseURL == "" || p.Tracks[1].PurchaseURL == "" {
		t.Error("other tiers' links must survive")
	}
}

// The reason --reresolve exists: recovering from links a tier got wrong. A
// malformed URL parses to a nonsense host, so the match must not depend on
// parsing one out.
func TestClearPurchaseURLsCatchesMalformedLinks(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{
		{PurchaseURL: "https://www.discogs.comhttps://www.discogs.com/release/1-X"},
	}}
	if n := clearPurchaseURLs(p, "discogs"); n != 1 {
		t.Fatalf("cleared %d, want 1 — a malformed link is exactly what needs clearing", n)
	}
}

func TestClearPurchaseURLsUnknownSource(t *testing.T) {
	p := &playlist.Playlist{Tracks: []playlist.Track{{PurchaseURL: "https://example.com/x"}}}
	if n := clearPurchaseURLs(p, "napster"); n != 0 {
		t.Errorf("cleared %d, want 0 for an unknown source", n)
	}
	if p.Tracks[0].PurchaseURL == "" {
		t.Error("an unknown source must not clear anything")
	}
}

// Every tier that can be selected must be recognisable from the URLs it writes,
// or --reresolve would silently do nothing for it.
func TestEveryPurchaseTierHasMarkers(t *testing.T) {
	for _, name := range purchaseTierOrder {
		if len(purchaseSourceMarkers[name]) == 0 {
			t.Errorf("tier %q has no purchaseSourceMarkers entry", name)
		}
	}
}

// A Discogs lookup makes two HTTP requests (search, then the release endpoint),
// so the per-lookup floor must be twice the per-request gap. Pacing it as one
// request ran a live sample at ~48 req/min against a 25/min limit and drew
// HTTP 429s, which the consecutive-error breaker would turn into an aborted
// tier mid-run.
func TestPurchasePaceForCountsRequestsPerLookup(t *testing.T) {
	single := purchasePaceFor("bandcamp", 0, false)
	if want := purchaseSourceRequestGap["bandcamp"]; single != want {
		t.Errorf("bandcamp pace = %v, want %v (one request per lookup)", single, want)
	}
	two := purchasePaceFor("discogs", 0, false)
	if want := 2 * purchaseSourceRequestGap["discogs"]; two != want {
		t.Errorf("discogs pace = %v, want %v (two requests per lookup)", two, want)
	}
	if two < 4800*time.Millisecond {
		t.Errorf("discogs pace %v is under 25 req/min for a two-request lookup", two)
	}
}

// A token raises Discogs to 60 req/min, so the floor drops — but still doubles
// for the second request.
func TestPurchasePaceForDiscogsToken(t *testing.T) {
	unauth := purchasePaceFor("discogs", 0, false)
	authed := purchasePaceFor("discogs", 0, true)
	if authed >= unauth {
		t.Errorf("authenticated pace %v should be shorter than unauthenticated %v", authed, unauth)
	}
	if want := 2 * discogsAuthedRequestGap; authed != want {
		t.Errorf("authenticated discogs pace = %v, want %v", authed, want)
	}
	// 60 req/min with two requests per lookup means no faster than ~2s/lookup.
	if authed < 2*time.Second {
		t.Errorf("authenticated pace %v exceeds 60 req/min for a two-request lookup", authed)
	}
}
