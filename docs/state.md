# Where this stands — August 2026

Written to be picked up months later without re-reading the code.

## What exists

One Go binary (`cmd/fantasy`), no dependencies. Python is gone: the port finished on
18 August 2026 and `fantasy/`, `fantasy.py` and the thirteen comparison harnesses in
`tools/` were deleted with it.

It runs on the Raspberry Pi as `laliga-fantasy`, behind Caddy at
`https://fantasy.platanosverdes.com` — reachable inside the tailnet only, with no basic
auth, because the network is the door. The homepage card sits under *Services* with a
`siteMonitor` on `/healthz`.

**The comparator** answers the question the player card never did: not "how good is he" but
"instead of whom". A `+` on the card and on every squad row fills a tray at the bottom of the
page (kept in `localStorage`, so the live re-render cannot wipe it); the tray also has a search
box, because adding a player should not mean finding him in a table first (`?q=` searches the
same world, accent-insensitive, prefix matches first). `Comparar` lays them out in one table, and
`+ mis MED` pulls in your own players of that line, best score first. With one
outsider against your own, a verdict line says whether he improves your best in that position,
only your worst, or none of them, and what the difference costs. `GET /api/compare?ids=…`
(`internal/server/compare.go`) builds it from the world already in memory, so it costs no LaLiga
request; only the profitable ceiling is fetched from futbolfantasy, in parallel and cached.

**Writes are on.** Nothing executes without being confirmed on the page: every operation is
prepared, shown with its amount and what it leaves in the bank, and confirmed with a
single-use token that expires after 120 s. `--read-only` refuses all of them.

**A cold start shows the league as it reads it.** Before the first build, `model.BuildPreview`
reads my squad (lineup, values, status, balance), then the market, then the rivals' squads,
and the page shows each as it lands, with "Cargando mercado…", "Cargando rivales…" for what is
missing; the offers, the activity, futbolfantasy and the advice come with the full panel. The
preview asks nothing the build does not ask too, with the same lifetimes, so the build reads it
back from the cache and the world comes out the same. It is a `model.Preview`, never a
`Universe`: until the first build is over `State.Loading()` is true, the guard and the server
refuse every operation with "Cargando la liga, espera a que termine" (503), the instructions,
the daily reward and the necroporra vote see no world, and an open tab reloads into the
preview. Later rebuilds keep serving the last page.

**The panel does not wait for the futbolfantasy player pages, and reads them far less.** Only
the profitable ceiling is judged by, and it is kept in `ff_ceilings.json` under the cache root
(`futbolfantasy.Detail`: `ideal_bid`, `url`, `fetched_at`, plus the informative extras below),
loaded at start ("ceilings
loaded" logs the file, how many are stored and fresh, and the TTL). A ceiling is fresh until the
night's price adjustment (00:30 Madrid) and for at most `FANTASY_CEILING_TTL` (12 h). The page is
drawn with every stored ceiling (`advice.Filler`): a stale one is used and marked with its age
(`detail_age`, "ficha de hace 9 h"), a player never read carries `detail_pending` ("cargando
ficha…"), and both are read again behind it in the order the page shows them: my squad, the
market, the rivals' players, the rest. Each batch that lands redraws the page (`State.Redraw`: the
cache key moves, the version does not) and pushes a `details` event, and an open tab refetches
`/api/views`. Everything that compares the market's ceilings (the swap plan, the buy verdicts and
stand-ins on *Decidir*, which offer to take on *Vender*, the plan's floor for an armed sale) is
held only while a candidate has never been read, with "Esperando N fichas de futbolfantasy…",
because a missing ceiling reads as futbolfantasy saying no; once they land the page equals the
one that waited. A 429 stops the pass, and what was left keeps its stored ceiling, or is drawn as
a failed fetch, for five minutes. A bid, an offer or an acceptance reads the player's page again
in `playerFor`; when it cannot be read the stored ceiling is used and its age said, and with
none the operation still goes through with "no he podido leer la ficha de futbolfantasy de X:
sin techo rentable, decide tú" and nothing is checked against it (`writes.Player.CeilingUnread`,
`CeilingAge`). The player card and the comparator read the same store.

**The same page's extras are drawn, never judged.** Each record also keeps the season's high
and low with their days (`max_value`, `max_date`, `min_value`, `min_date`, worked out the way the
page's own script does) and the injury futbolfantasy still has open on its value chart (`mark`:
`state`, `reason`, `since`; an empty `mark` is "nothing open", a missing one is unknown). They
share the record's TTL and refresh; a record stored before them simply lacks them and shows
nothing. The page reads them through `render.FFPages`: the player card adds "Máx 21.0M (11 oct)
· Mín 5.8M (11 ago) · ahora al 82 % de su máximo"; *Decidir* tags a sale within `NearBand` (10 %)
of the season high "cerca de su máximo" and a signing within 10 % of the low "cerca de su mínimo"
(only when the two bands do not overlap); and the eleven, the squad and the cards carry
"futbolfantasy: lesionado / duda / sancionado / disponible" only when that differs from the
status the panel goes by. The chart only marks injuries, so its "available" never contradicts a
sanction; an image it does not know says nothing. Stale records say their age. No advice, plan,
card order, score, policy or automatic action reads any of it, and
`TestTheExtrasChangeNoDecision` holds that. futbolfantasy's value history is not read: LaLiga's
own is.

futbolfantasy has no bulk source for the ceiling: the market list (`/analytics/laliga-fantasy/
mercado`, ~670 players) carries value, trend and acceleration but no ceiling, the site's own
`app.min.js` loads `/analytics/{game}/mercado/detalle/{id}` one player at a time, and its `/api`
routes are lineups, polygons and seasons. The detail page also carries the value history and
the season's high and low; they are still parsed (it costs no request) but no longer written
onto the rows, which nothing read. Before, about 80 tracked players were read every 4 h, up to
~480 requests a day; now each is read at most twice a day (~160), once with a 24 h TTL, and a
restart reads none.

## Deploying

1. Branch, PR against `main`, squash merge.
2. The `auto-tag` workflow creates `vYYYY.MM.DD.N` — **with a dot**. A dash suffix breaks
   rpi-services' `sort -V`, which reads `v2026.08.18-36` as older than `v2026.08.18.7`; that
   is how a Python image once got deployed on top of the Go one without anybody noticing.
3. In `rpi-services`, point `LALIGA_FANTASY_VERSION` at the new tag and push. **The Pi's
   webhook deploys on its own.**
4. Do not also run `docker compose up --build` over ssh: it queues a second Go build on ARM
   (five minutes each) and seven of them once piled up.
5. Do not deploy while somebody is confirming an operation. A container replaced mid-request
   leaves the confirmation without an answer — and the bid it was confirming still went
   through.

## Logs

The container prints JSON on stdout (`FANTASY_LOG_JSON=1`) at info level; Vector's docker
source ships every container to VictoriaLogs with no per-service configuration. Query by
`container:laliga-fantasy`. Port 9428 is not published on the host, so a query has to go
through a container on the same network (`docker exec vector wget -qO- ...`) or the Grafana
data source.

Where a slow load spent its time is in two lines. `build phases` comes once per start, with
the milliseconds of each step of the first build (`mine_ms`, `listings_ms` and `rivals_ms` for
the preview, then `squads_ms`, `calendar_ms`, … and `page_ms` for drawing the page inside it);
`preview published` marks when each stage of the preview reached the page. `page rendered` comes with every render, split the same way
(`advice_ms`, `details_ms` for the futbolfantasy pages already in hand, `necro_ms`,
`document_ms`). `details filled` closes each background pass over the missing and stale pages:
`players` (the requests it made), `read`, `failed`, `limited` for a 429, `ms`, and
`first_ms`/`last_ms` for when the first and the last page landed. `ceilings loaded` at start
says how many ceilings came back from disk and how many of them are fresh, which is the
requests a restart no longer makes.
`page served while redrawing` means a browser got the previous page at once while the new one
was drawn behind it; `serving the wait page` means it got the progress page instead.

## Things only learnt by breaking them

* The API **refuses a second bid** on the same listing with a bare 400. Your own bid travels
  *inside* the market entry (`bid: {id, money, status}`) and that is the only place it is
  exposed: `GET .../bid` is 405.
* The profitable ceiling (`ideal_bid`) is not in the model. It lives in futbolfantasy's page
  as `parsePujaIdeal(N)`. Without reading it the server warned "futbolfantasy sees no margin"
  on every single bid, whatever the amount.
* Player photos are absent from the public players list. They appear only in squads and in
  the market payload, which is why faces are collected from both.
* `display:flex` beats the `hidden` attribute. Any class hidden that way needs its own
  `[hidden]{display:none}` rule.
* Go's `%v` goes scientific at ten million — the middle of every price in this game. One
  `render.PyFloat` decides how a number becomes text, everywhere.
* Blind surname matching once marked 24 healthy players injured (Pau López inherited Diego
  López's injury). Absences match on unique surnames plus compatible first names.
* The bulk `/players` list lags each player's own `/player/{id}` on status: on 2026-10-12 an
  injury showed on the player page and the lineup for over an hour while the list still said
  `ok`. Our own squad's status is read per player, and the graver of the two wins.
* Offers can arrive for a player that is not listed. They are read for the whole squad, but
  answering one needs a market id it does not have, so the panel says to answer it in the app.
* Cash cannot be read for rivals. It is reconstructed from the transfer log and anchored on
  the one balance that *can* be read — ours — so the residual error is only the difference in
  daily rewards claimed.

## What the port was checked against, before Python was deleted

Every piece was compared against the Python implementation over the same frozen snapshot:
the model (52 fields × 729 players), 141 scraped pages, 596 matcher pairs, 22 advice buckets,
the policy plan on real and synthetic squads, 74 render cells, 15 tables, the whole page byte
for byte (796,081 bytes) built from Go's own world, 36 scheduler decisions, and 11 write calls
with 15 validation rows — none of them sent. The Go halves of those harnesses are still here
and still useful on their own: `model --json`, `advise-json`, `plan`, `match`, `scrape`,
`cells`, `section`, `shell`, `page`, `calls`, `checks`, `wake` and `probe`. With
`FANTASY_FREEZE=1` they read from cache and refuse the network.

## Loans (premium, so not built)

The game added loan offers, and the routes were mapped by probing before finding out the feature
is premium — so nothing reads them. Kept here rather than in dead code, because reading them cost
twelve API calls on every rebuild for something the account cannot use:

* `POST   .../league/{league}/loan` — make a loan offer (body unknown, never sent)
* `GET    .../league/{league}/playerTeam/{playerTeamId}/loan` — the ones received, `[]` today
* `.../league/{league}/loan/{id}/accept | reject | cancel` — exist under other verbs

## What is missing

* **Writes not yet exercised live**: `modify_bid`, `cancel_bid`, `accept_offer`,
  `decline_offer`, `pay_clause`, `raise_clause`, `shield_player` and `save_lineup` are
  implemented and checked through `prepare` + `dry_run`, but only `bid` has actually been
  executed. `pay_clause` was attempted on 2026-09-04 and refused by the clause window
  ([clause-window.md](clause-window.md)), which is why the guard now knows about it.
* **Tests**: only `engine` and `writes` have any. The swap planner (`advice/swaps.go`) and the
  renderer have none, and the planner is the part most likely to change.
* **The swap plan** only proposes one-for-one, same-position moves. Selling two to buy one is
  not modelled, and neither is the fixture calendar — an easy run of three matchdays is worth
  more than a hard one and the plan cannot see it.
* **Crest cache**: `crests.json` is filled by hand, so a newly promoted club has no badge
  until it is regenerated.
* The CLI still prints `(C)`/`(F)` for home and away while the page uses 🏠/✈️: emoji width
  varies by terminal and the CLI tables align by counting columns.
