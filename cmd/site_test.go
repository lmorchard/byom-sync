package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestSiteCommandBuilds(t *testing.T) {
	hub := t.TempDir()
	if err := os.WriteFile(filepath.Join(hub, "x.yaml"),
		[]byte("title: X\ncreator: me\ntracks:\n  - {title: T, artist: A}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()

	viper.Reset()
	viper.Set("dir", hub)
	viper.Set("site.out_dir", out)
	viper.Set("site.base_url", "https://x.test")
	viper.Set("site.title", "mixtapes")
	viper.Set("site.player_src", "https://cdn/p.js")
	viper.Set("site.provider", "youtube")

	if err := runSite(nil, nil); err != nil {
		t.Fatalf("runSite: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "index.html")); err != nil {
		t.Errorf("no index.html: %v", err)
	}
}

func TestSiteCommandRequiresBaseURL(t *testing.T) {
	viper.Reset()
	viper.Set("dir", t.TempDir())
	viper.Set("site.out_dir", t.TempDir())
	// no base_url
	if err := runSite(nil, nil); err == nil {
		t.Error("expected error when base_url is empty")
	}
}
