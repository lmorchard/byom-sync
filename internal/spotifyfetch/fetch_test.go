package spotifyfetch

import (
	"context"
	"fmt"
	"testing"

	"github.com/zmb3/spotify/v2"
)

func TestParseID(t *testing.T) {
	cases := map[string]spotify.ID{
		"37i9dQZF1DXcBWIGoYBM5M":                                          "37i9dQZF1DXcBWIGoYBM5M",
		"spotify:playlist:37i9dQZF1DX0XUsuxWHRQd":                         "37i9dQZF1DX0XUsuxWHRQd",
		"https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M":        "37i9dQZF1DXcBWIGoYBM5M",
		"https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M?si=abc": "37i9dQZF1DXcBWIGoYBM5M",
	}
	for in, want := range cases {
		got, err := ParseID(in)
		if err != nil {
			t.Errorf("ParseID(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseID(%q) = %q, want %q", in, got, want)
		}
	}

	if _, err := ParseID("  "); err == nil {
		t.Error("expected error for empty reference")
	}
	if _, err := ParseID("https://open.spotify.com/album/xyz"); err == nil {
		t.Error("expected error for non-playlist URL")
	}
}

func TestSelectOwnedIDs(t *testing.T) {
	playlists := []spotify.SimplePlaylist{
		{ID: "mine1", Owner: spotify.User{ID: "les"}},
		{ID: "followed1", Owner: spotify.User{ID: "spotify"}},
		{ID: "mine2", Owner: spotify.User{ID: "les"}},
		{ID: "followed2", Owner: spotify.User{ID: "someoneelse"}},
	}

	owned := selectOwnedIDs(playlists, "les", false)
	if len(owned) != 2 || owned[0] != "mine1" || owned[1] != "mine2" {
		t.Errorf("owned-only should return [mine1 mine2], got %v", owned)
	}

	all := selectOwnedIDs(playlists, "les", true)
	if len(all) != 4 {
		t.Errorf("include-followed should return all 4, got %v", all)
	}
}

func TestIsCatalogStub(t *testing.T) {
	stub := &spotify.FullTrack{}
	stub.Name = ""
	stub.Artists = nil
	if !isCatalogStub(stub) {
		t.Error("empty title + no artists should be a stub")
	}

	titled := &spotify.FullTrack{}
	titled.Name = "Something"
	if isCatalogStub(titled) {
		t.Error("a track with a title is not a stub")
	}

	arted := &spotify.FullTrack{}
	arted.Artists = []spotify.SimpleArtist{{Name: "A"}}
	if isCatalogStub(arted) {
		t.Error("a track with artists is not a stub")
	}
}

func TestPickImage(t *testing.T) {
	imgs := []spotify.Image{{URL: "xl", Width: 1000}, {URL: "l", Width: 640}, {URL: "s", Width: 64}}
	if got := PickImage(imgs, 640); got != "l" {
		t.Errorf("largest<=640: got %q", got)
	}
	if got := PickImage([]spotify.Image{{URL: "xl", Width: 1000}}, 640); got != "xl" {
		t.Errorf("fallback smallest-above-cap: got %q", got)
	}
	if got := PickImage(nil, 640); got != "" {
		t.Errorf("empty: got %q", got)
	}
}

func TestConvertCapturesImage(t *testing.T) {
	item := spotify.PlaylistItem{Track: spotify.PlaylistItemTrack{Track: &spotify.FullTrack{
		SimpleTrack: spotify.SimpleTrack{Name: "T", Artists: []spotify.SimpleArtist{{Name: "A"}}},
		Album:       spotify.SimpleAlbum{Name: "Alb", Images: []spotify.Image{{URL: "cover", Width: 640}}},
	}}}
	got := convert(item)
	if got.Image != "cover" {
		t.Errorf("convert should capture album art: %q", got.Image)
	}
}

func TestConvert(t *testing.T) {
	ft := &spotify.FullTrack{}
	ft.Name = "My Song"
	ft.Artists = []spotify.SimpleArtist{{Name: "First"}, {Name: "Second"}}
	ft.Album = spotify.SimpleAlbum{Name: "The Album"}
	ft.Duration = 240000
	ft.ExternalIDs = map[string]string{"isrc": "USABC1234567"}
	ft.ID = "abc123trackid"
	ft.ExternalURLs = map[string]string{"spotify": "https://open.spotify.com/track/abc123trackid"}

	item := spotify.PlaylistItem{
		AddedAt: "2025-01-15T12:00:00Z",
		Track:   spotify.PlaylistItemTrack{Track: ft},
	}

	got := convert(item)

	if got.Title != "My Song" {
		t.Errorf("title: %q", got.Title)
	}
	if got.Artist != "First, Second" {
		t.Errorf("artists not joined: %q", got.Artist)
	}
	if got.Album != "The Album" {
		t.Errorf("album: %q", got.Album)
	}
	if got.ISRC != "USABC1234567" {
		t.Errorf("isrc: %q", got.ISRC)
	}
	if got.SpotifyID != "abc123trackid" {
		t.Errorf("spotify_id: %q", got.SpotifyID)
	}
	if got.SpotifyURL != "https://open.spotify.com/track/abc123trackid" {
		t.Errorf("spotify_url: %q", got.SpotifyURL)
	}
	if got.DurationMS != 240000 {
		t.Errorf("duration: %d", got.DurationMS)
	}
	if got.AddedAt != "2025-01-15T12:00:00Z" {
		t.Errorf("added_at: %q", got.AddedAt)
	}
	if !got.SyncState.SpotifyPresent {
		t.Errorf("should be marked present")
	}
}

type fakeTrackGetter struct {
	byID  map[string]*spotify.FullTrack
	calls [][]spotify.ID
}

func (f *fakeTrackGetter) GetTracks(_ context.Context, ids []spotify.ID, _ ...spotify.RequestOption) ([]*spotify.FullTrack, error) {
	f.calls = append(f.calls, ids)
	out := make([]*spotify.FullTrack, len(ids))
	for i, id := range ids {
		out[i] = f.byID[string(id)] // nil when absent
	}
	return out, nil
}

func TestFetchTrackArt(t *testing.T) {
	withArt := func(id, url string) *spotify.FullTrack {
		return &spotify.FullTrack{
			SimpleTrack: spotify.SimpleTrack{ID: spotify.ID(id)},
			Album:       spotify.SimpleAlbum{Images: []spotify.Image{{URL: url, Width: 640}}},
		}
	}
	g := &fakeTrackGetter{byID: map[string]*spotify.FullTrack{
		"a": withArt("a", "art-a"),
		"c": withArt("c", ""), // resolved but no images -> skipped
		// "b" not found -> nil
	}}
	got, err := FetchTrackArt(context.Background(), g, []string{"a", "b", "c"}, 640)
	if err != nil {
		t.Fatalf("FetchTrackArt: %v", err)
	}
	if got["a"] != "art-a" {
		t.Errorf("a: got %q", got["a"])
	}
	if _, ok := got["b"]; ok {
		t.Errorf("not-found id should be absent: %v", got["b"])
	}
	if _, ok := got["c"]; ok {
		t.Errorf("no-image id should be absent: %v", got["c"])
	}
}

func TestFetchTrackArt_Chunks(t *testing.T) {
	ids := make([]string, 120)
	for i := range ids {
		ids[i] = fmt.Sprintf("id%d", i)
	}
	g := &fakeTrackGetter{byID: map[string]*spotify.FullTrack{}}
	if _, err := FetchTrackArt(context.Background(), g, ids, 640); err != nil {
		t.Fatalf("FetchTrackArt: %v", err)
	}
	if len(g.calls) != 3 { // 120 ids -> 50 + 50 + 20
		t.Errorf("expected 3 chunked calls, got %d", len(g.calls))
	}
	if len(g.calls[0]) != 50 || len(g.calls[2]) != 20 {
		t.Errorf("chunk sizes wrong: %d / %d", len(g.calls[0]), len(g.calls[2]))
	}
}
