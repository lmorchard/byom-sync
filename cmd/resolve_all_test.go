package cmd

import (
	"context"
	"errors"
	"strings"
	"testing"
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
