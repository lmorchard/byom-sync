package youtube

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

// YtdlpResolver resolves a track by shelling out to yt-dlp's built-in YouTube
// search ("ytsearch1:<artist> <title>") and reading back the top video id. No
// API key and no quota — but it depends on the yt-dlp binary and is scraping-
// based, so it can break when YouTube changes.
type YtdlpResolver struct {
	Bin string // yt-dlp binary; defaults to "yt-dlp"
	// run executes the command and returns stdout; injectable for tests.
	run func(ctx context.Context, name string, args ...string) (string, error)
}

func (YtdlpResolver) Name() string { return "yt-dlp" }

func (y YtdlpResolver) Resolve(ctx context.Context, t playlist.Track) (Result, error) {
	query := strings.TrimSpace(t.Artist + " " + t.Title)
	if query == "" {
		return Result{}, nil
	}
	bin := y.Bin
	if bin == "" {
		bin = "yt-dlp"
	}
	run := y.run
	if run == nil {
		run = execRun
	}

	out, err := run(ctx, bin, "--no-warnings", "--flat-playlist", "--print", "id", "ytsearch1:"+query)
	if err != nil {
		return Result{}, fmt.Errorf("yt-dlp: %w", err)
	}
	id := strings.TrimSpace(firstLine(out))
	if id == "" {
		return Result{}, nil // no result
	}
	return Result{VideoID: id, Source: "yt-dlp"}, nil
}

// execRun runs a command with the given context, returning stdout. On failure it
// includes the first line of stderr for a useful error.
func execRun(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("%w: %s", err, firstLine(msg))
		}
		return "", err
	}
	return stdout.String(), nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
