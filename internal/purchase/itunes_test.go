package purchase

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func serveFile(t *testing.T, name string, capture *url.Values) *httptest.Server {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			*capture = r.URL.Query()
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestITunesHit(t *testing.T) {
	var q url.Values
	srv := serveFile(t, "itunes_hit.json", &q)
	it := NewITunes(srv.Client(), srv.URL)

	got, err := it.Lookup(context.Background(), Query{Artist: "Clan of Xymox", Album: "Medusa"})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.URL != "https://music.apple.com/us/album/medusa/123456" {
		t.Errorf("URL = %q", got.URL)
	}
	if q.Get("entity") != "album" {
		t.Errorf("entity = %q, want album", q.Get("entity"))
	}
}

// The failure that made the confidence gate mandatory: iTunes answers a
// "Theatre Is Evil" query with the real-but-different "Piano Is Evil".
func TestITunesRejectsWrongAlbum(t *testing.T) {
	srv := serveFile(t, "itunes_wrong_album.json", nil)
	it := NewITunes(srv.Client(), srv.URL)

	got, err := it.Lookup(context.Background(), Query{Artist: "Amanda Palmer", Album: "Theatre Is Evil"})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.URL != "" {
		t.Errorf("URL = %q, want empty — Piano Is Evil is a different album", got.URL)
	}
}

// A right-album result with no price is an Apple Music stream, not a purchase.
func TestITunesRejectsStreamOnly(t *testing.T) {
	srv := serveFile(t, "itunes_streamonly.json", nil)
	it := NewITunes(srv.Client(), srv.URL)

	got, err := it.Lookup(context.Background(), Query{Artist: "Rob Zombie", Album: "Hellbilly Deluxe"})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.URL != "" {
		t.Errorf("URL = %q, want empty — collectionPrice <= 0 means not purchasable", got.URL)
	}
}

func TestITunesTrackQueryUsesSongEntity(t *testing.T) {
	var q url.Values
	srv := serveFile(t, "itunes_hit.json", &q)
	it := NewITunes(srv.Client(), srv.URL)

	if _, err := it.Lookup(context.Background(), Query{Artist: "Beach House", Title: "Superstar"}); err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if q.Get("entity") != "song" {
		t.Errorf("entity = %q, want song", q.Get("entity"))
	}
}

func TestITunesName(t *testing.T) {
	if got := NewITunes(nil, "").Name(); got != "itunes" {
		t.Errorf("Name() = %q, want \"itunes\"", got)
	}
}
