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
