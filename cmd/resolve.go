package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/lmorchard/byom-sync/internal/artstore"
	"github.com/lmorchard/byom-sync/internal/auth"
	"github.com/lmorchard/byom-sync/internal/coverart"
	"github.com/lmorchard/byom-sync/internal/playlist"
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

	artRoot := input
	if fi, statErr := os.Stat(input); statErr == nil && !fi.IsDir() {
		artRoot = filepath.Dir(input)
	}
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
		if countMissingArt(p) == 0 {
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

var clearMissesOnly bool

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
		return nil
	},
}

var resolveCacheClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Delete cache entries (all, or --misses-only)",
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openCache()
		if err != nil {
			return fmt.Errorf("open cache: %w", err)
		}
		defer func() { _ = db.Close() }()
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

// hubPaths returns the YAML files to process: a single file, or every *.yaml in
// a directory.
func hubPaths(input string) ([]string, error) {
	info, err := os.Stat(input)
	if err != nil {
		return nil, fmt.Errorf("input %s: %w", input, err)
	}
	if !info.IsDir() {
		return []string{input}, nil
	}
	matches, err := filepath.Glob(filepath.Join(input, "*.yaml"))
	if err != nil {
		return nil, err
	}
	return matches, nil
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

	resolveCmd.AddCommand(resolvePrimeCmd)
	resolvePrimeCmd.Flags().StringVar(&primeInput, "input", "", "hub YAML file or directory (default: config dir)")
	resolvePrimeCmd.Flags().BoolVar(&primeAssumeEmbeddable, "assume-embeddable", true, "mark seeded ids as embeddable (skip re-verify within the embed TTL)")

	resolveCmd.AddCommand(resolveCacheCmd)
	resolveCacheCmd.AddCommand(resolveCacheStatsCmd)
	resolveCacheCmd.AddCommand(resolveCacheClearCmd)
	resolveCacheClearCmd.Flags().BoolVar(&clearMissesOnly, "misses-only", false, "clear only negative (miss) entries, keeping resolved ids")
}
