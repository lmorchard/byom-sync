package site

import (
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
)

// WriteAssets copies the embedded assets/ directory into outDir/assets/.
func WriteAssets(outDir string) error {
	return fs.WalkDir(embedded, "assets", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dst := filepath.Join(outDir, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		data, err := embedded.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
}

// WriteCNAME writes a GitHub Pages CNAME file with the host from baseURL. It is
// a no-op when baseURL is empty or has no host.
func WriteCNAME(outDir, baseURL string) error {
	if baseURL == "" {
		return nil
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return nil
	}
	return os.WriteFile(filepath.Join(outDir, "CNAME"), []byte(u.Host+"\n"), 0o644)
}
