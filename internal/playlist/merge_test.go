package playlist

import (
	"testing"
	"time"
)

var testNow = time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)

func track(title, artist, isrc string) Track {
	return Track{Title: title, Artist: artist, ISRC: isrc, SyncState: SyncState{SpotifyPresent: true}}
}

func findTrack(p Playlist, title string) (Track, bool) {
	for _, t := range p.Tracks {
		if t.Title == title {
			return t, true
		}
	}
	return Track{}, false
}

func TestMerge_ArchiveAddsAndOrphans(t *testing.T) {
	local := Playlist{
		SpotifyID: "PID", Title: "Old Title",
		Tracks: []Track{track("Keep", "A", "ISRC-K"), track("Gone", "B", "ISRC-G")},
	}
	remote := Playlist{
		SpotifyID: "PID", Title: "New Title", Creator: "Les",
		Tracks: []Track{track("Keep", "A", "ISRC-K"), track("New", "C", "ISRC-N")},
	}

	out := Merge(local, remote, Archive, testNow)

	// Metadata comes from remote.
	if out.Title != "New Title" || out.Creator != "Les" {
		t.Errorf("metadata not from remote: %+v", out)
	}
	// Remote tracks present and marked present.
	keep, ok := findTrack(out, "Keep")
	if !ok || !keep.SyncState.SpotifyPresent {
		t.Errorf("Keep missing or not present: %+v", keep)
	}
	newt, ok := findTrack(out, "New")
	if !ok || !newt.SyncState.SpotifyPresent {
		t.Errorf("New track missing or not present: %+v", newt)
	}
	// Local-only track orphaned, not deleted.
	gone, ok := findTrack(out, "Gone")
	if !ok {
		t.Fatalf("orphaned track was deleted")
	}
	if gone.SyncState.SpotifyPresent {
		t.Errorf("orphaned track still marked present")
	}
	if gone.SyncState.DateOrphaned != testNow.Format(time.RFC3339) {
		t.Errorf("orphan date not set to now: got %q", gone.SyncState.DateOrphaned)
	}
	// Order: remote tracks first, orphans appended.
	if out.Tracks[len(out.Tracks)-1].Title != "Gone" {
		t.Errorf("orphan not appended last: %+v", out.Tracks)
	}
}

func TestMerge_ArchivePreservesExistingOrphanDate(t *testing.T) {
	orphaned := track("Gone", "B", "ISRC-G")
	orphaned.SyncState = SyncState{SpotifyPresent: false, DateOrphaned: "2020-01-01T00:00:00Z"}
	local := Playlist{SpotifyID: "PID", Tracks: []Track{orphaned}}
	remote := Playlist{SpotifyID: "PID", Tracks: []Track{track("New", "C", "ISRC-N")}}

	out := Merge(local, remote, Archive, testNow)

	gone, ok := findTrack(out, "Gone")
	if !ok {
		t.Fatalf("orphan deleted")
	}
	if gone.SyncState.DateOrphaned != "2020-01-01T00:00:00Z" {
		t.Errorf("existing orphan date overwritten: got %q", gone.SyncState.DateOrphaned)
	}
}

func TestMerge_MirrorDiscardsLocalOnly(t *testing.T) {
	local := Playlist{SpotifyID: "PID", Tracks: []Track{track("Keep", "A", "ISRC-K"), track("Gone", "B", "ISRC-G")}}
	remote := Playlist{SpotifyID: "PID", Title: "R", Tracks: []Track{track("Keep", "A", "ISRC-K")}}

	out := Merge(local, remote, Mirror, testNow)

	if len(out.Tracks) != 1 {
		t.Fatalf("mirror should have exactly remote tracks: got %d", len(out.Tracks))
	}
	if _, ok := findTrack(out, "Gone"); ok {
		t.Errorf("mirror kept local-only track")
	}
	if !out.Tracks[0].SyncState.SpotifyPresent {
		t.Errorf("mirror track not marked present")
	}
	if out.Title != "R" {
		t.Errorf("mirror metadata not from remote")
	}
}

func TestMerge_ArchiveRevivesOrphanWhenBackInRemote(t *testing.T) {
	// A previously-orphaned local track that reappears in remote should be
	// present again with a cleared orphan date.
	orphaned := track("Back", "A", "ISRC-B")
	orphaned.SyncState = SyncState{SpotifyPresent: false, DateOrphaned: "2020-01-01T00:00:00Z"}
	local := Playlist{SpotifyID: "PID", Tracks: []Track{orphaned}}
	remote := Playlist{SpotifyID: "PID", Tracks: []Track{track("Back", "A", "ISRC-B")}}

	out := Merge(local, remote, Archive, testNow)

	back, ok := findTrack(out, "Back")
	if !ok {
		t.Fatalf("track missing")
	}
	if !back.SyncState.SpotifyPresent || back.SyncState.DateOrphaned != "" {
		t.Errorf("revived track not cleared: %+v", back.SyncState)
	}
	if len(out.Tracks) != 1 {
		t.Errorf("track duplicated: got %d", len(out.Tracks))
	}
}

func TestMerge_PreservesLocallyDerivedTrackFields(t *testing.T) {
	// A track that is still on the remote playlist carries local work that
	// Spotify knows nothing about: resolved YouTube ids, downloaded art, the
	// enrichment opt-out, and pending enrich candidates. The freshly fetched
	// remote track has none of these, so replacing the local track wholesale
	// destroys every `resolve` run.
	no := false
	local := track("Keep", "A", "ISRC-K")
	local.YouTubeID = "yt-keep"
	local.ImageFile = "art/ab/abcdef.jpg"
	local.Spotify = &no
	local.EnrichCandidates = []EnrichCandidate{{SpotifyID: "cand-1"}}
	local.Image = "http://mb/art.jpg" // filled by resolve art, not by Spotify

	remote := track("Keep", "A", "ISRC-K")
	remote.DurationMS = 12345 // remote metadata should still win

	out := Merge(
		Playlist{SpotifyID: "PID", Tracks: []Track{local}},
		Playlist{SpotifyID: "PID", Tracks: []Track{remote}},
		Archive, testNow,
	)

	keep, ok := findTrack(out, "Keep")
	if !ok {
		t.Fatal("track missing")
	}
	if keep.YouTubeID != "yt-keep" {
		t.Errorf("YouTubeID = %q, want yt-keep (resolve youtube work destroyed)", keep.YouTubeID)
	}
	if keep.ImageFile != "art/ab/abcdef.jpg" {
		t.Errorf("ImageFile = %q, want art/ab/abcdef.jpg (downloaded art reference lost)", keep.ImageFile)
	}
	if keep.Spotify == nil || *keep.Spotify {
		t.Errorf("Spotify opt-out lost: %v", keep.Spotify)
	}
	if len(keep.EnrichCandidates) != 1 || keep.EnrichCandidates[0].SpotifyID != "cand-1" {
		t.Errorf("EnrichCandidates lost: %+v", keep.EnrichCandidates)
	}
	// Remote still owns descriptive metadata.
	if keep.DurationMS != 12345 {
		t.Errorf("DurationMS = %d, want 12345 from remote", keep.DurationMS)
	}
}

func TestMerge_RemoteImageWinsWhenPresent(t *testing.T) {
	// Spotify's album art is authoritative when it has any; the local URL is
	// only a fallback for tracks Spotify had no art for.
	local := track("Keep", "A", "ISRC-K")
	local.Image = "http://mb/old.jpg"
	remote := track("Keep", "A", "ISRC-K")
	remote.Image = "http://spotify/new.jpg"

	out := Merge(
		Playlist{SpotifyID: "PID", Tracks: []Track{local}},
		Playlist{SpotifyID: "PID", Tracks: []Track{remote}},
		Archive, testNow,
	)

	keep, _ := findTrack(out, "Keep")
	if keep.Image != "http://spotify/new.jpg" {
		t.Errorf("Image = %q, want the remote URL", keep.Image)
	}
}

func TestMerge_MirrorPreservesLocallyDerivedTrackFields(t *testing.T) {
	// Mirror drops local-only *tracks*; it should not un-resolve the tracks that
	// survive.
	local := track("Keep", "A", "ISRC-K")
	local.YouTubeID = "yt-keep"

	out := Merge(
		Playlist{SpotifyID: "PID", Tracks: []Track{local, track("Gone", "B", "ISRC-G")}},
		Playlist{SpotifyID: "PID", Tracks: []Track{track("Keep", "A", "ISRC-K")}},
		Mirror, testNow,
	)

	if len(out.Tracks) != 1 {
		t.Fatalf("mirror should keep only remote tracks: got %d", len(out.Tracks))
	}
	if out.Tracks[0].YouTubeID != "yt-keep" {
		t.Errorf("mirror lost YouTubeID: %q", out.Tracks[0].YouTubeID)
	}
}

func TestMerge_PreservesLocalPresentationFields(t *testing.T) {
	// Playlist-level presentation is authored locally and has no Spotify
	// counterpart, so a sync must not clear it.
	local := Playlist{
		SpotifyID: "PID", Featured: true,
		Image: "http://local/hero.jpg", ImageFile: "art/cd/hero.jpg",
	}
	remote := Playlist{SpotifyID: "PID", Title: "New Title"}

	out := Merge(local, remote, Archive, testNow)

	if !out.Featured {
		t.Error("Featured lost — a sync would silently un-feature the playlist")
	}
	if out.Image != "http://local/hero.jpg" {
		t.Errorf("Image = %q, want the local hero URL", out.Image)
	}
	if out.ImageFile != "art/cd/hero.jpg" {
		t.Errorf("ImageFile = %q, want the local hero path", out.ImageFile)
	}
	if out.Title != "New Title" {
		t.Errorf("Title = %q, want remote's", out.Title)
	}
}

// purchase_url is locally derived — Spotify never sends it back — so Merge must
// carry it across or a single sync silently deletes every resolved link.
func TestMergePreservesPurchaseURL(t *testing.T) {
	local := Playlist{Tracks: []Track{{
		Title: "Superstar", Artist: "Beach House", Album: "Once Twice Melody",
		PurchaseURL: "https://beachhouse.bandcamp.com/album/once-twice-melody",
	}}}
	remote := Playlist{Tracks: []Track{{
		Title: "Superstar", Artist: "Beach House", Album: "Once Twice Melody",
	}}}

	for _, strat := range []Strategy{Archive, Mirror} {
		got := Merge(local, remote, strat, time.Now())
		if len(got.Tracks) != 1 {
			t.Fatalf("%s: got %d tracks, want 1", strat, len(got.Tracks))
		}
		if got.Tracks[0].PurchaseURL != local.Tracks[0].PurchaseURL {
			t.Errorf("%s: purchase_url = %q, want %q — sync would silently wipe it",
				strat, got.Tracks[0].PurchaseURL, local.Tracks[0].PurchaseURL)
		}
	}
}
