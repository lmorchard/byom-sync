package playlist

import (
	"testing"
	"time"
)

func trackAt(added string) Track { return Track{Title: "t", Artist: "a", AddedAt: added} }

func TestRefreshDates_MinMaxAcrossTracks(t *testing.T) {
	p := Playlist{
		DateImported: time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC),
		Tracks: []Track{
			trackAt("2024-11-07T22:04:35Z"),
			trackAt("2020-01-01T00:00:00Z"), // earliest
			trackAt("2025-06-15T12:00:00Z"), // latest
		},
	}
	p.RefreshDates()
	if want := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC); !p.DateCreated.Equal(want) {
		t.Errorf("DateCreated: got %v want %v", p.DateCreated, want)
	}
	if want := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC); !p.DateUpdated.Equal(want) {
		t.Errorf("DateUpdated: got %v want %v", p.DateUpdated, want)
	}
}

func TestRefreshDates_IgnoresMissingAndUnparseable(t *testing.T) {
	p := Playlist{
		DateImported: time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC),
		Tracks: []Track{
			trackAt(""),             // no added_at
			trackAt("not-a-date"),   // unparseable
			trackAt("2023-03-03T03:03:03Z"),
		},
	}
	p.RefreshDates()
	want := time.Date(2023, 3, 3, 3, 3, 3, 0, time.UTC)
	if !p.DateCreated.Equal(want) || !p.DateUpdated.Equal(want) {
		t.Errorf("single valid track: created=%v updated=%v want %v", p.DateCreated, p.DateUpdated, want)
	}
}

func TestRefreshDates_FallsBackToImportedWhenNoDates(t *testing.T) {
	imported := time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)
	p := Playlist{
		DateImported: imported,
		Tracks:       []Track{trackAt(""), {Title: "x", Artist: "y"}},
	}
	p.RefreshDates()
	if !p.DateCreated.Equal(imported) || !p.DateUpdated.Equal(imported) {
		t.Errorf("fallback: created=%v updated=%v want %v", p.DateCreated, p.DateUpdated, imported)
	}
}

func TestEnsureImportedDate_PromotesOldCreated(t *testing.T) {
	created := time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)
	p := Playlist{DateCreated: created} // pre-migration file: imported absent
	p.EnsureImportedDate()
	if !p.DateImported.Equal(created) {
		t.Errorf("DateImported: got %v want %v", p.DateImported, created)
	}
}

func TestEnsureImportedDate_Idempotent(t *testing.T) {
	imported := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	created := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	p := Playlist{DateImported: imported, DateCreated: created}
	p.EnsureImportedDate()
	if !p.DateImported.Equal(imported) {
		t.Errorf("should not overwrite existing imported: got %v want %v", p.DateImported, imported)
	}
}
