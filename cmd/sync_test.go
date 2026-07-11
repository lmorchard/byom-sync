package cmd

import (
	"reflect"
	"testing"
	"time"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

func TestSelectTargets(t *testing.T) {
	config := []string{"cfg1", "cfg2"}

	// Positional args override the config list entirely.
	if got := selectTargets([]string{"argA", "argB"}, config); !reflect.DeepEqual(got, []string{"argA", "argB"}) {
		t.Errorf("args should override config: got %v", got)
	}

	// No args → use config list.
	if got := selectTargets(nil, config); !reflect.DeepEqual(got, config) {
		t.Errorf("empty args should use config: got %v", got)
	}

	// Both empty → empty.
	if got := selectTargets(nil, nil); len(got) != 0 {
		t.Errorf("both empty should be empty: got %v", got)
	}
}

func TestImportedDate(t *testing.T) {
	now := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)

	// First sync (no existing file): stamp now.
	if got := importedDate(playlist.Playlist{}, false, now); !got.Equal(now) {
		t.Errorf("first sync: got %v want %v", got, now)
	}

	// Re-sync of a file that already has date_imported: preserve it.
	imported := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	local := playlist.Playlist{DateImported: imported, DateCreated: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}
	if got := importedDate(local, true, now); !got.Equal(imported) {
		t.Errorf("resync preserve: got %v want %v", got, imported)
	}

	// Re-sync of a pre-migration file (only date_created): migrate it up.
	oldCreated := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	preMigration := playlist.Playlist{DateCreated: oldCreated}
	if got := importedDate(preMigration, true, now); !got.Equal(oldCreated) {
		t.Errorf("resync migrate: got %v want %v", got, oldCreated)
	}
}
