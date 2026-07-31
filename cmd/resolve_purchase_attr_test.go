package cmd

import "testing"

// Attribution decides what --reresolve destroys, so it has to be exact in both
// directions: clear what the tier really wrote, spare everything else.
func TestMatchesPurchaseSource(t *testing.T) {
	bandcamp := purchaseSourceMarkers["bandcamp"]
	itunes := purchaseSourceMarkers["itunes"]
	discogs := purchaseSourceMarkers["discogs"]

	for _, tc := range []struct {
		name    string
		url     string
		markers []string
		want    bool
	}{
		{"plain bandcamp album", "https://beachhouse.bandcamp.com/album/once-twice-melody", bandcamp, true},
		{"bandcamp track", "https://beachhouse.bandcamp.com/track/superstar", bandcamp, true},
		{"apple album", "https://music.apple.com/us/album/medusa/123", itunes, true},
		{"legacy itunes host", "https://itunes.apple.com/us/album/x/1", itunes, true},
		{"discogs release", "https://www.discogs.com/release/11662135-Rob-Zombie", discogs, true},

		// The malformed URL byom-sync itself once emitted. Its parsed host is
		// "www.discogs.comhttps", so a host-*suffix* test would miss it — and
		// this is precisely the link --reresolve exists to clear.
		{
			"doubled-host discogs URL must still be attributed",
			"https://www.discogs.comhttps://www.discogs.com/release/11662135-Rob-Zombie",
			discogs, true,
		},

		// A hand-authored URL that merely mentions a store elsewhere must
		// survive: the marker appears in the query, not the host.
		{"marker only in query string", "https://duckduckgo.com/?q=bandcamp.com+beach+house", bandcamp, false},
		{"marker only in path", "https://example.com/links/bandcamp.com/beach-house", bandcamp, false},

		// Cross-tier: one tier's clear must not take another's links.
		{"bandcamp markers vs apple link", "https://music.apple.com/us/album/x/1", bandcamp, false},
		{"discogs markers vs bandcamp link", "https://beachhouse.bandcamp.com/album/x", discogs, false},

		// No host to inspect — fall back to the whole string rather than
		// silently sparing a link some tier wrote.
		{"unparseable but clearly bandcamp", "bandcamp.com/album/x", bandcamp, true},
		{"empty", "", bandcamp, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchesPurchaseSource(tc.url, tc.markers); got != tc.want {
				t.Errorf("matchesPurchaseSource(%q, %v) = %v, want %v", tc.url, tc.markers, got, tc.want)
			}
		})
	}
}
