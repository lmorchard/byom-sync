package youtube

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

func TestYtdlpResolvesTopID(t *testing.T) {
	var gotArgs []string
	y := YtdlpResolver{run: func(_ context.Context, _ string, args ...string) (string, error) {
		gotArgs = args
		return "vidZ\n", nil
	}}
	res, err := y.Resolve(context.Background(), playlist.Track{Artist: "Kavinsky", Title: "Nightcall"})
	if err != nil || res.VideoID != "vidZ" || res.Source != "yt-dlp" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	joined := strings.Join(gotArgs, " ")
	if !strings.Contains(joined, "ytsearch1:Kavinsky Nightcall") {
		t.Errorf("args missing ytsearch query: %q", joined)
	}
}

func TestYtdlpTakesFirstLineOnly(t *testing.T) {
	y := YtdlpResolver{run: func(_ context.Context, _ string, _ ...string) (string, error) {
		return "first\nsecond\n", nil
	}}
	res, _ := y.Resolve(context.Background(), playlist.Track{Artist: "a", Title: "b"})
	if res.VideoID != "first" {
		t.Errorf("VideoID=%q, want first", res.VideoID)
	}
}

func TestYtdlpEmptyOutputIsMiss(t *testing.T) {
	y := YtdlpResolver{run: func(_ context.Context, _ string, _ ...string) (string, error) {
		return "\n", nil
	}}
	res, err := y.Resolve(context.Background(), playlist.Track{Artist: "a", Title: "b"})
	if err != nil || res.VideoID != "" {
		t.Errorf("want clean miss, got res=%+v err=%v", res, err)
	}
}

func TestYtdlpErrorPropagates(t *testing.T) {
	y := YtdlpResolver{run: func(_ context.Context, _ string, _ ...string) (string, error) {
		return "", errors.New("exec fail")
	}}
	_, err := y.Resolve(context.Background(), playlist.Track{Artist: "a", Title: "b"})
	if err == nil || !strings.Contains(err.Error(), "yt-dlp") {
		t.Errorf("want wrapped yt-dlp error, got %v", err)
	}
}

func TestYtdlpEmptyQuerySkips(t *testing.T) {
	var called bool
	y := YtdlpResolver{run: func(_ context.Context, _ string, _ ...string) (string, error) {
		called = true
		return "", nil
	}}
	res, err := y.Resolve(context.Background(), playlist.Track{})
	if err != nil || res.VideoID != "" {
		t.Errorf("want miss, got res=%+v err=%v", res, err)
	}
	if called {
		t.Error("should not shell out for an empty query")
	}
}
