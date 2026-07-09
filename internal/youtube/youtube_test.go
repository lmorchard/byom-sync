package youtube

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestSearcher(h http.HandlerFunc) (HTTPSearcher, *httptest.Server) {
	srv := httptest.NewServer(h)
	return HTTPSearcher{APIKey: "KEY", Client: srv.Client(), baseURL: srv.URL}, srv
}

func TestHTTPSearcherReturnsTopVideoID(t *testing.T) {
	var gotQuery, gotKey string
	s, srv := newTestSearcher(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("q")
		gotKey = r.URL.Query().Get("key")
		_, _ = w.Write([]byte(`{"items":[{"id":{"videoId":"vid42"}}]}`))
	})
	defer srv.Close()

	id, err := s.Search(context.Background(), "Kavinsky Nightcall")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if id != "vid42" {
		t.Errorf("id = %q, want vid42", id)
	}
	if gotQuery != "Kavinsky Nightcall" || gotKey != "KEY" {
		t.Errorf("q=%q key=%q", gotQuery, gotKey)
	}
}

func TestHTTPSearcherEmptyResult(t *testing.T) {
	s, srv := newTestSearcher(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"items":[]}`))
	})
	defer srv.Close()
	id, err := s.Search(context.Background(), "no match")
	if err != nil || id != "" {
		t.Errorf("want empty/no-error, got id=%q err=%v", id, err)
	}
}

func TestHTTPSearcherQuotaExceeded(t *testing.T) {
	s, srv := newTestSearcher(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"errors":[{"reason":"quotaExceeded"}]}}`))
	})
	defer srv.Close()
	_, err := s.Search(context.Background(), "x")
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("want ErrQuotaExceeded, got %v", err)
	}
}

func TestHTTPSearcherOtherErrorStatus(t *testing.T) {
	s, srv := newTestSearcher(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer srv.Close()
	_, err := s.Search(context.Background(), "x")
	if err == nil || errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("want a non-quota error, got %v", err)
	}
}
