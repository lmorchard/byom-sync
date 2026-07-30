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

**A research pass changed the primary source.** After the first spec was written,
a round of source research found that Bandcamp's own undocumented search endpoint
does MusicBrainz's job better: one request instead of up to four, an exact album
URL with no edition-picking, and a measured 53% hit rate on the real hub. The
design moved to a four-tier cascade with MusicBrainz demoted to the sanctioned
fallback. Cover art from the same endpoint was split out to byom-sync#54 and
dropped from this session entirely.

## Verified rather than assumed

Everything in the source table was probed live, not read off documentation:

- Bandcamp has no *public* API — `bandcamp.com/developer` is sales reporting for
  artists already selling there. But the undocumented endpoint its own site calls
  works fine server-side, where CORS is irrelevant. That last clause is the part
  the first spec got wrong: it ruled out unsanctioned endpoints on CORS grounds
  that only apply in a browser, and the architecture had already moved resolution
  into Go.
- MusicBrainz `?inc=url-rels` yields typed purchase relations, but the Amanda
  Palmer probe hit on only one of three same-titled releases — the edition-picking
  problem, and the reason for the 3-lookup cap.
- iTunes answered a *Theatre Is Evil* query with *Piano Is Evil*. A real album,
  wrong one, no signal. That single result is why the confidence gate is a
  requirement rather than a nicety.
- Odesli looked promising (keyed by Spotify id, which the hub already has) but
  carries no Bandcamp coverage at all and caps at 10 req/min. Rejected.

## The measurement habit paid off twice

Both times the useful move was to stop arguing and query the hub or the API:

- The sidecar-vs-per-track question dissolved once the duplication factor turned
  out to be 1.93 rather than the ~10x being implicitly assumed.
- "Is Bandcamp worth building" dissolved once a 30-album sample returned 47%,
  and the miss list showed the failures were mostly compilations and major-label
  catalog — i.e. correct misses, not fixable ones. The two that *were* fixable
  pointed straight at the comma-joined-artist normalization.

## Open questions for implementation

- Is 0.8 the right confidence threshold for purchase sources? Inherited from
  `spotifyenrich`, never tuned against these responses.
- MusicBrainz candidate strategy: release search scored by artist+title, or
  release-group first then browse its releases? Worth a spike before tier 2.
- Is the 3-lookup edition cap right? A cost bound, not measured against hit rate.
- **Tiers 3 and 4 rest on single probes.** Their hit rate against the residue that
  Bandcamp and MusicBrainz both miss is unmeasured. Sample them the way tier 1 was
  sampled before building them — the residue is disproportionately compilations
  and major-label catalog, which is exactly where iTunes should do well and
  Bandcamp can't, but that's a hypothesis, not a measurement.

## Next

Nothing is scheduled. Phase B (player panel) is shippable without Phase A because
the search-URL fallback covers every row, so either can go first. Within Phase A,
tier 1 alone is a legitimate stopping point: cheapest pass, highest yield.
