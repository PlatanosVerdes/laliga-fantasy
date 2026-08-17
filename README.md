# laliga-fantasy

Decision support for [LaLiga Fantasy](https://fantasy.laliga.com/): who to sign, who to
sell and which buyout clauses to pay, given your cash, your squad and your rivals' squads.

It merges two sources:

| Source | What it gives | Auth |
|---|---|---|
| `fantasy-api.llt-services.com` (official API) | 733 players with price, status, last-season points, live points; teams; calendar; daily market-value history; **your league, squad, cash, rivals' squads and clauses** | Bearer, league data only |
| `futbolfantasy.com` (HTML scrape) | value deltas over 1/2/3/7/14/30 days, trend, acceleration, next fixture, **odds of starting it**, "puja máxima rentable", per-matchday points and injury history | none |

Read-only: it never bids, sells or changes your lineup.

No third-party packages. Python 3.11+ and the standard library.

## Quick start

```bash
python3 fantasy.py market --no-auth --global-only   # public ranking, no session needed
python3 fantasy.py auth browser                     # log in (two steps, see below)
python3 fantasy.py advise                           # the main command
python3 fantasy.py report --open --deep              # HTML dashboard
python3 fantasy.py serve --port 8777                # or keep it live on a port
```

## Session

LaLiga authenticates through an Azure AD B2C tenant (`login.laliga.es`). Access tokens last
24h and league endpoints reject anonymous calls.

### `auth browser` — the one that works app-only

LaLiga Fantasy is effectively app-only: there is no web client to log into, so there is no
browser session to lift tokens from. But the **mobile app's own OAuth client** accepts the
authorization-code + PKCE flow from any browser, federated logins included:

```bash
python3 fantasy.py auth browser          # prints the URL, stores the PKCE verifier
python3 fantasy.py auth code '<url>'     # exchanges the code it redirected to
```

1. The first command builds the B2C `/authorize` URL with the app's client
   (`af88bcff-1157-40a0-b579-030728aacf0b`) and its redirect
   `authredirect://com.lfp.laligafantasy`, and saves the PKCE verifier plus `state` to
   `data/pending_auth.json` (valid 15 minutes). The page offers Google / Apple / Facebook,
   the same options the app does.
2. You log in. Google may ask for extra scopes on LaLiga's behalf (YouTube, gender, birth
   date, postal address) — leave every box unchecked, none of it is used here.
3. The browser then tries to open that custom scheme and **hangs on a blank page**: Chrome
   cannot handle the scheme and does not update the address bar. Open DevTools → Network
   with "Preserve log" *before* finishing, and the redirect is the last row, marked
   `(canceled)`; right-click → Copy link address gives you
   `authredirect://com.lfp.laligafantasy/?state=…&code=…`.
4. Pass that URL (or the bare code) to `auth code`. It is exchanged locally against the token
   endpoint using the stored verifier, and you get an **access token plus a refresh token**.

Splitting it in two also means neither command needs a TTY, so the flow works over ssh.
`state` is checked and B2C errors are surfaced. From then on `auth refresh` (called
automatically when the token is within 5 minutes of expiry) keeps the session alive.
`auth status` shows what is stored. Tokens live in `data/tokens.json`, mode `0600`.

### Other routes

* `auth snippet` + `auth paste` — if you ever do get a browser session, this lifts the tokens
  out of `localStorage`/`sessionStorage`. Kept as a fallback.
* `auth login` — B2C resource-owner-password flow. Only works for accounts that actually have
  a password, not federated ones.

## Commands

| Command | Purpose |
|---|---|
| `advise` | Signings, payable clauses, sell candidates and clause-risk warnings |
| `squad` | Your squad valued, with xPts and trend per player |
| `market` | Global ranking. `--sort score\|value\|trend\|xpts\|price`, `--position DEF`, `--max-price`, `--free` |
| `player <name>` | Full profile. `--history 10` adds the per-matchday table from futbolfantasy |
| `standings` / `leagues` | League table and your leagues |
| `activity` | The league's transfer log (`--pages N`) |
| `report` | Self-contained HTML dashboard with sortable tables (`--open`, `--json`) |
| `serve` | Serve the report over HTTP and regenerate it on an interval — see [deploy/raspberry.md](deploy/raspberry.md) |
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

## Logging

Human-readable lines on stderr (warnings only, `-v` for everything) and JSON lines in
`data/fantasy.log`, rotated at 5 MB × 3. Each record carries `service`, `level`, `host` and
the event's own fields (`url`, `status`, `ms`, `matched`, ...).

Set `FANTASY_LOG_JSON=1` to emit JSON on **stdout** instead of the terminal format — that is
what the container image does, so Vector's docker source collects it with no configuration.

Set `FANTASY_LOG_URL` to also push records straight into VictoriaLogs:

```bash
FANTASY_LOG_URL='http://<pi>:9428/insert/jsonline?_stream_fields=service' python3 fantasy.py advise
```

Pushes are fire-and-forget on a daemon thread, so an unreachable sink never blocks or fails
a command. Alternatively, if the repo lives on the homeserver, Vector already tails `*.log`
from repo roots into VictoriaLogs and needs no configuration.

## Layout

```
fantasy.py            entry point
fantasy/config.py     endpoints, ids, squad rules, team aliases
fantasy/http.py       urllib client with on-disk TTL cache and retries
fantasy/auth.py       token store, B2C refresh, browser snippet
fantasy/laliga.py     official API client
fantasy/futbolfantasy.py  the three scrapers
fantasy/matching.py   team and player identity resolution across sources
fantasy/analysis.py   xPts, score, rival cash, recommendations
fantasy/report.py     HTML report
fantasy/serve.py      HTTP server mode
fantasy/logs.py       logging
Dockerfile            dependency-free image for the homeserver
deploy/raspberry.md   compose service, Caddy route, session bootstrap
data/                 cache, tokens, settings, logs, report (gitignored)
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
