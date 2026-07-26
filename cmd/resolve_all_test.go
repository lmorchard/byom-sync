package cmd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCheckPrereqs_AllPresent(t *testing.T) {
	err := checkPrereqs([]prereq{
		{name: "one", check: func() error { return nil }, remedy: "n/a"},
		{name: "two", check: func() error { return nil }, remedy: "n/a"},
	})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

// A user missing two prerequisites should learn about both from one run,
// rather than fixing one and immediately hitting the other.
func TestCheckPrereqs_ReportsEveryFailure(t *testing.T) {
	err := checkPrereqs([]prereq{
		{name: "Spotify token", check: func() error { return errors.New("no token stored") }, remedy: "run `byom-sync auth`"},
		{name: "ok thing", check: func() error { return nil }, remedy: "n/a"},
		{name: "yt-dlp", check: func() error { return errors.New("not found in PATH") }, remedy: "install yt-dlp"},
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	for _, want := range []string{"Spotify token", "no token stored", "run `byom-sync auth`", "yt-dlp", "not found in PATH", "install yt-dlp"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error should mention %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "ok thing") {
		t.Errorf("error should not mention satisfied prerequisites:\n%s", msg)
	}
}

func stageNames(stages []stage) []string {
	names := make([]string, 0, len(stages))
	for _, s := range stages {
		names = append(names, s.name)
	}
	return names
}

// Order is a data dependency, not a preference: resolve spotify writes the
// ISRCs that the art and youtube stages use as their cache identity.
func TestResolveAllStages_DependencyOrder(t *testing.T) {
	got := stageNames(resolveAllStages(false, false, false))
	want := []string{"spotify", "art", "youtube"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestResolveAllStages_SkipFlags(t *testing.T) {
	tests := []struct {
		name                         string
		skipSpotify, skipArt, skipYT bool
		want                         []string
	}{
		{"skip spotify", true, false, false, []string{"art", "youtube"}},
		{"skip art", false, true, false, []string{"spotify", "youtube"}},
		{"skip youtube", false, false, true, []string{"spotify", "art"}},
		{"skip all", true, true, true, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stageNames(resolveAllStages(tt.skipSpotify, tt.skipArt, tt.skipYT))
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("got %v want %v", got, tt.want)
			}
		})
	}
}

func prereqNames(reqs []prereq) []string {
	names := make([]string, 0, len(reqs))
	for _, r := range reqs {
		names = append(names, r.name)
	}
	return names
}

// A failing stage must abort the pipeline rather than pressing on: the later
// stages depend on data the failed one was supposed to write.
func TestRunStages_AbortsOnFirstError(t *testing.T) {
	var ran []string
	boom := errors.New("stage exploded")
	stages := []stage{
		{name: "first", run: func(context.Context) error { ran = append(ran, "first"); return nil }},
		{name: "second", run: func(context.Context) error { ran = append(ran, "second"); return boom }},
		{name: "third", run: func(context.Context) error { ran = append(ran, "third"); return nil }},
	}

	err := runStages(context.Background(), stages)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error should wrap the stage's own error, got %v", err)
	}
	if !strings.Contains(err.Error(), "second") {
		t.Errorf("error should name the failing stage:\n%s", err)
	}
	if strings.Join(ran, ",") != "first,second" {
		t.Errorf("third stage should not have run, got %v", ran)
	}
}

func TestRunStages_RunsAllInOrder(t *testing.T) {
	var ran []string
	stages := []stage{
		{name: "a", run: func(context.Context) error { ran = append(ran, "a"); return nil }},
		{name: "b", run: func(context.Context) error { ran = append(ran, "b"); return nil }},
	}
	if err := runStages(context.Background(), stages); err != nil {
		t.Fatalf("runStages: %v", err)
	}
	if strings.Join(ran, ",") != "a,b" {
		t.Errorf("got %v want [a b]", ran)
	}
}

// Skipped stages must not have their prerequisites checked — --skip-youtube is
// exactly how you run this on a box with no yt-dlp.
func TestResolveAllPrereqs_OnlyForEnabledStages(t *testing.T) {
	tests := []struct {
		name   string
		stages []stage
		want   []string
	}{
		{"all stages", resolveAllStages(false, false, false), []string{"Spotify token", "yt-dlp"}},
		{"no youtube", resolveAllStages(false, false, true), []string{"Spotify token"}},
		{"youtube only", resolveAllStages(true, true, false), []string{"yt-dlp"}},
		{"art only still needs a token", resolveAllStages(true, false, true), []string{"Spotify token"}},
		{"nothing", resolveAllStages(true, true, true), []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := prereqNames(resolveAllPrereqs(tt.stages))
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("got %v want %v", got, tt.want)
			}
		})
	}
}

// runResolveAll fans its flags out to package-level globals that every stage,
// and every other test in this package, reads. This test is the only one that
// mutates them, so it must restore all of them — leaving e.g. artDownload or
// resolveNoCache flipped would silently change what a later test sees, since
// Go tests in one package share process state.
func TestRunResolveAll_FansOutFlagsAndGatesDelay(t *testing.T) {
	origResolveDelay, origArtDelay, origEnrichDelay := resolveDelay, artDelay, enrichDelay
	origResolveInput, origArtInput, origEnrichInput := resolveInput, artInput, enrichInput
	origResolveLimit, origArtLimit, origEnrichLimit := resolveLimit, artLimit, enrichLimit
	origResolveNoCache, origArtNoCache, origEnrichNoCache := resolveNoCache, artNoCache, enrichNoCache
	origArtDownload := artDownload
	origAllSkipSpotify, origAllSkipArt, origAllSkipYouTube := allSkipSpotify, allSkipArt, allSkipYouTube
	origAllInput, origAllNoCache, origAllDelay := allInput, allNoCache, allDelay
	t.Cleanup(func() {
		resolveDelay, artDelay, enrichDelay = origResolveDelay, origArtDelay, origEnrichDelay
		resolveInput, artInput, enrichInput = origResolveInput, origArtInput, origEnrichInput
		resolveLimit, artLimit, enrichLimit = origResolveLimit, origArtLimit, origEnrichLimit
		resolveNoCache, artNoCache, enrichNoCache = origResolveNoCache, origArtNoCache, origEnrichNoCache
		artDownload = origArtDownload
		allSkipSpotify, allSkipArt, allSkipYouTube = origAllSkipSpotify, origAllSkipArt, origAllSkipYouTube
		allInput, allNoCache, allDelay = origAllInput, origAllNoCache, origAllDelay

		// t.Cleanup can restore the Go variables above, but not pflag's own
		// per-flag Changed bool. Without resetting it here, this test's
		// Parse("--delay=3s") call leaves resolveAllCmd's "delay" flag marked
		// Changed for the rest of the test binary's life, so a later test
		// calling runResolveAll would silently inherit it.
		for _, name := range []string{"delay", "skip-spotify", "skip-art", "skip-youtube"} {
			resolveAllCmd.Flags().Lookup(name).Changed = false
		}
	})

	// Skip every stage so runResolveAll returns "every stage skipped" without
	// touching the network or requiring credentials — but the fan-out and the
	// prereq check both run before that early return, so this still exercises
	// them.
	if err := resolveAllCmd.Flags().Parse([]string{"--skip-spotify", "--skip-art", "--skip-youtube"}); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	allInput = "/tmp/fake-hub-for-test"
	allNoCache = true

	err := runResolveAll(context.Background(), resolveAllCmd)
	if err == nil || !strings.Contains(err.Error(), "every stage skipped") {
		t.Fatalf("expected \"every stage skipped\" error, got %v", err)
	}

	if resolveInput != allInput || artInput != allInput || enrichInput != allInput {
		t.Errorf("input fan-out: resolveInput=%q artInput=%q enrichInput=%q, want all %q",
			resolveInput, artInput, enrichInput, allInput)
	}
	if !resolveNoCache || !artNoCache || !enrichNoCache {
		t.Errorf("no-cache fan-out: resolveNoCache=%v artNoCache=%v enrichNoCache=%v, want all true",
			resolveNoCache, artNoCache, enrichNoCache)
	}
	if resolveLimit != allLimit || artLimit != allLimit || enrichLimit != allLimit {
		t.Errorf("limit fan-out: resolveLimit=%d artLimit=%d enrichLimit=%d, want all %d",
			resolveLimit, artLimit, enrichLimit, allLimit)
	}
	// --download defaults to true, which is the spec's most surprising
	// decision (and, per the cover-art store root bug, its most consequential
	// one) — pin that the art stage actually receives it.
	if artDownload != allDownload {
		t.Errorf("download fan-out: artDownload=%v, want allDownload=%v", artDownload, allDownload)
	}

	// Without --delay, each stage's own tuned pacing must survive untouched.
	// DurationVar assigns these defaults at flag-registration time, so they're
	// genuinely live in the globals here, not just at rest in the flag spec.
	if resolveDelay != 500*time.Millisecond {
		t.Errorf("resolveDelay = %v, want untouched youtube default 500ms", resolveDelay)
	}
	if artDelay != 1100*time.Millisecond {
		t.Errorf("artDelay = %v, want untouched art default 1100ms", artDelay)
	}
	if enrichDelay != 200*time.Millisecond {
		t.Errorf("enrichDelay = %v, want untouched spotify default 200ms", enrichDelay)
	}

	// With --delay explicitly passed, it must override all three.
	if err := resolveAllCmd.Flags().Parse([]string{"--skip-spotify", "--skip-art", "--skip-youtube", "--delay=3s"}); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	if err := runResolveAll(context.Background(), resolveAllCmd); err == nil || !strings.Contains(err.Error(), "every stage skipped") {
		t.Fatalf("expected \"every stage skipped\" error, got %v", err)
	}
	if resolveDelay != 3*time.Second || artDelay != 3*time.Second || enrichDelay != 3*time.Second {
		t.Errorf("delay override: resolveDelay=%v artDelay=%v enrichDelay=%v, want all 3s",
			resolveDelay, artDelay, enrichDelay)
	}
}
