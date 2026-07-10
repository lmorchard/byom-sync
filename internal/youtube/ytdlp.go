package youtube

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

const defaultCandidates = 5

// YtdlpResolver resolves a track by shelling out to yt-dlp's built-in YouTube
// search. It flat-searches the top N results, then picks the first that is
// embeddable (yt-dlp's playable_in_embed) — so a video with embedding disabled
// (which plays on youtube.com but fails in an embedded player) isn't chosen. No
// API key and no quota, but it depends on the yt-dlp binary and is scraping-
// based, so it can break when YouTube changes.
type YtdlpResolver struct {
	Bin        string // yt-dlp binary; defaults to "yt-dlp"
	Candidates int    // search results to consider; defaults to 5
	// run executes the command and returns stdout; injectable for tests.
	run func(ctx context.Context, name string, args ...string) (string, error)
}

func (YtdlpResolver) Name() string { return "yt-dlp" }

func (y YtdlpResolver) tool() (string, func(context.Context, string, ...string) (string, error)) {
	bin := y.Bin
	if bin == "" {
		bin = "yt-dlp"
	}
	run := y.run
	if run == nil {
		run = execRun
	}
	return bin, run
}

func (y YtdlpResolver) Resolve(ctx context.Context, t playlist.Track) (Result, error) {
	query := strings.TrimSpace(t.Artist + " " + t.Title)
	if query == "" {
		return Result{}, nil
	}
	bin, run := y.tool()
	n := y.Candidates
	if n <= 0 {
		n = defaultCandidates
	}

	out, err := run(ctx, bin, "--no-warnings", "--flat-playlist", "--print", "id",
		fmt.Sprintf("ytsearch%d:%s", n, query))
	if err != nil {
		return Result{}, fmt.Errorf("yt-dlp: %w", err)
	}
	// Take the first candidate that is embeddable. A verify error is propagated
	// (transient) rather than silently falling to a worse match.
	for _, id := range nonEmptyLines(out) {
		embeddable, err := y.isEmbeddable(ctx, id)
		if err != nil {
			return Result{}, err
		}
		if embeddable {
			yes := true
			return Result{VideoID: id, Source: "yt-dlp", Embeddable: &yes}, nil
		}
	}
	return Result{}, nil // no embeddable candidate — leave unresolved
}

// IsEmbeddable reports whether a video allows embedded playback (yt-dlp's
// playable_in_embed). Used to verify candidates and to re-check existing ids.
func (y YtdlpResolver) IsEmbeddable(ctx context.Context, videoID string) (bool, error) {
	return y.isEmbeddable(ctx, videoID)
}

func (y YtdlpResolver) isEmbeddable(ctx context.Context, videoID string) (bool, error) {
	bin, run := y.tool()
	out, err := run(ctx, bin, "--no-warnings", "--print", "%(playable_in_embed)s",
		"https://www.youtube.com/watch?v="+videoID)
	if err != nil {
		return false, fmt.Errorf("yt-dlp: %w", err)
	}
	return strings.TrimSpace(firstLine(out)) == "True", nil
}

// nonEmptyLines splits s into trimmed, non-empty lines.
func nonEmptyLines(s string) []string {
	var out []string
	for _, ln := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(ln); t != "" {
			out = append(out, t)
		}
	}
	return out
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
