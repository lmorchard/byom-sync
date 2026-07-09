package youtube

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lmorchard/byom-sync/internal/playlist"
)

// ytdlpStub fakes the yt-dlp runner: a --flat-playlist call returns searchOut;
// any other call is a verify (playable_in_embed) and returns embed[id] (default
// "False"). Records which ids were verified and whether a search ran.
type ytdlpStub struct {
	searchOut string
	searchErr error
	embed     map[string]string // videoId -> "True"/"False"
	searched  bool
	verified  []string
}

func (s *ytdlpStub) run(_ context.Context, _ string, args ...string) (string, error) {
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--flat-playlist") {
		s.searched = true
		return s.searchOut, s.searchErr
	}
	url := args[len(args)-1]
	id := strings.TrimPrefix(url, "https://www.youtube.com/watch?v=")
	s.verified = append(s.verified, id)
	v := s.embed[id]
	if v == "ERR" { // e.g. age-gated: yt-dlp exits non-zero on verify
		return "", errors.New("Sign in to confirm your age")
	}
	if v == "" {
		v = "False"
	}
	return v + "\n", nil
}

func trackAT() playlist.Track { return playlist.Track{Artist: "Kavinsky", Title: "Nightcall"} }

func TestYtdlpPicksFirstEmbeddableCandidate(t *testing.T) {
	s := &ytdlpStub{searchOut: "id1\nid2\nid3\n", embed: map[string]string{"id2": "True"}}
	res, err := YtdlpResolver{run: s.run}.Resolve(context.Background(), trackAT())
	if err != nil || res.VideoID != "id2" {
		t.Fatalf("res=%+v err=%v, want id2", res, err)
	}
	if strings.Join(s.verified, ",") != "id1,id2" {
		t.Errorf("verified=%v, want [id1 id2] (id3 not reached)", s.verified)
	}
}

func TestYtdlpUsesTopWhenEmbeddable(t *testing.T) {
	s := &ytdlpStub{searchOut: "id1\nid2\n", embed: map[string]string{"id1": "True"}}
	res, err := YtdlpResolver{run: s.run}.Resolve(context.Background(), trackAT())
	if err != nil || res.VideoID != "id1" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if len(s.verified) != 1 {
		t.Errorf("verified %v, want only id1", s.verified)
	}
}

func TestYtdlpNoEmbeddableCandidateIsMiss(t *testing.T) {
	s := &ytdlpStub{searchOut: "id1\nid2\n", embed: map[string]string{}} // all False
	res, err := YtdlpResolver{run: s.run}.Resolve(context.Background(), trackAT())
	if err != nil || res.VideoID != "" {
		t.Errorf("want clean miss, got res=%+v err=%v", res, err)
	}
}

func TestYtdlpSearchErrorPropagates(t *testing.T) {
	s := &ytdlpStub{searchErr: errors.New("boom")}
	_, err := YtdlpResolver{run: s.run}.Resolve(context.Background(), trackAT())
	if err == nil || !strings.Contains(err.Error(), "yt-dlp") {
		t.Errorf("want wrapped error, got %v", err)
	}
}

func TestYtdlpSkipsUnverifiableCandidate(t *testing.T) {
	// id1 fails to verify (e.g. age-gated); id2 is embeddable — use id2.
	s := &ytdlpStub{searchOut: "id1\nid2\n", embed: map[string]string{"id1": "ERR", "id2": "True"}}
	res, err := YtdlpResolver{run: s.run}.Resolve(context.Background(), trackAT())
	if err != nil || res.VideoID != "id2" {
		t.Fatalf("res=%+v err=%v, want id2 (id1 skipped)", res, err)
	}
	if strings.Join(s.verified, ",") != "id1,id2" {
		t.Errorf("verified=%v, want [id1 id2]", s.verified)
	}
}

func TestYtdlpAllUnverifiableIsMiss(t *testing.T) {
	s := &ytdlpStub{searchOut: "id1\nid2\n", embed: map[string]string{"id1": "ERR", "id2": "ERR"}}
	res, err := YtdlpResolver{run: s.run}.Resolve(context.Background(), trackAT())
	if err != nil || res.VideoID != "" {
		t.Errorf("want clean miss (no track error), got res=%+v err=%v", res, err)
	}
}

func TestYtdlpEmptyQuerySkips(t *testing.T) {
	s := &ytdlpStub{}
	res, err := YtdlpResolver{run: s.run}.Resolve(context.Background(), playlist.Track{})
	if err != nil || res.VideoID != "" {
		t.Errorf("want miss, got res=%+v err=%v", res, err)
	}
	if s.searched {
		t.Error("should not search for an empty query")
	}
}

func TestYtdlpSearchesConfiguredCandidateCount(t *testing.T) {
	var gotArgs string
	run := func(_ context.Context, _ string, args ...string) (string, error) {
		gotArgs = strings.Join(args, " ")
		return "", nil
	}
	_, _ = YtdlpResolver{run: run}.Resolve(context.Background(), trackAT())
	if !strings.Contains(gotArgs, "ytsearch5:Kavinsky Nightcall") {
		t.Errorf("args = %q, want ytsearch5 query", gotArgs)
	}
}

func TestIsEmbeddable(t *testing.T) {
	s := &ytdlpStub{embed: map[string]string{"yes": "True", "no": "False"}}
	r := YtdlpResolver{run: s.run}
	if ok, err := r.IsEmbeddable(context.Background(), "yes"); err != nil || !ok {
		t.Errorf("yes: ok=%v err=%v", ok, err)
	}
	if ok, err := r.IsEmbeddable(context.Background(), "no"); err != nil || ok {
		t.Errorf("no: ok=%v err=%v", ok, err)
	}
}
