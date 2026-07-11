package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

// setImportFlags resets the package-level import flag vars for a test and points
// output at dir. Package vars persist across tests, so each test sets them.
func setImportFlags(t *testing.T, dir, title, creator string, force bool) {
	t.Helper()
	importDir = dir
	importTitle = title
	importCreator = creator
	importForce = force
	t.Cleanup(func() {
		importDir, importTitle, importCreator, importForce = "", "", "", false
	})
}

func writeTxt(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestRunImport_WritesNativePlaylist(t *testing.T) {
	tmp := t.TempDir()
	txt := writeTxt(t, tmp, "in.txt", "# title: My List\n# creator: Les\nA - One\nB - Two\n")
	out := t.TempDir()
	setImportFlags(t, out, "", "", false)

	if err := runImport(txt); err != nil {
		t.Fatalf("runImport: %v", err)
	}

	path := filepath.Join(out, "my-list.yaml")
	p, err := playlist.LoadFile(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	if p.Title != "My List" || p.Creator != "Les" {
		t.Errorf("metadata: title=%q creator=%q", p.Title, p.Creator)
	}
	if !p.IsNative() {
		t.Errorf("import should be native, spotify_id=%q", p.SpotifyID)
	}
	if len(p.Tracks) != 2 || p.Tracks[0].Artist != "A" || p.Tracks[0].Title != "One" {
		t.Errorf("tracks: %+v", p.Tracks)
	}
	if p.DateImported.IsZero() {
		t.Errorf("date_imported should be stamped")
	}
	if !p.DateCreated.Equal(p.DateImported) || !p.DateUpdated.Equal(p.DateImported) {
		t.Errorf("native created/updated should fall back to imported: imported=%v created=%v updated=%v",
			p.DateImported, p.DateCreated, p.DateUpdated)
	}
}

func TestRunImport_FlagOverridesHeaderAndFilenameDefault(t *testing.T) {
	tmp := t.TempDir()
	// no header → title should fall back to the filename stem unless --title given
	txt := writeTxt(t, tmp, "mymix.txt", "A - One\n")
	out := t.TempDir()

	// filename-default
	setImportFlags(t, out, "", "", false)
	if err := runImport(txt); err != nil {
		t.Fatalf("runImport (filename default): %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "mymix.yaml")); err != nil {
		t.Errorf("expected mymix.yaml from filename stem: %v", err)
	}

	// flag override wins over the "# title:" header
	txt2 := writeTxt(t, tmp, "in2.txt", "# title: Header Title\nA - One\n")
	setImportFlags(t, out, "Flag Title", "", false)
	if err := runImport(txt2); err != nil {
		t.Fatalf("runImport (flag override): %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "flag-title.yaml")); err != nil {
		t.Errorf("expected flag-title.yaml from --title override: %v", err)
	}
}

func TestRunImport_RefusesOverwriteUnlessForce(t *testing.T) {
	tmp := t.TempDir()
	txt := writeTxt(t, tmp, "in.txt", "# title: Dup\nA - One\n")
	out := t.TempDir()

	setImportFlags(t, out, "", "", false)
	if err := runImport(txt); err != nil {
		t.Fatalf("first import: %v", err)
	}
	// second import without --force must fail
	setImportFlags(t, out, "", "", false)
	if err := runImport(txt); err == nil {
		t.Fatal("expected error importing over an existing file without --force")
	}
	// with --force it succeeds
	setImportFlags(t, out, "", "", true)
	if err := runImport(txt); err != nil {
		t.Fatalf("import with --force: %v", err)
	}
}

func TestRunImport_NoTracksIsError(t *testing.T) {
	tmp := t.TempDir()
	txt := writeTxt(t, tmp, "empty.txt", "# title: Nothing\n# just comments\n\n")
	setImportFlags(t, t.TempDir(), "", "", false)
	if err := runImport(txt); err == nil {
		t.Fatal("expected error when no tracks parsed")
	}
}
