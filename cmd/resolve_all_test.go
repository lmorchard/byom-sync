package cmd

import (
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
