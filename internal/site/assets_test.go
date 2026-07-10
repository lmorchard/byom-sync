package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAssetsAndCNAME(t *testing.T) {
	out := t.TempDir()
	if err := WriteAssets(out); err != nil {
		t.Fatalf("WriteAssets: %v", err)
	}
	js, err := os.ReadFile(filepath.Join(out, "assets", "site-nav.js"))
	if err != nil {
		t.Fatalf("site-nav.js: %v", err)
	}
	if !strings.Contains(string(js), "customElements.define('byom-site-nav'") {
		t.Error("site-nav.js missing component registration")
	}
	if _, err := os.Stat(filepath.Join(out, "assets", "site.css")); err != nil {
		t.Errorf("site.css missing: %v", err)
	}
	if err := WriteCNAME(out, "https://mixtapes.lmorchard.com"); err != nil {
		t.Fatal(err)
	}
	cname, _ := os.ReadFile(filepath.Join(out, "CNAME"))
	if strings.TrimSpace(string(cname)) != "mixtapes.lmorchard.com" {
		t.Errorf("CNAME = %q", cname)
	}
}
