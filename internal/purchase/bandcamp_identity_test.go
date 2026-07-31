package purchase

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Every case here is a real Bandcamp response observed against the live hub.
// The impostor rows are the reason this check exists: each one scores a
// legitimate 1.000 through the confidence gate, because band_name is free text
// the uploader controls.
func TestHostIsArtist(t *testing.T) {
	for _, tc := range []struct {
		name, artist, url string
		want              bool
	}{
		// The artist's own account, exact.
		{"exact", "Radiohead", "https://radiohead.bandcamp.com/album/ok-computer", true},
		{"exact two words", "His Name Is Alive", "https://hisnameisalive.bandcamp.com/album/x", true},
		// Accounts commonly carry a suffix — these must survive.
		{"band suffix", "GLOSSER", "https://glosserband.bandcamp.com/album/x", true},
		{"city suffix", "Ghost Cop", "https://ghostcopnyc.bandcamp.com/album/x", true},
		{"music suffix", "MØAA", "https://moaamusic.bandcamp.com/album/x", true},
		{"punctuation", "M!R!M", "https://m-r-m.bandcamp.com/album/x", true},
		{"digits", "Galaxie 500", "https://galaxie500.bandcamp.com/album/x", true},
		// A dropped leading article.
		{"artist keeps the", "The Beatles", "https://beatles.bandcamp.com/album/x", true},
		{"host keeps the", "Daysleepers", "https://thedaysleepers.bandcamp.com/album/x", true},

		// Impostors: band_name says the real artist, the account does not.
		{"uploader impostor", "Lady Gaga", "https://diegovalente2.bandcamp.com/album/mayhem-for-djs", false},
		{"cover band", "My Chemical Romance", "https://firsttoelevenstems.bandcamp.com/album/welcome-to-the-black-parade", false},
		{"one letter off", "Rage Against the Machine", "https://gageagainstthemachine.bandcamp.com/album/gage-against-the-machine", false},
		// Tribute accounts embed the artist mid-string — this is why the test is
		// a prefix and not a substring.
		{"tribute embeds artist", "Nirvana", "https://nevermindatributetonirvana.bandcamp.com/album/x", false},
		{"tribute suffix", "Audioslave", "https://audiostone-tribute.bandcamp.com/album/x", false},

		{"empty artist", "", "https://radiohead.bandcamp.com/album/x", false},
		{"unparseable", "Radiohead", "://nope", false},
		{"no host", "Radiohead", "/album/x", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := hostIsArtist(tc.artist, tc.url); got != tc.want {
				t.Errorf("hostIsArtist(%q, %q) = %v, want %v", tc.artist, tc.url, got, tc.want)
			}
		})
	}
}

// A result can clear the confidence gate at a perfect 1.000 and still be the
// wrong account. Lookup must reject it rather than returning a URL.
func TestBandcampRejectsImpostorAccount(t *testing.T) {
	// band_name and album match the query exactly; only the host betrays it.
	const body = `{"auto":{"results":[{"type":"a","name":"Mayhem for DJs","band_name":"Lady Gaga",` +
		`"item_url_path":"https://diegovalente2.bandcamp.com/album/mayhem-for-djs"}]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	q := Query{Artist: "Lady Gaga", Album: "MAYHEM"}

	// Guard the premise: the gate alone really does accept this.
	if score, ok := Accept(q, "Lady Gaga", "Mayhem for DJs"); !ok {
		t.Fatalf("premise broken: gate rejected the impostor on its own (score %.3f) — "+
			"this test no longer proves the host check is what saves us", score)
	}

	got, err := NewBandcamp(srv.Client(), srv.URL).Lookup(context.Background(), q)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.URL != "" {
		t.Errorf("URL = %q, want empty — diegovalente2 is not Lady Gaga", got.URL)
	}
}

// The artist's own account must still resolve.
func TestBandcampAcceptsGenuineAccount(t *testing.T) {
	const body = `{"auto":{"results":[{"type":"a","name":"Once Twice Melody","band_name":"Beach House",` +
		`"item_url_path":"https://beachhouse.bandcamp.com/album/once-twice-melody"}]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	got, err := NewBandcamp(srv.Client(), srv.URL).Lookup(context.Background(),
		Query{Artist: "Beach House", Album: "Once Twice Melody"})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.URL != "https://beachhouse.bandcamp.com/album/once-twice-melody" {
		t.Errorf("URL = %q, want the genuine account", got.URL)
	}
}
