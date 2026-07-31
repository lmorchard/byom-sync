package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/lmorchard/byom-sync/internal/artstore"
	"github.com/lmorchard/byom-sync/internal/auth"
	"github.com/lmorchard/byom-sync/internal/coverart"
	"github.com/lmorchard/byom-sync/internal/playlist"
	"github.com/lmorchard/byom-sync/internal/purchase"
	"github.com/lmorchard/byom-sync/internal/rcache"
	"github.com/lmorchard/byom-sync/internal/spotifyenrich"
	"github.com/lmorchard/byom-sync/internal/spotifyfetch"
	"github.com/lmorchard/byom-sync/internal/youtube"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/zmb3/spotify/v2"
)

var (
	resolveInput     string
	resolveLimit     int
	resolveDelay     time.Duration
	resolveFlush     int
	resolveReresolve bool
	resolveNoCache   bool
)

var (
	artInput    string
	artLimit    int
	artDelay    time.Duration
	artNoCache  bool
	artDownload bool
)

var (
	enrichInput        string
	enrichLimit        int
	enrichDelay        time.Duration
	enrichFlush        int
	enrichNoCache      bool
	enrichCanonicalize bool
)

var (
	purchaseInput     string
	purchaseSource    string
	purchaseLimit     int
	purchaseDelay     time.Duration
	purchaseNoCache   bool
	purchaseReresolve bool
)

var (
	allInput        string
	allLimit        int
	allDelay        time.Duration
	allNoCache      bool
	allDownload     bool
	allSkipSpotify  bool
	allSkipArt      bool
	allSkipYouTube  bool
	allSkipPurchase bool
)

// defaultCachePath mirrors the auth config-dir logic: $XDG_CONFIG_HOME/byom-sync
// (or ~/.config/byom-sync), file cache.db.
func defaultCachePath() string {
	var base string
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		base = filepath.Join(v, "byom-sync")
	} else if home, err := os.UserHomeDir(); err == nil {
		base = filepath.Join(home, ".config", "byom-sync")
	} else {
		base = "byom-sync"
	}
	return filepath.Join(base, "cache.db")
}

// openCache opens the resolution cache unless --no-cache is set (then nil, nil).
func openCache() (*rcache.DB, error) {
	if resolveNoCache {
		return nil, nil
	}
	path := viper.GetString("cache_path")
	if path == "" {
		path = defaultCachePath()
	}
	return rcache.Open(path)
}

var resolveCmd = &cobra.Command{
	Use:   "resolve",
	Short: "Resolve external IDs for hub tracks (e.g. YouTube video IDs)",
}

var resolveYouTubeCmd = &cobra.Command{
	Use:   "youtube",
	Short: "Resolve a YouTube video id for tracks missing one and store it in the hub",
	Long: `Resolve a YouTube video ID for each hub track that has no youtube_id yet and
write it back into the YAML. Only missing tracks are attempted, so runs are
incremental.

Resolvers, tried in order per track:
  1. yt-dlp — YouTube's own search via the yt-dlp binary. Searches the top few
     results and picks the first that allows embedded playback. Free, no quota,
     no key. Requires yt-dlp on PATH (or set ytdlp_path). Primary.
  2. youtube-search — the YouTube Data API text search, used only as a fallback
     and only when youtube_api_key is set. It spends the ~100 searches/day quota
     and mostly duplicates yt-dlp, so it's rarely needed.

--limit caps tracks attempted per run; --delay paces requests under rate limits.
--reresolve re-checks tracks that already have an id and replaces any that are no
longer embeddable. Resolution stops early (persisting progress) on quota
exhaustion or sustained rate limiting.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runResolveYouTube(context.Background())
	},
}

func runResolveYouTube(ctx context.Context) error {
	input := resolveInput
	if input == "" {
		input = viper.GetString("dir")
	}

	paths, err := hubPaths(input)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		log.Warnf("no playlist YAML files found under %s — nothing to resolve", input)
		return nil
	}

	// yt-dlp (free, no quota, no key) is the primary resolver; the YouTube Data
	// API search is an optional fallback, added only when a key is configured
	// (it spends the ~100/day search quota, and mostly duplicates yt-dlp).
	ytdlpBin := viper.GetString("ytdlp_path")
	if ytdlpBin == "" {
		ytdlpBin = "yt-dlp"
	}
	if _, err := exec.LookPath(ytdlpBin); err != nil {
		return fmt.Errorf("%q not found in PATH — install yt-dlp (https://github.com/yt-dlp/yt-dlp) or set ytdlp_path", ytdlpBin)
	}
	ytdlp := youtube.YtdlpResolver{Bin: ytdlpBin}
	resolvers := []youtube.Resolver{ytdlp}
	names := "yt-dlp"
	if apiKey := viper.GetString("youtube_api_key"); apiKey != "" {
		resolvers = append(resolvers, youtube.SearchResolver{Searcher: youtube.HTTPSearcher{APIKey: apiKey}})
		names = "yt-dlp, youtube-search"
	}
	chain := youtube.NewChain(resolvers...)
	chain.OnDisable = func(name string, err error) {
		log.Warnf("resolver %q exhausted (%v) — continuing without it", name, err)
	}

	cache, err := openCache()
	if err != nil {
		return fmt.Errorf("open cache: %w", err)
	}
	if cache != nil {
		defer func() { _ = cache.Close() }()
	}
	missTTL := viper.GetDuration("cache_miss_ttl")
	embedTTL := viper.GetDuration("cache_embed_ttl")

	var budget *int
	if resolveLimit > 0 {
		budget = &resolveLimit
		log.Infof("resolving YouTube ids across %d file(s) under %s [%s] (limit %d, delay %s)", len(paths), input, names, resolveLimit, resolveDelay)
	} else {
		log.Infof("resolving YouTube ids across %d file(s) under %s [%s] (delay %s)", len(paths), input, names, resolveDelay)
	}

	total := 0
	stopped := "done"
	for _, path := range paths {
		p, err := playlist.LoadFile(path)
		if err != nil {
			return fmt.Errorf("load %s: %w", path, err)
		}
		missing := countMissingYouTube(p)
		base := filepath.Base(path)
		if missing == 0 && !resolveReresolve {
			log.Infof("%s: all %d tracks already resolved, skipping", base, len(p.Tracks))
			continue
		}
		if resolveReresolve {
			log.Infof("%s: re-resolving (%d of %d missing; existing ids re-checked)", base, missing, len(p.Tracks))
		} else {
			log.Infof("%s: %d of %d tracks need a YouTube id", base, missing, len(p.Tracks))
		}

		// Per-track narration + per-file tallies. Errors/removals go to WARN so
		// they surface without --verbose; the rest are DEBUG (--verbose).
		var got, kept, replaced, removed int
		report := func(e youtube.Event) {
			switch e.Kind {
			case youtube.KindResolved:
				got++
				log.Debugf("  resolved: %s - %s -> %s (via %s)", e.Artist, e.Title, e.VideoID, e.Source)
			case youtube.KindReplaced:
				replaced++
				log.Debugf("  replaced: %s - %s -> %s (was non-embeddable)", e.Artist, e.Title, e.VideoID)
			case youtube.KindKept:
				kept++
				log.Debugf("  kept: %s - %s (still embeddable)", e.Artist, e.Title)
			case youtube.KindRemoved:
				removed++
				log.Warnf("  removed: %s - %s (non-embeddable, no alternative found)", e.Artist, e.Title)
			case youtube.KindMiss:
				log.Debugf("  no match: %s - %s", e.Artist, e.Title)
			case youtube.KindError:
				log.Warnf("  error: %s - %s: %v", e.Artist, e.Title, e.Err)
			}
		}

		// Persist incrementally so a long run is granularly resumable, but batch
		// writes (every resolveFlush resolutions) so we don't rewrite a large
		// playlist file on every single track.
		sinceSave := 0
		savedTotal := 0
		save := func() error { return playlist.SaveFile(path, p) }
		onResolved := func() error {
			sinceSave++
			savedTotal++
			if sinceSave >= resolveFlush {
				sinceSave = 0
				if err := save(); err != nil {
					return err
				}
				log.Infof("  %s: checkpoint — %d ids saved to disk", base, savedTotal)
			}
			return nil
		}

		opts := youtube.ResolveOptions{
			Budget:     budget,
			Pace:       resolveDelay,
			Report:     report,
			OnResolved: onResolved,
			Reresolve:  resolveReresolve,
			Verify:     ytdlp.IsEmbeddable,
			MissTTL:    missTTL,
			EmbedTTL:   embedTTL,
		}
		// Assign only when non-nil: a typed-nil *rcache.DB in the interface field
		// would read as non-nil and Resolve would call methods on a nil DB.
		if cache != nil {
			opts.Cache = cache
		}
		n, stop, err := youtube.Resolve(ctx, chain, &p, opts)
		// Flush any resolutions since the last batched save (also covers an early
		// stop). Do this before surfacing a resolve error so partial progress sticks.
		if sinceSave > 0 {
			if serr := save(); serr != nil {
				return fmt.Errorf("save %s: %w", path, serr)
			}
		}
		if err != nil {
			return fmt.Errorf("resolve %s: %w", path, err)
		}
		if resolveReresolve {
			log.Infof("%s: re-checked — %d kept, %d replaced, %d removed, %d newly resolved", base, kept, replaced, removed, got)
		} else if got > 0 {
			log.Infof("%s: resolved %d id(s), saved", base, got)
		} else {
			log.Infof("%s: nothing resolved", base)
		}
		total += n
		if stop == youtube.StopQuota {
			log.Warnf("YouTube daily quota exceeded — stopping (progress saved). Re-run tomorrow to continue.")
			stopped = "quota"
			break
		}
		if stop == youtube.StopRateLimit {
			log.Warnf("YouTube rate limit hit repeatedly — stopping (progress saved). Retry later or raise --delay.")
			stopped = "ratelimit"
			break
		}
		if budget != nil && *budget <= 0 {
			stopped = "limit"
			break
		}
	}
	log.Warnf("YouTube resolve done: %d ids resolved; stopped: %s", total, stopped)
	return nil
}

var resolveSpotifyCmd = &cobra.Command{
	Use:   "spotify",
	Short: "Enrich hub tracks with Spotify metadata (ISRC, ids, duration, art)",
	Long: `Look up each hub track that lacks a spotify_id in Spotify and fill its
technical fields (isrc, spotify_id, spotify_url, duration_ms, album, image),
leaving your authored title/artist/album text intact. Only confident matches are
written; ambiguous tracks get an enrich_candidates list in their YAML — to accept
one, copy its spotify_id up to the track's own spotify_id and re-run.

--limit caps tracks attempted per run; --delay paces requests. --canonicalize
overwrites authored text with Spotify's official strings (off by default).
Typically run before 'resolve youtube' so downstream identity keys on ISRC.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runResolveSpotify(context.Background())
	},
}

var resolveArtCmd = &cobra.Command{
	Use:   "art",
	Short: "Fill missing cover art (Spotify first, then MusicBrainz/Cover Art Archive)",
	Long: `Find cover art for every hub track that has no image yet and write the URL
into the YAML. Spotify-first: tracks with a spotify_id get their album art in a
fast batched lookup by id (needs a token — run 'byom-sync auth'; without one this
step is skipped with a warning). Tracks still missing art then fall back to
MusicBrainz (release-group by artist+album when an album is present, else the
recording by artist+title) → the Cover Art Archive front cover. Independent of
spotify:false, so off-Spotify tracks get art too.

--limit and --delay bound only the MusicBrainz fallback pass (the Spotify pass is
batched and unbounded): --limit caps tracks attempted there per run; --delay
paces MusicBrainz requests (its ~1 req/sec policy).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runResolveArt(context.Background())
	},
}

// purchaseTierOrder is the measured-best cascade. Bandcamp first: it resolves
// ~47% of hub albums in one request each and gives artist-friendly, DRM-free
// links. iTunes second at ~65% of what's left. Discogs last — a secondhand
// physical listing, which doesn't fill a digital gap unless the record is
// ripped. MusicBrainz is deliberately absent: measured at 3% with zero unique
// contribution.
var purchaseTierOrder = []string{"bandcamp", "itunes", "discogs"}

// purchaseSourcePaces are each store's own floor. A single --delay cannot
// express three different rate limits, so --delay acts only as an extra floor.
var purchaseSourcePaces = map[string]time.Duration{
	"bandcamp": 1100 * time.Millisecond, // undocumented endpoint — stay polite
	"itunes":   3100 * time.Millisecond, // ~20 req/min
	"discogs":  2500 * time.Millisecond, // 25 req/min unauthenticated
}

// purchaseSourceMarkers identify which tier produced an existing purchase_url,
// so --reresolve can drop that tier's links without touching the others'. The
// hub stores only the URL, so the store has to be recognised from it.
//
// Matched as a substring of the whole URL rather than by parsing out the host,
// deliberately: the point of --reresolve is recovering from links a tier got
// wrong, and a malformed URL has no host a parser would recognise. byom-sync
// itself once emitted "https://www.discogs.comhttps://www.discogs.com/release/…",
// whose parsed host is "www.discogs.comhttps" — a host-suffix check would leave
// exactly the links that most need clearing.
var purchaseSourceMarkers = map[string][]string{
	"bandcamp": {"bandcamp.com"},
	"itunes":   {"music.apple.com", "itunes.apple.com"},
	"discogs":  {"discogs.com"},
}

// clearPurchaseURLs blanks every purchase_url attributable to source, so the
// tier's next pass sees those albums as unresolved again. Returns how many
// tracks were cleared.
func clearPurchaseURLs(p *playlist.Playlist, source string) int {
	markers := purchaseSourceMarkers[source]
	if len(markers) == 0 {
		return 0
	}
	n := 0
	for i := range p.Tracks {
		u := p.Tracks[i].PurchaseURL
		if u == "" {
			continue
		}
		for _, m := range markers {
			if strings.Contains(u, m) {
				p.Tracks[i].PurchaseURL = ""
				n++
				break
			}
		}
	}
	return n
}

// purchaseSourcesFor builds the tier list for a --source value. "all" is the
// full cascade in measured order; any single source name is that tier alone.
func purchaseSourcesFor(name, discogsToken string) ([]purchase.Source, error) {
	build := func(n string) (purchase.Source, error) {
		switch n {
		case "bandcamp":
			return purchase.NewBandcamp(nil, ""), nil
		case "itunes":
			return purchase.NewITunes(nil, ""), nil
		case "discogs":
			return purchase.NewDiscogs(nil, "", discogsToken), nil
		default:
			return nil, fmt.Errorf("unknown purchase source %q (want one of: all, %s)",
				n, strings.Join(purchaseTierOrder, ", "))
		}
	}
	if name == "all" {
		out := make([]purchase.Source, 0, len(purchaseTierOrder))
		for _, n := range purchaseTierOrder {
			s, err := build(n)
			if err != nil {
				return nil, err
			}
			out = append(out, s)
		}
		return out, nil
	}
	s, err := build(name)
	if err != nil {
		return nil, err
	}
	return []purchase.Source{s}, nil
}

// purchasePaceFor returns the larger of the source's own floor and an explicit
// --delay, so a user can slow a tier down but never speed it past its limit.
func purchasePaceFor(name string, explicit time.Duration) time.Duration {
	pace := purchaseSourcePaces[name]
	if explicit > pace {
		return explicit
	}
	return pace
}

var resolvePurchaseCmd = &cobra.Command{
	Use:   "purchase",
	Short: "Fill missing purchase links (Bandcamp, then iTunes, then Discogs)",
	Long: `Find a best-effort "where to buy this" URL for every hub album that has no
purchase_url yet and write it into the YAML.

Runs tier-at-a-time, each tier a full pass over whatever is still unresolved:
Bandcamp (~47% of albums, artist-friendly and DRM-free), then iTunes (~65% of
what's left, DRM-free downloads, accepted only when the album carries a real
price rather than being stream-only), then Discogs (secondhand physical media,
accepted only when copies are actually listed for sale).

Every match passes a confidence gate, because stores will return a real but
wrong album — iTunes answers "Theatre Is Evil" with "Piano Is Evil".

--limit caps network lookups per tier; --delay is an extra floor on top of each
source's own rate limit. Set discogs_token in config to raise Discogs from
25/min to 60/min. Stopping after Bandcamp is a reasonable outcome: it is the
cheapest pass and gives the links most worth having.

--reresolve un-writes a tier's previous answers before re-running it: it drops
that tier's cached rows and blanks every purchase_url in the hub that points at
that store, then resolves them fresh. This is the recovery path for a tier that
filled the hub with bad links. Note that a link the tier no longer finds stays
gone, so pair it with --source rather than re-running the whole cascade unless
that is what you mean.

A tier stops early if its lookups fail repeatedly in a row — a store that has
started refusing us should not receive thousands more requests. Later tiers
still run.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runResolvePurchase(context.Background())
	},
}

// runResolvePurchase runs one or more purchase-link tiers over the hub. Each
// tier is a full pass over every file: albums that already have a
// purchase_url are skipped by purchase.Resolve, so an earlier tier's fill is
// what lets a later tier "see" less work.
func runResolvePurchase(ctx context.Context) error {
	input := purchaseInput
	if input == "" {
		input = viper.GetString("dir")
	}
	paths, err := hubPaths(input)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		log.Warnf("no playlist YAML files found under %s — nothing to do", input)
		return nil
	}

	sources, err := purchaseSourcesFor(purchaseSource, viper.GetString("discogs_token"))
	if err != nil {
		return err
	}

	resolveNoCache = purchaseNoCache
	cache, err := openCache()
	if err != nil {
		return fmt.Errorf("open cache: %w", err)
	}
	if cache != nil {
		defer func() { _ = cache.Close() }()
	}
	missTTL := viper.GetDuration("cache_miss_ttl")

	total := 0
	for _, src := range sources {
		// Budget is per tier, not per file: --limit caps lookups across the
		// whole pass, shared by every file in this tier.
		var budget *int
		if purchaseLimit > 0 {
			b := purchaseLimit
			budget = &b
		}
		pace := purchasePaceFor(src.Name(), purchaseDelay)
		// Pacing and the consecutive-error streak belong to the source, not to
		// one file. Resolve runs once per file, so without this shared state the
		// first lookup in every file would go out unpaced — most lookups, once
		// earlier tiers have filled the bulk of the hub.
		tier := &purchase.Tier{}

		if purchaseReresolve && cache != nil {
			cleared, cerr := cache.ClearPurchaseSource(src.Name())
			if cerr != nil {
				return fmt.Errorf("clear %s purchase cache: %w", src.Name(), cerr)
			}
			log.Warnf("--reresolve: cleared %d cached %s row(s)", cleared, src.Name())
		}

		var tierFilled, tierMissed, tierFiles, tierDropped int
		stoppedOnErrors := false
		for _, path := range paths {
			p, lerr := playlist.LoadFile(path)
			if lerr != nil {
				return fmt.Errorf("load %s: %w", path, lerr)
			}
			base := filepath.Base(path)
			tierFiles++

			if purchaseReresolve {
				if dropped := clearPurchaseURLs(&p, src.Name()); dropped > 0 {
					tierDropped += dropped
					log.Debugf("  %s: dropped %d existing %s link(s) for re-resolution", base, dropped, src.Name())
				}
			}

			report := func(e purchase.Event) {
				switch e.Kind {
				case purchase.KindFilled:
					log.Debugf("  %s: filled: %s - %s -> %s (via %s)", base, e.Artist, e.Album, e.URL, e.Source)
				case purchase.KindMissed:
					tierMissed++
					log.Debugf("  %s: no match: %s - %s", base, e.Artist, e.Album)
				case purchase.KindError:
					log.Warnf("  %s: error: %s - %s: %v", base, e.Artist, e.Album, e.Err)
				}
			}

			opts := purchase.Options{
				Budget:   budget,
				Pace:     pace,
				MissTTL:  missTTL,
				Report:   report,
				OnFilled: func() error { return playlist.SaveFile(path, p) },
				Tier:     tier,
			}
			if cache != nil {
				opts.Cache = cache
			}
			n, stopped, rerr := purchase.Resolve(ctx, src, &p, opts)
			// Always persist: a resolve error partway through should not lose
			// whatever was filled (and saved via OnFilled) before it — nor, under
			// --reresolve, the links this file just dropped.
			if serr := playlist.SaveFile(path, p); serr != nil {
				return fmt.Errorf("save %s: %w", path, serr)
			}
			if rerr != nil {
				return fmt.Errorf("resolve purchase %s (%s): %w", path, src.Name(), rerr)
			}
			tierFilled += n
			total += n
			if stopped == purchase.StopErrors {
				// The source has been failing every request for a while: it is
				// refusing us, not flaking. Abandon this tier (progress saved)
				// and let the remaining tiers, which are separate services, run.
				log.Warnf("tier %s: stopped — too many consecutive lookup errors, the source appears to be refusing requests (progress saved). Retry later or raise --delay.", src.Name())
				stoppedOnErrors = true
				break
			}
			if budget != nil && *budget <= 0 {
				break
			}
		}
		if tierDropped > 0 {
			log.Warnf("tier %s: --reresolve dropped %d existing link(s) before re-running", src.Name(), tierDropped)
		}
		how := "done"
		if stoppedOnErrors {
			how = "stopped early on errors"
		}
		log.Infof("tier %s: filled %d, missed %d across %d file(s) (%s)", src.Name(), tierFilled, tierMissed, tierFiles, how)
	}
	log.Warnf("purchase resolve done: %d link(s) filled", total)
	return nil
}

// applyTrackArt fills Image for tracks that lack one and whose spotify_id has art
// in artByID. Returns how many were filled.
func applyTrackArt(p *playlist.Playlist, artByID map[string]string) int {
	filled := 0
	for i := range p.Tracks {
		t := &p.Tracks[i]
		if t.Image != "" || t.SpotifyID == "" {
			continue
		}
		if url, ok := artByID[t.SpotifyID]; ok && url != "" {
			t.Image = url
			filled++
		}
	}
	return filled
}

// artStoreRoot returns the directory the cover-art store lives under. image_file
// values are hub-relative, so the store must be anchored at the hub root even
// when --input narrows the run to one playlist or one section; otherwise the
// site resolves image_file against the wrong directory and every cover 404s.
func artStoreRoot(input, hubDir string) string {
	candidate := input
	if fi, statErr := os.Stat(input); statErr == nil && !fi.IsDir() {
		candidate = filepath.Dir(input)
	}
	if hubDir == "" {
		return candidate
	}
	// filepath.Rel errors when one argument is absolute and the other
	// relative (e.g. hubDir from the "./playlists" config default against an
	// absolute --input), which previously fell through to the pre-fix
	// fallback — exactly the bug this function exists to close. Resolve both
	// to absolute paths for the comparison only; the hub form the caller
	// configured (relative or not) is still what gets returned, since it's
	// used to build image_file values and the log line.
	absHub, hubErr := filepath.Abs(hubDir)
	absCandidate, candErr := filepath.Abs(candidate)
	if hubErr != nil || candErr != nil {
		return candidate
	}
	rel, err := filepath.Rel(absHub, absCandidate)
	if err != nil {
		return candidate
	}
	if rel == "." || !strings.HasPrefix(rel, "..") {
		return hubDir
	}
	return candidate
}

func runResolveArt(ctx context.Context) error {
	input := artInput
	if input == "" {
		input = viper.GetString("dir")
	}
	paths, err := hubPaths(input)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		log.Warnf("no playlist YAML files found under %s — nothing to do", input)
		return nil
	}

	artRoot := artStoreRoot(input, viper.GetString("dir"))
	var store *artstore.Store
	if artDownload {
		store = &artstore.Store{Root: artRoot, HTTP: http.DefaultClient}
	}

	// Spotify pass client (best art source). Optional: degrade to MusicBrainz-only
	// when there's no token.
	var spotClient *spotify.Client
	if client, tok, aerr := auth.Client(ctx, viper.GetString("client_id"), viper.GetInt("redirect_port")); aerr != nil {
		log.Warnf("no Spotify token (%v) — filling art from MusicBrainz only; run `byom-sync auth` for Spotify art", aerr)
	} else {
		spotClient = client
		defer auth.PersistRefreshed(client, tok)
	}

	ua := viper.GetString("musicbrainz_user_agent")
	if ua == "" {
		ua = coverart.DefaultUserAgent
	}
	resolver := coverart.Resolver{
		MB:  &coverart.MBClient{HTTP: http.DefaultClient, BaseURL: coverart.MBBaseURL, UserAgent: ua},
		CAA: &coverart.CAAClient{HTTP: http.DefaultClient, BaseURL: coverart.CAABaseURL},
	}

	resolveNoCache = artNoCache
	cache, err := openCache()
	if err != nil {
		return fmt.Errorf("open cache: %w", err)
	}
	if cache != nil {
		defer func() { _ = cache.Close() }()
	}
	missTTL := viper.GetDuration("cache_miss_ttl")

	var budget *int
	if artLimit > 0 {
		budget = &artLimit
	}

	total := 0
	for _, path := range paths {
		p, lerr := playlist.LoadFile(path)
		if lerr != nil {
			return fmt.Errorf("load %s: %w", path, lerr)
		}
		if countMissingArt(p) == 0 && (store == nil || countNeedingDownload(p) == 0) {
			log.Infof("%s: all tracks have art (%d tracks)", filepath.Base(path), len(p.Tracks))
			continue
		}
		base := filepath.Base(path)

		// Spotify pass: batch-fetch album art by id for imageless tracks.
		spot := 0
		if spotClient != nil {
			var ids []string
			for _, t := range p.Tracks {
				if t.Image == "" && t.SpotifyID != "" {
					ids = append(ids, t.SpotifyID)
				}
			}
			if len(ids) > 0 {
				artByID, ferr := spotifyfetch.FetchTrackArt(ctx, spotClient, ids, spotifyfetch.DefaultImageMaxWidth)
				if ferr != nil {
					return fmt.Errorf("spotify art %s: %w", path, ferr)
				}
				spot = applyTrackArt(&p, artByID)
			}
		}

		// MusicBrainz pass: fill whatever still lacks art.
		need := countMissingArt(p)
		var got, missed int
		if need > 0 {
			log.Infof("%s: %d from Spotify; %d remaining for MusicBrainz", base, spot, need)
			report := func(e coverart.Event) {
				switch e.Kind {
				case coverart.KindFilled:
					got++
					log.Debugf("  art: %s - %s -> %s (via %s)", e.Artist, e.Title, e.ImageURL, e.Source)
				case coverart.KindMiss:
					missed++
					log.Debugf("  no art: %s - %s", e.Artist, e.Title)
				case coverart.KindError:
					log.Warnf("  error: %s - %s: %v", e.Artist, e.Title, e.Err)
				}
			}
			opts := coverart.Options{Budget: budget, Pace: artDelay, Report: report, MissTTL: missTTL}
			if cache != nil {
				opts.Cache = cache
			}
			// got is tallied solely from the report events (KindFilled) above; the
			// return value here is used only to detect a resolve error, not counted
			// again, to avoid double-counting fills.
			_, rerr := coverart.Resolve(ctx, resolver, &p, opts)
			if rerr != nil {
				if serr := playlist.SaveFile(path, p); serr != nil {
					return fmt.Errorf("save %s: %w", path, serr)
				}
				return fmt.Errorf("resolve art %s: %w", path, rerr)
			}
		} else {
			log.Infof("%s: %d filled from Spotify (none left for MusicBrainz)", base, spot)
		}

		if store != nil {
			dl := 0
			for i := range p.Tracks {
				t := &p.Tracks[i]
				if t.Image == "" || t.ImageFile != "" {
					continue
				}
				rel, derr := store.Save(ctx, t.Image)
				if derr != nil {
					log.Warnf("  download art: %s - %s: %v", t.Artist, t.Title, derr)
					continue
				}
				t.ImageFile = rel
				dl++
			}
			// The explicit playlist hero image (hand-authored URL) downloads the
			// same way, into the same content-addressed store.
			if p.Image != "" && p.ImageFile == "" {
				rel, derr := store.Save(ctx, p.Image)
				if derr != nil {
					log.Warnf("  download playlist art: %v", derr)
				} else {
					p.ImageFile = rel
					dl++
				}
			}
			if dl > 0 {
				log.Infof("%s: downloaded %d cover(s) into %s/art", base, dl, artRoot)
			}
		}

		if serr := playlist.SaveFile(path, p); serr != nil {
			return fmt.Errorf("save %s: %w", path, serr)
		}
		total += spot + got
		log.Infof("%s: %d art filled (%d Spotify, %d MusicBrainz), %d no-art", base, spot+got, spot, got, missed)
		if budget != nil && *budget <= 0 {
			log.Warnf("art limit reached — stopping (progress saved)")
			break
		}
	}
	log.Warnf("Cover art done: %d track(s) filled", total)
	return nil
}

// countMissingArt counts tracks with no image yet.
func countMissingArt(p playlist.Playlist) int {
	n := 0
	for _, t := range p.Tracks {
		if t.Image == "" {
			n++
		}
	}
	return n
}

// countNeedingDownload counts art references that have a source URL but haven't
// been downloaded to a local file yet — the playlist hero image plus each track.
func countNeedingDownload(p playlist.Playlist) int {
	n := 0
	if p.Image != "" && p.ImageFile == "" {
		n++
	}
	for _, t := range p.Tracks {
		if t.Image != "" && t.ImageFile == "" {
			n++
		}
	}
	return n
}

func runResolveSpotify(ctx context.Context) error {
	input := enrichInput
	if input == "" {
		input = viper.GetString("dir")
	}
	paths, err := hubPaths(input)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		log.Warnf("no playlist YAML files found under %s — nothing to enrich", input)
		return nil
	}

	client, tok, err := auth.Client(ctx, viper.GetString("client_id"), viper.GetInt("redirect_port"))
	if err != nil {
		return err
	}
	defer auth.PersistRefreshed(client, tok)
	searcher := spotifyenrich.ClientSearcher{Client: client}

	// Enrichment cache lives in the same cache.db (a second table). --no-cache
	// bypasses it. openCache honors the shared resolveNoCache flag, so set it.
	resolveNoCache = enrichNoCache
	cache, err := openCache()
	if err != nil {
		return fmt.Errorf("open cache: %w", err)
	}
	if cache != nil {
		defer func() { _ = cache.Close() }()
	}
	missTTL := viper.GetDuration("cache_miss_ttl")

	var budget *int
	if enrichLimit > 0 {
		budget = &enrichLimit
	}

	total := 0
	for _, path := range paths {
		p, lerr := playlist.LoadFile(path)
		if lerr != nil {
			return fmt.Errorf("load %s: %w", path, lerr)
		}
		need := countNeedingEnrich(p)
		base := filepath.Base(path)
		if need == 0 {
			log.Infof("%s: nothing to enrich (%d tracks)", base, len(p.Tracks))
			continue
		}
		log.Infof("%s: %d of %d tracks need enrichment", base, need, len(p.Tracks))

		var got, ambiguous, missed, skipped int
		report := func(e spotifyenrich.Event) {
			switch e.Kind {
			case spotifyenrich.KindEnriched:
				got++
				log.Debugf("  enriched: %s - %s -> %s (score %.2f)", e.Artist, e.Title, e.SpotifyID, e.Score)
			case spotifyenrich.KindPicked:
				got++
				log.Debugf("  picked: %s - %s -> %s", e.Artist, e.Title, e.SpotifyID)
			case spotifyenrich.KindAmbiguous:
				ambiguous++
				log.Debugf("  ambiguous: %s - %s (best %.2f) — candidates written", e.Artist, e.Title, e.Score)
			case spotifyenrich.KindMiss:
				missed++
				log.Debugf("  no match: %s - %s", e.Artist, e.Title)
			case spotifyenrich.KindSkipped:
				skipped++
				log.Debugf("  skipped: %s - %s (spotify: false)", e.Artist, e.Title)
			case spotifyenrich.KindError:
				log.Warnf("  error: %s - %s: %v", e.Artist, e.Title, e.Err)
			}
		}

		sinceSave := 0
		onEnriched := func() error {
			sinceSave++
			if sinceSave >= enrichFlush {
				sinceSave = 0
				return playlist.SaveFile(path, p)
			}
			return nil
		}

		opts := spotifyenrich.Options{
			Budget:       budget,
			Pace:         enrichDelay,
			Report:       report,
			OnEnriched:   onEnriched,
			Canonicalize: enrichCanonicalize,
			MissTTL:      missTTL,
		}
		if cache != nil {
			opts.Cache = cache
		}
		n, eerr := spotifyenrich.Enrich(ctx, searcher, &p, opts)
		// Always persist: ambiguous runs wrote enrich_candidates even when n==0.
		if serr := playlist.SaveFile(path, p); serr != nil {
			return fmt.Errorf("save %s: %w", path, serr)
		}
		if eerr != nil {
			return fmt.Errorf("enrich %s: %w", path, eerr)
		}
		log.Infof("%s: %d enriched, %d ambiguous (candidates written), %d no-match, %d skipped (spotify:false)", base, got, ambiguous, missed, skipped)
		total += n
		if budget != nil && *budget <= 0 {
			log.Warnf("enrichment limit reached — stopping (progress saved)")
			break
		}
	}
	log.Warnf("Spotify enrich done: %d track(s) enriched", total)
	return nil
}

// countNeedingEnrich counts tracks that require an enrichment pass: any track
// still carrying enrich_candidates (a pending pick, or stale candidates to clear
// on a now-opted-out track), plus unresolved tracks not opted out with
// spotify:false. Tracks with a spotify_id and no candidates, and opted-out tracks
// with no candidates, need nothing.
func countNeedingEnrich(p playlist.Playlist) int {
	n := 0
	for _, t := range p.Tracks {
		optedOut := t.Spotify != nil && !*t.Spotify
		switch {
		case len(t.EnrichCandidates) > 0:
			n++ // a pick to apply, or stale candidates to clear
		case !optedOut && t.SpotifyID == "":
			n++ // unresolved and not opted out
		}
	}
	return n
}

var (
	primeInput            string
	primeAssumeEmbeddable bool
)

var resolvePrimeCmd = &cobra.Command{
	Use:   "prime",
	Short: "Seed the resolution cache from tracks that already have a youtube_id",
	Long: `Walk the hub and upsert every track that already has a youtube_id into the
resolution cache, so subsequent resolve runs reuse that work instead of hitting
the network. Positive entries only — misses can't be reconstructed from the YAML.

--assume-embeddable (default true) marks seeded ids as embeddable, so --reresolve
trusts them for the embed TTL window. Set --assume-embeddable=false to seed them
unverified (the next --reresolve then checks each once). The default trusts the
hub, which was resolved by the embeddable-preferring resolver; the tradeoff is
that a video gone private/dead since resolution isn't caught until the TTL lapses
or you clear the cache.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if resolveNoCache {
			return fmt.Errorf("--no-cache is incompatible with prime")
		}
		input := primeInput
		if input == "" {
			input = viper.GetString("dir")
		}
		paths, err := hubPaths(input)
		if err != nil {
			return err
		}
		db, err := openCache()
		if err != nil {
			return fmt.Errorf("open cache: %w", err)
		}
		defer func() { _ = db.Close() }()
		seeded, dupes, err := primeCache(paths, db, primeAssumeEmbeddable, time.Now())
		if err != nil {
			return err
		}
		log.Infof("primed cache: %d keys seeded, %d cross-playlist duplicates collapsed (assume-embeddable=%v)", seeded, dupes, primeAssumeEmbeddable)
		return nil
	},
}

var (
	clearMissesOnly bool
	clearSource     string
)

var resolveCacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Inspect or clear the resolution cache",
}

var resolveCacheStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show resolution cache coverage",
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openCache()
		if err != nil {
			return fmt.Errorf("open cache: %w", err)
		}
		defer func() { _ = db.Close() }()
		missTTL := viper.GetDuration("cache_miss_ttl")
		s, err := db.Stats(time.Now().Add(-missTTL))
		if err != nil {
			return err
		}
		log.Infof("cache: %d entries — %d resolved, %d misses (%d expired, re-attempted next run)",
			s.Total, s.Positive, s.Negative, s.ExpiredNegative)
		es, err := db.EnrichStats(time.Now().Add(-missTTL))
		if err != nil {
			return err
		}
		log.Infof("enrichment cache: %d entries — %d resolved, %d misses (%d expired)",
			es.Total, es.Positive, es.Negative, es.ExpiredNegative)
		as, err := db.ArtStats(time.Now().Add(-missTTL))
		if err != nil {
			return err
		}
		log.Infof("art cache: %d entries — %d found, %d misses (%d expired)",
			as.Total, as.Positive, as.Negative, as.ExpiredNegative)
		ps, err := db.PurchaseStats(time.Now().Add(-missTTL))
		if err != nil {
			return err
		}
		log.Infof("purchase cache: %d entries — %d linked, %d misses (%d expired)",
			ps.Total, ps.Positive, ps.Negative, ps.ExpiredNegative)
		return nil
	},
}

var resolveCacheClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Delete cache entries (all, --misses-only, or one purchase --source)",
	Long: `Delete entries from the resolution cache.

With no flags this wipes all four tables. --misses-only keeps resolved entries
and drops the negative ones, so unmatched tracks are re-attempted next run.

--source scopes the clear to one purchase tier's rows (bandcamp, itunes,
discogs), leaving the other tiers' work — and the YouTube, enrichment, and art
caches — intact. That is the cheap way to make a tier look again after it
returned bad links; to also un-write the links it already wrote into the hub,
run 'resolve purchase --source <tier> --reresolve', which does both.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Validate before opening the cache, so a typo doesn't create a cache.db.
		if clearSource != "" {
			if clearMissesOnly {
				return fmt.Errorf("--source and --misses-only are mutually exclusive")
			}
			if _, known := purchaseSourceMarkers[clearSource]; !known {
				return fmt.Errorf("unknown purchase source %q (want one of: %s)",
					clearSource, strings.Join(purchaseTierOrder, ", "))
			}
		}
		db, err := openCache()
		if err != nil {
			return fmt.Errorf("open cache: %w", err)
		}
		defer func() { _ = db.Close() }()

		if clearSource != "" {
			n, cerr := db.ClearPurchaseSource(clearSource)
			if cerr != nil {
				return cerr
			}
			log.Warnf("cleared %d %s row(s) from the purchase cache", n, clearSource)
			return nil
		}

		n, err := db.Clear(clearMissesOnly)
		if err != nil {
			return err
		}
		what := "entries"
		if clearMissesOnly {
			what = "miss entries"
		}
		log.Warnf("cleared %d %s from the resolution cache", n, what)
		return nil
	},
}

// primeCache seeds the cache from tracks that already have a youtube_id. It
// returns how many keys were seeded and how many cross-playlist duplicates were
// collapsed onto an already-seen key.
func primeCache(paths []string, db *rcache.DB, assumeEmbeddable bool, now time.Time) (seeded, dupes int, err error) {
	seen := map[string]bool{}
	for _, path := range paths {
		p, lerr := playlist.LoadFile(path)
		if lerr != nil {
			return seeded, dupes, fmt.Errorf("load %s: %w", path, lerr)
		}
		for _, t := range p.Tracks {
			if t.YouTubeID == "" {
				continue
			}
			key := t.Key()
			if seen[key] {
				dupes++
			} else {
				seen[key] = true
				seeded++
			}
			e := rcache.Entry{VideoID: t.YouTubeID, Source: "prime", ResolvedAt: now, CheckedAt: now}
			if assumeEmbeddable {
				yes := true
				e.Embeddable = &yes
			}
			if perr := db.Put(key, e); perr != nil {
				return seeded, dupes, fmt.Errorf("cache put: %w", perr)
			}
		}
	}
	return seeded, dupes, nil
}

// countMissingYouTube counts tracks in p that still lack a YouTube id.
func countMissingYouTube(p playlist.Playlist) int {
	n := 0
	for _, t := range p.Tracks {
		if t.YouTubeID == "" {
			n++
		}
	}
	return n
}

// hubPaths returns every playlist YAML under input, recursively. Thin
// delegation to playlist.HubPaths so the resolvers, the exporters, and the site
// generator share one definition of the hub.
func hubPaths(input string) ([]string, error) {
	return playlist.HubPaths(input)
}

// prereq is one external requirement of an enrichment stage: a name for the
// message, a cheap local check, and what the user should do about it.
type prereq struct {
	name   string
	check  func() error
	remedy string
}

// checkPrereqs runs every check and reports all failures in a single error.
// Deliberately not fail-fast: someone missing both a Spotify token and yt-dlp
// should learn that once, not discover the second after fixing the first.
func checkPrereqs(reqs []prereq) error {
	var missing []string
	for _, r := range reqs {
		if err := r.check(); err != nil {
			missing = append(missing, fmt.Sprintf("  - %s: %v\n    fix: %s", r.name, err, r.remedy))
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("missing prerequisites:\n%s", strings.Join(missing, "\n"))
}

// stage is one step of the resolve-all pipeline, named for logging and for the
// prerequisite mapping.
type stage struct {
	name string
	run  func(context.Context) error
}

// resolveAllStages returns the enabled stages in dependency order. The order is
// a data dependency, not a preference: `resolve spotify` writes the ISRCs that
// the art and youtube stages use as their cache identity (Track.Key()). The
// purchase stage has no such dependency — it only reads Track.Artist/Album/Title
// — so its position relative to youtube is a preference, not a requirement; it
// runs after art here just to keep visual/shopping metadata grouped together.
func resolveAllStages(skipSpotify, skipArt, skipPurchase, skipYouTube bool) []stage {
	stages := make([]stage, 0, 4)
	if !skipSpotify {
		stages = append(stages, stage{name: "spotify", run: runResolveSpotify})
	}
	if !skipArt {
		stages = append(stages, stage{name: "art", run: runResolveArt})
	}
	if !skipPurchase {
		stages = append(stages, stage{name: "purchase", run: runResolvePurchase})
	}
	if !skipYouTube {
		stages = append(stages, stage{name: "youtube", run: runResolveYouTube})
	}
	return stages
}

// resolveAllPrereqs returns the external requirements of the enabled stages,
// deduplicated (spotify and art both need the same token).
func resolveAllPrereqs(stages []stage) []prereq {
	var needToken, needYtdlp bool
	for _, s := range stages {
		switch s.name {
		case "spotify", "art":
			needToken = true
		case "youtube":
			needYtdlp = true
		}
	}

	reqs := make([]prereq, 0, 2)
	if needToken {
		reqs = append(reqs, prereq{
			name:   "Spotify token",
			check:  func() error { _, err := auth.LoadToken(); return err },
			remedy: "run `byom-sync auth` (or `byom-sync auth --manual` on a headless host)",
		})
	}
	if needYtdlp {
		bin := viper.GetString("ytdlp_path")
		if bin == "" {
			bin = "yt-dlp"
		}
		reqs = append(reqs, prereq{
			name:   "yt-dlp",
			check:  func() error { _, err := exec.LookPath(bin); return err },
			remedy: "install yt-dlp (https://github.com/yt-dlp/yt-dlp) or set ytdlp_path",
		})
	}
	return reqs
}

var resolveAllCmd = &cobra.Command{
	Use:   "all",
	Short: "Run the full enrichment pipeline: spotify, then art, then purchase, then youtube",
	Long: `Run every enrichment stage over the hub in dependency order:

  1. resolve spotify  — isrc, spotify_id, spotify_url, duration_ms, album, image
  2. resolve art      — cover art, downloaded into <hub>/art by default
  3. resolve purchase — a best-effort purchase_url (Bandcamp, then iTunes, then Discogs)
  4. resolve youtube  — a playable youtube_id per track

The spotify → art → youtube ordering is a data dependency: the spotify stage
writes the ISRCs that the art and youtube stages use as their cache identity,
so running them in this sequence reuses work instead of repeating it. The
purchase stage has no such dependency on the others; it runs third here
simply to keep it near the art stage.

--limit is a per-stage budget, with two documented exceptions: the art stage's
Spotify pass is batched and always unbounded, and the purchase stage spends the
budget per tier rather than per stage, so with all three tiers enabled it can
make up to 3x --limit lookups. That is deliberate — each purchase tier is a
separate service with its own rate limit and its own full pass, so "N lookups
per tier" is the unit that means something there; a single shared budget would
usually mean "stop somewhere inside Bandcamp" and the later tiers would never
run at all.

Prerequisites for every enabled stage are checked before any work starts, so a
missing yt-dlp is reported immediately rather than after a long art crawl. Use
--skip-youtube on a host without yt-dlp (its prerequisite is then not checked).

Unlike the individual commands, a missing Spotify token is fatal here rather
than degrading to MusicBrainz-only art: running the full pipeline implies you
want the Spotify data. Run 'resolve art' on its own for the degrading behavior.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runResolveAll(context.Background(), cmd)
	},
}

func runResolveAll(ctx context.Context, cmd *cobra.Command) error {
	input := allInput
	if input == "" {
		input = viper.GetString("dir")
	}

	// Fan this command's flags out to the per-stage globals the run functions
	// read. resolveNoCache is shared state that both runResolveArt and
	// runResolveSpotify assign to (see their openCache calls), so driving all
	// four cache flags from one value keeps the stages consistent instead of
	// letting whichever ran last decide for the youtube stage.
	resolveInput, artInput, enrichInput, purchaseInput = input, input, input, input
	resolveLimit, artLimit, enrichLimit, purchaseLimit = allLimit, allLimit, allLimit, allLimit
	resolveNoCache, artNoCache, enrichNoCache, purchaseNoCache = allNoCache, allNoCache, allNoCache, allNoCache
	artDownload = allDownload

	// Each stage has a different sensible pace (youtube 500ms, spotify 200ms,
	// art 1100ms for MusicBrainz's ~1 req/sec policy), so only override them
	// when the user actually passed --delay. purchaseDelay is deliberately left
	// out of this fan-out: it is only ever an extra floor on top of each
	// purchase source's own per-store minimum (Bandcamp ~1/s, iTunes ~20/min,
	// Discogs 25/min), and `resolve all`'s single --delay can't express three
	// different rate limits, so purchaseDelay stays at its own default (0, i.e.
	// no extra floor beyond each source's own).
	if cmd.Flags().Changed("delay") {
		resolveDelay, artDelay, enrichDelay = allDelay, allDelay, allDelay
	}

	stages := resolveAllStages(allSkipSpotify, allSkipArt, allSkipPurchase, allSkipYouTube)
	if len(stages) == 0 {
		return fmt.Errorf("every stage skipped — nothing to do")
	}
	if err := checkPrereqs(resolveAllPrereqs(stages)); err != nil {
		return err
	}
	if err := runStages(ctx, stages); err != nil {
		return err
	}
	log.Infof("resolve all: %d stage(s) complete over %s", len(stages), input)
	return nil
}

// runStages executes stages in order, aborting on the first failure — the later
// stages consume data the failed one was supposed to write, so continuing would
// just produce confusing downstream errors. Split out from runResolveAll so the
// sequencing is testable with fake stages, without network or credentials.
func runStages(ctx context.Context, stages []stage) error {
	for i, s := range stages {
		log.Infof("resolve all [%d/%d] %s: starting", i+1, len(stages), s.name)
		if err := s.run(ctx); err != nil {
			return fmt.Errorf("%s stage: %w", s.name, err)
		}
	}
	return nil
}

func init() {
	rootCmd.AddCommand(resolveCmd)
	resolveCmd.AddCommand(resolveYouTubeCmd)
	resolveYouTubeCmd.Flags().StringVar(&resolveInput, "input", "", "hub YAML file or directory (default: config dir)")
	resolveYouTubeCmd.Flags().IntVar(&resolveLimit, "limit", 0, "max searches this run (0 = unlimited; quota is the backstop)")
	resolveYouTubeCmd.Flags().DurationVar(&resolveDelay, "delay", 500*time.Millisecond, "pause between searches to stay under the API rate limit")
	resolveYouTubeCmd.Flags().IntVar(&resolveFlush, "flush", 20, "write resolved ids to disk every N resolutions (granular resume)")
	resolveYouTubeCmd.Flags().BoolVar(&resolveReresolve, "reresolve", false, "re-check tracks that already have a youtube_id and replace ones no longer embeddable")
	resolveYouTubeCmd.Flags().BoolVar(&resolveNoCache, "no-cache", false, "bypass the resolution cache (pure network resolution)")

	resolveCmd.AddCommand(resolveSpotifyCmd)
	resolveSpotifyCmd.Flags().StringVar(&enrichInput, "input", "", "hub YAML file or directory (default: config dir)")
	resolveSpotifyCmd.Flags().IntVar(&enrichLimit, "limit", 0, "max tracks attempted this run (0 = unlimited)")
	resolveSpotifyCmd.Flags().DurationVar(&enrichDelay, "delay", 200*time.Millisecond, "pause between Spotify lookups")
	resolveSpotifyCmd.Flags().IntVar(&enrichFlush, "flush", 20, "write enriched fields to disk every N fills (granular resume)")
	resolveSpotifyCmd.Flags().BoolVar(&enrichNoCache, "no-cache", false, "bypass the enrichment cache")
	resolveSpotifyCmd.Flags().BoolVar(&enrichCanonicalize, "canonicalize", false, "overwrite authored title/artist/album with Spotify's strings")

	resolveCmd.AddCommand(resolveArtCmd)
	resolveArtCmd.Flags().StringVar(&artInput, "input", "", "hub YAML file or directory (default: config dir)")
	resolveArtCmd.Flags().IntVar(&artLimit, "limit", 0, "max tracks attempted in the MusicBrainz fallback pass (0 = unlimited; Spotify pass is unbounded)")
	resolveArtCmd.Flags().DurationVar(&artDelay, "delay", 1100*time.Millisecond, "pause between MusicBrainz lookups (~1 req/sec policy)")
	resolveArtCmd.Flags().BoolVar(&artNoCache, "no-cache", false, "bypass the art cache")
	resolveArtCmd.Flags().BoolVar(&artDownload, "download", false, "download resolved cover art into a local <hub>/art store and record image_file")

	resolveCmd.AddCommand(resolvePurchaseCmd)
	resolvePurchaseCmd.Flags().StringVar(&purchaseInput, "input", "", "hub YAML file or directory (default: config dir)")
	resolvePurchaseCmd.Flags().StringVar(&purchaseSource, "source", "all", "which tier to run: all, bandcamp, itunes, discogs")
	resolvePurchaseCmd.Flags().IntVar(&purchaseLimit, "limit", 0, "max lookups per tier this run (0 = unlimited)")
	resolvePurchaseCmd.Flags().DurationVar(&purchaseDelay, "delay", 0, "extra floor on the pause between lookups (each source has its own minimum)")
	resolvePurchaseCmd.Flags().BoolVar(&purchaseNoCache, "no-cache", false, "bypass the purchase cache")
	resolvePurchaseCmd.Flags().BoolVar(&purchaseReresolve, "reresolve", false, "re-run the selected tier(s) from scratch: drop their cached rows and the purchase_urls they wrote, then resolve again")

	resolveCmd.AddCommand(resolvePrimeCmd)
	resolvePrimeCmd.Flags().StringVar(&primeInput, "input", "", "hub YAML file or directory (default: config dir)")
	resolvePrimeCmd.Flags().BoolVar(&primeAssumeEmbeddable, "assume-embeddable", true, "mark seeded ids as embeddable (skip re-verify within the embed TTL)")

	resolveCmd.AddCommand(resolveAllCmd)
	resolveAllCmd.Flags().StringVar(&allInput, "input", "", "hub YAML file or directory (default: config dir)")
	resolveAllCmd.Flags().IntVar(&allLimit, "limit", 0, "max tracks attempted per stage (0 = unlimited; the art stage's Spotify pass is always unbounded, and the purchase stage spends it per tier — up to 3x)")
	resolveAllCmd.Flags().DurationVar(&allDelay, "delay", 0, "override every stage's request pacing (default: each stage's own)")
	resolveAllCmd.Flags().BoolVar(&allNoCache, "no-cache", false, "bypass the resolution caches for every stage")
	resolveAllCmd.Flags().BoolVar(&allDownload, "download", true, "download cover art into <hub>/art and record image_file")
	resolveAllCmd.Flags().BoolVar(&allSkipSpotify, "skip-spotify", false, "skip the Spotify enrichment stage")
	resolveAllCmd.Flags().BoolVar(&allSkipArt, "skip-art", false, "skip the cover-art stage")
	resolveAllCmd.Flags().BoolVar(&allSkipPurchase, "skip-purchase", false, "skip the purchase-link stage")
	resolveAllCmd.Flags().BoolVar(&allSkipYouTube, "skip-youtube", false, "skip the YouTube resolution stage")

	resolveCmd.AddCommand(resolveCacheCmd)
	resolveCacheCmd.AddCommand(resolveCacheStatsCmd)
	resolveCacheCmd.AddCommand(resolveCacheClearCmd)
	resolveCacheClearCmd.Flags().BoolVar(&clearMissesOnly, "misses-only", false, "clear only negative (miss) entries, keeping resolved ids")
	resolveCacheClearCmd.Flags().StringVar(&clearSource, "source", "", "clear only one purchase tier's rows (bandcamp, itunes, discogs), leaving the other caches alone")
}
