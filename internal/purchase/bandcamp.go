package purchase

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// bandcampEndpoint is the search endpoint Bandcamp's own site calls. It is
// undocumented and unauthenticated: there is no public Bandcamp search API, and
// this is the only way to reach the catalogue. Measured as the best single
// source — one request, the exact album URL, and a clean zero-result miss rather
// than a wrong guess. Because it is unsanctioned it is deliberately one tier of
// a cascade: if it breaks or starts refusing traffic, the pass degrades to the
// remaining tiers instead of failing. Keep the request rate polite (~1/sec).
const bandcampEndpoint = "https://bandcamp.com/api/bcsearch_public_api/1/autocomplete_elastic"

// Bandcamp resolves purchase URLs from Bandcamp's search endpoint.
type Bandcamp struct {
	client   *http.Client
	endpoint string
}

// NewBandcamp returns a Bandcamp source. A zero endpoint means the real one;
// tests inject an httptest URL.
func NewBandcamp(client *http.Client, endpoint string) *Bandcamp {
	if client == nil {
		client = defaultHTTPClient()
	}
	if endpoint == "" {
		endpoint = bandcampEndpoint
	}
	return &Bandcamp{client: client, endpoint: endpoint}
}

func (*Bandcamp) Name() string { return "bandcamp" }

type bandcampResponse struct {
	Auto struct {
		Results []struct {
			Type        string `json:"type"`
			Name        string `json:"name"`
			BandName    string `json:"band_name"`
			ItemURLPath string `json:"item_url_path"`
		} `json:"results"`
	} `json:"auto"`
}

// Lookup searches Bandcamp for the query and returns the best-scoring result
// that clears the threshold. An empty result set is a miss, not an error.
func (b *Bandcamp) Lookup(ctx context.Context, q Query) (Result, error) {
	filter := "a"
	if q.Kind() == KindTrack {
		filter = "t"
	}
	payload, err := json.Marshal(map[string]any{
		"search_text":   q.Text(),
		"search_filter": filter,
		"full_page":     false,
		"fan_id":        nil,
	})
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.endpoint, bytes.NewReader(payload))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := b.client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("bandcamp search: status %d", resp.StatusCode)
	}

	var body bandcampResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Result{}, err
	}

	best := Result{Kind: q.Kind()}
	for _, r := range body.Auto.Results {
		if r.ItemURLPath == "" {
			continue
		}
		s, ok := Accept(q, r.BandName, r.Name)
		if !ok {
			continue // wrong record, or too weak a match to trust
		}
		if !hostIsArtist(q.Artist, r.ItemURLPath) {
			continue // right name, wrong account — see hostIsArtist
		}
		if s > best.Score {
			best.Score, best.URL = s, r.ItemURLPath
		}
	}
	if best.URL == "" {
		return Result{Kind: q.Kind()}, nil // clean miss
	}
	return best, nil
}

// hostIsArtist reports whether a Bandcamp result's subdomain plausibly belongs
// to the artist we asked for.
//
// This exists because `band_name` is free text the uploader controls, and the
// confidence gate can only compare strings. Cover bands, DJ edits, karaoke and
// stem packs routinely put the *original* artist's name in that field, so an
// impostor scores a perfectly legitimate 1.000 on both artist and album. Three
// real examples from the live hub, all accepted by the gate before this check:
//
//	Lady Gaga / MAYHEM            -> diegovalente2.bandcamp.com  (band_name "Lady Gaga")
//	My Chemical Romance / …Parade -> firsttoelevenstems.bandcamp.com
//	Rage Against the Machine      -> gageagainstthemachine.bandcamp.com
//
// The subdomain is the *account*, not a display name, so it is far harder to
// spoof convincingly. It is the only field in the response that distinguishes
// these cases.
//
// Matching is by prefix, not substring, and that distinction is load-bearing.
// An artist's own account commonly carries a *suffix* ("glosserband",
// "ghostcopnyc", "moaamusic"), so a prefix test keeps those. A tribute or cover
// account, by contrast, tends to embed the artist's name in the *middle*:
// "nevermindatributetonirvana" contains "nirvana" and passed a substring test
// while being exactly the impostor class this function exists to reject.
//
// A leading "the" is stripped from both sides, since it travels freely between
// an artist's metadata and their account name ("thebeatles" vs "beatles").
//
// The cost is that a legitimate *label* page whose name resembles neither the
// artist nor a prefix of it is rejected. That trade favours precision, and a
// rejected album is not a dead end: byom-player falls back to a constructed
// Bandcamp search URL.
func hostIsArtist(artist, itemURL string) bool {
	u, err := url.Parse(itemURL)
	if err != nil || u.Host == "" {
		return false
	}
	sub := alnumFold(strings.SplitN(u.Host, ".", 2)[0])
	a := alnumFold(artist)
	if sub == "" || a == "" {
		return false
	}
	if prefixEither(sub, a) {
		return true
	}
	// A leading article travels in both directions: hub metadata may say
	// "Daysleepers" where the account is "thedaysleepers", or the reverse.
	// Compare with it stripped from both sides.
	bs, ba := stripArticle(sub), stripArticle(a)
	if bs == "" || ba == "" {
		return false
	}
	return prefixEither(bs, ba)
}

// stripArticle removes a leading "the" so an artist and their account agree
// regardless of which one carries it.
func stripArticle(s string) string { return strings.TrimPrefix(s, "the") }

// prefixEither reports whether either string is a prefix of the other.
func prefixEither(a, b string) bool {
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

// alnumFold reduces a string to lowercase ASCII letters and digits, folding the
// handful of accented forms that show up in artist names so "MØAA" and the
// account "moaamusic" still agree. Anything it can't fold is dropped.
func alnumFold(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if f, ok := foldRunes[r]; ok {
			b.WriteString(f)
			continue
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// foldRunes maps accented and stylised letters common in band names onto ASCII.
var foldRunes = map[rune]string{
	'ø': "o", 'å': "a", 'ä': "a", 'á': "a", 'à': "a", 'â': "a", 'ã': "a",
	'é': "e", 'è': "e", 'ê': "e", 'ë': "e",
	'í': "i", 'ì': "i", 'î': "i", 'ï': "i",
	'ó': "o", 'ò': "o", 'ô': "o", 'ö': "o", 'õ': "o",
	'ú': "u", 'ù': "u", 'û': "u", 'ü': "u",
	'ñ': "n", 'ç': "c", 'ß': "ss", 'æ': "ae", 'œ': "oe", 'ð': "d", 'þ': "th",
}
