package artstore

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSave_WritesContentAddressedFileAndDedups(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("JPEGBYTES"))
	}))
	defer srv.Close()

	root := t.TempDir()
	s := Store{Root: root, HTTP: srv.Client()}

	rel, err := s.Save(context.Background(), srv.URL+"/cover")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !strings.HasPrefix(rel, "art/") || !strings.HasSuffix(rel, ".jpg") {
		t.Errorf("rel path shape: %q", rel)
	}
	// file exists on disk under root
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
		t.Errorf("file not written: %v", err)
	}
	// sharded: art/<2 chars>/<hash>.jpg
	parts := strings.Split(rel, "/")
	if len(parts) != 3 || len(parts[1]) != 2 {
		t.Errorf("expected art/<hh>/<hash>.jpg, got %q", rel)
	}

	// second Save of the same bytes → same path. Dedup only skips the disk
	// write (the target file already exists), not the fetch itself: the
	// content hash is only known after downloading, so both calls hit the
	// network once each, for a total of 2 hits.
	rel2, err := s.Save(context.Background(), srv.URL+"/cover")
	if err != nil {
		t.Fatalf("Save 2: %v", err)
	}
	if rel2 != rel {
		t.Errorf("dedup: got %q want %q", rel2, rel)
	}
	if hits != 2 {
		t.Errorf("expected 2 network hits (dedup skips only the write), got %d", hits)
	}
}

func TestSave_Non200IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	s := Store{Root: t.TempDir(), HTTP: srv.Client()}
	if _, err := s.Save(context.Background(), srv.URL+"/x"); err == nil {
		t.Fatal("expected error on 404")
	}
}
