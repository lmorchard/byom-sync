// Package match provides normalized string similarity shared by the enrichment
// and purchase-link resolvers. Extracted from spotifyenrich so both score
// candidate matches the same way.
package match

import (
	"strings"
	"unicode"
)

// Norm lowercases and reduces a string to space-separated alphanumeric tokens,
// so punctuation and casing don't distort similarity.
func Norm(s string) string {
	var b strings.Builder
	prevSpace := false
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
			prevSpace = false
		} else if !prevSpace {
			b.WriteRune(' ')
			prevSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

// minContainLen is the shortest a pattern string (the shorter of the two inputs
// to Sim) may be for the containment/sliding-window path to apply. Below this
// length, a short token can trivially appear as a substring of almost any longer
// string (e.g. "go" inside "going home"), which would spuriously score 1.0.
// Short patterns instead fall back to a stricter, symmetric full-string edit
// ratio. Tunable.
const minContainLen = 5

// Sim is a 0..1 similarity that rewards the shorter of the two strings matching
// a contiguous run of the longer one — a "partial ratio". This keeps loosely
// authored strings scoring high against fuller catalog strings ("come together"
// vs "come together remastered 2019") while wrong matches stay low. Two empty
// strings are identical (1.0); one empty and one not is 0.0. Inputs are expected
// already normalized (see Norm).
func Sim(a, b string) float64 {
	if a == b {
		return 1.0
	}
	if a == "" || b == "" {
		return 0.0
	}
	ra, rb := []rune(a), []rune(b)
	if len(ra) > len(rb) {
		ra, rb = rb, ra // ra is the shorter string — the pattern
	}
	if len(ra) < minContainLen {
		d := Levenshtein(ra, rb)
		return 1.0 - float64(d)/float64(len(rb))
	}
	best := 0.0
	for i := 0; i+len(ra) <= len(rb); i++ {
		d := Levenshtein(ra, rb[i:i+len(ra)])
		r := 1.0 - float64(d)/float64(len(ra))
		if r > best {
			best = r
			if best == 1.0 {
				break
			}
		}
	}
	return best
}

// Levenshtein computes edit distance between two rune slices.
func Levenshtein(a, b []rune) int {
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
