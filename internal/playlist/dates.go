package playlist

import "time"

// RefreshDates recomputes DateCreated and DateUpdated from the tracks' added_at
// values: DateCreated is the earliest parseable added_at, DateUpdated the latest
// (both normalized to UTC). All tracks contribute, including orphaned ones.
// When no track has a parseable added_at (e.g. a native playlist), both fall
// back to DateImported so the fields are never left zero in normal operation.
func (p *Playlist) RefreshDates() {
	var earliest, latest time.Time
	found := false
	for _, t := range p.Tracks {
		if t.AddedAt == "" {
			continue
		}
		ts, err := time.Parse(time.RFC3339, t.AddedAt)
		if err != nil {
			continue
		}
		ts = ts.UTC()
		if !found {
			earliest, latest, found = ts, ts, true
			continue
		}
		if ts.Before(earliest) {
			earliest = ts
		}
		if ts.After(latest) {
			latest = ts
		}
	}
	if !found {
		p.DateCreated = p.DateImported
		p.DateUpdated = p.DateImported
		return
	}
	p.DateCreated = earliest
	p.DateUpdated = latest
}

// EnsureImportedDate migrates a pre-existing file whose DateCreated held the
// original "first seen" stamp: when DateImported is zero but DateCreated is set,
// it promotes DateCreated to DateImported. Idempotent — a file that already has
// DateImported is left unchanged. Call before RefreshDates so the fallback and
// the recomputation both see the correct import date.
func (p *Playlist) EnsureImportedDate() {
	if p.DateImported.IsZero() && !p.DateCreated.IsZero() {
		p.DateImported = p.DateCreated
	}
}
