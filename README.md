# laliga-fantasy

Decision support for [LaLiga Fantasy](https://fantasy.laliga.com/): who to sign, who to
sell and which buyout clauses to pay, given your cash, your squad and your rivals' squads.

It merges two sources:

| Source | What it gives | Auth |
|---|---|---|
| `fantasy-api.llt-services.com` (official API) | 733 players with price, status, last-season points, live points; teams; calendar; daily market-value history; **your league, squad, cash, rivals' squads and clauses** | Bearer, league data only |
| `futbolfantasy.com` (HTML scrape) | value deltas over 1/2/3/7/14/30 days, trend, acceleration, next fixture, **odds of starting it**, "puja máxima rentable", per-matchday points and injury history | none |

It can also act: bid, sell, accept an offer, pay a clause, save a lineup. Nothing moves
without confirming it first — every operation is prepared, shown with its amount and what it
leaves in the bank, and only then confirmed with a single-use token. `--read-only` refuses all
of them.

No dependencies. Go 1.26 and the standard library.

## Quick start

```bash
go build -o fantasy ./cmd/fantasy

./fantasy auth browser              # log in (two steps, see below)
./fantasy advise                    # the main command
./fantasy report                    # HTML dashboard, written to the state directory
./fantasy serve --port 8777         # or keep it live on a port, with the write API
```

## Session

LaLiga authenticates through an Azure AD B2C tenant (`login.laliga.es`). Access tokens last
24h and league endpoints reject anonymous calls.

### `auth browser` — the one that works app-only

LaLiga Fantasy is effectively app-only: <https://fantasy.laliga.com/> is a landing page, not
a web client, so there is no browser session to lift tokens from. If you do not have an
account yet, create it in the mobile app first
([iOS](https://apps.apple.com/app/id968915185) /
[Android](https://play.google.com/store/apps/details?id=com.lfp.laligafantasy)).

The way in is the **mobile app's own OAuth client**, which accepts the authorization-code +
PKCE flow from any desktop browser, federated logins included. Two commands, because the
browser step happens in between:

```bash
./fantasy auth browser          # prints the URL, stores the PKCE verifier
./fantasy auth code '<url>'     # exchanges the code the browser redirected to
```

`serve` does the same thing without a terminal: with no session, the page itself shows the
login link and a box to paste the redirect into, which is how a fresh deploy authenticates.

**Step 1 — get the URL.** `auth browser` builds it and saves the PKCE verifier and `state`
to `pending_auth.json` in the config directory (valid 15 minutes). It looks like this, with the app's client id
and its native redirect:

```
https://login.laliga.es/laligadspprob2c.onmicrosoft.com/oauth2/v2.0/authorize
  ?p=B2C_1A_5ULAIP_PARAMETRIZED_SIGNIN
  &client_id=af88bcff-1157-40a0-b579-030728aacf0b
  &response_type=code
  &redirect_uri=authredirect%3A%2F%2Fcom.lfp.laligafantasy
  &scope=openid+offline_access
  &code_challenge=<generated>&code_challenge_method=S256
  &state=<generated>&nonce=<generated>
```

Use the URL the command prints, not this one: the challenge and state are generated per
attempt and the exchange fails without the matching verifier.

**Step 2 — open DevTools _before_ logging in.** This is the part that catches everyone out.
In Chrome: `Cmd+Opt+I` / `Ctrl+Shift+I` → **Network** tab → tick **Preserve log**. Then paste
the URL and log in.

**Step 3 — log in.** The page offers Google / Apple / Facebook, the same options as the app.
Google may then ask for extra scopes on LaLiga's behalf (YouTube, gender, birth date, postal
address): **leave every box unchecked**, none of it is used here, and click Continue.

**Step 4 — grab the code from the Network log.** The browser now tries to open
`authredirect://com.lfp.laligafantasy` and **hangs on a blank page with the address bar
unchanged** — Chrome cannot handle a custom scheme, and this is what success looks like. The
redirect is in the Network log as the **last row, status `(canceled)`**, its name starting
with `?state=…&code=…`:

```
authresp?state=StateProperties%3D…        302   document   3.9 kB
?state=hYTieapYtmRTbp_dfsN5JQ&cod…  (canceled)  document   0.0 kB   ← this one
```

Right-click it → **Copy → Copy link address**. (Or click the row → **Headers** → copy
**Request URL**.) You get the full redirect:

```
authredirect://com.lfp.laligafantasy/?state=hYTieap…&code=eyJraWQiOiJDandFaWN0…
```

**Step 5 — exchange it.** Quote it: the `&` would otherwise split in the shell.

```bash
./fantasy auth code 'authredirect://com.lfp.laligafantasy/?state=…&code=…'
# Sesion guardada. tu@email · caduca en 1439 min · refresh: si
```

The `code` is a JWE and single-use, so if the exchange fails for any reason other than a
`state` mismatch, start again from step 1.

Splitting the flow in two also means neither command needs a TTY, so it works over ssh.
`state` is verified and B2C errors are surfaced verbatim. From then on `auth refresh` — called
automatically when the token is within 2 minutes of expiry, at most once every 10 minutes —
keeps the session alive indefinitely, exactly as the app does; you should never have to log in
again. `auth status` shows what is stored. Tokens live in `tokens.json`, mode `0600`, in the config
directory — see [Where things live](#where-things-live).

### Other routes

* Pasting a whole `tokens.json` into the page's login box works too, and is checked against
  `/user/me` before it is accepted. Useful when a session already exists on another machine.

## Commands

| Command | Purpose |
|---|---|
| `advise` | Signings, payable clauses, sell candidates and clause-risk warnings |
| `squad` | Your squad valued, with xPts and trend per player |
| `market` | Global ranking. `--sort score\|value\|trend\|xpts\|price`, `--position DEF`, `--max-price`, `--free` |
| `player <name>` | Full profile. `--history 10` adds the per-matchday table from futbolfantasy |
| `standings` / `leagues` | League table and your leagues |
| `activity` | The league's transfer log (`--pages N`) |
| `lineup` | Who is on the pitch, who cannot play, and the best legal eleven. `--fix` saves it |
| `report` | Self-contained HTML dashboard with sortable tables (`--open`, `--json`) |
| `serve` | Serve the report over HTTP with a JSON API and SSE push — see [deploy/docker.md](deploy/docker.md) |
| `probe <what>` | Dump a raw endpoint payload — use it when the API shape changes |
| `cache` | Cache size, `--clear` to wipe |

Useful flags: `--budget N` to override the cash reading, `--deep` (see below), `--fresh` to
bypass the futbolfantasy cache, `-v` for verbose logs.

## The model

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

### Rival cash

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

### Caveats

* At the very start of a season everything rests on last-season output and the odds of
  starting. `w` grows to 1 by matchday 9.
* ~93% of futbolfantasy's market rows resolve to a LaLiga player. The rest are coaches and
  youth players who are not in the game. LaLiga only publishes short nicknames
  ("f de jong", "aitor fdez"), so matching runs in passes from exact to fuzzy and only
  commits pairs that are unambiguous in both directions.
* Team ids differ between the two sources (LaLiga 3 = Athletic, futbolfantasy 3 = Barcelona);
  everything is matched by normalized team name.

## Where things live

Files follow the [XDG base directory spec](https://specifications.freedesktop.org/basedir-spec/latest/),
split by what they are — so a backup of the first directory is a backup of everything that
cannot be regenerated:

| Directory | Default | Holds |
|---|---|---|
| config | `~/.config/laliga-fantasy/` | `tokens.json` (`0600`), `settings.json`, `favourites.json`, `policies.json` |
| state | `~/.local/state/laliga-fantasy/` | `report.html`, `fantasy.log` |
| cache | `~/.cache/laliga-fantasy/` | scraped futbolfantasy pages, 24 h TTL — safe to delete |

`XDG_CONFIG_HOME`, `XDG_STATE_HOME` and `XDG_CACHE_HOME` are honoured. Two overrides:

* **`FANTASY_DATA_DIR`** collapses all three into one directory. That is what a container
  wants — a single volume to mount — and it is what the image sets.
* If a `data/` directory next to the code already has a session in it, it keeps being used, and
  the first run moves those files to the locations above and tells you so. Upgrading never
  costs a login.

**Seeding a container without an interactive login.** The refresh token *rotates*: the API
can hand back a new one on every renewal, so it has to be written somewhere — which is why
`gh`, `aws` and `kubectl` all keep rotating credentials in a file rather than an environment
variable. To hand over the first one, set **`FANTASY_TOKENS`** (the whole `tokens.json` as
JSON) or **`FANTASY_REFRESH_TOKEN`** (just the refresh token, exchanged on startup). Either
one is read only when no token file exists, is written to the file at `0600`, and rotation
takes over from there — so you can drop the variable after the first start.

## Refreshing

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

**Paying a clause re-reads it first.** The plan can be built from data half an hour old,
and this is the one operation where that gap is expensive: the owner can raise the clause
or shield the player at any moment, and paying is irreversible. So before the write, one
request re-reads that squad slot, and the raid stands down if the player is shielded, the
clause is still locked, or it has risen above your `max_pay`.

## Logging

Human-readable lines on stderr (warnings only, `-v` for everything) and JSON lines in
`fantasy.log` in the state directory, rotated at 5 MB × 3. Each record carries `service`, `level`, `host` and
the event's own fields (`url`, `status`, `ms`, `matched`, ...).

Set `FANTASY_LOG_JSON=1` to emit JSON on **stdout** instead of the terminal format — that is
what the container image does, so Vector's docker source collects it with no configuration.

Set `FANTASY_LOG_URL` to also push records straight into VictoriaLogs:

```bash
FANTASY_LOG_URL='http://<pi>:9428/insert/jsonline?_stream_fields=service' ./fantasy advise
```

Pushes are fire-and-forget on a daemon thread, so an unreachable sink never blocks or fails
a command. Alternatively, if the repo lives on the homeserver, Vector already tails `*.log`
from repo roots into VictoriaLogs and needs no configuration.

## Layout

```
cmd/fantasy/              the commands
internal/config/          endpoints, ids, squad rules, paths
internal/httpx/           HTTP client with on-disk TTL cache, retries and a freeze switch
internal/auth/            token store, B2C refresh, the login flow
internal/api/             official API client
internal/futbolfantasy/   the four scrapers
internal/matching/        team and player identity resolution across sources
internal/model/           the universe: squads, clauses, cash, absences, xPts, score
internal/advice/          what to do: signings, clauses, sales, offers
internal/policies/        standing instructions, and what counts as a good offer
internal/render/          the HTML page, cell by cell
internal/server/          HTTP surface: JSON API, SSE push, writes, session setup
internal/state/           the built world, its version and who is watching
internal/engine/          when to wake up and what to invalidate
internal/schedule/        deadlines, live matches, change detection
internal/writes/          the operations, their validation and the two-step guard
internal/cli/             terminal tables and colour
assets/                   the page's CSS, JS and HTML pieces
Dockerfile                dependency-free image for the homeserver
deploy/docker.md          running it as a container, endpoints, write flags
```

## Endpoint notes

Season 26/27 moved the API to `fantasy-api.llt-services.com` and put most routes under
`/v1/competition/1/`. The old `api-fantasy.llt-services.com` host is frozen on 25/26.
Watch out for the inconsistency the official app also carries: standings and squads live
under `/leagues/{id}/...`, market and buyout under `/league/{id}/...`.

Public (no session): `/v1/competition/1/players`, `/v3/teams-master`,
`/v1/competition/1/week/current`, `/v1/competition/1/calendar?weekNumber=N`,
`/v1/competition/1/player/{id}`, `/v1/competition/1/player/{id}/market-value`.

Session required: `/v4/user/me`, `/v1/competition/1/leagues`, `.../leagues/{id}/standing`,
`.../leagues/{id}/teams/{teamId}`, `.../leagues/{id}/activity/{index}`,
`.../teams/{teamId}/money` (own team only), `.../league/{id}/market`.

`/money` answers `{"teamMoney": N, "teamInvestment": N}`. Squad slots carry `buyoutClause` and
`buyoutClauseLockedEndTime` — at the start of a season every clause in the league is locked
for a couple of weeks, so the raid list being empty is the game's rule, not a bug.

Two fields to know about. `lastSeasonPoints` is **absent** for players the game re-registered
this season (Mbappé, Lamine, Vini, Bellingham, Pedri — ids 3100-3104) and zero for promoted
clubs, in the list *and* in the player detail. And `positionId` 5 is the coach: 23 of the 733
"players" are managers, excluded everywhere.
