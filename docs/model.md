# The model

## Sources and operations

It merges two sources:

| Source | What it gives | Auth |
|---|---|---|
| `fantasy-api.llt-services.com` (official API) | 733 players with price, status, last-season points, live points; teams; calendar; daily market-value history; **your league, squad, cash, rivals' squads and clauses** | Bearer, league data only |
| `futbolfantasy.com` (HTML scrape) | value deltas over 1/2/3/7/14/30 days, trend, acceleration, next fixture, **odds of starting it**, "puja máxima rentable", per-matchday points, injury history and the **jerarquía** each player holds in his own squad | none |

It can also act: bid, sell, accept an offer, pay a clause, shield one of your own players,
save a lineup. Nothing moves
without confirming it first — every operation is prepared, shown with its amount and what it
leaves in the bank, and only then confirmed with a single-use token. `--read-only` refuses all
of them.

`xPts` is an estimate of points per matchday:

```
base    = w · (points this season / weeks played) + (1-w) · (last-season points / 38)
          w = min(1, completed weeks / 8)
xPts    = base · availability · fixture · confidence
```

* **availability** — odds of starting from futbolfantasy, relative to a 85% baseline,
  clamped to [0.20, 1.10]. `base` already averages over missed weeks, so this only corrects
  for a changed role, not for absences twice.
* **fixture** — opponent strength (rank percentile of squad value and last-season points)
  and home/away, roughly ±12% and ±4%.
* **confidence** — ×0.75 when futbolfantasy has no row for the player, ×0.9 when the
  baseline came from the price prior.
* Injured / suspended players get `xPts = 0`; doubtful ones ×0.55.

**Price prior.** Promoted clubs (Málaga, Racing, Deportivo) carry zero last-season points
because those points only exist for LaLiga, and the game recreated the star records this
season with no history at all (Mbappé, Lamine, Vini, Bellingham, Pedri). For them the
baseline comes from a log-price → points-per-week curve fitted per position on the players
who *do* have history, extrapolated past its ends. `--deep` replaces that guess with the
real thing by reading their futbolfantasy page (validated exactly against the API where
both exist: Raphinha 179 = 179, Unai Simón 211 = 211).

**Score** is a weighted blend of standard scores: points per million (0.30, winsorized so a
handful of near-free players cannot flatten the distribution), absolute xPts (0.35),
projected 7-day value change (0.20) and odds of starting (0.15).

`advise` then ranks free agents you can afford, rival players whose unlocked clause fits
your spending power, your own worst holdings, and your good players whose clause sits close
to their value.

## Whether a clause is worth paying

Two tables answer that, and until now only one of them could. Both rank rivals' clauses; the
difference is the clock. **Cláusulas pagables** is what can be paid today, **Cláusulas de rivales
que se abren** is what unlocks next, with the hour.

The second one carried `¿Renta?`, which compares the points per million bought *by paying the
clause* against the median of your own squad, and the first one did not: the list of clauses you
could act on this minute said how much they cost and never whether they were worth it. It says
both now, and the verdict is graded within each list, because a bargain has to mean the best of
what you can actually do rather than the best of what you cannot do yet.

The reference is your own squad on purpose. Benchmarking against the market brands everything a
bargain, since a bad market day drags the median down and says nothing about the player. And the
answer is often no: a 30.00M clause on a player worth 14.59M comes out at **0.9x** your squad's
points per million, which is the number behind "paying over value buys protection, not points".

## Rival cash

The API serves `/money` for your own team only — `teamMoney` is `null` for every other team,
in the standings and in their squad payload alike. And the standings figure is **not** total
worth: `teamValue` equals the sum of the squad's market values exactly, carrying no cash at
all (verified across every team in a real league).

So cash is reconstructed from the transfer log instead:

```
cash = base + sales - purchases
```

Purchases and sales are all in `/leagues/{id}/activity/{index}`, whose events are pure ids:
`activityTypeId` (**31** bought from market, **33** sold to market, **1** transfer between
managers where `user1` pays and `user2` receives, **9** joined the league), `user1Id`,
`user2Id`, `playerMasterId`, `amount`.

The one term the log does not record is the daily reward (+100.000 a day, claimed or not), so
rather than guess it, the whole league is anchored on the single cash figure that *is*
readable — yours: `base = my_cash - my_net_flow` folds starting cash and your own claimed
rewards into one measured constant, and every rival gets that base plus their own net flow.
The residual error is therefore only the *difference* in rewards claimed between managers,
bounded by 100k a day.

In a real 13-team league this resolved to a base of exactly **100.500.000** — 100M of
starting cash plus 500k of claimed rewards — and two independent checks hold: a manager who
has never traded lands exactly on the base, and squad value plus cash comes out in a tight
214–236M band for everyone. `advise` prints the base, whether it is anchored, and the margin.

That turns the clause-risk section from "this clause looks low" into "these three managers can
pay it right now, and the richest is X".

## The matchday in play

The **Partidos** tab opens with the matchday as it stands, because the number everybody quotes at
each other on a Sunday is the league's live table and halfway through a matchday it says the wrong
thing: a manager two points behind you with six men still to kick off is not behind you.

So every row carries what is still to come beside what is already scored, and the seat each one
would end in rides along:

* **Puntos** is the game's own `livePoints` from the standings, not a reconstruction. Verified
  against the per-manager totals the week-scoped lineup route serves: all thirteen agree to the
  point.
* **Por sumar** is the xPts of that manager's players whose match has not kicked off. Pending is
  judged by kick-off and never by the match state, because the calendar is cached for hours and
  its states go stale while a kick-off time cannot.
* **Acabaria en** is the two added together, with an arrow when it disagrees with the order the
  table is sorted in. On a real matchday the manager leading on points was heading for fourth and
  the one lying fifth was heading for first, which is the entire reason the column is there.

It is a ceiling rather than a forecast, and says so on the page: a rival's saved eleven is not
readable without a request per manager, so what is counted is every player of his still to kick
off, capped at eleven because only eleven score. The injured and the suspended are left out.

Once the last ball is kicked the four forward-looking columns hold nothing, so they go, along with
the paragraph that explained them: a finished matchday is a result.

## Who looks worst next matchday

The cash table says who is rich; it cannot say who is about to have a bad weekend. So the league
is also read forward: one row per manager in the **Rivales** tab, ranked by the points each squad
can be expected to score on the first matchday nobody has played yet. Deliberately not the one
under way, however many matches it still has to come: half its points are real by then, and the
section above reads those.

```
eleven = the best legal eleven, by expected points, of everything he owns
xPts   = Σ base · availability · fixture(that matchday) · confidence · fitness
```

Four inputs, three of which the page already had:

* **the eleven**: formations are the rule, so only eleven in a legal shape score. A squad that
  cannot fill one carries its empty slots as zeros: nine players field nine, and it says so.
* **the fixture of that matchday**: read from the calendar rather than from whatever
  futbolfantasy calls a given player's next game, so every squad is measured against the *same*
  matchday. Opponent strength is the 0..1 club percentile the model already uses (±12%), plus
  ±4% for home or away, which is why a Barça and a Girona do not count the same.
* **who is out**: the API's status, and above it futbolfantasy's verdict for that exact
  matchday ("Baja confirmada para la jornada 3", "Duda para la jornada 3", "Disponible para la
  jornada 3"). That verdict wins when it names the matchday being asked about, because the API
  lags: it keeps a player flagged for days after he is training again. A doubt is ×0.55, the
  same discount the model applies everywhere; a suspension and a player who has left LaLiga are
  zero.
* **fitness against depth**: the same eleven is computed twice, once as it stands and once with
  every absence cleared. The gap is the news: an injury three deep on the bench costs nothing,
  and the same injury in a squad of eleven costs the whole player. One number separates a bad
  squad from a bad week.

Two things it does not know, and says so on the page. What a rival will actually line up, so
this is only the best eleven he *could* field: he can do worse, never better. And the odds of
somebody getting injured, which nobody publishes: what exists is who is out, until when, and the
odds of starting, which is where the risk of not playing lives.

## Jerarquia

The odds of starting answer "will he play on Saturday". They do not answer "is this a footballer
his coach builds around", and those are different questions: a 70% can be a starter nobody would
drop or a rotation man whose turn it happens to be. futbolfantasy publishes the second answer as
**jerarquia**, its verdict on where a player stands in his own squad, and it sorts:

| | |
|---|---:|
| Dios | 60 |
| Clave | 50 |
| Importante | 40 |
| Rotacion | 30 |
| Suplente | below |

It separates players the rest of the numbers put side by side. Camello and Berenguer were both
70% to start the same weekend; Camello is Importante and Berenguer is Rotacion. Xavi Espart, whose
value had gone from 703K to 23.88M in a month, is Rotacion at 30%.

It lives only on the player's own page, never in the market listing, so it costs a request per
player and is read only for the one being looked at: the `player` command and the web card, both
cached. The page is reached by a slug built from the name, and a slug can land on a stranger
(`angel-perez` is a Segunda player, not the Alaves one), so nothing is read from a page whose own
`data-jugador` id disagrees with the player asked about. That guard also covers the starting
probability, which the same page has been filling in since the market listing started omitting it
for some players.

## Caveats

* At the very start of a season everything rests on last-season output and the odds of
  starting. `w` grows to 1 by matchday 9.
* ~93% of futbolfantasy's market rows resolve to a LaLiga player. The rest are coaches and
  youth players who are not in the game. LaLiga only publishes short nicknames
  ("f de jong", "aitor fdez"), so matching runs in passes from exact to fuzzy and only
  commits pairs that are unambiguous in both directions.
* Team ids differ between the two sources (LaLiga 3 = Athletic, futbolfantasy 3 = Barcelona);
  everything is matched by normalized team name.
