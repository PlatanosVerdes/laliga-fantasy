# Refreshing

A fixed interval is the wrong shape for this game. Nothing happens for hours, and then
several things happen at an exact second: a clause unlocks, an auction closes, an offer
expires. Polling often enough to catch the second wastes the hours; polling calmly enough
for the hours arrives late to the second. So the loop asks two questions instead of one.

**Has anything moved?** The activity log and the market listing — two requests — are
digested into a fingerprint. Bid and offer counts are in it, because a rival bidding
changes what the page should say even though nothing has been transferred. Unchanged
fingerprint and no deadline near: the loop stops there. Both responses are stored in the
cache, so when the answer *is* yes, the rebuild reuses those very responses.

**When does something next matter?** Every deadline already present in the data: each
listing's expiry, each received offer's expiry, and the unlock time of any clause with a
raid scheduled on it. The loop sleeps until the soonest, minus two seconds, instead of
counting to 120 with its eyes shut — which is how a scheduled raid stops arriving up to
two minutes late.

Three kinds of wake-up, and `/healthz` names the next one (`next_wake_in`,
`next_wake_why`):

| Wake-up | When | What it does |
|---|---|---|
| probe | every `--interval`; ×4 only when no deadline is within 10 min **and** nobody has the page open | 2 requests, then usually nothing |
| deadline | 2 s before an expiry, a scheduled unlock, a kick-off, a final whistle or the matchday close | rebuilds unconditionally: this is the instant we may have to act |
| live match | every 2 min while one of *our* players is on the pitch | rebuilds, and drops the six-hour player cache first |
| rebuild | 15 min since the last full one | catches the drift nothing announces — values, futbolfantasy |

`/healthz` also reports the request counter (`requests`, `cache_hits`, `errors`), so the
cost of a refresh policy is a measurement rather than a claim.

**A match is the third thing that changes the world.** It touches neither the transfer
log nor the market, so the probe is blind to it, and the data that does move sits behind
the longest TTLs in the app: points come from the player master, cached for six hours
because it barely changes — except while a match is on, when it is the only thing that
changes. So the week's fixtures are part of the model, and their kick-offs, final
whistles and the matchday close are deadlines like any other.

Liveness is judged by the clock, not by a state code: `matchState` 1 is pending and 7 is
finished, and no live value has been observed, so a match counts as under way from
kick-off until 130 minutes later unless it says finished. Over-polling a postponed match
costs a request; missing a live one costs the points. While one of *our* players is on
the pitch the cadence tightens to two minutes and the player, lineup and week caches are
dropped each time; somebody else's fixture only moves the standings, so it gets the base
tick. The panel then reports the two figures a match moves: `Puntos de la plantilla` and
`Bajas` — the latter coloured the other way round, since more of them is not good news.

**Anything that moves the world says what it moved.** A completed sale changes the
cash, the squad, the market and every recommendation derived from them, so the world is
rebuilt rather than patched, and the rebuild is bracketed by a snapshot: the
before/after of cash, squad, squad value, listings and received offers is pushed over
SSE, shown on the page, and kept in `/healthz` as `last_effect`. An empty diff
publishes nothing, so a no-op never claims to have changed something.

Note what is *not* a sale. Listing a player leaves him yours, moves no cash and
transfers nobody — so `sell_to_market` invalidates the market and the squad slot's own
state, and the panel reports one line, `En el mercado 0 → 1`. The figures move when the
sale completes, and that moment is often nobody's click: a rival buys the player you had
listed, and it arrives through the activity probe. That is why every rebuild is
bracketed and not just the ones we caused — `traspaso` when the log moved, `mercado`
when only listings did. Squad value is left out of those, because the market revalues
players on its own and a panel for that would cry wolf; after an operation of ours it is
reported, because there it means something.

Instructions that fire unattended get the same treatment: the page built during the
cycle that accepted an offer describes the world from before its own action, so it is
rebuilt again.

**The page does not wait for futbolfantasy's player pages, and reads them far less.** The
profitable ceiling lives on one page per player, and futbolfantasy has no bulk source for it:
the market list, the site's own JavaScript and its `/api` routes were checked, and the site
itself opens `/mercado/detalle/{id}` one player at a time. So every ceiling read is kept in
`ff_ceilings.json` beside the cache (`/data/cache` in the container), loaded at start, and only
read again when it is stale: read before the night's price adjustment (00:17, taken as 00:30
Madrid) or older than `FANTASY_CEILING_TTL` (12 h by default). The panel is drawn at once with
what is stored. A stale ceiling is used and its row says how old it is (`ficha de hace 9 h`); a
player never read says `cargando ficha…`. Both are read again behind the page, my squad first,
then the market, then the rivals' players, then the watchlist. Each batch that lands (the first
at once, then at most one every two seconds) redraws the page and pushes a `details` event, and
an open tab asks for the views again and patches what changed, without a reload. What compares
the market's ceilings (the plan, the buy verdicts on *Decidir*, the stand-in for a sale, which
offer to take) waits only for candidates never read, with "Esperando N fichas de
futbolfantasy…", so nothing is proposed from half; once they are in, the page is the one that
waited. A 429 stops the pass and leaves the rest alone for five minutes. A bid or an offer reads
the player's page there and then; when it cannot be read it still goes through, with the stored
ceiling and its age, or warned that the ceiling is unknown ("sin techo rentable, decide tú"),
never as unprofitable.

The same record keeps the player's season high and low and the injury futbolfantasy still has
open. They are only drawn: a line on the player card, a "cerca de su máximo" / "cerca de su
mínimo" tag on *Decidir*'s sales and signings (within 10 %), and a "futbolfantasy: …" badge where
futbolfantasy disagrees with the panel's status. Nothing decides on them (see [state.md](state.md)).

**Paying a clause re-reads it first.** The plan can be built from data half an hour old,
and this is the one operation where that gap is expensive: the owner can raise the clause
or shield the player at any moment, and paying is irreversible. So before the write, one
request re-reads that squad slot, and the raid stands down if the player is shielded, the
clause is still locked, or it has risen above your `max_pay`.

Standing down is not the same as still being armed, and one case ends the instruction rather
than postponing it: the target changing hands. A clause belongs to a squad slot, so a sale
hands the player a new one, at the buyer's price and locked for a fortnight, and the clause the
raid was armed against no longer exists. So the raid records the manager it was aimed at, and it
is disarmed the moment the player is somebody else's or back in the market, the same treatment a
raid gets when the player becomes yours. Two things it will not disarm on: a player missing from a
short read, which is not a sale, and an instruction armed before the owner was recorded, where
there is nothing to compare.
