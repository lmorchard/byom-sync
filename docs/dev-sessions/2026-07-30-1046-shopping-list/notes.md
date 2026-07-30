# Notes — shopping list

## Session summary

Brainstorm-only session. Produced `spec.md` and two GitHub issues; no code
written.

## How the design moved

The idea arrived as "a shopping list from a playlist in the player." Two
corrections reshaped it.

**Fetching belongs in site generation, not the browser.** The first proposal put
MusicBrainz lookups in byom-player behind an opt-in background pass. Les redirected:
front-load as much fetching as possible into the site-generation phase, bake the
results in, and let the player display what it finds. This splits the feature
across both repos and keeps rate-limited network work out of the browser. It also
reuses the MusicBrainz client and SQLite cache byom-sync already has.

**The album sidecar was wrong, and measuring proved it.** The recommendation was
an album-keyed sidecar file to avoid duplicating an album-scoped URL across every
track. Les pushed back that it would produce a massive YAML file. Measuring the
live hub settled it: 7,165 distinct albums against 13,832 tracks with albums — a
duplication factor of only 1.93. The sidecar would have concentrated 7,165 entries
into one ~30k-line file to save about half the lines. Per-track storage won.

The general lesson: the dedup argument assumed roughly 10x repetition. The actual
number was under 2x. One `python3` pass over the hub was cheaper than the
architecture it prevented.

**The sweep must be summoned.** Les tightened the panel design so a full-playlist
availability scan never starts on its own — only on explicit user action, with
progress indication.

## Verified rather than assumed

Both checked against live APIs during the session:

- Bandcamp has no public search or metadata API. `bandcamp.com/developer` is sales
  reporting for artists and labels already selling there. Everything else is
  scraping, which is CORS-blocked from a browser regardless.
- MusicBrainz `ws/2` returns `access-control-allow-origin: *`, and `?inc=url-rels`
  on a release yields typed purchase relations. The Amanda Palmer probe returned a
  real Bandcamp URL — but only on one of three same-titled releases, which is what
  exposed the edition-picking problem and set the 3-lookup cap.

## Open questions for implementation

- Which MusicBrainz search strategy picks candidate releases best: release search
  scored by artist+title, or release-group first then browse its releases? The
  spec assumes release search; worth a spike against real hub data before
  committing.
- Whether the 3-lookup cap is the right ceiling. It was chosen as a cost bound,
  not measured against hit rate.
- Cold-fill cost is multiple hours of wall clock for the full hub. Chunking via
  `--limit` works, but the ergonomics of resuming a long fill are untested.

## Next

Neither issue is scheduled. Phase B (player panel) is shippable without Phase A
because the search-URL fallback covers every row, so either can go first.
