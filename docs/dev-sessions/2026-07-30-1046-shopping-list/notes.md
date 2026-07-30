# Notes — shopping list

## Session summary

Brainstorm-and-measure session. Produced `spec.md` and three GitHub issues; no
code written. A substantial part of the value was measurement: three design
decisions were reversed by data collected during the session.

## How the design moved

The idea arrived as "a shopping list from a playlist in the player." Several
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

**Two research passes changed the design; the second reversed the first.** After the first spec was written,
a round of source research found that Bandcamp's own undocumented search endpoint
does MusicBrainz's job better: one request instead of up to four, an exact album
URL with no edition-picking, and a roughly 50% hit rate on the real hub (later
pinned at 47%). The
design moved to a four-tier cascade with MusicBrainz demoted to sanctioned
fallback. Cover art from the same endpoint was split out to byom-sync#54 and
dropped from this session entirely.

Then the tiers were actually measured, and MusicBrainz was dropped outright. Run
against the 32 albums Bandcamp missed it returned **1 hit for 60 requests** — and
that one hit, Protocell's *Magonia*, resolves to an Apple Music id that iTunes
returns directly. Its unique contribution across 60 albums was zero. iTunes turned
out to be the real second tier at **65%** of the residue for one request each.

Final funnel on a 60-album sample: Bandcamp 28/60 (47%), iTunes 20/31 of the
residue (65%), Discogs 13/31 with 2 unique. **85% cumulative, identical with or
without MusicBrainz.**

The lesson is the same one the sidecar question taught, applied to a source
instead of a schema: the mechanism working says nothing about whether the data is
there. MusicBrainz's `url-rels` endpoint does exactly what the docs say. It just
doesn't have many purchase URLs in it.

## Verified rather than assumed

Everything in the source table was probed live, not read off documentation:

- Bandcamp has no *public* API — `bandcamp.com/developer` is sales reporting for
  artists already selling there. But the undocumented endpoint its own site calls
  works fine server-side, where CORS is irrelevant. That last clause is the part
  the first spec got wrong: it ruled out unsanctioned endpoints on CORS grounds
  that only apply in a browser, and the architecture had already moved resolution
  into Go.
- MusicBrainz `?inc=url-rels` yields typed purchase relations, but the Amanda
  Palmer probe hit on only one of three same-titled releases. That looked like an
  edition-picking problem to solve with a lookup cap; measurement later showed it
  was simply thin coverage, and the tier was dropped rather than tuned.
- iTunes answered a *Theatre Is Evil* query with *Piano Is Evil*. A real album,
  wrong one, no signal. That single result is why the confidence gate is a
  requirement rather than a nicety.
- Odesli looked promising (keyed by Spotify id, which the hub already has) but
  carries no Bandcamp coverage at all and caps at 10 req/min. Rejected.

## The measurement habit paid off twice

Both times the useful move was to stop arguing and query the hub or the API:

- The sidecar-vs-per-track question dissolved once the duplication factor turned
  out to be 1.93 rather than the ~10x being implicitly assumed.
- The tier order inverted once measured. MusicBrainz was the confident first
  choice on two separate drafts and turned out to contribute nothing.
- A hit-rate claim got corrected by re-sampling: 53% from one 30-album run did
  not reproduce at n=60, so 47% is the planning number and the normalization
  "lift" was noise. Cheap normalization is still worth keeping; the number
  attached to it was not.
- "Is Bandcamp worth building" dissolved once a 30-album sample returned 47%,
  and the miss list showed the failures were mostly compilations and major-label
  catalog — i.e. correct misses, not fixable ones. The two that *were* fixable
  pointed straight at the comma-joined-artist normalization.

## The confidence gate earned its place empirically

It was added because iTunes answered a *Theatre Is Evil* query with *Piano Is
Evil*. In the measured run it caught the same failure three more times: Ride's
*Peace Sign* → "Classical Music for Zodiac Signs" (0.37), Rob Zombie's *Hellbilly
Deluxe* → "The Sinister Urge" (0.62), Sara Lov's *I Already Love You* →
"Summertime Blues - EP" (0.21).

Scores came out bimodal — accepted matches at 1.00, rejected at 0.62 and below —
so the inherited 0.8 threshold sits in a wide gap rather than on a cliff.

Worth noting against our own interest: two of those three rejections are real
albums iTunes probably stocks, and Discogs found both. The gate did its job; the
query construction is what failed. That is the biggest remaining lever.

## A preference that turned on a factual correction

Les raised disliking that iTunes sells DRM'd music, and asked whether privileging
Discogs would help. Checking the premise was the useful move: iTunes Store
purchases have been DRM-free since 2009 (iTunes Plus, 256kbps AAC). Apple Music
*streaming* is protected; a bought download isn't. The confusion is very
reasonable, and it turned out to point at something real anyway — our
`collectionViewUrl` lands on `music.apple.com`, where the foregrounded action is
"listen", not "buy". So the link was bad even though the file wouldn't be.

Privileging Discogs would also not have served the underlying goal. Discogs sells
secondhand physical media: it doesn't fill a digital gap unless the record gets
ripped, and the artist sees nothing. Coverage wouldn't move either — 11 of its 13
hits overlap iTunes, so it would have swapped a DRM-free download for a vinyl
listing on exactly those.

What actually serves the preference: Bandcamp stays first (artist-friendly *and*
DRM-free), iTunes gets a `collectionPrice > 0` gate, and every row gains
constructed DRM-free store search links.

The general shape is the same as the MusicBrainz reversal — check the premise
before redesigning around it — but inverted. There the mechanism worked and the
data was missing; here the concern was well-founded in spirit and wrong in fact,
and acting on it literally would have made the feature worse.

## A hypothesis that didn't survive

The measured run left one confident-sounding lead: two of iTunes' three gate
rejections were real albums Discogs found, so 65% "must" have understated iTunes
and better query construction would recover them. It was stated as the biggest
remaining lever, likely worth more than building another tier.

Four constructions, 124 requests, and it was simply wrong. Baseline 20/31, wider
limit 20/31, `mixTerm` 20/31, `albumTerm` 17/31 — no variant rescued a single
gated album, and `albumTerm` was worse because dropping the artist from the query
lets same-titled albums by other artists win.

The tell was in the baseline output all along and got read past: **8 of the 11
gated cases return "no priced result at all"**, not a wrong album. That's absence
or stream-only status, not a matching failure. Rob Zombie's *Hellbilly Deluxe*
returns nothing priced even when searching its exact title — for a platinum
record, Apple has stopped selling it.

Two consequences. 65% is iTunes' real ceiling. And the Discogs recommendation
inverts: it had been argued down as competing with the iTunes fix for the same 2
albums, but with no iTunes fix, Discogs is the only route to them.

Worth noting the pattern across the session — three of four confident
recommendations were reversed by measurement (MusicBrainz tier, the sidecar, this
one), and a fourth by checking a premise (the DRM question). The measurements
were cheap; the confidence was not calibrated.

## Open questions for implementation
- Is Discogs worth building? 2 unique albums out of 60 is 3 percentage points,
  but it is now the only route to them. A judgement call, not a technical
  question.
- Tiers 2 and 3 have one measurement each. Bandcamp has two agreeing samples.
- Bleep and Boomkat search URLs: guessed, unverifiable by script (curl,
  Playwright, WebFetch all failed for different reasons), then checked by Les in
  a browser and confirmed **wrong**. Dropped rather than chased. Qobuz and
  Bandcamp are verified and sufficient. The lesson is small but real: when three
  automated approaches can't verify something, that's the signal to hand it to
  the human with a browser rather than to keep guessing at patterns.

## Next

Nothing is scheduled. Phase B (player panel) is shippable without Phase A because
the search-URL fallback covers every row, so either can go first. Within Phase A,
tier 1 alone is a legitimate stopping point: cheapest pass, best links.

Raw measurement output is untracked scratch at `tmp/purchase-research/` in the
primary checkout — stage1/2/3 JSON with the full hit and miss lists, should any of
this need re-checking. Not committed, and not durable; the numbers that matter are
in `spec.md`.
